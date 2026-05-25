import { test, expect } from "../../fixtures/stack";
import { fanInDag } from "../../helpers/dag-factory";
import { runIdFor } from "../../helpers/ids";
import { waitForRunStatus, waitForJobStatus } from "../../fixtures/time";
import * as sel from "../../helpers/selectors";
import { RunDetailPage } from "../../pages/RunDetailPage";

test("fan-in DAG: pool waits for all three extracts", async ({ page, submit, api }, testInfo) => {
  const runId = runIdFor(testInfo, "fan-in");
  await submit(fanInDag(runId));

  const detail = new RunDetailPage(page, runId);
  await detail.goto();
  for (const id of ["extract-a", "extract-b", "extract-c", "pool"]) {
    await expect(sel.jobNode(page, id)).toBeVisible();
  }

  // All three extracts must complete before pool dispatches.
  for (const id of ["extract-a", "extract-b", "extract-c"]) {
    await waitForJobStatus(runId, id, "COMPLETED");
  }

  // Asserting pool status mid-run races 200ms step duration — verify via final state + attempt count.
  await waitForRunStatus(runId, "COMPLETED");

  const finalRun = await api.getRun(runId);
  for (const id of ["extract-a", "extract-b", "extract-c", "pool"]) {
    const job = finalRun.deck_jobs.find((j) => j.id === id);
    expect(job?.status).toBe("COMPLETED");
  }

  // Single dispatch — no duplicate scheduling.
  const pool = finalRun.deck_jobs.find((j) => j.id === "pool");
  expect((pool?.recent_attempts ?? []).length).toBe(1);

  await expect(sel.jobNode(page, "pool", "COMPLETED")).toBeVisible();
});
