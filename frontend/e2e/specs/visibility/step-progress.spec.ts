import { test, expect } from "../../fixtures/stack";
import { runIdFor } from "../../helpers/ids";
import { RunDetailPage } from "../../pages/RunDetailPage";
import * as sel from "../../helpers/selectors";

/**
 * Pre-fix: UI showed only steps[0] during RUNNING. chaos.hang_after_step
 * freezes mid-step — 200ms step duration races the 1Hz poll otherwise.
 */
test("multi-step job: DAG node and JobRow show step X/Y while RUNNING", async ({
  page,
  api,
  submit,
}, testInfo) => {
  const runId = runIdFor(testInfo, "step-progress");

  await api.patchChaos("deck-1", { hang_after_step: 2 });

  await submit({
    id: runId,
    deck_jobs: [
      {
        id: "work",
        deck_id: "deck-1",
        depends_on: [],
        steps: [
          { type: "prepare", description: "Prep plate" },
          { type: "transfer", description: "Transfer reagent" },
          { type: "incubate", description: "Incubate" },
          { type: "measure", description: "Read OD600" },
        ],
      },
    ],
  });

  const detail = new RunDetailPage(page, runId);
  await detail.goto();

  await expect
    .poll(
      async () => {
        const run = await api.getRun(runId);
        const job = run.deck_jobs.find((j) => j.id === "work");
        return job?.last_completed_step ?? 0;
      },
      { timeout: 10_000, intervals: [200, 400] },
    )
    .toBe(2);

  await expect(page.locator("text=/deck-1 · step 2\\/4/").first()).toBeVisible({
    timeout: 5_000,
  });

  await expect(page.locator("text=/^step 2\\/4$/").first()).toBeVisible({ timeout: 4_000 });

  // hang_after_step promotes to chaos.hang — clear both to release via broadcast.
  await api.patchChaos("deck-1", { hang_after_step: 0, hang: false });

  await expect
    .poll(
      async () => {
        const run = await api.getRun(runId);
        return run.status;
      },
      { timeout: 10_000, intervals: [200, 500] },
    )
    .toBe("COMPLETED");

  await expect(sel.jobNode(page, "work", "COMPLETED")).toBeVisible();

  const finalRun = await api.getRun(runId);
  const work = finalRun.deck_jobs.find((j) => j.id === "work");
  expect(work?.last_completed_step).toBe(4);
  expect(work?.total_steps).toBe(4);
});
