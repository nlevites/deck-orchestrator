package testutil

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"deck-fleet/backend/internal/store"
	storegen "deck-fleet/backend/internal/store/gen"
)

// OpenDB returns a fresh *store.DB backed by a per-test SQLite file in
// t.TempDir(). Migrations are applied; Close is registered with t.Cleanup.
//
// We use a tempdir file (not :memory:) because store.Open opens two
// connection pools against the same DSN; in-memory databases are
// per-connection unless you use shared cache, which complicates the
// store layer's WAL pragmas. A tempdir file is fast enough (SQLite WAL
// on tmpfs) and exercises the real migrate path.
func OpenDB(t *testing.T) *store.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(context.Background(), store.Config{
		Path:         path,
		BusyTimeout:  2 * time.Second,
		MaxReadConns: 2,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("testutil: open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func Tx(t *testing.T, db *store.DB, fn func(q *storegen.Queries)) {
	t.Helper()
	if err := db.WithTx(context.Background(), func(q *storegen.Queries) error {
		fn(q)
		return nil
	}); err != nil {
		t.Fatalf("testutil: tx: %v", err)
	}
}

// MustExec runs raw SQL against db.Write and t.Fatal's on error. Reserved
// for asserting/poking schema-level invariants the sqlc surface doesn't
// expose (e.g. peeking at the events table seq counter).
func MustExec(t *testing.T, db *store.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Write.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("testutil: exec %q: %v", query, err)
	}
}
