package aborts_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/aborts"
	storegen "deck-fleet/backend/internal/store/gen"
	"deck-fleet/backend/internal/testutil"
	"deck-fleet/backend/internal/timeouts"
)

func TestDialer_Schedule_deliversToExecutor(t *testing.T) {
	var hit bool
	var hitAttemptID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		hitAttemptID = r.URL.Path[len("/executor/abort/"):]
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q, testutil.WithDeckEndpoint(srv.URL))
	})

	d := &aborts.Dialer{
		Store:       db,
		HTTPClient:  &http.Client{},
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Timeouts:    timeouts.Config{AbortRetryInitial: 10 * time.Millisecond, AbortRetryMaxDuration: 500 * time.Millisecond},
		Concurrency: 2,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	d.Schedule(ctx, testutil.DefaultDeckID, "attempt-abc")
	d.Wait()

	require.True(t, hit, "abort endpoint must have been hit")
	require.Equal(t, "attempt-abc", hitAttemptID)
}

func TestDialer_Schedule_executorReturns404_isTerminalSuccess(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q, testutil.WithDeckEndpoint(srv.URL))
	})

	d := &aborts.Dialer{
		Store:       db,
		HTTPClient:  &http.Client{},
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Timeouts:    timeouts.Config{AbortRetryInitial: 10 * time.Millisecond, AbortRetryMaxDuration: 500 * time.Millisecond},
		Concurrency: 2,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	d.Schedule(ctx, testutil.DefaultDeckID, "attempt-gone")
	d.Wait()

	require.Equal(t, 1, calls, "404 should not trigger retries")
}

func TestDialer_Schedule_deckNotRegistered_doesNotPanic(t *testing.T) {
	db := testutil.OpenDB(t)
	d := &aborts.Dialer{
		Store:       db,
		HTTPClient:  &http.Client{},
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Timeouts:    timeouts.Config{AbortRetryInitial: 10 * time.Millisecond, AbortRetryMaxDuration: 50 * time.Millisecond},
		Concurrency: 2,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NotPanics(t, func() {
		d.Schedule(ctx, "unknown-deck", "attempt-x")
		d.Wait()
	})
}

func TestDialer_Schedule_contextCancellation(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q, testutil.WithDeckEndpoint(srv.URL))
	})

	d := &aborts.Dialer{
		Store:       db,
		HTTPClient:  &http.Client{},
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Timeouts:    timeouts.Config{AbortRetryInitial: 500 * time.Millisecond, AbortRetryMaxDuration: 10 * time.Second},
		Concurrency: 2,
	}

	ctx, cancel := context.WithCancel(context.Background())
	d.Schedule(ctx, testutil.DefaultDeckID, "attempt-cancel")
	cancel()
	d.Wait()
}
