package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	apigen "deck-fleet/backend/internal/api/gen"
)

// TestOrchestratorRestart_StepProgressNotLost reproduces the race that
// stranded `last_completed_step = 0` on COMPLETED jobs after an
// orchestrator restart mid-run.
//
// Sequence:
//
//  1. Submit a multi-step job and wait for it to start.
//  2. Pause the executor's egress so STEP_COMPLETED events buffer in
//     the outbox instead of shipping immediately.
//  3. Restart the orchestrator while the executor is still working
//     locally. The executor finishes the attempt during the gap; its
//     local store flips to COMPLETED.
//  4. After the restart, the orchestrator's startupReconcile dials
//     /executor/state and observes state=COMPLETED. It applies the
//     terminal transition (status COMPLETED + attempt outcome) before
//     the outbox flusher gets a chance to ship the buffered STEPs.
//  5. Resume egress. The buffered STEP_COMPLETED events arrive at a
//     deck_job whose attempt outcome is already set, hit the
//     `attempt.Outcome.Valid` short-circuit in the events handler,
//     and get logged as ExecutorEventConflictLogged. The
//     `last_completed_step` cursor is never advanced.
//
// Expected behaviour after the fix: `last_completed_step == total_steps`
// when the job reaches COMPLETED, regardless of which arm of the race
// won.
func TestOrchestratorRestart_StepProgressNotLost(t *testing.T) {
	t.Parallel()

	// Slow steps so the run is mid-flight when we trigger the restart.
	spec := defaultExecutorSpec("deck-1")
	spec.Worker.StepDuration = 300 * time.Millisecond

	h := newHarness(t, harnessOptions{
		Executors:     []executorSpec{spec},
		AwaitDegraded: true,
	})

	body, err := json.Marshal(map[string]any{
		"id": "restart-step-progress",
		"deck_jobs": []map[string]any{
			{
				"id":         "work",
				"deck_id":    "deck-1",
				"depends_on": []string{},
				"steps": []map[string]any{
					{"type": "prepare", "description": "step 1"},
					{"type": "incubate", "description": "step 2"},
					{"type": "measure", "description": "step 3"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if resp, raw := h.Client.SubmitRunJSON(t, body); resp.StatusCode != http.StatusCreated {
		t.Fatalf("submit: %d body=%s", resp.StatusCode, raw)
	}

	// Wait until the worker has actually started running so a step
	// event is mid-flight or imminent.
	h.WaitForJobStatus(t, "restart-step-progress", "work", apigen.DeckJobStatusRUNNING, 3*time.Second)

	// Pause egress: any further STEP_COMPLETED + the eventual
	// COMPLETED stay in the outbox until ResumeEgress.
	h.Executors["deck-1"].PauseEgress()

	// Give the worker enough wall time to finish all three steps locally.
	time.Sleep(1500 * time.Millisecond)

	// Restart the orchestrator. The executor is still alive; its local
	// store should now show the attempt as COMPLETED with all step
	// cursors bumped, and the outbox has the buffered STEP + COMPLETED
	// events.
	h.Restart(t)

	// Resume egress. The buffered events ship to the new orchestrator.
	h.Executors["deck-1"].ResumeEgress()

	// The run must reach COMPLETED — that's the existing guarantee.
	final := h.WaitForRunStatus(t, "restart-step-progress", apigen.COMPLETED, 5*time.Second)

	// The bug: deck_job.last_completed_step stays at 0 because the
	// reconciler stamps the terminal status without bumping the
	// cursor, and the buffered STEP events arrive too late to update
	// a terminal job.
	for _, j := range final.DeckJobs {
		if j.Id != "work" {
			continue
		}
		if j.LastCompletedStep == nil {
			t.Fatalf("last_completed_step missing on COMPLETED job")
		}
		if j.TotalSteps == nil {
			t.Fatalf("total_steps missing on COMPLETED job")
		}
		if *j.LastCompletedStep != *j.TotalSteps {
			t.Errorf("last_completed_step = %d, want %d (total_steps); "+
				"reconciler raced the outbox flush and the cursor stranded at zero",
				*j.LastCompletedStep, *j.TotalSteps)
		}
		return
	}
	t.Fatalf("deck_job 'work' not found in run detail")
}
