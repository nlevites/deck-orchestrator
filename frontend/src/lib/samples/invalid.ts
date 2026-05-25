/**
 * Validation fixtures — one per code in backend/internal/dag/validate.go.
 * Each isolates a single violation so the inline validator on the Submit
 * page surfaces exactly that error code.
 */

import type { SampleDag } from "./types";

const noJobs = {
  id: "invalid-no-jobs",
  deck_jobs: [],
};

const duplicateJobId = {
  id: "invalid-duplicate-job-id",
  deck_jobs: [
    {
      id: "j1",
      deck_id: "deck-1",
      depends_on: [],
      steps: [{ type: "prepare", description: "First j1" }],
    },
    {
      id: "j1",
      deck_id: "deck-2",
      depends_on: [],
      steps: [{ type: "prepare", description: "Second j1 (collides)" }],
    },
  ],
};

const jobNoSteps = {
  id: "invalid-job-no-steps",
  deck_jobs: [
    {
      id: "empty-steps",
      deck_id: "deck-1",
      depends_on: [],
      steps: [],
    },
  ],
};

const unknownDependency = {
  id: "invalid-unknown-dependency",
  deck_jobs: [
    {
      id: "later",
      deck_id: "deck-1",
      depends_on: ["does-not-exist"],
      steps: [{ type: "measure", description: "Reads after a phantom job" }],
    },
  ],
};

const cycle = {
  id: "invalid-cycle",
  deck_jobs: [
    {
      id: "a",
      deck_id: "deck-1",
      depends_on: ["c"],
      steps: [{ type: "prepare", description: "Step A" }],
    },
    {
      id: "b",
      deck_id: "deck-2",
      depends_on: ["a"],
      steps: [{ type: "prepare", description: "Step B" }],
    },
    {
      id: "c",
      deck_id: "deck-3",
      depends_on: ["b"],
      steps: [
        {
          type: "prepare",
          description: "Step C, depends on B which depends on A which depends on C",
        },
      ],
    },
  ],
};

const unknownDeck = {
  id: "invalid-unknown-deck",
  deck_jobs: [
    {
      id: "ghost",
      deck_id: "deck-999",
      depends_on: [],
      steps: [{ type: "measure", description: "Targets a deck the fleet doesn't have" }],
    },
  ],
};

export const invalidSamples: SampleDag[] = [
  {
    id: "invalid-no-jobs",
    label: "Empty DAG",
    description: "deck_jobs is empty.",
    topology: "0 jobs",
    category: "validation",
    expectInvalid: true,
    badge: "DAG_HAS_NO_JOBS",
    json: noJobs,
  },
  {
    id: "invalid-duplicate-job-id",
    label: "Duplicate job id",
    description: "Two jobs share id 'j1'.",
    topology: "2 jobs",
    category: "validation",
    expectInvalid: true,
    badge: "DUPLICATE_JOB_ID",
    json: duplicateJobId,
  },
  {
    id: "invalid-job-no-steps",
    label: "Job with no steps",
    description: "One job, steps: [].",
    topology: "1 job",
    category: "validation",
    expectInvalid: true,
    badge: "JOB_HAS_NO_STEPS",
    json: jobNoSteps,
  },
  {
    id: "invalid-unknown-dependency",
    label: "Unknown dependency",
    description: "depends_on references a job id that isn't in the DAG.",
    topology: "1 job",
    category: "validation",
    expectInvalid: true,
    badge: "UNKNOWN_DEPENDENCY",
    json: unknownDependency,
  },
  {
    id: "invalid-cycle",
    label: "Cycle (A → B → C → A)",
    description: "Three jobs in a dependency loop.",
    topology: "3 jobs",
    category: "validation",
    expectInvalid: true,
    badge: "DAG_HAS_CYCLE",
    json: cycle,
  },
  {
    id: "invalid-unknown-deck",
    label: "Unknown deck",
    description: "References deck-999 — not in the demo fleet (deck-1..deck-4).",
    topology: "1 job",
    category: "validation",
    expectInvalid: true,
    badge: "UNKNOWN_DECK",
    json: unknownDeck,
  },
];
