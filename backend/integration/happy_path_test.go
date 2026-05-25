package integration

import (
	"testing"
	"time"

	apigen "deck-fleet/backend/internal/api/gen"
)

type happyPathCase struct {
	sample  string
	runID   string
	decks   []string
	timeout time.Duration
	jobIDs  []string
	deckMap map[string]string // job_id -> deck_id (for slot-free check)
}

func TestHappyPath_AllSamples(t *testing.T) {
	t.Parallel()

	cases := []happyPathCase{
		{
			sample: "linear", runID: "linear-pipeline",
			decks:  []string{"deck-1", "deck-2", "deck-3"},
			jobIDs: []string{"prep", "incubate", "measure"},
		},
		{
			sample: "parallel-tracks", runID: "parallel-assays",
			decks:  []string{"deck-1", "deck-2"},
			jobIDs: []string{"track-a", "track-b"},
		},
		{
			sample: "fan-out", runID: "fanout-aliquot",
			decks:  []string{"deck-1", "deck-2", "deck-3", "deck-4"},
			jobIDs: []string{"source-prep", "assay-warm", "assay-ambient", "assay-cool"},
		},
		{
			sample: "fan-in", runID: "fanin-pool",
			decks:  []string{"deck-1", "deck-2", "deck-3", "deck-4"},
			jobIDs: []string{"extract-a", "extract-b", "extract-c", "pool-and-measure"},
		},
		{
			sample: "mixed", runID: "mixed-protocol",
			decks:  []string{"deck-1", "deck-2", "deck-3", "deck-4"},
			jobIDs: []string{"prep", "assay-warm", "assay-cool", "compare"},
		},
		{
			// same-deck.json uses deck-3 for two convergent jobs;
			// the per-deck slot invariant must serialize them.
			sample: "same-deck", runID: "same-deck-convergence",
			decks:  []string{"deck-1", "deck-2", "deck-3"},
			jobIDs: []string{"extract-a", "extract-b", "process-a", "process-b"},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.sample, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t, harnessOptions{
				Executors:     specsFor(c.decks...),
				AwaitDegraded: true,
			})

			submitted := h.Client.SubmitRunFromFile(t, c.sample)
			if submitted.Id != c.runID {
				t.Fatalf("submitted run id = %q want %q", submitted.Id, c.runID)
			}

			timeout := c.timeout
			if timeout == 0 {
				timeout = 8 * time.Second // headroom under parallel SQLite load
			}
			final := h.WaitForRunStatus(t, c.runID, apigen.COMPLETED, timeout)

			// Guards MaterializeRunStatus: COMPLETED run must not retain non-COMPLETED jobs.
			for _, jobID := range c.jobIDs {
				var found bool
				for _, j := range final.DeckJobs {
					if j.Id != jobID {
						continue
					}
					found = true
					if j.Status != apigen.DeckJobStatusCOMPLETED {
						t.Errorf("job %q final status = %s want COMPLETED", jobID, j.Status)
					}
					break
				}
				if !found {
					t.Errorf("job %q missing from final run", jobID)
				}
			}

			// Flakes here usually mean a slot was not released on JOB_COMPLETED.
			decks := h.Client.ListDecks(t)
			for _, d := range decks {
				if d.CurrentJob != nil {
					t.Errorf("deck %s still has current_job %+v after run completion", d.Id, *d.CurrentJob)
				}
			}
		})
	}
}
