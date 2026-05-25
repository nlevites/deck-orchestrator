package liveness_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/liveness"
	"deck-fleet/backend/internal/reconciler"
	"deck-fleet/backend/internal/store"
	storegen "deck-fleet/backend/internal/store/gen"
	"deck-fleet/backend/internal/testutil"
	"deck-fleet/backend/internal/timeouts"
)

// runSweep runs one synchronous sweep then cancels Run.
//
// BUG: sweep uses time.Now(); no injection seam. Tests use wall-clock offsets.
func runSweep(t *testing.T, m *liveness.Monitor) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.Run(ctx)
	}()
	time.Sleep(250 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runSweep: monitor.Run did not exit after context cancel")
	}
}

func newTestMonitor(db *store.DB, rec *reconciler.Reconciler, tc timeouts.Config) *liveness.Monitor {
	return &liveness.Monitor{
		Store:         db,
		Reconciler:    rec,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Timeouts:      tc,
		SweepInterval: 10 * time.Second,
	}
}

func newTestReconciler(db *store.DB, client *http.Client) *reconciler.Reconciler {
	return &reconciler.Reconciler{
		Store:          db,
		HTTPClient:     client,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		RequestTimeout: 5 * time.Second,
	}
}

var tightTimeouts = timeouts.Config{
	StaleThreshold:      30 * time.Second,
	AttemptDeadlineBase: 30 * time.Minute,
	AmbiguousDeadline:   2 * time.Minute,
}

func TestMonitor_healthyDeckWithFreshHeartbeat_noTransition(t *testing.T) {
	db := testutil.OpenDB(t)
	rec := newTestReconciler(db, &http.Client{})
	m := newTestMonitor(db, rec, tightTimeouts)

	freshHB := time.Now().UTC().Add(-10 * time.Second)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q, testutil.WithDeckHeartbeat(freshHB))
	})

	eventsBefore := testutil.CountEventsOfKind(t, db, "DECK_HEALTH_CHANGED")

	runSweep(t, m)

	require.Equal(t, gen.HEALTHY, gen.DeckHealth(testutil.DeckHealthFromDB(t, db, testutil.DefaultDeckID)),
		"deck with fresh heartbeat must stay HEALTHY")
	require.Equal(t, eventsBefore, testutil.CountEventsOfKind(t, db, "DECK_HEALTH_CHANGED"),
		"no DECK_HEALTH_CHANGED event expected for fresh-heartbeat deck")
}

func TestMonitor_staleDeck_transitionsToSTALEThenUNREACHABLE(t *testing.T) {
	db := testutil.OpenDB(t)
	rec := newTestReconciler(db, &http.Client{})
	m := newTestMonitor(db, rec, tightTimeouts)

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q, testutil.WithDeckHeartbeat(testutil.Epoch))
	})

	eventsBefore := testutil.CountEventsOfKind(t, db, "DECK_HEALTH_CHANGED")

	runSweep(t, m)

	require.Equal(t, gen.UNREACHABLE, gen.DeckHealth(testutil.DeckHealthFromDB(t, db, testutil.DefaultDeckID)),
		"deck whose heartbeat is far past AmbiguousDeadline must escalate to UNREACHABLE")
	require.Equal(t, eventsBefore+2, testutil.CountEventsOfKind(t, db, "DECK_HEALTH_CHANGED"),
		"two DECK_HEALTH_CHANGED events emitted: HEALTHY→STALE and STALE→UNREACHABLE")
}

func TestMonitor_recentlyStaleDeck_staysSTALE(t *testing.T) {
	db := testutil.OpenDB(t)
	rec := newTestReconciler(db, &http.Client{})
	m := newTestMonitor(db, rec, tightTimeouts)

	recentStale := time.Now().UTC().Add(-45 * time.Second)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q, testutil.WithDeckHeartbeat(recentStale))
	})

	eventsBefore := testutil.CountEventsOfKind(t, db, "DECK_HEALTH_CHANGED")

	runSweep(t, m)

	require.Equal(t, gen.STALE, gen.DeckHealth(testutil.DeckHealthFromDB(t, db, testutil.DefaultDeckID)),
		"recently-stale deck (within AmbiguousDeadline) must stay STALE, not escalate")
	require.Equal(t, eventsBefore+1, testutil.CountEventsOfKind(t, db, "DECK_HEALTH_CHANGED"),
		"exactly one DECK_HEALTH_CHANGED event: HEALTHY→STALE only")
}

