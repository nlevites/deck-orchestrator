package integration

import (
	"net/http"
	"testing"

	apigen "deck-fleet/backend/internal/api/gen"
)

// Re-arms DEGRADED_MODE manually; natural startup window is too small to observe reliably.
func TestDegraded_BlocksMutationsAllowsReads(t *testing.T) {
	t.Parallel()

	h := newHarness(t, harnessOptions{
		AwaitDegraded: true,
	})
	h.Orch.Degraded.Store(true)
	defer h.Orch.Degraded.Store(false)

	body := mustJSON(t, map[string]any{
		"id":        "degraded-test",
		"deck_jobs": []map[string]any{},
	})
	resp, raw := h.Client.SubmitRunJSON(t, body)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("POST /api/runs during DEGRADED_MODE: status %d want 503; body=%s", resp.StatusCode, raw)
	}
	env := decodeError(t, raw)
	if env.Error.Code != apigen.ErrorCodeDEGRADEDMODE {
		t.Errorf("code = %s want DEGRADED_MODE; body=%s", env.Error.Code, raw)
	}

	// Reads stay open during DEGRADED_MODE (ARCHITECTURE.md §4.1).
	if listResp, _ := h.Client.do(t, http.MethodGet, "/api/runs", nil); listResp.StatusCode != http.StatusOK {
		t.Errorf("GET /api/runs during DEGRADED_MODE: status %d want 200", listResp.StatusCode)
	}
	if listResp, _ := h.Client.do(t, http.MethodGet, "/api/decks", nil); listResp.StatusCode != http.StatusOK {
		t.Errorf("GET /api/decks during DEGRADED_MODE: status %d want 200", listResp.StatusCode)
	}

	// Executor endpoints stay open so reconciliation can finish.
	if pollResp, _ := h.Client.do(t, http.MethodGet, "/executor/poll?deck_id=deck-x", nil); pollResp.StatusCode != http.StatusNoContent {
		t.Errorf("GET /executor/poll during DEGRADED_MODE: status %d want 204", pollResp.StatusCode)
	}
}
