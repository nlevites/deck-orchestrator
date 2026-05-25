import { test, expect } from "../../fixtures/stack";
import { parallelDag } from "../../helpers/dag-factory";
import { runIdFor } from "../../helpers/ids";
import { waitForRunStatus } from "../../fixtures/time";
import * as sel from "../../helpers/selectors";
import { RunDetailPage } from "../../pages/RunDetailPage";

test("parallel DAG: two independent tracks run concurrently", async ({
  page,
  submit,
  api,
}, testInfo) => {
  const runId = runIdFor(testInfo, "parallel");
  await submit(parallelDag(runId));

  const detail = new RunDetailPage(page, runId);
  await detail.goto();
  await expect(sel.jobNode(page, "track-a")).toBeVisible();
  await expect(sel.jobNode(page, "track-b")).toBeVisible();

  // Both tracks must advance concurrently — serial dispatch would leave one PENDING/READY.
  await expect
    .poll(
      async () => {
        const run = await api.getRun(runId);
        const a = run.deck_jobs.find((j) => j.id === "track-a")?.status;
        const b = run.deck_jobs.find((j) => j.id === "track-b")?.status;
        const advanced = new Set(["DISPATCHED", "RUNNING", "COMPLETED"]);
        return advanced.has(a ?? "") && advanced.has(b ?? "");
      },
      { timeout: 8_000, intervals: [100, 200] },
    )
    .toBeTruthy();

  await waitForRunStatus(runId, "COMPLETED");
  await expect(sel.jobNode(page, "track-a", "COMPLETED")).toBeVisible();
  await expect(sel.jobNode(page, "track-b", "COMPLETED")).toBeVisible();
});