func TestMonitor_alreadyStale_escalatesWhenOld(t *testing.T) {
	db := testutil.OpenDB(t)
	rec := newTestReconciler(db, &http.Client{})
	m := newTestMonitor(db, rec, tightTimeouts)

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q,
			testutil.WithDeckHeartbeat(testutil.Epoch),
			testutil.WithDeckHealth(gen.STALE),
		)
	})

	eventsBefore := testutil.CountEventsOfKind(t, db, "DECK_HEALTH_CHANGED")

	runSweep(t, m)

	require.Equal(t, gen.UNREACHABLE, gen.DeckHealth(testutil.DeckHealthFromDB(t, db, testutil.DefaultDeckID)),
		"already-STALE deck with Epoch heartbeat must escalate to UNREACHABLE")
	require.Equal(t, eventsBefore+1, testutil.CountEventsOfKind(t, db, "DECK_HEALTH_CHANGED"),
		"one DECK_HEALTH_CHANGED event: STALE→UNREACHABLE")
}

// STALE→HEALTHY recovery is on the heartbeat path, not the Monitor.
// See TestHeartbeat_freshHeartbeatAfterStale_recoversToHealthy in
// internal/handlers/executor_heartbeat_test.go.

func TestMonitor_staleWithInFlightJob_triggersReconcile(t *testing.T) {
	db := testutil.OpenDB(t)
	es := testutil.NewExecutorServer(t)

	rec := newTestReconciler(db, &http.Client{})
	m := newTestMonitor(db, rec, tightTimeouts)

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q,
			testutil.WithDeckHeartbeat(testutil.Epoch),
			testutil.WithDeckEndpoint(es.URL),
		)
	})

	var (
		attemptID  string
		updateErr  error
		updateRows int64
	)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
		job := testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusDISPATCHED),
		)
		attemptID = testutil.SeedAttempt(t, q,
			testutil.DefaultRunID, testutil.DefaultJobID, testutil.DefaultDeckID,
			testutil.WithAttemptDispatchedAt(time.Now().UTC()),
		)
		updateRows, updateErr = q.UpdateDeckJobStatusVersioned(
			context.Background(),
			storegen.UpdateDeckJobStatusVersionedParams{
				Status:           string(gen.DeckJobStatusDISPATCHED),
				CurrentAttemptID: sql.NullString{String: attemptID, Valid: true},
				Error:            sql.NullString{},
				RunID:            testutil.DefaultRunID,
				ID:               testutil.DefaultJobID,
				Version:          job.Version,
			},
		)
	})
	require.NoError(t, updateErr, "set current_attempt_id on job")
	require.Equal(t, int64(1), updateRows, "job must have been updated")

	runSweep(t, m)

	require.Equal(t, gen.UNREACHABLE, gen.DeckHealth(testutil.DeckHealthFromDB(t, db, testutil.DefaultDeckID)))

	hits := es.Hits()
	require.NotEmpty(t, hits, "reconciler must have contacted the executor state endpoint for the in-flight job")
	require.Equal(t, "/executor/state", hits[0].Path)
}

func TestMonitor_perStepDeadline_skipsLongJobBelowCeiling(t *testing.T) {
	// 50-step job, base 1s + per-step 100ms = 6s ceiling. Dispatch 4s ago
	// should NOT trigger a reconcile.
	db := testutil.OpenDB(t)
	es := testutil.NewExecutorServer(t)

	rec := newTestReconciler(db, &http.Client{})
	m := newTestMonitor(db, rec, timeouts.Config{
		StaleThreshold:         30 * time.Second,
		AttemptDeadlineBase:    1 * time.Second,
		AttemptDeadlinePerStep: 100 * time.Millisecond,
		AmbiguousDeadline:      2 * time.Minute,
	})

	steps := make([]gen.Step, 50)
	for i := range steps {
		steps[i] = gen.Step{Type: "noop", Description: "s"}
	}

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q,
			testutil.WithDeckHeartbeat(time.Now().UTC()),
			testutil.WithDeckEndpoint(es.URL),
		)
	})
	dispatchedAt := time.Now().UTC().Add(-4 * time.Second)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
		job := testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusDISPATCHED),
			testutil.WithJobSteps(steps...),
		)
		attemptID := testutil.SeedAttempt(t, q,
			testutil.DefaultRunID, testutil.DefaultJobID, testutil.DefaultDeckID,
			testutil.WithAttemptDispatchedAt(dispatchedAt),
		)
		_, err := q.UpdateDeckJobStatusVersioned(
			context.Background(),
			storegen.UpdateDeckJobStatusVersionedParams{
				Status:           string(gen.DeckJobStatusDISPATCHED),
				CurrentAttemptID: sql.NullString{String: attemptID, Valid: true},
				Error:            sql.NullString{},
				RunID:            testutil.DefaultRunID,
				ID:               testutil.DefaultJobID,
				Version:          job.Version,
			},
		)
		require.NoError(t, err)
	})

	runSweep(t, m)

	require.Empty(t, es.Hits(),
		"50-step attempt within scaled ceiling must not be reconciled yet")
}

