import { test, expect } from "../../fixtures/stack";
import { linearDag } from "../../helpers/dag-factory";
import { runIdFor } from "../../helpers/ids";
import { waitForRunStatus, waitForJobStatus } from "../../fixtures/time";

// Failure mode #2: pause_egress → outbox backs up → restore drains; no duplicate dispatch.
test("network flake (paused egress): deck recovers and run completes", async ({
  submit,
  api,
}, testInfo) => {
  const runId = runIdFor(testInfo, "flake");

  // Clear before attempt_deadline (4s) — else j2 escalates to AMBIGUOUS.
  await api.patchChaos("deck-2", { pause_egress: true });
  await submit(linearDag(runId));

  await waitForJobStatus(runId, "j1", "COMPLETED", { timeout: 6_000 });

  await api.patchChaos("deck-2", { pause_egress: false });

  await waitForRunStatus(runId, "COMPLETED", { timeout: 15_000 });

  const run = await api.getRun(runId);
  for (const job of run.deck_jobs) {
    expect((job.recent_attempts ?? []).length, `job ${job.id} attempts`).toBe(1);
  }
});
