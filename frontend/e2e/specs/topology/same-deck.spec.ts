import { test, expect } from "../../fixtures/stack";
import { sameDeckDag } from "../../helpers/dag-factory";
import { runIdFor } from "../../helpers/ids";
import { waitForRunStatus } from "../../fixtures/time";

test("same-deck DAG: two jobs on deck-3 serialize", async ({ page, submit, api }, testInfo) => {
  const runId = runIdFor(testInfo, "samedeck");
  await submit(sameDeckDag(runId));

  await page.goto(`/runs/${encodeURIComponent(runId)}`);
  await expect(page.getByRole("heading", { name: runId })).toBeVisible();

  // Poll API — if both process-a and process-b are active, deck-3 didn't serialize.
  let bothActive = false;
  const stop = Date.now() + 12_000;
  while (Date.now() < stop) {
    const run = await api.getRun(runId);
    const pa = run.deck_jobs.find((j) => j.id === "process-a")?.status;
    const pb = run.deck_jobs.find((j) => j.id === "process-b")?.status;
    const active = new Set(["DISPATCHED", "RUNNING"]);
    if (active.has(pa ?? "") && active.has(pb ?? "")) {
      bothActive = true;
      break;
    }
    if (run.status === "COMPLETED") break;
    await new Promise((r) => setTimeout(r, 100));
  }
  expect(
    bothActive,
    "process-a and process-b co-ran on deck-3 (orchestrator did not serialize)",
  ).toBe(false);

  await waitForRunStatus(runId, "COMPLETED");
});
