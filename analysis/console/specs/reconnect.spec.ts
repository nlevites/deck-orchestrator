/**
 * Reconnect: take the page offline for 30s, then back online; observe
 * how long until the cache reflects events that landed during the blackout.
 *
 * Feeds the C6 inefficiency check (reconnect recovery cost).
 */
import { test, takeHeapSnapshot, sampleMetrics, expect } from "./_fixture";
import { STACK } from "../playwright.config";
import { request } from "@playwright/test";

test.use({ scenarioName: "reconnect" });

test("reconnect — offline 30s, then online; measure cache recovery", async ({
  contextWithCapture,
  scenarioDir,
}) => {
  test.setTimeout(3 * 60_000);

  const page = await contextWithCapture.newPage();
  await page.goto("/fleet");
  await takeHeapSnapshot(page, scenarioDir, "start");

  // Sample 10s of steady-state pre-blackout.
  await sampleMetrics(page, scenarioDir, 10_000, 500);

  // Drive a couple server-side mutations during the blackout so the
  // cache must reconcile something non-trivial on reconnect.
  await contextWithCapture.setOffline(true);
  const api = await request.newContext();
  for (let i = 0; i < 3; i++) {
    const r = await api.post(`${STACK.orchestratorUrl}/api/runs`, {
      data: {
        id: `reconnect-${i}-${Date.now()}`,
        deck_jobs: [
          {
            id: "j1",
            deck_id: "deck-1",
            depends_on: [],
            steps: [{ type: "prepare", description: "p" }],
          },
        ],
      },
    });
    expect(r.ok()).toBe(true);
  }
  await page.waitForTimeout(30_000);
  await contextWithCapture.setOffline(false);

  // Sample 30s post-reconnect — long enough for at least one bootstrap
  // poll to land + cache to refresh.
  await sampleMetrics(page, scenarioDir, 30_000, 500);

  await takeHeapSnapshot(page, scenarioDir, "end");
});
