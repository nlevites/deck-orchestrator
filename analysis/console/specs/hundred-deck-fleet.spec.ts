/**
 * 100-deck fleet snapshot. Run with DFO_ANALYSIS_DECKS=100 so the
 * supervisor launches 100 executors; the spec assumes they're already
 * registered and just records what the console looks like at design scale.
 *
 * Reference: frontend/e2e/specs/scale/hundred-decks.spec.ts (the workflow
 * shape — but without the assertions).
 */
import { test, takeHeapSnapshot, sampleMetrics, expect } from "./_fixture";
import { STACK } from "../playwright.config";
import { request } from "@playwright/test";

test.use({ scenarioName: "hundred-deck-fleet" });

test("100 decks — fleet view + grid view, 2 min snapshot", async ({
  instrumentedPage,
  scenarioDir,
}) => {
  test.setTimeout(5 * 60_000);

  if (STACK.decks < 100) {
    test.skip(true, `requires DFO_ANALYSIS_DECKS=100 (got ${STACK.decks})`);
  }

  // Wait for fleet registration — at 100 decks the registration POSTs
  // serialize through the single write conn; up to 60s slack.
  const api = await request.newContext();
  await expect
    .poll(
      async () => {
        const r = await api.get(`${STACK.orchestratorUrl}/api/decks`);
        const body = await r.json();
        return body.decks?.length ?? 0;
      },
      { timeout: 60_000, intervals: [500, 1000] },
    )
    .toBe(100);

  await instrumentedPage.goto("/fleet");
  await takeHeapSnapshot(instrumentedPage, scenarioDir, "fleet-start");
  await sampleMetrics(instrumentedPage, scenarioDir, 60_000, 1000);

  // Then the grid view — virtualized at 100 cards, exercises the
  // TanStack Virtual code path under live polling.
  await instrumentedPage.goto("/fleet/grid");
  await sampleMetrics(instrumentedPage, scenarioDir, 60_000, 1000);
  await takeHeapSnapshot(instrumentedPage, scenarioDir, "grid-end");
});