func TestMonitor_perStepDeadline_triggersLongJobOverCeiling(t *testing.T) {
	// Same 50-step job, dispatched 7s ago — past the 6s ceiling.
	db := testutil.OpenDB(t)
	es := testutil.NewExecutorServer(t)

	rec := newTestReconciler(db, &http.Client{})
	m := newTestMonitor(db, rec, timeouts.Config{
		StaleThreshold:         30 * time.Second,
		AttemptDeadlineBase:    1 * time.Second,
		AttemptDeadlinePerStep: 100 * time.Millisecond,
		AmbiguousDeadline:      2 * time.Minute,
	})

	steps := make([]gen.Step, 50)
	for i := range steps {
		steps[i] = gen.Step{Type: "noop", Description: "s"}
	}

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q,
			testutil.WithDeckHeartbeat(time.Now().UTC()),
			testutil.WithDeckEndpoint(es.URL),
		)
	})
	dispatchedAt := time.Now().UTC().Add(-7 * time.Second)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
		job := testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusDISPATCHED),
			testutil.WithJobSteps(steps...),
		)
		attemptID := testutil.SeedAttempt(t, q,
			testutil.DefaultRunID, testutil.DefaultJobID, testutil.DefaultDeckID,
			testutil.WithAttemptDispatchedAt(dispatchedAt),
		)
		_, err := q.UpdateDeckJobStatusVersioned(
			context.Background(),
			storegen.UpdateDeckJobStatusVersionedParams{
				Status:           string(gen.DeckJobStatusDISPATCHED),
				CurrentAttemptID: sql.NullString{String: attemptID, Valid: true},
				Error:            sql.NullString{},
				RunID:            testutil.DefaultRunID,
				ID:               testutil.DefaultJobID,
				Version:          job.Version,
			},
		)
		require.NoError(t, err)
	})

	runSweep(t, m)

	hits := es.Hits()
	require.NotEmpty(t, hits, "overdue attempt must be reconciled")
	require.Equal(t, "/executor/state", hits[0].Path)
}

func TestMonitor_overdueAttempt_triggersReconcile(t *testing.T) {
	db := testutil.OpenDB(t)
	es := testutil.NewExecutorServer(t)

	rec := newTestReconciler(db, &http.Client{})
	m := newTestMonitor(db, rec, timeouts.Config{
		StaleThreshold:      30 * time.Second,
		AttemptDeadlineBase: 30 * time.Second,
		AmbiguousDeadline:   2 * time.Minute,
	})

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q,
			testutil.WithDeckHeartbeat(time.Now().UTC()),
			testutil.WithDeckEndpoint(es.URL),
		)
	})

	var (
		updateErr  error
		updateRows int64
	)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
		job := testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusDISPATCHED),
		)
		attemptID := testutil.SeedAttempt(t, q,
			testutil.DefaultRunID, testutil.DefaultJobID, testutil.DefaultDeckID,
		)
		updateRows, updateErr = q.UpdateDeckJobStatusVersioned(
			context.Background(),
			storegen.UpdateDeckJobStatusVersionedParams{
				Status:           string(gen.DeckJobStatusDISPATCHED),
				CurrentAttemptID: sql.NullString{String: attemptID, Valid: true},
				Error:            sql.NullString{},
				RunID:            testutil.DefaultRunID,
				ID:               testutil.DefaultJobID,
				Version:          job.Version,
			},
		)
	})
	require.NoError(t, updateErr, "set current_attempt_id on job")
	require.Equal(t, int64(1), updateRows, "job must have been updated")

	runSweep(t, m)

	hits := es.Hits()
	require.NotEmpty(t, hits, "reconciler must have contacted the executor state endpoint for the overdue attempt")
	require.Equal(t, "/executor/state", hits[0].Path)
}
