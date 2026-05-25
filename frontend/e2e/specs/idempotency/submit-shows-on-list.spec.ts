import { test, expect } from "../../fixtures/stack";
import { linearDag } from "../../helpers/dag-factory";
import { runIdFor } from "../../helpers/ids";
import { runRowAny } from "../../helpers/selectors";

// Regression: RUN_SUBMITTED used to be a no-op reducer → list stale ~60s until rebootstrap.
test("submit run shows up on /runs within seconds", async ({ page, submit }, testInfo) => {
  const runId = runIdFor(testInfo, "submit-on-list");

  // Mount useLiveState before submit — mirrors operator flow (Runs tab open first).
  await page.goto("/runs");
  // level-1 only — empty-state h3 "No runs yet" also matches /Runs/i (strict-mode).
  await expect(page.getByRole("heading", { level: 1, name: "Runs" })).toBeVisible();

  await submit(linearDag(runId));

  // Row must appear via live cache — no reload, no re-navigation.
  await expect(runRowAny(page, runId)).toBeVisible({ timeout: 5_000 });
});
