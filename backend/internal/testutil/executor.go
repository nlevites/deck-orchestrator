package testutil

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"deck-fleet/backend/internal/executor/localstore"
	"deck-fleet/backend/internal/store"
)

func OpenLocalStore(t *testing.T) *localstore.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "deck.db")
	s, err := localstore.Open(context.Background(), path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("testutil: open localstore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func CountEventsOfKind(t *testing.T, db *store.DB, kind string) int {
	t.Helper()
	var n int
	if err := db.Write.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM events WHERE kind = ?", kind).Scan(&n); err != nil {
		t.Fatalf("testutil: count events %q: %v", kind, err)
	}
	return n
}

func CountEventsOfKindForDeck(t *testing.T, db *store.DB, kind, deckID string) int {
	t.Helper()
	var n int
	if err := db.Write.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM events WHERE kind = ? AND deck_id = ?", kind, deckID).Scan(&n); err != nil {
		t.Fatalf("testutil: count events %q for deck %q: %v", kind, deckID, err)
	}
	return n
}

func DeckHealthFromDB(t *testing.T, db *store.DB, deckID string) string {
	t.Helper()
	var h string
	if err := db.Write.QueryRowContext(context.Background(),
		"SELECT last_known_health FROM decks WHERE id = ?", deckID).Scan(&h); err != nil {
		t.Fatalf("testutil: read deck health for %q: %v", deckID, err)
	}
	return h
}
