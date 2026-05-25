package integration

import (
	"sort"
	"testing"
	"time"

	apigen "deck-fleet/backend/internal/api/gen"
)

// TestEventLog_OrderingAndScope is the broad audit-trail invariant
// test: across a happy-path run, every emitted event has a monotonic
// `seq`, scope fields populated per the API.md §10 table, and the
// expected per-job event sequence (READY -> DISPATCHED -> RUNNING ->
// COMPLETED).
func TestEventLog_OrderingAndScope(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1", "deck-2", "deck-3"),
		AwaitDegraded: true,
	})

	run := h.Client.SubmitRunFromFile(t, "linear")
	h.WaitForRunStatus(t, run.Id, apigen.COMPLETED, 8*time.Second)

	events := h.ListEvents(t)
	if len(events) == 0 {
		t.Fatalf("no events recorded")
	}

	for i := 1; i < len(events); i++ {
		if events[i].Seq <= events[i-1].Seq {
			t.Fatalf("events.seq not monotonic at i=%d: %d <= %d", i, events[i].Seq, events[i-1].Seq)
		}
	}

	// API.md §10 scope contracts for operator-UI event kinds.
	for _, e := range events {
		switch e.Kind {
		case "RUN_SUBMITTED", "RUN_STATUS_CHANGED":
			if e.RunID == "" {
				t.Errorf("%s missing run_id", e.Kind)
			}
		case "JOB_READY":
			if e.RunID == "" || e.JobID == "" || e.DeckID == "" {
				t.Errorf("JOB_READY missing run/job/deck: %+v", e)
			}
		case "JOB_DISPATCHED", "JOB_RUNNING", "JOB_COMPLETED", "JOB_FAILED", "JOB_AMBIGUOUS":
			if e.RunID == "" || e.JobID == "" || e.DeckID == "" || e.AttemptID == "" {
				t.Errorf("%s missing scope field: %+v", e.Kind, e)
			}
		case "DECK_REGISTERED", "DECK_HEALTH_CHANGED":
			if e.DeckID == "" {
				t.Errorf("%s missing deck_id", e.Kind)
			}
		}
	}

	for _, jobID := range []string{"prep", "incubate", "measure"} {
		kinds := jobEventKinds(events, run.Id, jobID)
		if !subsequenceMatches(kinds, []string{"JOB_READY", "JOB_DISPATCHED", "JOB_RUNNING", "JOB_COMPLETED"}) {
			t.Errorf("job %s event order = %v; expected subsequence READY,DISPATCHED,RUNNING,COMPLETED", jobID, kinds)
		}
	}
}

func jobEventKinds(events []EventRow, runID, jobID string) []string {
	sort.SliceStable(events, func(i, j int) bool { return events[i].Seq < events[j].Seq })
	var out []string
	for _, e := range events {
		if e.RunID == runID && e.JobID == jobID {
			out = append(out, e.Kind)
		}
	}
	return out
}

// subsequenceMatches reports whether want appears in order within got (non-contiguous allowed).
func subsequenceMatches(got, want []string) bool {
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	return i == len(want)
}

