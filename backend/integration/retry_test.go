package integration

import (
	"net/http"
	"testing"
	"time"

	apigen "deck-fleet/backend/internal/api/gen"
)

// TestRetry_FailedJobAllocatesNewAttempt drives a job to FAILED (by
// resolving an AMBIGUOUS attempt to FAILED — the only operator path
// that produces a FAILED status without re-running the executor),
// then retries and asserts a fresh attempt id appears.
//
// Why this construction:
//   - The executor's simulator only ever produces COMPLETED in the
//     happy path; without a FAILED-producing executor knob, FAILED
//     arises from operator resolution or from reconciliation. We use
//     a hang + AMBIGUOUS escalation + operator FAILED resolution to
//     get a clean FAILED state with one attempt on record.
//   - Then we retry. The Dispatcher inside the retry handler can't
//     immediately allocate (the deck slot is still claimed by the
//     CANCELLED... wait no, after AMBIGUOUS resolve to FAILED, the
//     slot is freed and the new dispatch lands).
func TestRetry_FailedJobAllocatesNewAttempt(t *testing.T) {
	t.Parallel()

	hang1 := defaultExecutorSpec("deck-1")
	hang1.Chaos.HangAfterStep = intPtr(1)
	hang1.Worker.StepDuration = 50 * time.Millisecond

	// Second deck runs a slow job that stays alive throughout the
	// test; this keeps the run materialized as RUNNING (not FAILED)
	// while we resolve j1 to FAILED and issue retry.
	keepalive := defaultExecutorSpec("deck-2")
	keepalive.Worker.StepDuration = 5 * time.Second // long enough for the whole test

	cfg := defaultOrchestratorConfig(t)
	cfg.Timeouts.AttemptDeadlineBase = 300 * time.Millisecond
	cfg.Timeouts.AmbiguousDeadline = 1 * time.Second

	h := newHarness(t, harnessOptions{
		Orchestrator:  cfg,
		Executors:     []executorSpec{hang1, keepalive},
		AwaitDegraded: true,
	})

	body := mustJSON(t, map[string]any{
		"id": "retry-run",
		"deck_jobs": []map[string]any{
			{"id": "j1", "deck_id": "deck-1", "depends_on": []string{}, "steps": []map[string]string{{"type": "incubate", "description": "hang"}, {"type": "report", "description": "never"}}},
			{"id": "j2", "deck_id": "deck-2", "depends_on": []string{}, "steps": []map[string]string{{"type": "incubate", "description": "long"}, {"type": "report", "description": "long"}}},
		},
	})
	if resp, raw := h.Client.SubmitRunJSON(t, body); resp.StatusCode != http.StatusCreated {
		t.Fatalf("submit: %d body=%s", resp.StatusCode, raw)
	}

	ambiguous := h.WaitForJobStatus(t, "retry-run", "j1", apigen.DeckJobStatusAMBIGUOUS, 5*time.Second)
	if ambiguous.CurrentAttemptId == nil {
		t.Fatalf("AMBIGUOUS job missing attempt id")
	}
	firstAttempt := ambiguous.CurrentAttemptId.String()

	resp, raw := h.Client.ResolveJob(t, "retry-run", "j1", ambiguous.Version, apigen.AttemptOutcomeFAILED, "test: declaring failure")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve: %d body=%s", resp.StatusCode, raw)
	}

	// Worker still hung; restart with Hang cleared so retry dispatch can complete.
	h.Executors["deck-1"].specOrig.Chaos.HangAfterStep = intPtr(0)
	h.Executors["deck-1"].specOrig.Chaos.Hang = boolPtr(false)
	h.Executors["deck-1"].Restart(t)

	failed := h.WaitForJobStatus(t, "retry-run", "j1", apigen.DeckJobStatusFAILED, 3*time.Second)
	retryResp, retryBody := h.Client.RetryJob(t, "retry-run", "j1", failed.Version)
	if retryResp.StatusCode != http.StatusOK {
		t.Fatalf("retry: %d body=%s", retryResp.StatusCode, retryBody)
	}

	completed := h.WaitForJobStatus(t, "retry-run", "j1", apigen.DeckJobStatusCOMPLETED, 5*time.Second)
	if completed.CurrentAttemptId == nil {
		t.Fatalf("retried job missing attempt id")
	}
	if completed.CurrentAttemptId.String() == firstAttempt {
		t.Errorf("retry reused attempt id %s; should be fresh", firstAttempt)
	}
	if completed.RecentAttempts == nil || len(*completed.RecentAttempts) < 2 {
		t.Errorf("expected ≥2 attempts in history after retry, got %v", completed.RecentAttempts)
	}

	found := false
	for _, e := range h.ListEvents(t) {
		if e.Kind == "JOB_RETRIED" && e.RunID == "retry-run" && e.JobID == "j1" {
			if jsonContains(t, []byte(e.Payload), "previous_attempt_id", firstAttempt) {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("no JOB_RETRIED event referencing previous_attempt_id=%s", firstAttempt)
	}
}

// TestRetry_FailedRunResumes asserts the FAILED-non-terminal contract
// (DESIGN.md "State machine — run status"): a run whose status has
// materialized to FAILED (because at least one deck_job is FAILED and no
// active jobs remain) is NOT terminal, retries are accepted, and the
// run resumes (status FAILED → RUNNING → eventually COMPLETED).
//
// Without this, fan-outs where some branches succeed and one fails get
// auto-locked: the materializer races the operator's hand to the Retry
// button.
func TestRetry_FailedRunResumes(t *testing.T) {
	t.Parallel()

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

	// Single-job run: j1 will hang → AMBIGUOUS → operator-resolve-FAILED.
	// With no other active jobs, the run materializes to FAILED.
	body := mustJSON(t, map[string]any{
		"id": "failed-run-retry",
		"deck_jobs": []map[string]any{
			{"id": "j1", "deck_id": "deck-1", "depends_on": []string{}, "steps": []map[string]string{{"type": "incubate", "description": "hang"}, {"type": "report", "description": "never"}}},
		},
	})
	if resp, raw := h.Client.SubmitRunJSON(t, body); resp.StatusCode != http.StatusCreated {
		t.Fatalf("submit: %d body=%s", resp.StatusCode, raw)
	}

	ambiguous := h.WaitForJobStatus(t, "failed-run-retry", "j1", apigen.DeckJobStatusAMBIGUOUS, 5*time.Second)
	resp, raw := h.Client.ResolveJob(t, "failed-run-retry", "j1", ambiguous.Version, apigen.AttemptOutcomeFAILED, "test: failed")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve: %d body=%s", resp.StatusCode, raw)
	}

	// Run materializes to FAILED. Critically: terminal_at must be NULL.
	failedRun := h.WaitForRunStatus(t, "failed-run-retry", apigen.FAILED, 3*time.Second)
	if failedRun.TerminalAt != nil {
		t.Errorf("FAILED run must NOT have terminal_at stamped (FAILED is non-terminal); got %v", failedRun.TerminalAt)
	}

	// Clear the hang so retry can dispatch and complete.
	h.Executors["deck-1"].specOrig.Chaos.HangAfterStep = intPtr(0)
	h.Executors["deck-1"].specOrig.Chaos.Hang = boolPtr(false)
	h.Executors["deck-1"].Restart(t)

	failedJob := h.WaitForJobStatus(t, "failed-run-retry", "j1", apigen.DeckJobStatusFAILED, 3*time.Second)

	// Retry must be accepted on the FAILED-status run.
	retryResp, retryBody := h.Client.RetryJob(t, "failed-run-retry", "j1", failedJob.Version)
	if retryResp.StatusCode != http.StatusOK {
		t.Fatalf("retry on FAILED-status run rejected: %d body=%s", retryResp.StatusCode, retryBody)
	}

	// The retry resumes the run; the job eventually completes.
	completedRun := h.WaitForRunStatus(t, "failed-run-retry", apigen.COMPLETED, 5*time.Second)
	if completedRun.TerminalAt == nil {
		t.Errorf("COMPLETED run must have terminal_at stamped")
	}
}

