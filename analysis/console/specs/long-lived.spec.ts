/**
 * 30 min long-lived session against the fleet view. Tests:
 *   - REBOOTSTRAP_INTERVAL_MS (5min) safety net fires multiple times
 *   - events cache growth on a tab that never closes
 *   - React heap drift across the session
 *
 * Run with DFO_ANALYSIS_DECKS=100 if a scale-stack writeup is wanted.
 */
import { test, takeHeapSnapshot, sampleMetrics } from "./_fixture";

test.use({ scenarioName: "long-lived" });

test("long-lived — single tab on /fleet for 30 min", async ({
  instrumentedPage,
  scenarioDir,
}) => {
  test.setTimeout(35 * 60_000);

  await instrumentedPage.goto("/fleet");
  await instrumentedPage.waitForSelector("body");
  await takeHeapSnapshot(instrumentedPage, scenarioDir, "start");

  // First 15 min, then mid snapshot to bracket heap growth.
  await sampleMetrics(instrumentedPage, scenarioDir, 15 * 60_000, 1000);
  await takeHeapSnapshot(instrumentedPage, scenarioDir, "mid");
  await sampleMetrics(instrumentedPage, scenarioDir, 15 * 60_000, 1000);
  await takeHeapSnapshot(instrumentedPage, scenarioDir, "end");
});
