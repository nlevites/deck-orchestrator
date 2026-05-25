import { test, expect } from "../../fixtures/stack";
import { fanOutDag } from "../../helpers/dag-factory";
import { runIdFor } from "../../helpers/ids";
import { waitForRunStatus, waitForJobStatus } from "../../fixtures/time";
import * as sel from "../../helpers/selectors";
import { RunDetailPage } from "../../pages/RunDetailPage";

test("fan-out DAG: source completes before three branches dispatch", async ({
  page,
  submit,
  api,
}, testInfo) => {
  const runId = runIdFor(testInfo, "fan-out");
  await submit(fanOutDag(runId));

  const detail = new RunDetailPage(page, runId);
  await detail.goto();
  for (const id of ["source", "branch-warm", "branch-ambient", "branch-cool"]) {
    await expect(sel.jobNode(page, id)).toBeVisible();
  }

  await waitForJobStatus(runId, "source", "COMPLETED");

  const run = await api.getRun(runId);
  for (const id of ["branch-warm", "branch-ambient", "branch-cool"]) {
    const job = run.deck_jobs.find((j) => j.id === id);
    expect(job, `branch ${id}`).toBeTruthy();
  }

  await waitForRunStatus(runId, "COMPLETED");
  for (const id of ["source", "branch-warm", "branch-ambient", "branch-cool"]) {
    await expect(sel.jobNode(page, id, "COMPLETED")).toBeVisible();
  }
});
