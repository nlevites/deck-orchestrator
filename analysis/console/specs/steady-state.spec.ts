/**
 * Baseline: single tab on /fleet, 5 min idle. Tells us what an operator's
 * tab costs sitting there with no DAG activity — pure poll loop.
 */
import { test, takeHeapSnapshot, sampleMetrics } from "./_fixture";

test.use({ scenarioName: "steady-state" });

test("steady-state — single tab idle on /fleet for 5 min", async ({
  instrumentedPage,
  scenarioDir,
}) => {
  test.setTimeout(6 * 60_000);

  await instrumentedPage.goto("/fleet");
  await instrumentedPage.waitForSelector("body");
  await takeHeapSnapshot(instrumentedPage, scenarioDir, "start");

  // 5 minutes of idle polling — long enough to see the 5-min rebootstrap
  // safety net at least once (REBOOTSTRAP_INTERVAL_MS).
  await sampleMetrics(instrumentedPage, scenarioDir, 5 * 60_000, 1000);

  await takeHeapSnapshot(instrumentedPage, scenarioDir, "end");
});
