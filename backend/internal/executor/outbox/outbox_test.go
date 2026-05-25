package outbox_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/executor/chaos"
	"deck-fleet/backend/internal/executor/client"
	"deck-fleet/backend/internal/executor/localstore"
	"deck-fleet/backend/internal/executor/outbox"
	"deck-fleet/backend/internal/testutil"
)

func boolPtr(b bool) *bool { return &b }

func seedOutboxEvent(t *testing.T, s *localstore.Store, attemptID string) {
	t.Helper()
	a := localstore.Attempt{
		AttemptID: attemptID, RunID: "r1", JobID: "j1", DeckID: "d1",
		StepsJSON: `[]`, ReceivedAt: time.Now().UTC(),
	}
	_, err := s.InsertReceived(context.Background(), a)
	require.NoError(t, err)
	require.NoError(t, s.EnqueueEvent(context.Background(), attemptID,
		localstore.EventCompleted, map[string]any{}, time.Now().UTC()))
}

func TestFlusher_deliversAndDeletes(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	s := testutil.OpenLocalStore(t)
	seedOutboxEvent(t, s, "att-1")

	c := client.New(srv.URL)
	f := outbox.New(outbox.Deps{Store: s, Client: c, Cfg: outbox.Config{Initial: 1 * time.Millisecond, Max: 10 * time.Millisecond}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go f.Run(ctx)

	require.Eventually(t, func() bool {
		_, ok, _ := s.NextOutbox(context.Background())
		return !ok
	}, 2*time.Second, 10*time.Millisecond, "outbox should be empty after delivery")

	require.Greater(t, calls.Load(), int32(0), "event must have been POSTed")
}

func TestFlusher_retriesOnServerError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	s := testutil.OpenLocalStore(t)
	seedOutboxEvent(t, s, "att-2")

	c := client.New(srv.URL)
	f := outbox.New(outbox.Deps{Store: s, Client: c, Cfg: outbox.Config{Initial: 1 * time.Millisecond, Max: 10 * time.Millisecond}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go f.Run(ctx)

	require.Eventually(t, func() bool {
		_, ok, _ := s.NextOutbox(context.Background())
		return !ok
	}, 4*time.Second, 10*time.Millisecond, "outbox should be empty after retries")

	require.GreaterOrEqual(t, calls.Load(), int32(3), "expected at least 3 delivery attempts")
}

func TestFlusher_dropDelivery_doesNotDeliver(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	s := testutil.OpenLocalStore(t)
	seedOutboxEvent(t, s, "att-3")

	c := client.New(srv.URL)
	chaosState := chaos.New(chaos.InitialState{DropEvents: boolPtr(true)})
	f := outbox.New(outbox.Deps{
		Store:  s,
		Client: c,
		Cfg:    outbox.Config{Initial: 1 * time.Millisecond, Max: 10 * time.Millisecond},
		Chaos:  chaosState,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	f.Run(ctx)

	require.Equal(t, int32(0), calls.Load(), "drop_events must not send HTTP calls")
	row, ok, _ := s.NextOutbox(context.Background())
	require.True(t, ok, "outbox row must still exist")
	require.Greater(t, row.Retries, int64(0), "retries must have been bumped")
}

func TestFlusher_contextCancel_exits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	s := testutil.OpenLocalStore(t)
	c := client.New(srv.URL)
	f := outbox.New(outbox.Deps{Store: s, Client: c, Cfg: outbox.Config{Initial: 1 * time.Millisecond, Max: 10 * time.Millisecond}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); f.Run(ctx) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("flusher.Run did not exit after context cancel")
	}
}

func TestFlusher_sendsCorrectEventShape(t *testing.T) {
	var (
		mu   sync.Mutex
		body map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		mu.Lock()
		body = got
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	s := testutil.OpenLocalStore(t)
	seedOutboxEvent(t, s, "att-4")

	c := client.New(srv.URL)
	f := outbox.New(outbox.Deps{Store: s, Client: c, Cfg: outbox.Config{Initial: 1 * time.Millisecond, Max: 10 * time.Millisecond}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go f.Run(ctx)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return body != nil
	}, 2*time.Second, 10*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "att-4", body["attempt_id"])
	require.Equal(t, "COMPLETED", body["kind"])
	require.NotEmpty(t, body["occurred_at"])
}
