package reconciler_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/eventlog"
	"deck-fleet/backend/internal/reconciler"
	"deck-fleet/backend/internal/store"
	storegen "deck-fleet/backend/internal/store/gen"
	"deck-fleet/backend/internal/testutil"
)

func newReconciler(db *store.DB, transport http.RoundTripper) *reconciler.Reconciler {
	hc := &http.Client{Transport: transport}
	if transport == nil {
		hc = &http.Client{}
	}
	return &reconciler.Reconciler{
		Store:          db,
		HTTPClient:     hc,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		RequestTimeout: 5 * time.Second,
	}
}

func executorStateServer(t *testing.T, state string, attemptID string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("attempt_id") == "" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"deck_id":"test","recent_attempts":[]}`))
			return
		}
		payload := map[string]any{
			"attempt_id":  attemptID,
			"state":       state,
			"received_at": testutil.Epoch.Format(time.RFC3339),
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(payload)
	}))
}

func seedDispatchedJob(t *testing.T, db *store.DB) string {
	t.Helper()
	var attemptID string
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusDISPATCHED),
		)
		attemptID = testutil.SeedAttempt(t, q,
			testutil.DefaultRunID, testutil.DefaultJobID, testutil.DefaultDeckID)
		_, err := q.UpdateDeckJobStatusVersioned(context.Background(),
			storegen.UpdateDeckJobStatusVersionedParams{
				Status:           string(gen.DeckJobStatusDISPATCHED),
				CurrentAttemptID: sql.NullString{String: attemptID, Valid: true},
				RunID:            testutil.DefaultRunID,
				ID:               testutil.DefaultJobID,
				Version:          1,
			})
		require.NoError(t, err)
	})
	return attemptID
}

func jobStatus(t *testing.T, db *store.DB) gen.DeckJobStatus {
	t.Helper()
	var s string
	require.NoError(t, db.Write.QueryRowContext(context.Background(),
		"SELECT status FROM deck_jobs WHERE id = ?", testutil.DefaultJobID).Scan(&s))
	return gen.DeckJobStatus(s)
}

func TestReconcileAttempt_deckNotRegistered(t *testing.T) {
	db := testutil.OpenDB(t)
	rec := newReconciler(db, nil)
	out, err := rec.ReconcileAttempt(context.Background(), "nonexistent-deck", "some-attempt")
	require.Error(t, err)
	require.Equal(t, reconciler.OutcomeUnreachable, out)
}

func TestReconcileAttempt_executorUnreachable(t *testing.T) {
	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q, testutil.WithDeckEndpoint("http://127.0.0.1:1"))
	})
	rec := newReconciler(db, nil)
	out, _ := rec.ReconcileAttempt(context.Background(), testutil.DefaultDeckID, "any")
	require.Equal(t, reconciler.OutcomeUnreachable, out)
}

func TestReconcileAttempt_executorReturns404_unknownNoPriorEvidence(t *testing.T) {
	db := testutil.OpenDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q, testutil.WithDeckEndpoint(srv.URL))
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusDISPATCHED))
		testutil.SeedAttempt(t, q,
			testutil.DefaultRunID, testutil.DefaultJobID, testutil.DefaultDeckID,
			testutil.WithAttemptID("attempt-1"))
		_, err := q.UpdateDeckJobStatusVersioned(context.Background(),
			storegen.UpdateDeckJobStatusVersionedParams{
				Status:           string(gen.DeckJobStatusDISPATCHED),
				CurrentAttemptID: sql.NullString{String: "attempt-1", Valid: true},
				RunID:            testutil.DefaultRunID,
				ID:               testutil.DefaultJobID,
				Version:          1,
			})
		require.NoError(t, err)
	})

	rec := newReconciler(db, nil)
	out, err := rec.ReconcileAttempt(context.Background(), testutil.DefaultDeckID, "attempt-1")
	require.NoError(t, err)
	require.Equal(t, reconciler.OutcomeNoDispatch, out)
}

func TestReconcileAttempt_executorReturns404_unknownWithPriorEvidence(t *testing.T) {
	db := testutil.OpenDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	const attemptID = "attempt-evidence"
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q, testutil.WithDeckEndpoint(srv.URL))
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusDISPATCHED))
		testutil.SeedAttempt(t, q,
			testutil.DefaultRunID, testutil.DefaultJobID, testutil.DefaultDeckID,
			testutil.WithAttemptID(attemptID))
		_, err := q.UpdateDeckJobStatusVersioned(context.Background(),
			storegen.UpdateDeckJobStatusVersionedParams{
				Status:           string(gen.DeckJobStatusDISPATCHED),
				CurrentAttemptID: sql.NullString{String: attemptID, Valid: true},
				RunID:            testutil.DefaultRunID,
				ID:               testutil.DefaultJobID,
				Version:          1,
			})
		require.NoError(t, err)
		_, err = eventlog.Append(context.Background(), q, eventlog.KindJobRunning,
			eventlog.Scope{
				RunID:     testutil.DefaultRunID,
				JobID:     testutil.DefaultJobID,
				DeckID:    testutil.DefaultDeckID,
				AttemptID: attemptID,
			}, testutil.Epoch, map[string]any{})
		require.NoError(t, err)
	})

	rec := newReconciler(db, nil)
	out, err := rec.ReconcileAttempt(context.Background(), testutil.DefaultDeckID, attemptID)
	require.NoError(t, err)
	require.Equal(t, reconciler.OutcomeAmbiguous, out)
	require.Equal(t, gen.DeckJobStatusAMBIGUOUS, jobStatus(t, db))
}

func TestReconcileAttempt_executorReportsINPROGRESS(t *testing.T) {
	db := testutil.OpenDB(t)
	const attemptID = "attempt-ip"
	srv := executorStateServer(t, "IN_PROGRESS", attemptID)
	t.Cleanup(srv.Close)

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q, testutil.WithDeckEndpoint(srv.URL))
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusDISPATCHED))
		testutil.SeedAttempt(t, q,
			testutil.DefaultRunID, testutil.DefaultJobID, testutil.DefaultDeckID,
			testutil.WithAttemptID(attemptID))
		_, err := q.UpdateDeckJobStatusVersioned(context.Background(),
			storegen.UpdateDeckJobStatusVersionedParams{
				Status:           string(gen.DeckJobStatusDISPATCHED),
				CurrentAttemptID: sql.NullString{String: attemptID, Valid: true},
				RunID:            testutil.DefaultRunID,
				ID:               testutil.DefaultJobID,
				Version:          1,
			})
		require.NoError(t, err)
	})

	rec := newReconciler(db, nil)
	out, err := rec.ReconcileAttempt(context.Background(), testutil.DefaultDeckID, attemptID)
	require.NoError(t, err)
	require.Equal(t, reconciler.OutcomeRunning, out)
	require.Equal(t, gen.DeckJobStatusRUNNING, jobStatus(t, db))
	require.Equal(t, 1, testutil.CountEventsOfKind(t, db, "JOB_RUNNING"))
}

func TestReconcileAttempt_executorReportsCOMPLETED(t *testing.T) {
	db := testutil.OpenDB(t)
	const attemptID = "attempt-done"
	srv := executorStateServer(t, "COMPLETED", attemptID)
	t.Cleanup(srv.Close)

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q, testutil.WithDeckEndpoint(srv.URL))
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusDISPATCHED))
		testutil.SeedAttempt(t, q,
			testutil.DefaultRunID, testutil.DefaultJobID, testutil.DefaultDeckID,
			testutil.WithAttemptID(attemptID))
		_, err := q.UpdateDeckJobStatusVersioned(context.Background(),
			storegen.UpdateDeckJobStatusVersionedParams{
				Status:           string(gen.DeckJobStatusDISPATCHED),
				CurrentAttemptID: sql.NullString{String: attemptID, Valid: true},
				RunID:            testutil.DefaultRunID,
				ID:               testutil.DefaultJobID,
				Version:          1,
			})
		require.NoError(t, err)
	})

	rec := newReconciler(db, nil)
	out, err := rec.ReconcileAttempt(context.Background(), testutil.DefaultDeckID, attemptID)
	require.NoError(t, err)
	require.Equal(t, reconciler.OutcomeApplied, out)
	require.Equal(t, gen.DeckJobStatusCOMPLETED, jobStatus(t, db))
	require.Equal(t, 1, testutil.CountEventsOfKind(t, db, "JOB_COMPLETED"))
}

func TestMarkAmbiguousDeadline_transitionsDispatchedToAmbiguous(t *testing.T) {
	db := testutil.OpenDB(t)
	attemptID := seedDispatchedJob(t, db)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q)
	})

	rec := newReconciler(db, nil)
	err := rec.MarkAmbiguousDeadline(context.Background(), attemptID, eventlog.AmbiguousReasonDeadlineElapsed)
	require.NoError(t, err)
	require.Equal(t, gen.DeckJobStatusAMBIGUOUS, jobStatus(t, db))
	require.Equal(t, 1, testutil.CountEventsOfKind(t, db, "JOB_AMBIGUOUS"))
}

func TestMarkAmbiguousDeadline_alreadyAmbiguous(t *testing.T) {
	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q)
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusAMBIGUOUS))
		testutil.SeedAttempt(t, q,
			testutil.DefaultRunID, testutil.DefaultJobID, testutil.DefaultDeckID,
			testutil.WithAttemptID("attempt-amb"))
		_, err := q.UpdateDeckJobStatusVersioned(context.Background(),
			storegen.UpdateDeckJobStatusVersionedParams{
				Status:           string(gen.DeckJobStatusAMBIGUOUS),
				CurrentAttemptID: sql.NullString{String: "attempt-amb", Valid: true},
				RunID:            testutil.DefaultRunID,
				ID:               testutil.DefaultJobID,
				Version:          1,
			})
		require.NoError(t, err)
	})

	rec := newReconciler(db, nil)
	err := rec.MarkAmbiguousDeadline(context.Background(), "attempt-amb", eventlog.AmbiguousReasonDeadlineElapsed)
	require.NoError(t, err)
	require.Equal(t, 0, testutil.CountEventsOfKind(t, db, "JOB_AMBIGUOUS"))
}

func TestMarkAmbiguousDeadline_nonexistentAttempt(t *testing.T) {
	db := testutil.OpenDB(t)
	rec := newReconciler(db, nil)
	err := rec.MarkAmbiguousDeadline(context.Background(), "does-not-exist", eventlog.AmbiguousReasonDeadlineElapsed)
	require.NoError(t, err)
}
