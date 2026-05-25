import { test, expect } from "../../fixtures/stack";
import { linearDag } from "../../helpers/dag-factory";
import { runIdFor } from "../../helpers/ids";
import { waitForJobStatus } from "../../fixtures/time";

// Two operators: cancel in tab A surfaces in tab B within ~1 poll tick.
test("multi-tab: cancel in tab A surfaces in tab B without manual refresh", async ({
  browser,
  submit,
  api,
}, testInfo) => {
  const runId = runIdFor(testInfo, "multitab");

  await api.patchChaos("deck-2", { hang: true });
  await submit(linearDag(runId));

  await waitForJobStatus(runId, "j2", "RUNNING", { timeout: 8_000 });

  // Isolated contexts — simulates two machines.
  const contextA = await browser.newContext();
  const contextB = await browser.newContext();
  const tabA = await contextA.newPage();
  const tabB = await contextB.newPage();

  try {
    await tabA.goto(`http://localhost:15173/runs/${encodeURIComponent(runId)}`);
    await tabB.goto(`http://localhost:15173/runs/${encodeURIComponent(runId)}`);

    await Promise.all([tabA.waitForTimeout(1_200), tabB.waitForTimeout(1_200)]);

    await expect(tabB.getByText("Running").first()).toBeVisible();

    // API cancel — modal version-race has its own spec.
    const fresh = await api.getRun(runId);
    await api.cancelRun(runId, fresh.version);

    await expect(tabB.getByText("Cancelled").first()).toBeVisible({ timeout: 5_000 });
  } finally {
    await contextA.close().catch(() => {});
    await contextB.close().catch(() => {});
  }
});
