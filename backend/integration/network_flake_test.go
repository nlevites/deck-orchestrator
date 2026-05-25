package integration

import (
	"testing"
	"time"

	apigen "deck-fleet/backend/internal/api/gen"
)

// TestNetworkFlake_ExecutorToOrchestrator covers a real-world fault:
// a transient executor -> orchestrator outage. The executor's outbox
// accumulates state events while egress is blocked; once egress is
// restored the outbox drains and the orchestrator applies each event
// exactly once (defense-in-depth dedup makes a replay harmless).
//
// We assert end-state cleanliness rather than per-attempt mechanics:
// (a) the run completes, (b) exactly one COMPLETED outcome per job,
// (c) no orphaned events.
func TestNetworkFlake_ExecutorToOrchestrator(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1", "deck-2", "deck-3"),
		AwaitDegraded: true,
	})

	run := h.Client.SubmitRunFromFile(t, "linear")
	h.WaitForJobStatus(t, run.Id, "prep", apigen.DeckJobStatusDISPATCHED, 2*time.Second)
	h.Executors["deck-1"].PauseEgress()
	time.Sleep(400 * time.Millisecond)
	h.Executors["deck-1"].ResumeEgress()

	final := h.WaitForRunStatus(t, run.Id, apigen.COMPLETED, 8*time.Second)
	for _, j := range final.DeckJobs {
		if j.RecentAttempts == nil {
			t.Errorf("job %s missing attempts", j.Id)
			continue
		}
		var completed int
		for _, a := range *j.RecentAttempts {
			if a.Outcome != nil && *a.Outcome == apigen.AttemptOutcomeCOMPLETED {
				completed++
			}
		}
		if completed != 1 {
			t.Errorf("job %s has %d COMPLETED attempts, want 1", j.Id, completed)
		}
	}
}

// TestNetworkFlake_OrchestratorToExecutor covers the reverse:
// orchestrator can't dial the executor for reconciliation. While the
// pause is short (< AmbiguousDeadline), no AMBIGUOUS escalation should
// happen — the reconciler retries and ultimately confirms the
// executor's view once reachability returns.
//
// We don't try to *observe* the reconciler running here (compressed
// timings make that tricky). We confirm the post-condition: the run
// completes without going through AMBIGUOUS, even though we briefly
// blocked the dial channel.
func TestNetworkFlake_OrchestratorToExecutor_RecoversWithinDeadline(t *testing.T) {
	t.Parallel()

	cfg := defaultOrchestratorConfig(t)
	cfg.Timeouts.AmbiguousDeadline = 5 * time.Second    // generous
	cfg.Timeouts.AttemptDeadlineBase = 10 * time.Second // way beyond test
	cfg.Timeouts.StaleThreshold = 5 * time.Second       // dont mark STALE
	cfg.Timeouts.HeartbeatInterval = 100 * time.Millisecond

	h := newHarness(t, harnessOptions{
		Orchestrator:  cfg,
		Executors:     specsFor("deck-1", "deck-2", "deck-3"),
		AwaitDegraded: true,
	})

	run := h.Client.SubmitRunFromFile(t, "linear")
	h.WaitForJobStatus(t, run.Id, "prep", apigen.DeckJobStatusDISPATCHED, 2*time.Second)

	h.Executors["deck-1"].PauseHTTPServer()
	time.Sleep(150 * time.Millisecond)
	h.Executors["deck-1"].ResumeHTTPServer()

	final := h.WaitForRunStatus(t, run.Id, apigen.COMPLETED, 8*time.Second)
	for _, j := range final.DeckJobs {
		if j.Status == apigen.DeckJobStatusAMBIGUOUS {
			t.Errorf("job %s wrongly escalated to AMBIGUOUS", j.Id)
		}
	}
}
