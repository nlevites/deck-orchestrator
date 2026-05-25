/**
 * One job with many steps. Exercises the live step-progress UI without
 * needing a long protocol — each step finishes in the executor's tick.
 */

import type { SampleDag } from "./types";

// Sized to fit the demo's per-step deadline budget (base 20s + per-step 2s):
// 12 steps × 2s = 24s actual runtime, 44s ceiling, 20s margin.
const STEP_COUNT = 12;

const longProtocol = {
  id: "stress-long-protocol",
  deck_jobs: [
    {
      id: "long",
      deck_id: "deck-1",
      depends_on: [],
      steps: Array.from({ length: STEP_COUNT }, (_, i) => ({
        type: i === 0 ? "prepare" : i === STEP_COUNT - 1 ? "measure" : "transfer",
        description: `Step ${i + 1} of ${STEP_COUNT}`,
      })),
    },
  ],
};

export const manyStepsSamples: SampleDag[] = [
  {
    id: "long-protocol",
    label: `Long protocol (${STEP_COUNT} steps)`,
    description: `One job on deck-1 with ${STEP_COUNT} steps. Demonstrates live step-progress.`,
    topology: `stress · 1 job · ${STEP_COUNT} steps`,
    category: "stress",
    json: longProtocol,
  },
];
