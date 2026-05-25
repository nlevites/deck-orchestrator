import { test, expect } from "../../fixtures/stack";
import { runIdFor } from "../../helpers/ids";
import * as sel from "../../helpers/selectors";

// useOperatorGate regression: buttons were clickable during OFFLINE/LIVE_PAUSED/DEGRADED_MODE.
// ?connection= URL override pins each state without killing the orchestrator.
// deck-5 (no executor) → PENDING run with Cancel visible, no chaos teardown.
test("operator-gate: run-detail Cancel button disabled in each non-OK state", async ({
  page,
  submit,
}, testInfo) => {
  const runId = runIdFor(testInfo, "gate");

  // deck-5: no executor → PENDING, Cancel always rendered.
  await submit({
    id: runId,
    deck_jobs: [
      {
        id: "j1",
        deck_id: "deck-5",
        depends_on: [],
        steps: [{ type: "work", description: "gate test — will never dispatch" }],
      },
    ],
  });

  const runPath = `/runs/${encodeURIComponent(runId)}`;

  await page.goto(`${runPath}?connection=offline`);
  await expect(sel.connectionBanner(page, "OFFLINE")).toBeVisible();
  const cancelOffline = page.getByRole("button", { name: "Cancel run" });
  await expect(cancelOffline).toBeDisabled({ timeout: 5_000 });
  const titleOffline = await cancelOffline.getAttribute("title");
  expect(titleOffline).toMatch(/offline/i);

  await page.goto(`${runPath}?connection=degraded`);
  await expect(sel.connectionBanner(page, "DEGRADED_MODE")).toBeVisible();
  const cancelDegraded = page.getByRole("button", { name: "Cancel run" });
  await expect(cancelDegraded).toBeDisabled({ timeout: 5_000 });
  const titleDegraded = await cancelDegraded.getAttribute("title");
  expect(titleDegraded).toMatch(/reconcil/i);

  await page.goto(`${runPath}?connection=live`);
  await expect(sel.connectionBanner(page, "LIVE_PAUSED")).toBeVisible();
  const cancelPaused = page.getByRole("button", { name: "Cancel run" });
  await expect(cancelPaused).toBeDisabled({ timeout: 5_000 });
  const titlePaused = await cancelPaused.getAttribute("title");
  expect(titlePaused).toMatch(/stale|paused/i);

  await page.goto(runPath);
  await expect(sel.connectionBanner(page)).toHaveCount(0);
  const cancelOk = page.getByRole("button", { name: "Cancel run" });
  await expect(cancelOk).toBeEnabled({ timeout: 5_000 });
  const titleOk = await cancelOk.getAttribute("title");
  expect(titleOk).toBeFalsy();
});

test("operator-gate: Submit button disabled with tooltip when OFFLINE", async ({ page }) => {
  // Submit renders even with invalid DAG — gate must disable + tooltip before valid paste.
  await page.goto("/submit?connection=offline");
  await expect(sel.connectionBanner(page, "OFFLINE")).toBeVisible();

  const submitBtn = page.getByRole("button", { name: /Submit run/i });
  await expect(submitBtn).toBeDisabled({ timeout: 5_000 });

  const title = await submitBtn.getAttribute("title");
  expect(title).toMatch(/offline/i);
});
