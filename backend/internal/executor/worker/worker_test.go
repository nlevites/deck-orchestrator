package worker_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/executor/chaos"
	"deck-fleet/backend/internal/executor/client"
	"deck-fleet/backend/internal/executor/localstore"
	"deck-fleet/backend/internal/executor/worker"
	"deck-fleet/backend/internal/testutil"
)

// dispatchOnce returns an httptest server that yields one dispatch then 204.
func dispatchOnce(t *testing.T, dispatch map[string]any, eventCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	var dispatched atomic.Bool
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/executor/poll":
			if dispatched.CompareAndSwap(false, true) {
				w.Header().Set("Content-Type", "application/json")
				buf, _ := json.Marshal(dispatch)
				_, _ = w.Write(buf)
			} else {
				w.WriteHeader(http.StatusNoContent)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/executor/events":
			if eventCalls != nil {
				eventCalls.Add(1)
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/executor/heartbeat":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func newWorker(s *localstore.Store, serverURL string) *worker.Worker {
	c := client.New(serverURL)
	cfg := worker.Config{
		DeckID:            "deck-1",
		EndpointURL:       serverURL,
		HeartbeatInterval: 0,
		PollInterval:      1 * time.Millisecond,
		StepDuration:      0,
	}
	return worker.New(worker.Deps{
		Cfg:       cfg,
		Store:     s,
		Client:    c,
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		CrashFn:   func() {},
		FatalExit: func(int) {},
	})
}

func TestWorker_happyPath_completes(t *testing.T) {
	s := testutil.OpenLocalStore(t)

	dispatch := map[string]any{
		"attempt_id": "att-w1",
		"run_id":     "run-1",
		"job_id":     "job-1",
		"steps":      []map[string]string{{"type": "noop", "description": "x"}},
	}
	srv := dispatchOnce(t, dispatch, nil)
	t.Cleanup(srv.Close)

	w_ := newWorker(s, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go w_.Run(ctx)

	require.Eventually(t, func() bool {
		a, err := s.GetAttempt(context.Background(), "att-w1")
		return err == nil && a.State == localstore.StateCompleted
	}, 4*time.Second, 10*time.Millisecond, "attempt must reach COMPLETED")
}

func TestWorker_idempotentRedelivery(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	ctx := context.Background()

	a := localstore.Attempt{
		AttemptID: "att-w2", RunID: "r1", JobID: "j1", DeckID: "deck-1",
		StepsJSON: `[{"type":"noop","description":"x"}]`, ReceivedAt: time.Now().UTC(),
	}
	_, err := s.InsertReceived(ctx, a)
	require.NoError(t, err)
	require.NoError(t, s.MarkInProgress(ctx, "att-w2", time.Now().UTC()))
	require.NoError(t, s.MarkTerminal(ctx, "att-w2", localstore.StateCompleted, `{}`, "", time.Now().UTC()))

	dispatch := map[string]any{
		"attempt_id": "att-w2",
		"run_id":     "r1",
		"job_id":     "j1",
		"steps":      []map[string]string{{"type": "noop", "description": "x"}},
	}
	var eventCalls atomic.Int32
	srv := dispatchOnce(t, dispatch, &eventCalls)
	t.Cleanup(srv.Close)

	w_ := newWorker(s, srv.URL)
	runCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go w_.Run(runCtx)
	<-runCtx.Done()

	final, err := s.GetAttempt(ctx, "att-w2")
	require.NoError(t, err)
	require.Equal(t, localstore.StateCompleted, final.State)
	require.Equal(t, int32(0), eventCalls.Load(), "no events should be POSTed for a terminal re-delivered attempt")
}

func TestWorker_chaosHang_resumesOnReset(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	chaosState := chaos.New(chaos.InitialState{})
	chaosState.SetHang(true)

	const attemptID = "att-hang-1"
	dispatch := map[string]any{
		"attempt_id": attemptID,
		"run_id":     "r-hang",
		"job_id":     "j-hang",
		"steps":      []map[string]string{{"type": "noop", "description": "x"}},
	}
	srv := dispatchOnce(t, dispatch, nil)
	t.Cleanup(srv.Close)

	c := client.New(srv.URL)
	cfg := worker.Config{
		DeckID:            "deck-1",
		EndpointURL:       srv.URL,
		HeartbeatInterval: 0,
		PollInterval:      1 * time.Millisecond,
		StepDuration:      0,
	}
	w_ := worker.New(worker.Deps{Cfg: cfg, Store: s, Client: c, Chaos: chaosState, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), CrashFn: func() {}, FatalExit: func(int) {}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go w_.Run(ctx)

	require.Eventually(t, func() bool {
		a, err := s.GetAttempt(context.Background(), attemptID)
		return err == nil && a.State == localstore.StateInProgress
	}, 2*time.Second, 5*time.Millisecond, "worker must reach IN_PROGRESS (parked on hang)")

	chaosState.SetHang(false)

	require.Eventually(t, func() bool {
		a, err := s.GetAttempt(context.Background(), attemptID)
		return err == nil && a.State == localstore.StateCompleted
	}, 2*time.Second, 5*time.Millisecond, "attempt must reach COMPLETED after unstick")
}

// TestWorker_chaosHang_ctxCancelStillWins: ctx cancel must unblock a
// parked hang (regression guard against leaked goroutines).
func TestWorker_chaosHang_ctxCancelStillWins(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	chaosState := chaos.New(chaos.InitialState{})
	chaosState.SetHang(true)

	const attemptID = "att-hang-2"
	dispatch := map[string]any{
		"attempt_id": attemptID,
		"run_id":     "r-hang",
		"job_id":     "j-hang",
		"steps":      []map[string]string{{"type": "noop", "description": "x"}},
	}
	srv := dispatchOnce(t, dispatch, nil)
	t.Cleanup(srv.Close)

	c := client.New(srv.URL)
	cfg := worker.Config{
		DeckID:            "deck-1",
		EndpointURL:       srv.URL,
		HeartbeatInterval: 0,
		PollInterval:      1 * time.Millisecond,
		StepDuration:      0,
	}
	w_ := worker.New(worker.Deps{Cfg: cfg, Store: s, Client: c, Chaos: chaosState, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), CrashFn: func() {}, FatalExit: func(int) {}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		w_.Run(ctx)
		close(done)
	}()

	require.Eventually(t, func() bool {
		a, err := s.GetAttempt(context.Background(), attemptID)
		return err == nil && a.State == localstore.StateInProgress
	}, 2*time.Second, 5*time.Millisecond, "worker must reach the hang park")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not return after ctx cancel")
	}

	final, err := s.GetAttempt(context.Background(), attemptID)
	require.NoError(t, err)
	require.Equal(t, localstore.StateInProgress, final.State)
}

// TestWorker_abortBetweenSteps: pre-seed abort_requested before dispatch
// so the step loop fails immediately (avoids StepDuration=0 race).
func TestWorker_abortBetweenSteps(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	ctx := context.Background()

	const attemptID = "att-w3"

	a := localstore.Attempt{
		AttemptID: attemptID, RunID: "r1", JobID: "j1", DeckID: "deck-1",
		StepsJSON:  `[{"type":"noop","description":"step1"},{"type":"noop","description":"step2"}]`,
		ReceivedAt: time.Now().UTC(),
	}
	_, err := s.InsertReceived(ctx, a)
	require.NoError(t, err)
	require.NoError(t, s.SetAbortRequested(ctx, attemptID))

	dispatch := map[string]any{
		"attempt_id": attemptID,
		"run_id":     "r1",
		"job_id":     "j1",
		"steps": []map[string]string{
			{"type": "noop", "description": "step1"},
			{"type": "noop", "description": "step2"},
		},
	}
	srv := dispatchOnce(t, dispatch, nil)
	t.Cleanup(srv.Close)

	w_ := newWorker(s, srv.URL)
	runCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go w_.Run(runCtx)

	require.Eventually(t, func() bool {
		loaded, loadErr := s.GetAttempt(context.Background(), attemptID)
		return loadErr == nil && loaded.State == localstore.StateFailed
	}, 4*time.Second, 10*time.Millisecond, "aborted attempt must reach FAILED")
}
