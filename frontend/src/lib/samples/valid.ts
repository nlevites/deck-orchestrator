/**
 * Six topologies from deck_fleet_orchestrator_assignment.md §"Sample DAGs".
 * Copied verbatim so the operator can demo every shape (linear, parallel
 * tracks, fan-out, fan-in, mixed, same-deck convergence) without typing.
 */

import type { SampleDag } from "./types";

const linear = {
  id: "linear-pipeline",
  deck_jobs: [
    {
      id: "prep",
      deck_id: "deck-1",
      depends_on: [],
      steps: [
        { type: "prepare", description: "Prep sample tray" },
        { type: "transfer", description: "Transfer reagent to plate" },
      ],
    },
    {
      id: "incubate",
      deck_id: "deck-2",
      depends_on: ["prep"],
      steps: [{ type: "incubate", description: "Incubate at 37°C for 30s" }],
    },
    {
      id: "measure",
      deck_id: "deck-3",
      depends_on: ["incubate"],
      steps: [{ type: "measure", description: "Read fluorescence" }],
    },
  ],
};

const parallelTracks = {
  id: "parallel-assays",
  deck_jobs: [
    {
      id: "track-a",
      deck_id: "deck-1",
      depends_on: [],
      steps: [
        { type: "transfer", description: "Aliquot sample A" },
        { type: "incubate", description: "Incubate 30s" },
        { type: "measure", description: "Read A absorbance" },
      ],
    },
    {
      id: "track-b",
      deck_id: "deck-2",
      depends_on: [],
      steps: [
        { type: "transfer", description: "Aliquot sample B" },
        { type: "incubate", description: "Incubate 30s" },
        { type: "measure", description: "Read B absorbance" },
      ],
    },
  ],
};

const fanOut = {
  id: "fanout-aliquot",
  deck_jobs: [
    {
      id: "source-prep",
      deck_id: "deck-1",
      depends_on: [],
      steps: [
        { type: "prepare", description: "Prep master mix" },
        { type: "aliquot", description: "Aliquot to three destination plates" },
      ],
    },
    {
      id: "assay-warm",
      deck_id: "deck-2",
      depends_on: ["source-prep"],
      steps: [
        { type: "incubate", description: "Incubate at 37°C" },
        { type: "measure", description: "Read condition 1" },
      ],
    },
    {
      id: "assay-ambient",
      deck_id: "deck-3",
      depends_on: ["source-prep"],
      steps: [
        { type: "incubate", description: "Incubate at 25°C" },
        { type: "measure", description: "Read condition 2" },
      ],
    },
    {
      id: "assay-cool",
      deck_id: "deck-4",
      depends_on: ["source-prep"],
      steps: [
        { type: "incubate", description: "Incubate at 4°C" },
        { type: "measure", description: "Read condition 3" },
      ],
    },
  ],
};

const fanIn = {
  id: "fanin-pool",
  deck_jobs: [
    {
      id: "extract-a",
      deck_id: "deck-1",
      depends_on: [],
      steps: [{ type: "extract", description: "Extract from source A" }],
    },
    {
      id: "extract-b",
      deck_id: "deck-2",
      depends_on: [],
      steps: [{ type: "extract", description: "Extract from source B" }],
    },
    {
      id: "extract-c",
      deck_id: "deck-3",
      depends_on: [],
      steps: [{ type: "extract", description: "Extract from source C" }],
    },
    {
      id: "pool-and-measure",
      deck_id: "deck-4",
      depends_on: ["extract-a", "extract-b", "extract-c"],
      steps: [
        { type: "pool", description: "Pool extracts into one plate" },
        { type: "measure", description: "Read pooled signal" },
      ],
    },
  ],
};

const mixed = {
  id: "mixed-protocol",
  deck_jobs: [
    {
      id: "prep",
      deck_id: "deck-1",
      depends_on: [],
      steps: [
        { type: "prepare", description: "Prep master mix" },
        { type: "aliquot", description: "Aliquot to two assay plates" },
      ],
    },
    {
      id: "assay-warm",
      deck_id: "deck-2",
      depends_on: ["prep"],
      steps: [
        { type: "incubate", description: "Warm incubation" },
        { type: "measure", description: "Warm OD600 read" },
      ],
    },
    {
      id: "assay-cool",
      deck_id: "deck-3",
      depends_on: ["prep"],
      steps: [
        { type: "incubate", description: "Cool incubation" },
        { type: "measure", description: "Cool OD600 read" },
      ],
    },
    {
      id: "compare",
      deck_id: "deck-4",
      depends_on: ["assay-warm", "assay-cool"],
      steps: [{ type: "analyze", description: "Compare warm vs cool readings" }],
    },
  ],
};

const sameDeck = {
  id: "same-deck-convergence",
  deck_jobs: [
    {
      id: "extract-a",
      deck_id: "deck-1",
      depends_on: [],
      steps: [{ type: "extract", description: "Extract from source A" }],
    },
    {
      id: "extract-b",
      deck_id: "deck-2",
      depends_on: [],
      steps: [{ type: "extract", description: "Extract from source B" }],
    },
    {
      id: "process-a",
      deck_id: "deck-3",
      depends_on: ["extract-a"],
      steps: [
        { type: "process", description: "Process A on shared instrument" },
        { type: "measure", description: "Read A result" },
      ],
    },
    {
      id: "process-b",
      deck_id: "deck-3",
      depends_on: ["extract-b"],
      steps: [
        { type: "process", description: "Process B on shared instrument" },
        { type: "measure", description: "Read B result" },
      ],
    },
  ],
};

export const validSamples: SampleDag[] = [
  {
    id: "linear",
    label: "Linear pipeline",
    description: "Prep → incubate → measure across three decks.",
    topology: "linear · 3 jobs",
    category: "topology",
    json: linear,
  },
  {
    id: "parallel",
    label: "Parallel tracks",
    description: "Two independent assays running side by side.",
    topology: "parallel · 2 jobs",
    category: "topology",
    json: parallelTracks,
  },
  {
    id: "fan-out",
    label: "Fan-out aliquot",
    description: "One prep step branching into three temperature conditions.",
    topology: "fan-out · 4 jobs",
    category: "topology",
    json: fanOut,
  },
  {
    id: "fan-in",
    label: "Fan-in pool",
    description: "Three extracts pooled into a single measurement.",
    topology: "fan-in · 4 jobs",
    category: "topology",
    json: fanIn,
  },
  {
    id: "mixed",
    label: "Mixed protocol",
    description: "Prep → 2 parallel assays → compare. Both fan-out and fan-in.",
    topology: "mixed · 4 jobs",
    category: "topology",
    json: mixed,
  },
  {
    id: "same-deck",
    label: "Same-deck convergence",
    description: "Two extracts converge onto a shared deck for processing.",
    topology: "same-deck · 4 jobs",
    category: "topology",
    json: sameDeck,
  },
];
