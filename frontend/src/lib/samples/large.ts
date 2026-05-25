/**
 * Stress-test DAGs generated inline so the file stays compact.
 *
 * `wide30` runs on the demo fleet (deck-1..deck-4) and demonstrates
 * per-deck queuing as 30 fan-out jobs serialize against 4 decks.
 *
 * `fleet100` needs a 100-executor stack to run end-to-end. On the
 * regular demo it sits in READY for deck-5..deck-100 since those slots
 * aren't attached.
 */

import type { SampleDag } from "./types";

function buildWideFanOut(opts: { id: string; jobCount: number; deckCount: number }): object {
  return {
    id: opts.id,
    deck_jobs: [
      {
        id: "prep",
        deck_id: "deck-1",
        depends_on: [],
        steps: [{ type: "prepare", description: "Prep master mix" }],
      },
      ...Array.from({ length: opts.jobCount }, (_, i) => {
        const idx = i + 1;
        const deck = (i % opts.deckCount) + 1;
        return {
          id: `assay-${String(idx).padStart(3, "0")}`,
          deck_id: `deck-${deck}`,
          depends_on: ["prep"],
          steps: [
            { type: "incubate", description: `Incubate condition ${idx}` },
            { type: "measure", description: `Read condition ${idx}` },
          ],
        };
      }),
    ],
  };
}

const wide30 = buildWideFanOut({ id: "stress-wide-30", jobCount: 30, deckCount: 4 });
const fleet100 = buildWideFanOut({ id: "stress-fleet-100", jobCount: 99, deckCount: 100 });

export const largeSamples: SampleDag[] = [
  {
    id: "wide-30",
    label: "Wide fan-out (30 jobs)",
    description:
      "1 prep → 30 fan-out jobs round-robin across 4 decks. Demonstrates per-deck queuing.",
    topology: "stress · 31 jobs",
    category: "stress",
    json: wide30,
  },
  {
    id: "fleet-100",
    label: "Fleet stress (100 jobs)",
    description: "1 prep → 99 fan-out jobs across deck-1..deck-100. Requires a 100-executor stack.",
    topology: "stress · 100 jobs",
    category: "stress",
    json: fleet100,
  },
];
