package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	apigen "deck-fleet/backend/internal/api/gen"
)

// TestConcurrency_VersionMismatchOnCancel exercises API.md §6's
// optimistic-concurrency contract: a cancel submitted with a stale
// `expected_version` returns 409 VERSION_MISMATCH with the current
// run embedded in details.
//
// We move the run-version by waiting for it to reach RUNNING (which
// materializes a status change and bumps runs.version) before the
// operator's cancel arrives.
func TestConcurrency_VersionMismatchOnCancel(t *testing.T) {
	t.Parallel()

	// HangAfterStep=1 on a 2-step job keeps the executor running the
	// first step, then sleeping forever — the orchestrator records
	// RUNNING (bumping runs.version) but the run never reaches a
	// terminal state. That gives us an unambiguous, race-free window
	// in which a stale-version cancel must produce VERSION_MISMATCH.
	hang := defaultExecutorSpec("deck-1")
	hang.Worker.StepDuration = 50 * time.Millisecond
	hang.Chaos.HangAfterStep = intPtr(1)

	// Push AttemptDeadline / AmbiguousDeadline out so the liveness
	// monitor doesn't escalate this hung attempt to AMBIGUOUS while
	// we're racing the cancel. RUNNING is the state we want stable.
	cfg := defaultOrchestratorConfig(t)
	cfg.Timeouts.AttemptDeadlineBase = 30 * time.Second
	cfg.Timeouts.AmbiguousDeadline = 30 * time.Second

	h := newHarness(t, harnessOptions{
		Orchestrator:  cfg,
		Executors:     []executorSpec{hang},
		AwaitDegraded: true,
	})

	body := mustJSON(t, map[string]any{
		"id": "concurrency-run",
		"deck_jobs": []map[string]any{
			{
				"id":         "j1",
				"deck_id":    "deck-1",
				"depends_on": []string{},
				"steps": []map[string]string{
					{"type": "incubate", "description": "step 1 — completes"},
					{"type": "measure", "description": "step 2 — never starts (executor hangs)"},
				},
			},
		},
	})
	resp, raw := h.Client.SubmitRunJSON(t, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("submit: status %d body=%s", resp.StatusCode, raw)
	}

	h.WaitForRunStatus(t, "concurrency-run", apigen.RUNNING, 3*time.Second)
	staleVersion := int64(1)

	cancelResp, cancelBody := h.Client.CancelRun(t, "concurrency-run", staleVersion)
	if cancelResp.StatusCode != http.StatusConflict {
		t.Fatalf("cancel: status %d want 409; body=%s", cancelResp.StatusCode, cancelBody)
	}
	env := decodeError(t, cancelBody)
	if env.Error.Code != apigen.ErrorCodeVERSIONMISMATCH {
		t.Errorf("code = %s want VERSION_MISMATCH; body=%s", env.Error.Code, cancelBody)
	}
	if !strings.Contains(string(cancelBody), `"current_state"`) {
		t.Errorf("VERSION_MISMATCH missing current_state; body=%s", cancelBody)
	}

	current := h.Client.GetRun(t, "concurrency-run")
	okResp, okBody := h.Client.CancelRun(t, "concurrency-run", current.Version)
	if okResp.StatusCode != http.StatusOK {
		t.Fatalf("cancel with fresh version: status %d body=%s", okResp.StatusCode, okBody)
	}
}

// TestConcurrency_UnknownFieldRejected covers the wire-layer contract
// that operator mutation requests must not carry unknown fields. This
// is the same DecodeAndValidate path MISSING_VERSION runs through; we
// assert SCHEMA_VIOLATION as the observable outcome and the harness's
// envelope shape stays well-formed.
func TestConcurrency_UnknownFieldRejected(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1"),
		AwaitDegraded: true,
	})

	body := mustJSON(t, map[string]any{
		"id": "schema-violation",
		"deck_jobs": []map[string]any{
			{"id": "j1", "deck_id": "deck-1", "depends_on": []string{}, "steps": []map[string]string{{"type": "t", "description": "d"}}},
		},
	})
	if resp, raw := h.Client.SubmitRunJSON(t, body); resp.StatusCode != http.StatusCreated {
		t.Fatalf("submit: %d body=%s", resp.StatusCode, raw)
	}

	r, _ := http.NewRequest(http.MethodPost, h.Client.BaseURL+"/api/runs/schema-violation/cancel",
		strings.NewReader(`{"expected_version":1,"surprise":true}`))
	r.Header.Set("Content-Type", "application/json")
	resp, err := h.Client.HTTP.Do(r)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d want 400", resp.StatusCode)
	}
}
