package state_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/eventlog"
	"deck-fleet/backend/internal/state"
	storegen "deck-fleet/backend/internal/store/gen"
	"deck-fleet/backend/internal/testutil"
)

func TestMaterializeRunStatus_noChange_returnsChangedFalse(t *testing.T) {
	db := testutil.OpenDB(t)
	ctx := context.Background()

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID)
	})

	var (
		newStatus gen.RunStatus
		changed   bool
	)
	err := db.WithTx(ctx, func(q *storegen.Queries) error {
		var e error
		newStatus, changed, e = state.MaterializeRunStatus(ctx, q, testutil.DefaultRunID, testutil.Epoch)
		return e
	})
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, gen.PENDING, newStatus)

	row := db.Write.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM events WHERE run_id = ? AND kind = 'RUN_STATUS_CHANGED'",
		testutil.DefaultRunID,
	)
	var count int
	require.NoError(t, row.Scan(&count))
	require.Equal(t, 0, count)
}

func TestMaterializeRunStatus_PENDINGtoRUNNING_emitsEvent(t *testing.T) {
	db := testutil.OpenDB(t)
	ctx := context.Background()

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusDISPATCHED))
	})

	var (
		newStatus gen.RunStatus
		changed   bool
	)
	err := db.WithTx(ctx, func(q *storegen.Queries) error {
		var e error
		newStatus, changed, e = state.MaterializeRunStatus(ctx, q, testutil.DefaultRunID, testutil.Epoch)
		return e
	})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, gen.RUNNING, newStatus)

	run, err := db.Queries.GetRun(ctx, testutil.DefaultRunID)
	require.NoError(t, err)
	require.Equal(t, string(gen.RUNNING), run.Status)

	var rawPayload string
	row := db.Write.QueryRowContext(ctx,
		"SELECT payload FROM events WHERE run_id = ? AND kind = 'RUN_STATUS_CHANGED'",
		testutil.DefaultRunID,
	)
	require.NoError(t, row.Scan(&rawPayload))

	var p eventlog.RunStatusChangedPayload
	require.NoError(t, json.Unmarshal([]byte(rawPayload), &p))
	require.Equal(t, gen.PENDING, p.From)
	require.Equal(t, gen.RUNNING, p.To)
}

func TestMaterializeRunStatus_toCompleted_stampsTerminalAt(t *testing.T) {
	db := testutil.OpenDB(t)
	ctx := context.Background()

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q, testutil.WithRunStatus(gen.RUNNING))
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusCOMPLETED))
	})

	var (
		newStatus gen.RunStatus
		changed   bool
	)
	err := db.WithTx(ctx, func(q *storegen.Queries) error {
		var e error
		newStatus, changed, e = state.MaterializeRunStatus(ctx, q, testutil.DefaultRunID, testutil.Epoch)
		return e
	})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, gen.COMPLETED, newStatus)

	run, err := db.Queries.GetRun(ctx, testutil.DefaultRunID)
	require.NoError(t, err)
	require.Equal(t, string(gen.COMPLETED), run.Status)
	require.True(t, run.TerminalAt.Valid, "terminal_at must be stamped for COMPLETED")
	require.Equal(t, testutil.Epoch.UnixMilli(), run.TerminalAt.Int64)
}

func TestMaterializeRunStatus_toFailed_doesNotStampTerminalAt(t *testing.T) {
	// FAILED is non-terminal at the run level: a FAILED run awaits operator
	// decision (retry or cancel) before terminalizing. See DESIGN.md.
	db := testutil.OpenDB(t)
	ctx := context.Background()

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q, testutil.WithRunStatus(gen.RUNNING))
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusFAILED))
	})

	var (
		newStatus gen.RunStatus
		changed   bool
	)
	err := db.WithTx(ctx, func(q *storegen.Queries) error {
		var e error
		newStatus, changed, e = state.MaterializeRunStatus(ctx, q, testutil.DefaultRunID, testutil.Epoch)
		return e
	})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, gen.FAILED, newStatus)

	run, err := db.Queries.GetRun(ctx, testutil.DefaultRunID)
	require.NoError(t, err)
	require.Equal(t, string(gen.FAILED), run.Status)
	require.False(t, run.TerminalAt.Valid, "terminal_at must NOT be stamped for FAILED")
}

func TestMaterializeRunStatus_toAmbiguous_doesNotStampTerminalAt(t *testing.T) {
	db := testutil.OpenDB(t)
	ctx := context.Background()

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q, testutil.WithRunStatus(gen.RUNNING))
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusAMBIGUOUS))
	})

	var (
		newStatus gen.RunStatus
		changed   bool
	)
	err := db.WithTx(ctx, func(q *storegen.Queries) error {
		var e error
		newStatus, changed, e = state.MaterializeRunStatus(ctx, q, testutil.DefaultRunID, testutil.Epoch)
		return e
	})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, gen.AMBIGUOUS, newStatus)

	run, err := db.Queries.GetRun(ctx, testutil.DefaultRunID)
	require.NoError(t, err)
	require.Equal(t, string(gen.AMBIGUOUS), run.Status)
	require.False(t, run.TerminalAt.Valid, "terminal_at must remain NULL for non-terminal AMBIGUOUS")
}
