import { test, expect } from "../../fixtures/stack";
import { runIdFor } from "../../helpers/ids";
import { waitForJobStatus, waitForRunStatus } from "../../fixtures/time";
import * as sel from "../../helpers/selectors";
import { RunDetailPage } from "../../pages/RunDetailPage";

// AMBIGUOUS → COMPLETED via Resolve modal; downstream j2 dispatches (STATE_MACHINE §8.2).
test("resolve AMBIGUOUS to COMPLETED via UI: downstream proceeds, run completes", async ({
  page,
  submit,
  api,
}, testInfo) => {
  const runId = runIdFor(testInfo, "resolve-completed");

  await api.patchChaos("deck-1", { hang: true });
  await submit({
    id: runId,
    deck_jobs: [
      {
        id: "j1",
        deck_id: "deck-1",
        depends_on: [],
        steps: [{ type: "work", description: "will hang" }],
      },
      {
        id: "j2",
        deck_id: "deck-2",
        depends_on: ["j1"],
        steps: [{ type: "work", description: "depends on j1" }],
      },
    ],
  });

  await waitForJobStatus(runId, "j1", "AMBIGUOUS", { timeout: 12_000 });

  await api.crashDeck("deck-1").catch(() => {});

  const detail = new RunDetailPage(page, runId);
  await detail.goto();
  await page.waitForTimeout(1_500);

  await detail.resolveAmbiguousAs("COMPLETED");

  await waitForRunStatus(runId, "COMPLETED", { timeout: 15_000 });

  const run = await api.getRun(runId);
  const j1 = run.deck_jobs.find((j) => j.id === "j1")!;
  const j2 = run.deck_jobs.find((j) => j.id === "j2")!;
  expect(j1.status).toBe("COMPLETED");
  expect(j2.status).toBe("COMPLETED");
  expect((j1.recent_attempts ?? [])[0]?.outcome_source).toBe("OPERATOR_RESOLUTION");

  await page.goto(`/runs/${encodeURIComponent(runId)}`);
  await expect(sel.jobNode(page, "j1", "COMPLETED")).toBeVisible();
  await expect(sel.jobNode(page, "j2", "COMPLETED")).toBeVisible();
});
