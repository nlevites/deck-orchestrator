package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	apigen "deck-fleet/backend/internal/api/gen"
)

// TestCancel_CascadeWhileRunning exercises the happy-path cancel flow:
// every non-terminal deck_job moves to CANCELLED, the cascade emits
// JOB_CANCELLED per job, and the orchestrator dials the abort endpoint
// on any deck whose attempt was in flight.
func TestCancel_CascadeWhileRunning(t *testing.T) {
	t.Parallel()

	// A hanging executor on deck-1 keeps the run in RUNNING so we can
	// cancel it. The other two decks finish their (still-PENDING)
	// downstream jobs only after `prep` completes, which never
	// happens — so we cancel before any of them runs.
	hang := defaultExecutorSpec("deck-1")
	hang.Chaos.HangAfterStep = intPtr(1)
	hang.Worker.StepDuration = 50 * time.Millisecond
	specs := []executorSpec{
		hang,
		defaultExecutorSpec("deck-2"),
		defaultExecutorSpec("deck-3"),
	}

	cfg := defaultOrchestratorConfig(t)
	cfg.Timeouts.AttemptDeadlineBase = 30 * time.Second
	cfg.Timeouts.AmbiguousDeadline = 30 * time.Second

	h := newHarness(t, harnessOptions{
		Orchestrator:  cfg,
		Executors:     specs,
		AwaitDegraded: true,
	})

	run := h.Client.SubmitRunFromFile(t, "linear")
	h.WaitForJobStatus(t, run.Id, "prep", apigen.DeckJobStatusRUNNING, 3*time.Second)

	current := h.Client.GetRun(t, run.Id)
	resp, body := h.Client.CancelRun(t, run.Id, current.Version)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel: status %d body=%s", resp.StatusCode, body)
	}

	final := h.WaitForRunStatus(t, run.Id, apigen.CANCELLED, 5*time.Second)
	for _, j := range final.DeckJobs {
		if j.Status != apigen.DeckJobStatusCANCELLED {
			t.Errorf("job %s status = %s want CANCELLED", j.Id, j.Status)
		}
	}

	cancelledEvents := 0
	for _, e := range h.ListEvents(t) {
		if e.Kind == "JOB_CANCELLED" && e.RunID == run.Id {
			cancelledEvents++
		}
	}
	if cancelledEvents != len(final.DeckJobs) {
		t.Errorf("JOB_CANCELLED count = %d want %d", cancelledEvents, len(final.DeckJobs))
	}

	decks := h.Client.ListDecks(t)
	for _, d := range decks {
		if d.CurrentJob != nil {
			t.Errorf("deck %s still occupied after cancel: %+v", d.Id, *d.CurrentJob)
		}
	}
}

// TestCancel_AbortWhileExecutorUnreachable exercises ARCHITECTURE.md
// §4.4's honest guarantees: abort delivery is best-effort, but the
// orchestrator's CANCELLED decision is authoritative. We pause the
// executor's HTTP server so the abort dial fails; the run still moves
// to CANCELLED and the cascade events fire.
func TestCancel_AbortWhileExecutorUnreachable(t *testing.T) {
	t.Parallel()

	hang := defaultExecutorSpec("deck-1")
	hang.Chaos.HangAfterStep = intPtr(1)
	hang.Worker.StepDuration = 50 * time.Millisecond

	cfg := defaultOrchestratorConfig(t)
	cfg.Timeouts.AttemptDeadlineBase = 30 * time.Second
	cfg.Timeouts.AmbiguousDeadline = 30 * time.Second
	// Short abort retry budget so the dialer gives up quickly.
	cfg.Timeouts.AbortRetryInitial = 50 * time.Millisecond
	cfg.Timeouts.AbortRetryMaxDuration = 300 * time.Millisecond

	h := newHarness(t, harnessOptions{
		Orchestrator: cfg,
		Executors: []executorSpec{
			hang,
			defaultExecutorSpec("deck-2"),
			defaultExecutorSpec("deck-3"),
		},
		AwaitDegraded: true,
	})

	run := h.Client.SubmitRunFromFile(t, "linear")
	h.WaitForJobStatus(t, run.Id, "prep", apigen.DeckJobStatusRUNNING, 3*time.Second)

	h.Executors["deck-1"].PauseHTTPServer()
	t.Cleanup(func() { h.Executors["deck-1"].ResumeHTTPServer() })

	current := h.Client.GetRun(t, run.Id)
	resp, body := h.Client.CancelRun(t, run.Id, current.Version)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel: status %d body=%s", resp.StatusCode, body)
	}
	final := h.WaitForRunStatus(t, run.Id, apigen.CANCELLED, 5*time.Second)
	for _, j := range final.DeckJobs {
		if j.Status != apigen.DeckJobStatusCANCELLED {
			t.Errorf("job %s status = %s want CANCELLED", j.Id, j.Status)
		}
	}
}

