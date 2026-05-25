package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	apigen "deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/executor/chaos"
)

// Chaos tests drive POST /api/decks/{deck_id}/chaos* (the UI path), not direct harness mutation.
// TestChaos_GetReturnsZeroState verifies the no-chaos baseline.
func TestChaos_GetReturnsZeroState(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1"),
		AwaitDegraded: true,
	})

	got := getChaos(t, h, "deck-1")
	if got.Hang || got.Silent || got.DropEvents || got.PauseEgress || got.PauseIngress {
		t.Errorf("fresh executor should have no flags set, got %+v", got)
	}
	if got.HangAfterStep != 0 || got.CrashAfterStep != 0 {
		t.Errorf("fresh executor should have step arms at 0, got %+v", got)
	}
}

// TestChaos_PatchAndReadBack confirms POST /chaos merges fields
// correctly: present fields overwrite, missing fields leave alone.
func TestChaos_PatchAndReadBack(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1"),
		AwaitDegraded: true,
	})

	state := patchChaos(t, h, "deck-1", apigen.ChaosPatch{
		Hang:       boolPtr(true),
		DropEvents: boolPtr(true),
	})
	if !state.Hang || !state.DropEvents {
		t.Fatalf("after patch want hang+drop_events; got %+v", state)
	}
	if state.Silent {
		t.Errorf("silent should be unchanged (false), got %+v", state)
	}

	state = patchChaos(t, h, "deck-1", apigen.ChaosPatch{
		Silent: boolPtr(true),
	})
	if !state.Hang || !state.DropEvents || !state.Silent {
		t.Fatalf("incremental patch should preserve prior flags; got %+v", state)
	}

	state = postEmpty(t, h, "/api/decks/deck-1/chaos/reset")
	if state.Hang || state.Silent || state.DropEvents {
		t.Errorf("reset should clear all flags, got %+v", state)
	}
}

// Set hang before submit so the worker blocks at the first step boundary (avoids racing the step loop).
func TestChaos_HangViaAPI(t *testing.T) {
	t.Parallel()
	cfg := defaultOrchestratorConfig(t)
	cfg.Timeouts.AttemptDeadlineBase = 300 * time.Millisecond
	cfg.Timeouts.AmbiguousDeadline = 1 * time.Second

	spec := defaultExecutorSpec("deck-1")
	spec.Worker.StepDuration = 50 * time.Millisecond

	h := newHarness(t, harnessOptions{
		Orchestrator:  cfg,
		Executors:     []executorSpec{spec, defaultExecutorSpec("deck-2"), defaultExecutorSpec("deck-3")},
		AwaitDegraded: true,
	})

	patchChaos(t, h, "deck-1", apigen.ChaosPatch{Hang: boolPtr(true)})

	run := h.Client.SubmitRunFromFile(t, "linear")

	h.WaitForJobStatus(t, run.Id, "prep", apigen.DeckJobStatusAMBIGUOUS, 5*time.Second)
}

// TestChaos_SilentViaAPI flips the silent flag and verifies the deck
// transitions HEALTHY -> STALE -> back to HEALTHY when cleared.
func TestChaos_SilentViaAPI(t *testing.T) {
	t.Parallel()

	cfg := defaultOrchestratorConfig(t)
	cfg.Timeouts.HeartbeatInterval = 100 * time.Millisecond
	cfg.Timeouts.StaleThreshold = 300 * time.Millisecond

	h := newHarness(t, harnessOptions{
		Orchestrator:  cfg,
		Executors:     []executorSpec{defaultExecutorSpec("deck-1")},
		AwaitDegraded: true,
	})

	patchChaos(t, h, "deck-1", apigen.ChaosPatch{Silent: boolPtr(true)})
	h.WaitForDeckHealth(t, "deck-1", apigen.STALE, 2*time.Second)

	patchChaos(t, h, "deck-1", apigen.ChaosPatch{Silent: boolPtr(false)})
	h.WaitForDeckHealth(t, "deck-1", apigen.HEALTHY, 2*time.Second)
}

// TestChaos_DropEventsViaAPI confirms the outbox accumulates while
// drop_events is set and drains when cleared. The orchestrator stays
// blind to deck-1's progress while drop_events is on; when the flag
// flips, the outbox flushes and the whole run reaches COMPLETED.
func TestChaos_DropEventsViaAPI(t *testing.T) {
	t.Parallel()
	cfg := defaultOrchestratorConfig(t)
	// Prevent AMBIGUOUS escalation while drop_events suppresses RUNNING events.
	cfg.Timeouts.AmbiguousDeadline = 10 * time.Second
	cfg.Timeouts.AttemptDeadlineBase = 10 * time.Second

	h := newHarness(t, harnessOptions{
		Orchestrator:  cfg,
		Executors:     specsFor("deck-1", "deck-2", "deck-3"),
		AwaitDegraded: true,
	})

	patchChaos(t, h, "deck-1", apigen.ChaosPatch{DropEvents: boolPtr(true)})

	run := h.Client.SubmitRunFromFile(t, "linear")
	time.Sleep(500 * time.Millisecond)

	patchChaos(t, h, "deck-1", apigen.ChaosPatch{DropEvents: boolPtr(false)})
	h.WaitForRunStatus(t, run.Id, apigen.COMPLETED, 8*time.Second)
}

