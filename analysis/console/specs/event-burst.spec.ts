/**
 * Event burst: submit 6 DAGs in a 5-second window. Each lands many
 * events per tick — measures reducer apply cost + React render storm.
 * Feeds the C4 inefficiency check (render storm on event burst).
 */
import { test, takeHeapSnapshot, sampleMetrics, expect } from "./_fixture";
import { STACK } from "../playwright.config";
import { request } from "@playwright/test";

test.use({ scenarioName: "event-burst" });

const burstDags = [
  "linear-pipeline",
  "parallel-assays",
  "fanout-aliquot",
  "fanin-pool",
  "mixed-protocol",
  "same-deck-convergence",
] as const;

function makeDag(idBase: string) {
  return {
    id: `${idBase}-${Date.now()}`,
    deck_jobs: Array.from({ length: 4 }, (_, i) => ({
      id: `j${i + 1}`,
      deck_id: `deck-${(i % 4) + 1}`,
      depends_on: [],
      steps: [{ type: "transfer", description: `s${i}` }],
    })),
  };
}

test("event-burst — submit 6 DAGs in 5s, observe reducer + render storm", async ({
  instrumentedPage,
  scenarioDir,
}) => {
  test.setTimeout(2 * 60_000);

  await instrumentedPage.goto("/fleet");
  await instrumentedPage.waitForSelector("body");
  await takeHeapSnapshot(instrumentedPage, scenarioDir, "pre-burst");

  // Submit all six in parallel via the operator API.
  const api = await request.newContext();
  const submits = burstDags.map(async (base) => {
    const r = await api.post(`${STACK.orchestratorUrl}/api/runs`, { data: makeDag(base) });
    expect([200, 201]).toContain(r.status());
  });
  await Promise.all(submits);

  // 90s of sampling — covers DAG completion at 2s/step on the 4-deck stack
  // plus the trailing terminal-status events.
  await sampleMetrics(instrumentedPage, scenarioDir, 90_000, 500);

  await takeHeapSnapshot(instrumentedPage, scenarioDir, "post-burst");
});
