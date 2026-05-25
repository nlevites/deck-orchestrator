package store_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/store"
	storegen "deck-fleet/backend/internal/store/gen"
	"deck-fleet/backend/internal/testutil"
)

func TestOpen_emptyPathReturnsError(t *testing.T) {
	_, err := store.Open(
		context.Background(),
		store.Config{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "empty database path")
}

func TestOpen_createsAndMigrates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(
		context.Background(),
		store.Config{
			Path:         path,
			BusyTimeout:  2 * time.Second,
			MaxReadConns: 2,
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runs, err := db.Queries.ListRuns(context.Background(), 10)
	require.NoError(t, err)
	require.Empty(t, runs)
}

func TestDB_Close_safeToCallTwice(t *testing.T) {
	db := testutil.OpenDB(t)
	require.NoError(t, db.Close())
	require.NoError(t, db.Close())
}

func TestWithTx_commitOnSuccess(t *testing.T) {
	db := testutil.OpenDB(t)
	ctx := context.Background()

	err := db.WithTx(ctx, func(q *storegen.Queries) error {
		return q.InsertRun(ctx, storegen.InsertRunParams{
			ID:          "run-commit",
			Status:      "PENDING",
			Dag:         `{"id":"run-commit","deck_jobs":[]}`,
			SubmittedAt: testutil.Epoch.UnixMilli(),
		})
	})
	require.NoError(t, err)

	run, err := db.Queries.GetRun(ctx, "run-commit")
	require.NoError(t, err)
	require.Equal(t, "run-commit", run.ID)
}

func TestWithTx_rollbackOnError(t *testing.T) {
	db := testutil.OpenDB(t)
	ctx := context.Background()

	sentinel := errors.New("forced rollback")
	err := db.WithTx(ctx, func(q *storegen.Queries) error {
		_ = q.InsertRun(ctx, storegen.InsertRunParams{
			ID:          "run-rollback",
			Status:      "PENDING",
			Dag:         `{"id":"run-rollback","deck_jobs":[]}`,
			SubmittedAt: testutil.Epoch.UnixMilli(),
		})
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)

	_, err = db.Queries.GetRun(ctx, "run-rollback")
	require.Error(t, err, "row must not exist after rollback")
}

func TestWithTx_rollbackOnPanic(t *testing.T) {
	db := testutil.OpenDB(t)
	ctx := context.Background()

	require.Panics(t, func() {
		_ = db.WithTx(ctx, func(q *storegen.Queries) error {
			_ = q.InsertRun(ctx, storegen.InsertRunParams{
				ID:          "run-panic",
				Status:      "PENDING",
				Dag:         `{"id":"run-panic","deck_jobs":[]}`,
				SubmittedAt: testutil.Epoch.UnixMilli(),
			})
			panic("test panic")
		})
	})

	_, err := db.Queries.GetRun(ctx, "run-panic")
	require.Error(t, err, "row must not exist after panic-driven rollback")
}
