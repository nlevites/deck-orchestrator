import { test, expect } from "../../fixtures/stack";
import { runIdFor } from "../../helpers/ids";
import { waitForJobStatus, waitForRunStatus } from "../../fixtures/time";
import type { DagSubmission } from "../../helpers/api";

/**
 * Network drop mid-execution: pause egress on deck-2 *while* j2 is RUNNING,
 * then heal the link after the orchestrator has already escalated to
 * AMBIGUOUS. The executor's outbox flushes its queued JOB_STEP_COMPLETED /
 * JOB_COMPLETED events on un-pause; the orchestrator must classify them as
 * EXECUTOR_CONFLICT_LOGGED (different outcome already recorded) and *not*
 * silently flip j2 back to COMPLETED. AMBIGUOUS resolution is the operator's.
 *
 * E2E config (config/e2e.yaml): step_duration=200ms, stale_threshold=1s,
 * ambiguous_deadline=3s, attempt_deadline_base=4s. A 30-step j2 (~6s of work)
 * gives chaos a generous window to land before the executor completes.
 */
test("late JOB_COMPLETED from outbox must not override AMBIGUOUS", async ({
  submit,
  api,
}, testInfo) => {
  const runId = runIdFor(testInfo, "drop-mid");

  const longSteps = Array.from({ length: 30 }, (_, i) => ({
    type: "work",
    description: `step-${i + 1}`,
  }));
  const dag: DagSubmission = {
    id: runId,
    deck_jobs: [
      {
        id: "j1",
        deck_id: "deck-1",
        depends_on: [],
        steps: [{ type: "work", description: "fast root" }],
      },
      {
        id: "j2",
        deck_id: "deck-2",
        depends_on: ["j1"],
        steps: longSteps,
      },
    ],
  };

  await submit(dag);

  // Catch j2 mid-flight, not after-the-fact. waitForJobStatus polls every
  // ~200ms; the gap between RUNNING and pausing must be << the 6s job, but
  // need not be sub-second.
  await waitForJobStatus(runId, "j2", "RUNNING", { timeout: 6_000 });
  await api.patchChaos("deck-2", { pause_egress: true });

  // Heartbeats stop -> deck goes STALE (1s) -> UNREACHABLE / ambiguous_deadline
  // (3s) -> in-flight attempt marked AMBIGUOUS. Generous timeout for CI jitter.
  await waitForRunStatus(runId, "AMBIGUOUS", { timeout: 12_000 });
  const ambiguous = await api.getRun(runId);
  const j2Ambiguous = ambiguous.deck_jobs.find((j) => j.id === "j2");
  expect(j2Ambiguous?.status, "j2 is AMBIGUOUS while chaos active").toBe("AMBIGUOUS");

  // Heal the link. The executor's outbox now has queued events for j2's
  // steps; these will flush within the next ~1s.
  await api.patchChaos("deck-2", { pause_egress: false });

  // Give the outbox flusher time to drain and the orchestrator time to
  // classify the late events. The conflict path is synchronous on the
  // orchestrator (insert EXECUTOR_CONFLICT_LOGGED, return 200), so 3s is
  // plenty of slack.
  await new Promise((r) => setTimeout(r, 3_000));

  // The load-bearing assertion: AMBIGUOUS must be sticky. A late
  // JOB_COMPLETED from the outbox is rejected by the (attempt_id, kind)
  // dedupe; the orchestrator must not silently flip j2 back to COMPLETED.
  const after = await api.getRun(runId);
  const j2After = after.deck_jobs.find((j) => j.id === "j2");
  expect(j2After?.status, "j2 stays AMBIGUOUS after outbox drains").toBe("AMBIGUOUS");
  expect(after.status, "run stays AMBIGUOUS until operator resolves").toBe("AMBIGUOUS");

  // The event log should carry a conflict marker for j2's attempt, which is
  // the wire proof that the late events arrived and were rejected (not
  // silently dropped by the chaos transport).
  const state = await api.getState(0);
  const conflict = state.events.find(
    (e) => e.kind === "EXECUTOR_CONFLICT_LOGGED" && e.run_id === runId && e.job_id === "j2",
  );
  expect(conflict, "EXECUTOR_CONFLICT_LOGGED event recorded for j2").toBeTruthy();
});