// Bootstrap ships full `decks`; delta ships `decks_delta` (touched-since
// slice) so steady-state cost is bounded by activity, not fleet size.
// Backs S3 in analysis/inefficiencies/inefficiencies.md.
func TestState_BootstrapAndDelta(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1", "deck-2", "deck-3"),
		AwaitDegraded: true,
	})

	first := h.Client.GetState(t, 0)
	if first.Runs == nil {
		t.Fatalf("bootstrap: runs slice should be present (even if empty)")
	}
	if first.Decks == nil {
		t.Fatalf("bootstrap: decks slice should be present")
	}
	if len(*first.Decks) < 3 {
		t.Fatalf("bootstrap: expected >=3 decks, got %d", len(*first.Decks))
	}
	if first.ServerSeq <= 0 {
		t.Fatalf("bootstrap: server_seq=%d; expected >0 after deck registration", first.ServerSeq)
	}
	for i := 1; i < len(first.Events); i++ {
		if first.Events[i].Seq <= first.Events[i-1].Seq {
			t.Fatalf("bootstrap: events not monotonic at i=%d: %d <= %d",
				i, first.Events[i].Seq, first.Events[i-1].Seq)
		}
	}
	bootstrapHeartbeats := make(map[string]time.Time, len(*first.Decks))
	for _, d := range *first.Decks {
		if d.LastHeartbeatAt != nil {
			bootstrapHeartbeats[d.Id] = *d.LastHeartbeatAt
		}
	}

	run := h.Client.SubmitRunFromFile(t, "linear")
	h.WaitForRunStatus(t, run.Id, apigen.COMPLETED, 8*time.Second)

	delta := h.Client.GetState(t, first.ServerSeq)
	if delta.Runs != nil {
		t.Errorf("delta: runs should be omitted, got %d", len(*delta.Runs))
	}
	if delta.Decks != nil {
		t.Errorf("delta: full decks slice should be omitted (decks_delta carries touched-since rows instead), got %d", len(*delta.Decks))
	}
	if delta.DecksDelta == nil {
		t.Fatalf("delta: decks_delta slice should be present (S3)")
	}
	// At least one deck must show up in the delta — the just-run linear
	// DAG touched deck-1..3 via JOB_DISPATCHED + JOB_COMPLETED events,
	// and recent heartbeats on those decks land in the freshness window.
	if len(*delta.DecksDelta) == 0 {
		t.Fatalf("delta: decks_delta empty; expected at least the decks involved in the just-completed run")
	}
	advanced := 0
	for _, d := range *delta.DecksDelta {
		if d.LastHeartbeatAt == nil {
			continue
		}
		if prev, ok := bootstrapHeartbeats[d.Id]; ok && d.LastHeartbeatAt.After(prev) {
			advanced++
		}
	}
	if advanced == 0 {
		t.Errorf("delta: expected at least one deck in decks_delta to have advanced last_heartbeat_at past bootstrap")
	}
	if delta.ServerSeq <= first.ServerSeq {
		t.Fatalf("delta: server_seq=%d should be > bootstrap %d", delta.ServerSeq, first.ServerSeq)
	}
	for _, e := range delta.Events {
		if e.Seq <= first.ServerSeq {
			t.Errorf("delta: event seq=%d should be > %d", e.Seq, first.ServerSeq)
		}
	}
	foundSubmitted := false
	for _, e := range delta.Events {
		if e.Kind == "RUN_SUBMITTED" && e.RunId != nil && *e.RunId == run.Id {
			foundSubmitted = true
			break
		}
	}
	if !foundSubmitted {
		t.Errorf("delta: expected a RUN_SUBMITTED event for %s, kinds=%v", run.Id, kindsOf(delta.Events))
	}

	tail := h.Client.GetState(t, delta.ServerSeq)
	if len(tail.Events) != 0 {
		for _, e := range tail.Events {
			if e.Seq <= delta.ServerSeq {
				t.Errorf("tail: event seq=%d should be > %d", e.Seq, delta.ServerSeq)
			}
		}
	}
}

