import { test, expect } from "../../fixtures/stack";
import * as sel from "../../helpers/selectors";

// ?connection= override pins each banner without depending on live signal timings.
test.describe("ConnectionBanner: distinct states render correctly", () => {
  for (const state of [
    { qs: "offline", label: "OFFLINE", title: "Offline" },
    { qs: "live", label: "LIVE_PAUSED", title: "Live updates paused" },
    { qs: "degraded", label: "DEGRADED_MODE", title: "Orchestrator reconciling" },
  ] as const) {
    test(`?connection=${state.qs} shows ${state.label}`, async ({ page }) => {
      await page.goto(`/fleet?connection=${state.qs}`);
      await expect(sel.connectionBanner(page, state.label)).toBeVisible();
      await expect(page.getByText(state.title).first()).toBeVisible();
    });
  }

  test("default (no override): banner is hidden", async ({ page }) => {
    await page.goto("/fleet");
    await expect(page.getByRole("link", { name: "Deck Fleet home" })).toBeVisible();
    await expect(page.locator(`[aria-label^="Connection "]`)).toHaveCount(0);
  });
});

// Window sentinel survives orchestrator restart — no full-page reload.
test("orchestrator restart does not full-page-reload the SPA", async ({ page, api }) => {
  await page.goto("/fleet");
  await page.evaluate(() => {
    (window as unknown as { __dfoE2eSentinel: number }).__dfoE2eSentinel = Date.now();
  });
  const before = await page.evaluate(
    () => (window as unknown as { __dfoE2eSentinel: number }).__dfoE2eSentinel,
  );

  await api.restartOrchestrator();
  await api.waitForOrchestratorHealth(20_000);
  await api.waitForDecksHealthy(undefined, 15_000);
  await page.waitForTimeout(2_000);

  const after = await page.evaluate(
    () => (window as unknown as { __dfoE2eSentinel: number | undefined }).__dfoE2eSentinel,
  );
  expect(after).toBe(before);
});
