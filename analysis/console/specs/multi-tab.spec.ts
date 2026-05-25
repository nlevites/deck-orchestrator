/**
 * Multi-tab: open three contexts in parallel (fleet + 2 run-detail tabs)
 * and measure the cost multiplier on the orchestrator. Reduces to the
 * C2/C8 inefficiency checks downstream.
 */
import { test, takeHeapSnapshot, sampleMetrics, expect } from "./_fixture";
import { STACK } from "../playwright.config";
import { request } from "@playwright/test";

test.use({ scenarioName: "multi-tab" });

test("multi-tab — fleet + 2 run-detail tabs, 3 min observation", async ({
  contextWithCapture,
  scenarioDir,
}) => {
  test.setTimeout(5 * 60_000);

  // Submit a couple DAGs so the run-detail tabs have real runs to bind to.
  const api = await request.newContext();
  const runIds: string[] = [];
  for (const id of ["multi-a", "multi-b"]) {
    const r = await api.post(`${STACK.orchestratorUrl}/api/runs`, {
      data: {
        id: `${id}-${Date.now()}`,
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
    runIds.push((await r.json()).id);
  }

  const fleetPage = await contextWithCapture.newPage();
  await fleetPage.goto("/fleet");

  const runPageA = await contextWithCapture.newPage();
  await runPageA.goto(`/runs/${runIds[0]}`);

  const runPageB = await contextWithCapture.newPage();
  await runPageB.goto(`/runs/${runIds[1]}`);

  await takeHeapSnapshot(fleetPage, scenarioDir, "fleet-start");
  await takeHeapSnapshot(runPageA, scenarioDir, "runA-start");

  // Sample fleet tab; the other two also poll concurrently — we count
  // their cost via the orchestrator NDJSON cross-reference downstream.
  await sampleMetrics(fleetPage, scenarioDir, 3 * 60_000, 1000);

  await takeHeapSnapshot(fleetPage, scenarioDir, "fleet-end");
  await takeHeapSnapshot(runPageA, scenarioDir, "runA-end");
});
