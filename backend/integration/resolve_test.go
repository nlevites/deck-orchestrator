package integration

import (
	"context"
	"net/http"
	"testing"
	"time"

	apigen "deck-fleet/backend/internal/api/gen"
)

func driveJobToAmbiguous(t *testing.T, runID string) (*Harness, apigen.DeckJob) {
	t.Helper()

	hang := defaultExecutorSpec("deck-1")
	hang.Chaos.HangAfterStep = intPtr(1)
	hang.Worker.StepDuration = 50 * time.Millisecond

	cfg := defaultOrchestratorConfig(t)
	cfg.Timeouts.AttemptDeadlineBase = 300 * time.Millisecond
	cfg.Timeouts.AmbiguousDeadline = 1 * time.Second

	h := newHarness(t, harnessOptions{
		Orchestrator:  cfg,
		Executors:     []executorSpec{hang},
		AwaitDegraded: true,
	})

	body := mustJSON(t, map[string]any{
		"id": runID,
		"deck_jobs": []map[string]any{
			{"id": "j1", "deck_id": "deck-1", "depends_on": []string{}, "steps": []map[string]string{{"type": "incubate", "description": "hang here"}, {"type": "measure", "description": "never reached"}}},
		},
	})
	if resp, raw := h.Client.SubmitRunJSON(t, body); resp.StatusCode != http.StatusCreated {
		t.Fatalf("submit: %d body=%s", resp.StatusCode, raw)
	}
	job := h.WaitForJobStatus(t, runID, "j1", apigen.DeckJobStatusAMBIGUOUS, 5*time.Second)
	return h, job
}

// TestResolve_ToCompleted: operator inspects the deck physically,
// declares COMPLETED. The attempt's outcome must be set to COMPLETED
// with outcome_source = OPERATOR_RESOLUTION; the deck slot must be
// freed; and a JOB_RESOLVED event must carry the operator note.
func TestResolve_ToCompleted(t *testing.T) {
	t.Parallel()
	h, ambiguous := driveJobToAmbiguous(t, "resolve-completed")

	resp, raw := h.Client.ResolveJob(t, "resolve-completed", "j1", ambiguous.Version,
		apigen.AttemptOutcomeCOMPLETED, "operator confirmed plate read succeeded")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve: %d body=%s", resp.StatusCode, raw)
	}

	final := h.WaitForJobStatus(t, "resolve-completed", "j1", apigen.DeckJobStatusCOMPLETED, 2*time.Second)
	if final.RecentAttempts == nil || len(*final.RecentAttempts) != 1 {
		t.Fatalf("expected exactly 1 attempt, got %v", final.RecentAttempts)
	}
	a := (*final.RecentAttempts)[0]
	if a.Outcome == nil || *a.Outcome != apigen.AttemptOutcomeCOMPLETED {
		t.Errorf("attempt outcome = %v want COMPLETED", a.Outcome)
	}
	if a.OutcomeSource == nil || *a.OutcomeSource != apigen.OPERATORRESOLUTION {
		t.Errorf("outcome_source = %v want OPERATOR_RESOLUTION", a.OutcomeSource)
	}

	found := false
	for _, e := range h.ListEvents(t) {
		if e.Kind == "JOB_RESOLVED" && e.JobID == "j1" {
			if jsonContains(t, []byte(e.Payload), "operator_note", "operator confirmed plate read succeeded") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("no JOB_RESOLVED event with the operator note")
	}

	for _, d := range h.Client.ListDecks(t) {
		if d.CurrentJob != nil {
			t.Errorf("deck %s still occupied after resolve: %+v", d.Id, *d.CurrentJob)
		}
	}
}

// TestResolve_ToFailed: operator declares FAILED. Attempt outcome
// must be FAILED; the run materializes as FAILED (only job, terminal).
func TestResolve_ToFailed(t *testing.T) {
	t.Parallel()
	h, ambiguous := driveJobToAmbiguous(t, "resolve-failed")

	resp, raw := h.Client.ResolveJob(t, "resolve-failed", "j1", ambiguous.Version,
		apigen.AttemptOutcomeFAILED, "operator: plate found broken")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve: %d body=%s", resp.StatusCode, raw)
	}
	final := h.WaitForRunStatus(t, "resolve-failed", apigen.FAILED, 2*time.Second)
	if final.DeckJobs[0].Status != apigen.DeckJobStatusFAILED {
		t.Errorf("job status = %s want FAILED", final.DeckJobs[0].Status)
	}
}

