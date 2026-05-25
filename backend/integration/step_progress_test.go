package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "deck-fleet/backend/internal/api/gen"
)

// Integration-level guarantee: step completions are discrete auditable facts via outbox + event log.
func TestStepProgress_ThreeStepJobEmitsStepEvents(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1"),
		AwaitDegraded: true,
	})

	body, err := json.Marshal(map[string]any{
		"id": "step-progress-test",
		"deck_jobs": []map[string]any{
			{
				"id":         "work",
				"deck_id":    "deck-1",
				"depends_on": []string{},
				"steps": []map[string]any{
					{"type": "prepare", "description": "Prep plate"},
					{"type": "incubate", "description": "Incubate 30s"},
					{"type": "measure", "description": "Read OD600"},
				},
			},
		},
	})
	require.NoError(t, err)

	resp, rawBody := h.Client.SubmitRunJSON(t, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("submit: status %d body=%s", resp.StatusCode, rawBody)
	}

	h.WaitForRunStatus(t, "step-progress-test", apigen.COMPLETED, 15*time.Second)

	events := h.ListEvents(t)

	var stepSeqs []int64
	var stepNums []float64
	for _, e := range events {
		if e.Kind == "JOB_STEP_COMPLETED" && e.JobID == "work" {
			var p map[string]any
			require.NoError(t, json.Unmarshal([]byte(e.Payload), &p),
				"step event payload must be valid JSON")
			n, _ := p["step"].(float64)
			tot, _ := p["total"].(float64)
			assert.Equal(t, float64(3), tot, "step event: wrong total")
			assert.NotEmpty(t, e.RunID, "step event: missing run_id")
			assert.NotEmpty(t, e.AttemptID, "step event: missing attempt_id")
			stepNums = append(stepNums, n)
			stepSeqs = append(stepSeqs, e.Seq)
		}
	}

	require.Equal(t, 3, len(stepNums), "expected exactly 3 JOB_STEP_COMPLETED events")
	assert.Equal(t, []float64{1, 2, 3}, stepNums, "step numbers must be 1, 2, 3")

	for i := 1; i < len(stepSeqs); i++ {
		assert.Greater(t, stepSeqs[i], stepSeqs[i-1], "step event seqs not monotonic")
	}

	var completedCount int
	for _, e := range events {
		if e.Kind == "JOB_COMPLETED" && e.JobID == "work" {
			completedCount++
		}
	}
	require.Equal(t, 1, completedCount, "expected exactly 1 JOB_COMPLETED event")

	detail := h.Client.GetRun(t, "step-progress-test")
	for _, j := range detail.DeckJobs {
		if j.Id == "work" {
			require.NotNil(t, j.LastCompletedStep, "last_completed_step must be set")
			assert.Equal(t, int64(3), *j.LastCompletedStep,
				"last_completed_step should equal total_steps after completion")
			require.NotNil(t, j.TotalSteps)
			assert.Equal(t, int64(3), *j.TotalSteps)
			return
		}
	}
	t.Fatal("deck_job 'work' not found in run detail")
}
