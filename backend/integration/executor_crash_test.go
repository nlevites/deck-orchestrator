package integration

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	apigen "deck-fleet/backend/internal/api/gen"
)

// TestExecutorCrash_OutboxFlushesAfterRestart covers ARCHITECTURE.md
// §5.4 scenario 3: the executor finishes physical work for a step,
// the worker bumps last_completed_step in local SQLite, then
// "crashes" (we simulate via the OnCrash hook) before the post-loop
// terminal-marking block runs. After restart, the worker resumes the
// IN_PROGRESS attempt, sees the cursor already at the end of the
// step list, skips re-execution, marks COMPLETED, and the outbox
// flusher delivers. The orchestrator records the COMPLETED.
func TestExecutorCrash_OutboxFlushesAfterRestart(t *testing.T) {
	t.Parallel()

	var hRef *Harness
	var crashed atomic.Bool
	crash := defaultExecutorSpec("deck-1")
	crash.Chaos.CrashAfterStep = intPtr(1)
	crash.Worker.StepDuration = 50 * time.Millisecond
	crash.OnCrash = func() {
		crashed.Store(true)
	}

	h := newHarness(t, harnessOptions{
		Executors:     []executorSpec{crash},
		AwaitDegraded: true,
	})
	hRef = h
	_ = hRef

	body := mustJSON(t, map[string]any{
		"id": "crash-run",
		"deck_jobs": []map[string]any{
			{"id": "j1", "deck_id": "deck-1", "depends_on": []string{}, "steps": []map[string]string{{"type": "incubate", "description": "one step then crash"}}},
		},
	})
	if resp, raw := h.Client.SubmitRunJSON(t, body); resp.StatusCode != 201 {
		t.Fatalf("submit: %d body=%s", resp.StatusCode, raw)
	}

	// CrashAfterStep=1 fires after last_completed_step is committed but before terminal marking.
	eventually(t, 3*time.Second, func() bool { return crashed.Load() }, "OnCrash never fired")

	h.Executors["deck-1"].specOrig.Chaos.CrashAfterStep = intPtr(0)
	h.Executors["deck-1"].Restart(t)

	h.WaitForJobStatus(t, "crash-run", "j1", apigen.DeckJobStatusCOMPLETED, 5*time.Second)
}

// TestExecutorCrash_MultiStepResumeSkipsCompletedSteps is the C2-fix
// regression test. Pre-fix, on a multi-step job the worker re-ran every
// step from index 0 after a mid-attempt crash, duplicating physical
// work for precious samples. With the per-step cursor, the worker
// commits `last_completed_step` after each step's sleep returns and
// skips i < cursor on resume.
//
// Mechanism: a 4-step job, CrashAfterStep=2. After restart, the
// worker should resume from step 3 (skipping 1 and 2). The assertion
// is that the post-restart wall-clock-active time is shorter than a
// from-scratch re-run would take. We give a generous margin: the
// post-restart work must complete in <= roughly the cost of running
// 2 steps + slack, not 4.
func TestExecutorCrash_MultiStepResumeSkipsCompletedSteps(t *testing.T) {
	t.Parallel()

	var crashed atomic.Bool
	const stepDur = 200 * time.Millisecond

	crash := defaultExecutorSpec("deck-1")
	crash.Chaos.CrashAfterStep = intPtr(2)
	crash.Worker.StepDuration = stepDur
	crash.OnCrash = func() {
		crashed.Store(true)
	}

	h := newHarness(t, harnessOptions{
		Executors:     []executorSpec{crash},
		AwaitDegraded: true,
	})

	body := mustJSON(t, map[string]any{
		"id": "multi-step-crash",
		"deck_jobs": []map[string]any{
			{
				"id": "j1", "deck_id": "deck-1", "depends_on": []string{},
				"steps": []map[string]string{
					{"type": "prepare", "description": "step 1"},
					{"type": "transfer", "description": "step 2"},
					{"type": "incubate", "description": "step 3"},
					{"type": "measure", "description": "step 4"},
				},
			},
		},
	})
	if resp, raw := h.Client.SubmitRunJSON(t, body); resp.StatusCode != 201 {
		t.Fatalf("submit: %d body=%s", resp.StatusCode, raw)
	}

	eventually(t, 5*time.Second, func() bool { return crashed.Load() }, "OnCrash never fired")

	// Pre-restart cursor proves per-step commits survived the crash (C2 regression guard).
	ctx := context.Background()
	cur, ok, err := h.Executors["deck-1"].Local.CurrentInFlight(ctx)
	if err != nil {
		t.Fatalf("read pre-restart attempt: %v", err)
	}
	if !ok {
		t.Fatal("expected an IN_PROGRESS attempt in local store at crash time")
	}
	if cur.LastCompletedStep != 2 {
		t.Fatalf("pre-restart cursor = %d want 2 (proves worker bumped after each step)", cur.LastCompletedStep)
	}

	h.Executors["deck-1"].specOrig.Chaos.CrashAfterStep = intPtr(0)

	// Threshold separates 2-step resume (~800ms) from 4-step re-run (~1200ms).
	start := time.Now()
	h.Executors["deck-1"].Restart(t)
	h.WaitForJobStatus(t, "multi-step-crash", "j1", apigen.DeckJobStatusCOMPLETED, 5*time.Second)
	elapsed := time.Since(start)
	threshold := 1000 * time.Millisecond
	if elapsed >= threshold {
		t.Fatalf("post-restart elapsed = %s; expected < %s (2 remaining steps + wiring). "+
			"Regression: worker likely re-ran completed steps.", elapsed, threshold)
	}
}
