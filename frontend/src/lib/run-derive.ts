import type { Deck, DeckJob, Run } from "@/lib/api-types";
import { jobBlockReason } from "@/lib/ui-derive";

export interface RunCounts {
  total: number;
  pending: number;
  /** READY jobs that can be dispatched (deck is HEALTHY, slot is free). */
  ready: number;
  /**
   * READY jobs whose target deck is unhealthy, decommissioned, or slot-held.
   * The orchestrator will not dispatch these until the fleet situation is
   * resolved. Subset of ready (status is still READY).
   */
  blocked: number;
  dispatched: number;
  running: number;
  completed: number;
  failed: number;
  ambiguous: number;
  cancelled: number;
}

export function countJobs(jobs: DeckJob[], decksById?: Map<string, Deck>): RunCounts {
  const counts: RunCounts = {
    total: jobs.length,
    pending: 0,
    ready: 0,
    blocked: 0,
    dispatched: 0,
    running: 0,
    completed: 0,
    failed: 0,
    ambiguous: 0,
    cancelled: 0,
  };
  for (const j of jobs) {
    switch (j.status) {
      case "PENDING":
        counts.pending++;
        break;
      case "READY":
        counts.ready++;
        if (decksById && jobBlockReason(j, decksById.get(j.deck_id)) !== null) {
          counts.blocked++;
        }
        break;
      case "DISPATCHED":
        counts.dispatched++;
        break;
      case "RUNNING":
        counts.running++;
        break;
      case "COMPLETED":
        counts.completed++;
        break;
      case "FAILED":
        counts.failed++;
        break;
      case "AMBIGUOUS":
        counts.ambiguous++;
        break;
      case "CANCELLED":
        counts.cancelled++;
        break;
    }
  }
  return counts;
}

export function ambiguousJobs(run: Run): DeckJob[] {
  return run.deck_jobs.filter((j) => j.status === "AMBIGUOUS");
}

export function failedJobs(run: Run): DeckJob[] {
  return run.deck_jobs.filter((j) => j.status === "FAILED");
}
