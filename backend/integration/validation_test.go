package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	apigen "deck-fleet/backend/internal/api/gen"
)

// TestValidation exercises every semantic-validation branch in
// internal/dag.Validate. Each case submits a malformed DAG and asserts
// the orchestrator returns 422 DAG_VALIDATION_FAILED with the right
// inner code(s). All cases share one harness — no executor traffic is
// involved, so a single orchestrator suffices.
//
// Decks referenced by these DAGs must be HEALTHY for the "unknown_deck"
// path to be the *only* failure, so we spin up a single deck-1 + deck-2
// pair. UNKNOWN_DECK targets are deliberately unregistered.
func TestValidation(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1", "deck-2"),
		AwaitDegraded: true,
	})

	type submission struct {
		ID       string `json:"id"`
		DeckJobs []any  `json:"deck_jobs"`
	}

	cases := []struct {
		name      string
		body      []byte
		wantInner string
	}{
		{
			name:      "no_jobs",
			body:      mustJSON(t, submission{ID: "empty-dag", DeckJobs: []any{}}),
			wantInner: "DAG_HAS_NO_JOBS",
		},
		{
			name: "duplicate_job_id",
			body: mustJSON(t, map[string]any{
				"id": "dup-job",
				"deck_jobs": []map[string]any{
					{"id": "j1", "deck_id": "deck-1", "depends_on": []string{}, "steps": []map[string]string{{"type": "t", "description": "d"}}},
					{"id": "j1", "deck_id": "deck-2", "depends_on": []string{}, "steps": []map[string]string{{"type": "t", "description": "d"}}},
				},
			}),
			wantInner: "DUPLICATE_JOB_ID",
		},
		{
			name: "job_has_no_steps",
			body: mustJSON(t, map[string]any{
				"id": "no-steps",
				"deck_jobs": []map[string]any{
					{"id": "j1", "deck_id": "deck-1", "depends_on": []string{}, "steps": []any{}},
				},
			}),
			wantInner: "JOB_HAS_NO_STEPS",
		},
		{
			name: "unknown_deck",
			body: mustJSON(t, map[string]any{
				"id": "unknown-deck",
				"deck_jobs": []map[string]any{
					{"id": "j1", "deck_id": "deck-9999", "depends_on": []string{}, "steps": []map[string]string{{"type": "t", "description": "d"}}},
				},
			}),
			wantInner: "UNKNOWN_DECK",
		},
		{
			name: "unknown_dependency",
			body: mustJSON(t, map[string]any{
				"id": "unknown-dep",
				"deck_jobs": []map[string]any{
					{"id": "j1", "deck_id": "deck-1", "depends_on": []string{"ghost"}, "steps": []map[string]string{{"type": "t", "description": "d"}}},
				},
			}),
			wantInner: "UNKNOWN_DEPENDENCY",
		},
		{
			name: "dag_has_cycle",
			body: mustJSON(t, map[string]any{
				"id": "cycle-dag",
				"deck_jobs": []map[string]any{
					{"id": "j1", "deck_id": "deck-1", "depends_on": []string{"j2"}, "steps": []map[string]string{{"type": "t", "description": "d"}}},
					{"id": "j2", "deck_id": "deck-2", "depends_on": []string{"j1"}, "steps": []map[string]string{{"type": "t", "description": "d"}}},
				},
			}),
			wantInner: "DAG_HAS_CYCLE",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			resp, raw := h.Client.SubmitRunJSON(t, c.body)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d want 422; body=%s", resp.StatusCode, raw)
			}
			env := decodeError(t, raw)
			if env.Error.Code != apigen.ErrorCodeDAGVALIDATIONFAILED {
				t.Fatalf("code = %s want DAG_VALIDATION_FAILED; body=%s", env.Error.Code, raw)
			}
			if !strings.Contains(string(raw), c.wantInner) {
				t.Fatalf("expected inner code %q in response; body=%s", c.wantInner, raw)
			}
			if env.Error.RequestID == "" {
				t.Errorf("missing request_id on error response: %s", raw)
			}
		})
	}
}

// TestValidation_InvalidJSON covers the wire-layer error paths the
// decoder produces — they're not strictly DAG validation but they
// share the same surface and we want one assertion that the envelope
// shape is well-formed for every 4xx the operator UI can encounter.
func TestValidation_InvalidJSON(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{AwaitDegraded: true})

	cases := []struct {
		name     string
		body     []byte
		wantCode apigen.ErrorCode
	}{
		{
			name:     "malformed_json",
			body:     []byte(`{not json`),
			wantCode: apigen.ErrorCodeINVALIDJSON,
		},
		{
			name:     "unknown_field",
			body:     []byte(`{"id":"x","deck_jobs":[],"extra":1}`),
			wantCode: apigen.ErrorCodeSCHEMAVIOLATION,
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			resp, raw := h.Client.SubmitRunJSON(t, c.body)
			env := decodeError(t, raw)
			if env.Error.Code != c.wantCode {
				t.Errorf("code = %s want %s; status=%d body=%s", env.Error.Code, c.wantCode, resp.StatusCode, raw)
			}
		})
	}
}

var _ = json.RawMessage(nil)
