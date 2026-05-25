import type { DagSubmission } from "./api";

type Step = { type: string; description: string };

interface JobSpec {
  id: string;
  deck_id: string;
  depends_on?: string[];
  steps?: Step[];
}

function withSteps(spec: JobSpec): JobSpec & { steps: Step[]; depends_on: string[] } {
  return {
    ...spec,
    depends_on: spec.depends_on ?? [],
    steps: spec.steps ?? [{ type: "work", description: `${spec.id} on ${spec.deck_id}` }],
  };
}

/** Linear A → B → C across deck-1, deck-2, deck-3. */
export function linearDag(runId: string): DagSubmission {
  return {
    id: runId,
    deck_jobs: [
      withSteps({ id: "j1", deck_id: "deck-1" }),
      withSteps({ id: "j2", deck_id: "deck-2", depends_on: ["j1"] }),
      withSteps({ id: "j3", deck_id: "deck-3", depends_on: ["j2"] }),
    ],
  };
}

/** Two independent jobs on deck-1 and deck-2 — both run concurrently. */
export function parallelDag(runId: string): DagSubmission {
  return {
    id: runId,
    deck_jobs: [
      withSteps({ id: "track-a", deck_id: "deck-1" }),
      withSteps({ id: "track-b", deck_id: "deck-2" }),
    ],
  };
}

/** One source → three branches. */
export function fanOutDag(runId: string): DagSubmission {
  return {
    id: runId,
    deck_jobs: [
      withSteps({ id: "source", deck_id: "deck-1" }),
      withSteps({ id: "branch-warm", deck_id: "deck-2", depends_on: ["source"] }),
      withSteps({ id: "branch-ambient", deck_id: "deck-3", depends_on: ["source"] }),
      withSteps({ id: "branch-cool", deck_id: "deck-4", depends_on: ["source"] }),
    ],
  };
}

/** Three parallel extracts → one pool job on deck-4. */
export function fanInDag(runId: string): DagSubmission {
  return {
    id: runId,
    deck_jobs: [
      withSteps({ id: "extract-a", deck_id: "deck-1" }),
      withSteps({ id: "extract-b", deck_id: "deck-2" }),
      withSteps({ id: "extract-c", deck_id: "deck-3" }),
      withSteps({
        id: "pool",
        deck_id: "deck-4",
        depends_on: ["extract-a", "extract-b", "extract-c"],
      }),
    ],
  };
}

/** Two extracts feed two same-deck (deck-3) processes; the orchestrator must serialize. */
export function sameDeckDag(runId: string): DagSubmission {
  return {
    id: runId,
    deck_jobs: [
      withSteps({ id: "extract-a", deck_id: "deck-1" }),
      withSteps({ id: "extract-b", deck_id: "deck-2" }),
      withSteps({ id: "process-a", deck_id: "deck-3", depends_on: ["extract-a"] }),
      withSteps({ id: "process-b", deck_id: "deck-3", depends_on: ["extract-b"] }),
    ],
  };
}

/** Mixed fan-out + fan-in. */
export function mixedDag(runId: string): DagSubmission {
  return {
    id: runId,
    deck_jobs: [
      withSteps({ id: "prep", deck_id: "deck-1" }),
      withSteps({ id: "warm", deck_id: "deck-2", depends_on: ["prep"] }),
      withSteps({ id: "cool", deck_id: "deck-3", depends_on: ["prep"] }),
      withSteps({ id: "compare", deck_id: "deck-4", depends_on: ["warm", "cool"] }),
    ],
  };
}

/** Cycle: j1 → j2 → j3 → j1 — frontend flags CYCLE_DETECTED. */
export function cycleDag(runId: string): DagSubmission {
  return {
    id: runId,
    deck_jobs: [
      withSteps({ id: "j1", deck_id: "deck-1", depends_on: ["j3"] }),
      withSteps({ id: "j2", deck_id: "deck-2", depends_on: ["j1"] }),
      withSteps({ id: "j3", deck_id: "deck-3", depends_on: ["j2"] }),
    ],
  };
}

/** Dangling dependency: j2 depends on a job that doesn't exist. */
export function danglingDepDag(runId: string): DagSubmission {
  return {
    id: runId,
    deck_jobs: [
      withSteps({ id: "j1", deck_id: "deck-1" }),
      withSteps({ id: "j2", deck_id: "deck-2", depends_on: ["nonexistent"] }),
    ],
  };
}

/** Unknown deck id. */
export function unknownDeckDag(runId: string): DagSubmission {
  return {
    id: runId,
    deck_jobs: [
      withSteps({ id: "j1", deck_id: "deck-1" }),
      withSteps({ id: "j2", deck_id: "deck-99-does-not-exist", depends_on: ["j1"] }),
    ],
  };
}

/** Duplicate job_id. */
export function duplicateJobIdDag(runId: string): DagSubmission {
  return {
    id: runId,
    deck_jobs: [
      withSteps({ id: "j1", deck_id: "deck-1" }),
      withSteps({ id: "j1", deck_id: "deck-2" }),
    ],
  };
}
