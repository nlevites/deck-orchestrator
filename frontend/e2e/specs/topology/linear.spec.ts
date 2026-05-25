import { test, expect } from "../../fixtures/stack";
import { linearDag } from "../../helpers/dag-factory";
import { runIdFor } from "../../helpers/ids";
import { waitForRunStatus } from "../../fixtures/time";
import * as sel from "../../helpers/selectors";
import { RunDetailPage } from "../../pages/RunDetailPage";

test("linear DAG: j1 → j2 → j3 completes in order", async ({ page, submit }, testInfo) => {
  const runId = runIdFor(testInfo, "linear");
  await submit(linearDag(runId));

  const detail = new RunDetailPage(page, runId);
  await detail.goto();

  await expect(sel.jobNode(page, "j1")).toBeVisible();
  await expect(sel.jobNode(page, "j2")).toBeVisible();
  await expect(sel.jobNode(page, "j3")).toBeVisible();

  await waitForRunStatus(runId, "COMPLETED");

  for (const id of ["j1", "j2", "j3"]) {
    await expect(sel.jobNode(page, id, "COMPLETED")).toBeVisible();
  }
});
