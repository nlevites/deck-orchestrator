package integration

import (
	"testing"
	"time"

	apigen "deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/executor/chaos"
)

// TestLiveness_StaleAndRecovery covers STATE_MACHINE.md §5 deck health
// state machine: a deck that stops heartbeating transitions HEALTHY ->
// STALE; resuming heartbeats moves it back to HEALTHY and emits the
// transition events on the audit log.
//
// We use the chaos.State Silent flag to suppress heartbeats at runtime
// (post-registration). Flipping the flag back to false re-enables
// them; the next heartbeat tick lands and the orchestrator's heartbeat
// handler emits DECK_HEALTH_CHANGED back to HEALTHY.
//
// This used to require restarting the executor with a different
// worker.Config — now we just flip the runtime flag.
func TestLiveness_StaleAndRecovery(t *testing.T) {
	t.Parallel()

	cfg := defaultOrchestratorConfig(t)
	cfg.Timeouts.HeartbeatInterval = 100 * time.Millisecond
	cfg.Timeouts.StaleThreshold = 300 * time.Millisecond

	h := newHarness(t, harnessOptions{
		Orchestrator:  cfg,
		Executors:     []executorSpec{defaultExecutorSpec("deck-1")},
		AwaitDegraded: true,
	})

	h.Executors["deck-1"].Chaos.Apply(chaos.InitialState{Silent: boolPtr(true)})

	h.WaitForDeckHealth(t, "deck-1", apigen.STALE, 2*time.Second)

	h.Executors["deck-1"].Chaos.Apply(chaos.InitialState{Silent: boolPtr(false)})
	h.WaitForDeckHealth(t, "deck-1", apigen.HEALTHY, 2*time.Second)

	var staleSeen, healthyAgain bool
	for _, e := range h.ListEvents(t) {
		if e.Kind != "DECK_HEALTH_CHANGED" {
			continue
		}
		if jsonContainsAll(t, []byte(e.Payload), map[string]string{"from": "HEALTHY", "to": "STALE"}) {
			staleSeen = true
		}
		if jsonContainsAll(t, []byte(e.Payload), map[string]string{"to": "HEALTHY"}) {
			healthyAgain = true
		}
	}
	if !staleSeen {
		t.Errorf("no DECK_HEALTH_CHANGED HEALTHY->STALE event recorded")
	}
	if !healthyAgain {
		t.Errorf("no DECK_HEALTH_CHANGED *->HEALTHY event recorded")
	}
}

func jsonContainsAll(t testing.TB, payload []byte, want map[string]string) bool {
	t.Helper()
	for k, v := range want {
		if !jsonContains(t, payload, k, v) {
			return false
		}
	}
	return true
}
