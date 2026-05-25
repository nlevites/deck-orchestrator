package integration

import (
	"testing"
	"time"

	apigen "deck-fleet/backend/internal/api/gen"
)

// TestOrchestratorRestart_DEGRADEDMODE asserts that an orchestrator
// restart enters DEGRADED_MODE during startup reconciliation and
// clears the flag once reconciliation completes. We don't try to
// observe a *non-empty* reconciliation here (that would race the
// degraded clear in compressed-time tests); we just verify the
// flag-lifecycle invariant from ARCHITECTURE.md §4.1.
func TestOrchestratorRestart_DEGRADEDMODE(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1", "deck-2", "deck-3"),
		AwaitDegraded: true,
	})

	run := h.Client.SubmitRunFromFile(t, "linear")
	h.WaitForRunStatus(t, run.Id, apigen.COMPLETED, 8*time.Second)

	h.Restart(t)
	again := h.Client.GetRun(t, run.Id)
	if again.Status != apigen.COMPLETED {
		t.Errorf("post-restart run status = %s want COMPLETED", again.Status)
	}
}

// TestOrchestratorRestart_InFlightReconciled covers ARCHITECTURE.md
// §5.4 scenario 1: orchestrator restarts while a deck_job is in
// flight. The startup reconciler dials the executor for ground truth;
// because the executor's local SQLite shows the attempt completed (we
// orchestrate this by letting the run finish at the executor side
// during the restart window), the job ends up COMPLETED with the
// correct attempt id.
func TestOrchestratorRestart_InFlightReconciled(t *testing.T) {
	t.Parallel()

	// Pause egress so orchestrator view is DISPATCHED at restart; reconciler must dial for ground truth.
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1"),
		AwaitDegraded: true,
	})
	ex := h.Executors["deck-1"]

	body := mustJSON(t, map[string]any{
		"id": "restart-run",
		"deck_jobs": []map[string]any{
			{"id": "j1", "deck_id": "deck-1", "depends_on": []string{}, "steps": []map[string]string{{"type": "incubate", "description": "fast"}}},
		},
	})
	if resp, raw := h.Client.SubmitRunJSON(t, body); resp.StatusCode != 201 {
		t.Fatalf("submit: %d body=%s", resp.StatusCode, raw)
	}

	pre := h.WaitForJobStatus(t, "restart-run", "j1", apigen.DeckJobStatusDISPATCHED, 2*time.Second)
	ex.PauseEgress()

	time.Sleep(200 * time.Millisecond)

	h.Restart(t)
	ex.ResumeEgress()

	final := h.WaitForJobStatus(t, "restart-run", "j1", apigen.DeckJobStatusCOMPLETED, 3*time.Second)
	if final.CurrentAttemptId == nil || final.CurrentAttemptId.String() != pre.CurrentAttemptId.String() {
		t.Errorf("attempt id changed across restart: pre=%v post=%v",
			pre.CurrentAttemptId, final.CurrentAttemptId)
	}
}