// TestCancel_AmbiguousJobAbortsExecutor verifies the Phase-1 fix: when
// a cancel cascade reaches an AMBIGUOUS deck_job that has a live
// current_attempt_id, the abort dialer is scheduled for that attempt.
//
// Pre-fix, collectAbortTargets only included DISPATCHED and RUNNING
// jobs. An AMBIGUOUS attempt still had a live executor (hung in
// <-ctx.Done()) that could keep consuming physical work even after the
// operator cancelled the run. With the fix, the executor receives
// POST /executor/abort/{attempt_id} and sets abort_requested = 1 in
// its local SQLite — verifiable directly via the harness.
//
// The executor is hung (HangAfterStep=1), so the worker can't observe
// abort_requested between steps. That's intentional: the test checks
// that the abort signal was *delivered* to the executor, not that the
// worker acted on it. Abort delivery is what the fix changes; abort
// observance is the executor's pre-existing responsibility.
func TestCancel_AmbiguousJobAbortsExecutor(t *testing.T) {
	t.Parallel()

	hang := defaultExecutorSpec("deck-1")
	hang.Chaos.HangAfterStep = intPtr(1)
	hang.Worker.StepDuration = 50 * time.Millisecond

	cfg := defaultOrchestratorConfig(t)
	// Short AttemptDeadline so the job escalates to AMBIGUOUS quickly
	// via the liveness monitor's attemptDeadlineScan path. With
	// SweepInterval=100ms (harness default), a 600ms deadline means
	// the escalation fires within ~1s of the job starting.
	cfg.Timeouts.AttemptDeadlineBase = 600 * time.Millisecond
	cfg.Timeouts.AmbiguousDeadline = 30 * time.Second // don't let the second sweep interfere
	cfg.Timeouts.AbortRetryInitial = 50 * time.Millisecond
	cfg.Timeouts.AbortRetryMaxDuration = 3 * time.Second

	h := newHarness(t, harnessOptions{
		Orchestrator:  cfg,
		Executors:     []executorSpec{hang},
		AwaitDegraded: true,
	})

	body := mustJSON(t, map[string]any{
		"id": "cancel-ambig-abort",
		"deck_jobs": []map[string]any{
			{
				"id":         "j1",
				"deck_id":    "deck-1",
				"depends_on": []string{},
				"steps":      []map[string]string{{"type": "work", "description": "hangs after step 1"}},
			},
		},
	})
	if resp, raw := h.Client.SubmitRunJSON(t, body); resp.StatusCode != 201 {
		t.Fatalf("submit: %d body=%s", resp.StatusCode, raw)
	}

	h.WaitForJobStatus(t, "cancel-ambig-abort", "j1", apigen.DeckJobStatusAMBIGUOUS, 8*time.Second)

	run := h.Client.GetRun(t, "cancel-ambig-abort")
	var j1 apigen.DeckJob
	for _, j := range run.DeckJobs {
		if j.Id == "j1" {
			j1 = j
		}
	}
	if j1.CurrentAttemptId == nil {
		t.Fatal("j1.current_attempt_id is nil after AMBIGUOUS escalation — test setup bug")
	}
	attemptID := j1.CurrentAttemptId.String()

	resp, raw := h.Client.CancelRun(t, "cancel-ambig-abort", run.Version)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel: status %d body=%s", resp.StatusCode, raw)
	}
	h.WaitForRunStatus(t, "cancel-ambig-abort", apigen.CANCELLED, 5*time.Second)

	// Abort dialer retries with backoff; poll until abort_requested lands in local SQLite.
	ctx := context.Background()
	eventually(t, 5*time.Second, func() bool {
		a, err := h.Executors["deck-1"].Local.GetAttempt(ctx, attemptID)
		if err != nil {
			return false
		}
		return a.AbortRequested
	}, "executor abort_requested never set for attempt %s after cancel-of-AMBIGUOUS; pre-fix this path skipped AMBIGUOUS jobs", attemptID)
}

// TestCancel_AlreadyTerminal asserts that cancelling a finished run
// returns 409 ALREADY_TERMINAL (with the terminal run in details).
func TestCancel_AlreadyTerminal(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1", "deck-2", "deck-3"),
		AwaitDegraded: true,
	})

	run := h.Client.SubmitRunFromFile(t, "linear")
	final := h.WaitForRunStatus(t, run.Id, apigen.COMPLETED, 8*time.Second)

	resp, body := h.Client.CancelRun(t, run.Id, final.Version)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("cancel: status %d body=%s", resp.StatusCode, body)
	}
	env := decodeError(t, body)
	if env.Error.Code != apigen.ErrorCodeALREADYTERMINAL {
		t.Errorf("code = %s want ALREADY_TERMINAL; body=%s", env.Error.Code, body)
	}
}