// TestState_RunScopedBootstrapAndDelta covers the run-detail variant:
// bootstrap returns the run + its scoped events; delta returns only
// new events for that run; unknown run_id is 404.
func TestState_RunScopedBootstrapAndDelta(t *testing.T) {
	t.Parallel()
	h := newHarness(t, harnessOptions{
		Executors:     specsFor("deck-1", "deck-2", "deck-3"),
		AwaitDegraded: true,
	})

	run := h.Client.SubmitRunFromFile(t, "linear")
	h.WaitForRunStatus(t, run.Id, apigen.COMPLETED, 8*time.Second)

	resp, snap, body := h.Client.GetRunState(t, run.Id, 0)
	if resp.StatusCode != 200 {
		t.Fatalf("getRunState bootstrap: status=%d body=%s", resp.StatusCode, body)
	}
	if snap.Run == nil {
		t.Fatalf("bootstrap: run should be populated")
	}
	if snap.Run.Id != run.Id {
		t.Fatalf("bootstrap: got run %s; want %s", snap.Run.Id, run.Id)
	}
	if len(snap.Events) == 0 {
		t.Fatalf("bootstrap: events should not be empty for a completed run")
	}
	for _, e := range snap.Events {
		if e.RunId == nil || *e.RunId != run.Id {
			t.Errorf("bootstrap: event %d has run_id=%v; want %s", e.Seq, e.RunId, run.Id)
		}
	}

	delta := h.Client.GetState(t, snap.ServerSeq)
	_ = delta // global snapshot's server_seq is the same watermark
	resp2, snap2, _ := h.Client.GetRunState(t, run.Id, snap.ServerSeq)
	if resp2.StatusCode != 200 {
		t.Fatalf("getRunState delta: status=%d", resp2.StatusCode)
	}
	if snap2.Run != nil {
		t.Errorf("delta: run should be omitted")
	}
	for _, e := range snap2.Events {
		if e.RunId == nil || *e.RunId != run.Id {
			t.Errorf("delta: event %d has run_id=%v; want %s", e.Seq, e.RunId, run.Id)
		}
	}

	bogus, _, body2 := h.Client.GetRunState(t, "no-such-run", 0)
	if bogus.StatusCode != 404 {
		t.Fatalf("getRunState unknown: status=%d body=%s; want 404", bogus.StatusCode, body2)
	}
}

func kindsOf(events []apigen.Event) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, string(e.Kind))
	}
	return out
}

// TestEventLog_ExecutorConflictLogged covers API.md §10.14: when the
// executor reports an outcome for an attempt the orchestrator already
// finalized differently, the conflict is logged (not applied) and an
// EXECUTOR_CONFLICT_LOGGED event lands.
//
// We construct this by cancelling a run mid-flight (orchestrator marks
// CANCELLED), then letting the still-hung executor finish locally and
// deliver its COMPLETED to a job we've already moved to CANCELLED.
func TestEventLog_ExecutorConflictLogged(t *testing.T) {
	t.Parallel()

	// Long step so we can cancel before completion.
	slow := defaultExecutorSpec("deck-1")
	slow.Worker.StepDuration = 500 * time.Millisecond

	cfg := defaultOrchestratorConfig(t)
	cfg.Timeouts.AttemptDeadlineBase = 30 * time.Second
	cfg.Timeouts.AmbiguousDeadline = 30 * time.Second

	h := newHarness(t, harnessOptions{
		Orchestrator:  cfg,
		Executors:     []executorSpec{slow},
		AwaitDegraded: true,
	})

	body := mustJSON(t, map[string]any{
		"id": "conflict-run",
		"deck_jobs": []map[string]any{
			{"id": "j1", "deck_id": "deck-1", "depends_on": []string{}, "steps": []map[string]string{{"type": "incubate", "description": "slow"}}},
		},
	})
	if resp, raw := h.Client.SubmitRunJSON(t, body); resp.StatusCode != 201 {
		t.Fatalf("submit: %d body=%s", resp.StatusCode, raw)
	}

	pre := h.WaitForJobStatus(t, "conflict-run", "j1", apigen.DeckJobStatusRUNNING, 2*time.Second)
	current := h.Client.GetRun(t, "conflict-run")
	if resp, raw := h.Client.CancelRun(t, "conflict-run", current.Version); resp.StatusCode != 200 {
		t.Fatalf("cancel: %d body=%s", resp.StatusCode, raw)
	}
	h.WaitForRunStatus(t, "conflict-run", apigen.CANCELLED, 2*time.Second)

	// Now wait for the executor to finish locally (500ms total step
	// duration). It posts COMPLETED to the orchestrator for an
	// attempt whose job is CANCELLED — the handler must log the
	// conflict.
	h.WaitForEvent(t,
		func(e EventRow) bool {
			return e.Kind == "EXECUTOR_CONFLICT_LOGGED" &&
				e.RunID == "conflict-run" &&
				e.AttemptID == pre.CurrentAttemptId.String()
		},
		5*time.Second,
	)
}