// TestRetry_NonFailedJobRejected covers the API.md §8.5 guard:
// retrying a job that isn't in FAILED returns 409 INVALID_TRANSITION.
func TestRetry_NonFailedJobRejected(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1", "deck-2", "deck-3"),
		AwaitDegraded: true,
	})
	run := h.Client.SubmitRunFromFile(t, "linear")
	final := h.WaitForRunStatus(t, run.Id, apigen.COMPLETED, 8*time.Second)

	job := final.DeckJobs[0]
	resp, body := h.Client.RetryJob(t, run.Id, job.Id, job.Version)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("retry COMPLETED: status %d body=%s", resp.StatusCode, body)
	}
	env := decodeError(t, body)
	// API.md §8.5: parent terminal -> ALREADY_TERMINAL (carries
	// RunStatus); job-state wrong -> INVALID_TRANSITION (carries
	// DeckJobStatus). Linear COMPLETED hits the ALREADY_TERMINAL
	// branch first because the parent run is terminal.
	if env.Error.Code != apigen.ErrorCodeALREADYTERMINAL && env.Error.Code != apigen.ErrorCodeINVALIDTRANSITION {
		t.Errorf("code = %s want ALREADY_TERMINAL or INVALID_TRANSITION; body=%s", env.Error.Code, body)
	}
}