// TestChaos_PauseEgressViaAPI mirrors network_flake_test but driven
// via the HTTP surface.
func TestChaos_PauseEgressViaAPI(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1", "deck-2", "deck-3"),
		AwaitDegraded: true,
	})

	run := h.Client.SubmitRunFromFile(t, "linear")
	h.WaitForJobStatus(t, run.Id, "prep", apigen.DeckJobStatusDISPATCHED, 2*time.Second)

	patchChaos(t, h, "deck-1", apigen.ChaosPatch{PauseEgress: boolPtr(true)})
	time.Sleep(400 * time.Millisecond)
	patchChaos(t, h, "deck-1", apigen.ChaosPatch{PauseEgress: boolPtr(false)})

	h.WaitForRunStatus(t, run.Id, apigen.COMPLETED, 8*time.Second)
}

// TestChaos_CrashViaAPI verifies the crash route invokes onCrash. In
// the harness, onCrash is wired to a counter so we can observe it
// without terminating the test process.
func TestChaos_CrashViaAPI(t *testing.T) {
	t.Parallel()

	crashed := make(chan struct{}, 1)
	spec := defaultExecutorSpec("deck-1")
	spec.OnCrash = func() {
		select {
		case crashed <- struct{}{}:
		default:
		}
	}

	h := newHarness(t, harnessOptions{
		Executors:     []executorSpec{spec},
		AwaitDegraded: true,
	})

	resp, body := h.Client.do(t, http.MethodPost, "/api/decks/deck-1/chaos/crash", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("crash POST: %d body=%s", resp.StatusCode, body)
	}

	select {
	case <-crashed:
	case <-time.After(2 * time.Second):
		t.Fatalf("onCrash was not invoked within 2s")
	}
}

// TestChaos_UnknownDeckReturns404 sanity-checks the proxy's lookup
// path.
func TestChaos_UnknownDeckReturns404(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1"),
		AwaitDegraded: true,
	})

	resp, _ := h.Client.do(t, http.MethodGet, "/api/decks/deck-unknown/chaos", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown deck GET /chaos status=%d want 404", resp.StatusCode)
	}
}

// TestAdmin_RestartTriggersSignal confirms POST /api/admin/restart
// returns 202 and closes the orchestrator's RestartCh, which would
// drive the entrypoint to graceful shutdown. The harness doesn't
// shut down the orchestrator (we'd have to rebuild the test fixture)
// — we just observe the signal landed.
func TestAdmin_RestartTriggersSignal(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1"),
		AwaitDegraded: true,
	})

	resp, body := h.Client.do(t, http.MethodPost, "/api/admin/restart", nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("restart POST: status=%d body=%s", resp.StatusCode, body)
	}

	select {
	case <-h.Orch.RestartCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("RestartCh was not closed within 2s after POST")
	}
}

func getChaos(t testing.TB, h *Harness, deckID string) apigen.ChaosState {
	t.Helper()
	resp, body := h.Client.do(t, http.MethodGet, fmt.Sprintf("/api/decks/%s/chaos", deckID), nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /chaos %s: status %d body=%s", deckID, resp.StatusCode, body)
	}
	var state apigen.ChaosState
	if err := json.Unmarshal(body, &state); err != nil {
		t.Fatalf("decode chaos state: %v", err)
	}
	return state
}

func patchChaos(t testing.TB, h *Harness, deckID string, patch apigen.ChaosPatch) apigen.ChaosState {
	t.Helper()
	body := mustJSON(t, patch)
	resp, raw := h.Client.do(t, http.MethodPost, fmt.Sprintf("/api/decks/%s/chaos", deckID), body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /chaos %s: status %d body=%s", deckID, resp.StatusCode, raw)
	}
	var state apigen.ChaosState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode chaos state: %v", err)
	}
	return state
}

func postEmpty(t testing.TB, h *Harness, path string) apigen.ChaosState {
	t.Helper()
	resp, body := h.Client.do(t, http.MethodPost, path, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s: status %d body=%s", path, resp.StatusCode, body)
	}
	var state apigen.ChaosState
	if err := json.Unmarshal(body, &state); err != nil {
		t.Fatalf("decode chaos state: %v", err)
	}
	return state
}

// silence unused-import linter when only some tests use this import.
var _ = chaos.InitialState{}
