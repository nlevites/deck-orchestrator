package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	apigen "deck-fleet/backend/internal/api/gen"
)

// TestExecutorHang_AmbiguousAtDeadline covers ARCHITECTURE.md §5.3
// path 3 (hang detection): the orchestrator's AttemptDeadline expires
// on a still-RUNNING attempt; the reconciler dials; the executor
// confirms IN_PROGRESS; the orchestrator escalates to AMBIGUOUS with
// reason DEADLINE_EXCEEDED. Downstream jobs do not advance past the
// AMBIGUOUS ancestor.
func TestExecutorHang_AmbiguousAtDeadline(t *testing.T) {
	t.Parallel()

	hang := defaultExecutorSpec("deck-1")
	hang.Chaos.HangAfterStep = intPtr(1)
	hang.Worker.StepDuration = 50 * time.Millisecond

	cfg := defaultOrchestratorConfig(t)
	cfg.Timeouts.AttemptDeadlineBase = 300 * time.Millisecond
	cfg.Timeouts.AmbiguousDeadline = 1 * time.Second

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
	ambiguous := h.WaitForJobStatus(t, run.Id, "prep", apigen.DeckJobStatusAMBIGUOUS, 5*time.Second)
	if ambiguous.Status != apigen.DeckJobStatusAMBIGUOUS {
		t.Fatalf("prep status = %s want AMBIGUOUS", ambiguous.Status)
	}

	var reasons []string
	for _, e := range h.ListEvents(t) {
		if e.Kind == "JOB_AMBIGUOUS" && e.RunID == run.Id && e.JobID == "prep" {
			reasons = append(reasons, e.Payload)
		}
	}
	if len(reasons) == 0 {
		t.Fatalf("no JOB_AMBIGUOUS event for prep")
	}
	ok := false
	for _, p := range reasons {
		if strings.Contains(p, "DEADLINE_EXCEEDED") || strings.Contains(p, "DEADLINE_ELAPSED") {
			ok = true
		}
	}
	if !ok {
		t.Errorf("JOB_AMBIGUOUS payloads %v missing DEADLINE_* reason", reasons)
	}

	current := h.Client.GetRun(t, run.Id)
	if current.Status != apigen.AMBIGUOUS {
		t.Errorf("run status = %s want AMBIGUOUS", current.Status)
	}
	for _, j := range current.DeckJobs {
		if j.Id == "prep" {
			continue
		}
		if j.Status != apigen.DeckJobStatusPENDING {
			t.Errorf("downstream job %s status = %s want PENDING", j.Id, j.Status)
		}
	}

	resp, raw := h.Client.ResolveJob(t, run.Id, "prep", ambiguous.Version,
		apigen.AttemptOutcomeCOMPLETED, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve: %d body=%s", resp.StatusCode, raw)
	}
	h.WaitForJobStatus(t, run.Id, "incubate", apigen.DeckJobStatusCOMPLETED, 5*time.Second)
	h.WaitForJobStatus(t, run.Id, "measure", apigen.DeckJobStatusCOMPLETED, 5*time.Second)
}