// TestResolve_FailedFiresAbort: when the operator resolves an AMBIGUOUS
// job to FAILED, the orchestrator must dial /executor/abort for that
// attempt so a still-alive executor can't go on to repeat the physical
// work. Symmetric to TestCancel_AmbiguousJobAbortsExecutor.
func TestResolve_FailedFiresAbort(t *testing.T) {
	t.Parallel()
	h, ambiguous := driveJobToAmbiguous(t, "resolve-failed-abort")
	if ambiguous.CurrentAttemptId == nil {
		t.Fatal("ambiguous job has no current_attempt_id; harness drove setup wrong")
	}
	attemptID := ambiguous.CurrentAttemptId.String()

	resp, raw := h.Client.ResolveJob(t, "resolve-failed-abort", "j1", ambiguous.Version,
		apigen.AttemptOutcomeFAILED, "operator: deck inspection — work did not complete")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve: %d body=%s", resp.StatusCode, raw)
	}

	ctx := context.Background()
	eventually(t, 5*time.Second, func() bool {
		a, err := h.Executors["deck-1"].Local.GetAttempt(ctx, attemptID)
		if err != nil {
			return false
		}
		return a.AbortRequested
	}, "executor abort_requested never set for attempt %s after resolve→FAILED", attemptID)
}

// TestResolve_CompletedDoesNotAbort: resolving to COMPLETED means the
// operator accepted the physical outcome; aborting would be incoherent.
// Verify no abort lands on the executor within a generous window.
func TestResolve_CompletedDoesNotAbort(t *testing.T) {
	t.Parallel()
	h, ambiguous := driveJobToAmbiguous(t, "resolve-completed-no-abort")
	if ambiguous.CurrentAttemptId == nil {
		t.Fatal("ambiguous job has no current_attempt_id; harness drove setup wrong")
	}
	attemptID := ambiguous.CurrentAttemptId.String()

	resp, raw := h.Client.ResolveJob(t, "resolve-completed-no-abort", "j1", ambiguous.Version,
		apigen.AttemptOutcomeCOMPLETED, "operator: confirmed plate read")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve: %d body=%s", resp.StatusCode, raw)
	}

	ctx := context.Background()
	// Give the dialer plenty of time to misfire if it were going to.
	time.Sleep(500 * time.Millisecond)
	a, err := h.Executors["deck-1"].Local.GetAttempt(ctx, attemptID)
	if err != nil {
		t.Fatalf("get attempt: %v", err)
	}
	if a.AbortRequested {
		t.Errorf("abort_requested set on COMPLETED resolution; expected no abort dial")
	}
}

// TestResolve_InvalidResolution: resolution other than COMPLETED/FAILED
// is rejected with 400 INVALID_RESOLUTION before the state machine is
// ever consulted (the handler validates body.Resolution explicitly).
func TestResolve_InvalidResolution(t *testing.T) {
	t.Parallel()
	h, ambiguous := driveJobToAmbiguous(t, "resolve-bad")

	body := mustJSON(t, map[string]any{
		"expected_version": ambiguous.Version,
		"resolution":       "RUNNING",
	})
	resp, raw := h.Client.do(t, http.MethodPost, "/api/runs/resolve-bad/jobs/j1/resolve", body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d want 400; body=%s", resp.StatusCode, raw)
	}
	env := decodeError(t, raw)
	if env.Error.Code != apigen.ErrorCodeINVALIDRESOLUTION {
		t.Errorf("code = %s want INVALID_RESOLUTION; body=%s", env.Error.Code, raw)
	}
}

// TestResolve_NonAmbiguousRejected: resolve against a job that isn't
// AMBIGUOUS returns 409 INVALID_TRANSITION carrying the job's actual
// status in details.
func TestResolve_NonAmbiguousRejected(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1", "deck-2", "deck-3"),
		AwaitDegraded: true,
	})
	run := h.Client.SubmitRunFromFile(t, "linear")
	final := h.WaitForRunStatus(t, run.Id, apigen.COMPLETED, 8*time.Second)
	job := final.DeckJobs[0]

	resp, raw := h.Client.ResolveJob(t, run.Id, job.Id, job.Version, apigen.AttemptOutcomeCOMPLETED, "")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d want 409; body=%s", resp.StatusCode, raw)
	}
	env := decodeError(t, raw)
	if env.Error.Code != apigen.ErrorCodeINVALIDTRANSITION {
		t.Errorf("code = %s want INVALID_TRANSITION; body=%s", env.Error.Code, raw)
	}
}
