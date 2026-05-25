import { test, expect } from "../../fixtures/stack";
import { linearDag } from "../../helpers/dag-factory";
import { runIdFor } from "../../helpers/ids";
import { waitForRunStatus, waitForJobStatus } from "../../fixtures/time";
import * as sel from "../../helpers/selectors";
import { RunDetailPage } from "../../pages/RunDetailPage";

// Failure mode #1: orchestrator restart mid-run — startup reconcile, no duplicate dispatch.
test("orchestrator restart mid-run: run completes, no duplicate dispatch", async ({
  page,
  submit,
  api,
}, testInfo) => {
  const runId = runIdFor(testInfo, "restart");
  await submit(linearDag(runId));

  // j1 COMPLETED exercises startup-reconcile on a partially progressed run.
  await waitForJobStatus(runId, "j1", "COMPLETED", { timeout: 10_000 });

  const detail = new RunDetailPage(page, runId);
  await detail.goto();

  await api.restartOrchestrator();
  await api.waitForOrchestratorHealth(20_000);
  await api.waitForDecksHealthy(undefined, 15_000);

  await waitForRunStatus(runId, "COMPLETED", { timeout: 20_000 });

  // One attempt per job — re-dispatch during reconcile would show ≥2.
  const run = await api.getRun(runId);
  for (const job of run.deck_jobs) {
    expect((job.recent_attempts ?? []).length, `job ${job.id} attempts`).toBe(1);
  }

  await expect(sel.connectionBanner(page)).toHaveCount(0);
});
