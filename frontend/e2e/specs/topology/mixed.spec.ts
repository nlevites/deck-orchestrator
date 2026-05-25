import { test, expect } from "../../fixtures/stack";
import { mixedDag } from "../../helpers/dag-factory";
import { runIdFor } from "../../helpers/ids";
import { waitForRunStatus, waitForJobStatus } from "../../fixtures/time";
import * as sel from "../../helpers/selectors";
import { RunDetailPage } from "../../pages/RunDetailPage";

test("mixed DAG: fan-out then fan-in completes end-to-end", async ({ page, submit }, testInfo) => {
  const runId = runIdFor(testInfo, "mixed");
  await submit(mixedDag(runId));

  const detail = new RunDetailPage(page, runId);
  await detail.goto();
  for (const id of ["prep", "warm", "cool", "compare"]) {
    await expect(sel.jobNode(page, id)).toBeVisible();
  }

  await waitForJobStatus(runId, "prep", "COMPLETED");
  await waitForJobStatus(runId, "warm", "COMPLETED");
  await waitForJobStatus(runId, "cool", "COMPLETED");
  await waitForRunStatus(runId, "COMPLETED");
  await expect(sel.jobNode(page, "compare", "COMPLETED")).toBeVisible();
});
