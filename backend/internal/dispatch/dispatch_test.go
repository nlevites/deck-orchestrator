package dispatch_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/dispatch"
	"deck-fleet/backend/internal/state"
	storegen "deck-fleet/backend/internal/store/gen"
	"deck-fleet/backend/internal/testutil"
)

func TestReadyForRun_dispatchesReadyJob(t *testing.T) {
	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
		testutil.SeedDeck(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusREADY))
	})

	ctx := context.Background()
	require.NoError(t, db.WithTx(ctx, func(q *storegen.Queries) error {
		_, err := dispatch.ReadyForRun(ctx, q, testutil.DefaultRunID, testutil.Epoch)
		return err
	}))

	job, err := db.ReadQueries.GetDeckJob(ctx, storegen.GetDeckJobParams{
		RunID: testutil.DefaultRunID, ID: testutil.DefaultJobID,
	})
	require.NoError(t, err)
	require.Equal(t, string(gen.DeckJobStatusDISPATCHED), job.Status)
	require.True(t, job.CurrentAttemptID.Valid, "current_attempt_id should be set after dispatch")
}

func TestReadyForDeck_skipsBusySlot(t *testing.T) {
	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q, testutil.WithRunID("run-a"))
		testutil.SeedDeck(t, q)
		testutil.SeedDeckJob(t, q, "run-a",
			testutil.WithJobID("job-a"),
			testutil.WithJobStatus(gen.DeckJobStatusDISPATCHED))
		attemptID := testutil.SeedAttempt(t, q, "run-a", "job-a", testutil.DefaultDeckID)
		_, err := q.UpdateDeckJobStatusVersioned(context.Background(), storegen.UpdateDeckJobStatusVersionedParams{
			Status:           string(gen.DeckJobStatusDISPATCHED),
			CurrentAttemptID: sql.NullString{String: attemptID, Valid: true},
			RunID:            "run-a",
			ID:               "job-a",
			Version:          0,
		})
		require.NoError(t, err)

		testutil.SeedRun(t, q, testutil.WithRunID("run-b"))
		testutil.SeedDeckJob(t, q, "run-b",
			testutil.WithJobID("job-b"),
			testutil.WithJobStatus(gen.DeckJobStatusREADY))
	})

	ctx := context.Background()
	require.NoError(t, db.WithTx(ctx, func(q *storegen.Queries) error {
		_, err := dispatch.ReadyForDeck(ctx, q, testutil.DefaultDeckID, testutil.Epoch)
		return err
	}))

	jobB, err := db.ReadQueries.GetDeckJob(ctx, storegen.GetDeckJobParams{
		RunID: "run-b", ID: "job-b",
	})
	require.NoError(t, err)
	require.Equal(t, string(gen.DeckJobStatusREADY), jobB.Status,
		"busy slot must keep job-b at READY (slot invariant)")
}

func TestReadyForDeck_skipsUnhealthyDeck(t *testing.T) {
	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
		testutil.SeedDeck(t, q, testutil.WithDeckHealth(gen.UNREACHABLE))
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusREADY))
	})

	ctx := context.Background()
	require.NoError(t, db.WithTx(ctx, func(q *storegen.Queries) error {
		_, err := dispatch.ReadyForDeck(ctx, q, testutil.DefaultDeckID, testutil.Epoch)
		return err
	}))

	job, err := db.ReadQueries.GetDeckJob(ctx, storegen.GetDeckJobParams{
		RunID: testutil.DefaultRunID, ID: testutil.DefaultJobID,
	})
	require.NoError(t, err)
	require.Equal(t, string(gen.DeckJobStatusREADY), job.Status,
		"UNREACHABLE deck must not receive dispatch")
}

func TestTryDispatch_lostRaceIsNoop(t *testing.T) {
	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
		testutil.SeedDeck(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusREADY))
	})

	ctx := context.Background()
	require.NoError(t, db.WithTx(ctx, func(q *storegen.Queries) error {
		jobRow, err := q.GetDeckJob(ctx, storegen.GetDeckJobParams{
			RunID: testutil.DefaultRunID, ID: testutil.DefaultJobID,
		})
		if err != nil {
			return err
		}
		_, err = state.ApplyVersioned(ctx, state.ApplyVersionedParams{
			Q:                 q,
			From:              gen.DeckJobStatusREADY,
			To:                gen.DeckJobStatusCANCELLED,
			Trigger:           state.TriggerOperatorCancel,
			RunID:             jobRow.RunID,
			JobID:             jobRow.ID,
			Version:           jobRow.Version,
			NewCurrentAttempt: jobRow.CurrentAttemptID,
			NewError:          jobRow.Error,
		})
		if err != nil {
			return err
		}
		_, err = dispatch.ReadyForRun(ctx, q, testutil.DefaultRunID, testutil.Epoch)
		return err
	}))

	job, err := db.ReadQueries.GetDeckJob(ctx, storegen.GetDeckJobParams{
		RunID: testutil.DefaultRunID, ID: testutil.DefaultJobID,
	})
	require.NoError(t, err)
	require.Equal(t, string(gen.DeckJobStatusCANCELLED), job.Status,
		"raced job stays CANCELLED; dispatcher is a no-op")
}

func TestPromoteDownstreamReady(t *testing.T) {
	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
		testutil.SeedDeck(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobID("upstream"),
			testutil.WithJobStatus(gen.DeckJobStatusCOMPLETED))
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobID("downstream"),
			testutil.WithJobDeps("upstream"),
			testutil.WithJobStatus(gen.DeckJobStatusPENDING))
	})

	ctx := context.Background()
	require.NoError(t, db.WithTx(ctx, func(q *storegen.Queries) error {
		return dispatch.PromoteDownstreamReady(ctx, q, testutil.DefaultRunID, testutil.Epoch)
	}))

	downstream, err := db.ReadQueries.GetDeckJob(ctx, storegen.GetDeckJobParams{
		RunID: testutil.DefaultRunID, ID: "downstream",
	})
	require.NoError(t, err)
	require.Equal(t, string(gen.DeckJobStatusREADY), downstream.Status,
		"downstream must promote to READY when its only dep is COMPLETED")
}
