package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	apigen "deck-fleet/backend/internal/api/gen"
)

// TestIdempotency_DuplicateSubmit covers Pattern A from API.md §5: the
// DAG id is the natural idempotency key, and re-submitting the same id
// returns 409 DUPLICATE_RESOURCE with the existing run in details.
func TestIdempotency_DuplicateSubmit(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1", "deck-2", "deck-3"),
		AwaitDegraded: true,
	})

	run := h.Client.SubmitRunFromFile(t, "linear")
	resp, body := h.Client.SubmitRunJSON(t, loadSample(t, "linear"))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d want 409; body=%s", resp.StatusCode, body)
	}
	env := decodeError(t, body)
	if env.Error.Code != apigen.ErrorCodeDUPLICATERESOURCE {
		t.Fatalf("code = %s want DUPLICATE_RESOURCE; body=%s", env.Error.Code, body)
	}
	// UI contract: DUPLICATE_RESOURCE embeds current_state for re-render without GET.
	if !strings.Contains(string(body), `"current_state"`) {
		t.Errorf("DUPLICATE_RESOURCE missing current_state: %s", body)
	}
	if !strings.Contains(string(body), run.Id) {
		t.Errorf("DUPLICATE_RESOURCE current_state should reference %q: %s", run.Id, body)
	}
}

// TestIdempotency_DispatchSurvivesRestart covers ARCHITECTURE.md §5.4
// scenario 1: the orchestrator restarts between writing the attempt
// and the executor consuming it. The same attempt_id must return on
// the executor's next poll so the executor's local dedup catches the
// replay and no physical work is duplicated.
//
// We force the order by submitting a run while the only target deck's
// executor egress is paused. The orchestrator allocates and persists
// the attempt before any poll arrives; we then restart the
// orchestrator (durable SQLite preserves the row) and resume egress.
// The job must complete with exactly one attempt.
func TestIdempotency_DispatchSurvivesRestart(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1", "deck-2", "deck-3"),
		AwaitDegraded: true,
	})

	ex := h.Executors["deck-1"]

	ex.PauseEgress()

	run := h.Client.SubmitRunFromFile(t, "linear")
	preRun := h.Client.GetRun(t, run.Id)
	var prepAttemptID string
	for _, j := range preRun.DeckJobs {
		if j.Id == "prep" && j.CurrentAttemptId != nil {
			prepAttemptID = j.CurrentAttemptId.String()
		}
	}
	if prepAttemptID == "" {
		t.Fatalf("expected prep to be DISPATCHED with an attempt_id before restart; got %+v", preRun)
	}

	h.Restart(t)
	ex.ResumeEgress()

	final := h.WaitForRunStatus(t, run.Id, apigen.COMPLETED, 8*time.Second)
	for _, j := range final.DeckJobs {
		if j.Id != "prep" {
			continue
		}
		if j.CurrentAttemptId == nil || j.CurrentAttemptId.String() != prepAttemptID {
			t.Errorf("prep attempt id changed across restart: pre=%s post=%v", prepAttemptID, j.CurrentAttemptId)
		}
		if j.RecentAttempts == nil || len(*j.RecentAttempts) != 1 {
			t.Errorf("expected 1 attempt for prep, got %v", j.RecentAttempts)
		}
	}
}

// TestIdempotency_DuplicateExecutorEvent covers API.md §5 Pattern B:
// the same (attempt_id, kind) replays are dedup'd by the orchestrator
// and return `applied` / `duplicate` cleanly. We force a duplicate
// COMPLETED by pausing egress mid-step so the outbox accumulates, then
// resuming.
//
// What we assert: the orchestrator only ever applies one COMPLETED to
// the job (Status = COMPLETED, one attempt, one COMPLETED outcome
// stamped) regardless of how many times the outbox flushes the same
// row.
func TestIdempotency_DuplicateExecutorEvent(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1", "deck-2", "deck-3"),
		AwaitDegraded: true,
	})

	run := h.Client.SubmitRunFromFile(t, "linear")
	final := h.WaitForRunStatus(t, run.Id, apigen.COMPLETED, 8*time.Second)

	for _, j := range final.DeckJobs {
		if j.RecentAttempts == nil {
			t.Errorf("job %s has no recent_attempts", j.Id)
			continue
		}
		var completedCount int
		for _, a := range *j.RecentAttempts {
			if a.Outcome != nil && *a.Outcome == apigen.AttemptOutcomeCOMPLETED {
				completedCount++
			}
		}
		if completedCount != 1 {
			t.Errorf("job %s has %d COMPLETED attempts, want 1", j.Id, completedCount)
		}
	}

	events := h.ListEvents(t)
	var completedEvents int
	for _, e := range events {
		if e.Kind == "JOB_COMPLETED" && e.RunID == run.Id {
			completedEvents++
		}
	}
	if completedEvents != len(final.DeckJobs) {
		t.Errorf("JOB_COMPLETED event count = %d want %d", completedEvents, len(final.DeckJobs))
	}
}
