/**
 * Topological + critical-path helpers for DAGs in operator views.
 *
 * Sibling of `dag-layout.ts` (which positions nodes for SVG rendering).
 * This module returns *order* + *path* projections used by the run-detail
 * job timeline and the DAG canvas critical-path overlay.
 */

import type { DeckJob, DeckJobStatus } from "@/lib/api-types";

interface TopoJob {
  id: string;
  depends_on: string[];
}

/**
 * Stable Kahn-sort. Within a topological "level" (jobs whose dependencies
 * are all already emitted), the order falls back to the input array
 * order so callers can pre-sort by id / submitted_at if they want a
 * deterministic tiebreak.
 *
 * Cycles are tolerated: any nodes the algorithm can't reach get appended
 * at the end in input order. The orchestrator rejects cyclic DAGs at
 * submit, so this is purely defensive — it just means a malformed run
 * still renders rather than throwing in the UI.
 */
export function topoSortJobs<J extends TopoJob>(jobs: J[]): J[] {
  const byId = new Map(jobs.map((j) => [j.id, j]));
  const indeg = new Map<string, number>(
    jobs.map((j) => [j.id, j.depends_on.filter((d) => byId.has(d)).length]),
  );
  const dependents = new Map<string, string[]>();
  for (const j of jobs) {
    for (const dep of j.depends_on) {
      if (!byId.has(dep)) continue;
      const arr = dependents.get(dep) ?? [];
      arr.push(j.id);
      dependents.set(dep, arr);
    }
  }

  const out: J[] = [];
  const seen = new Set<string>();
  // Re-scan the input order each pass so the tiebreak is "first declared
  // first emitted" once dependencies have cleared.
  let progressed = true;
  while (progressed) {
    progressed = false;
    for (const j of jobs) {
      if (seen.has(j.id)) continue;
      if ((indeg.get(j.id) ?? 0) > 0) continue;
      out.push(j);
      seen.add(j.id);
      progressed = true;
      for (const dep of dependents.get(j.id) ?? []) {
        indeg.set(dep, (indeg.get(dep) ?? 0) - 1);
      }
    }
  }
  // Append any unreachable nodes (cycle) so nothing is silently dropped.
  for (const j of jobs) {
    if (!seen.has(j.id)) out.push(j);
  }
  return out;
}

/**
 * The critical path is the longest unfinished dependency chain through
 * the DAG measured in *job count* (not wall-clock — we don't have step
 * durations). Used by the DAG canvas to highlight which path the
 * operator should be watching: this is the chain that determines
 * when the run finishes.
 *
 * Logic:
 *   - "Unfinished" = anything that hasn't completed yet (PENDING / READY
 *     / DISPATCHED / RUNNING / FAILED / AMBIGUOUS / CANCELLED). FAILED /
 *     AMBIGUOUS / CANCELLED stay on the path because their downstream
 *     jobs are blocked until the operator resolves them.
 *   - For each unfinished sink (no unfinished dependents), walk back
 *     through dependencies, choosing the deepest predecessor at each
 *     step.
 *   - Returns the set of job ids on the longest chain. Empty when the
 *     run is fully completed / cancelled.
 *
 * Ties broken by id for determinism so re-renders don't flicker the
 * highlight to a different chain.
 */
export function criticalPathIds(jobs: DeckJob[]): Set<string> {
  const FINISHED: ReadonlySet<DeckJobStatus> = new Set(["COMPLETED"]);
  const byId = new Map(jobs.map((j) => [j.id, j]));
  const unfinished = jobs.filter((j) => !FINISHED.has(j.status));
  if (unfinished.length === 0) return new Set();

  const dependents = new Map<string, string[]>();
  for (const j of jobs) {
    for (const dep of j.depends_on) {
      const arr = dependents.get(dep) ?? [];
      arr.push(j.id);
      dependents.set(dep, arr);
    }
  }

  // Depth from each unfinished node to its deepest unfinished sink.
  const depth = new Map<string, number>();
  const next = new Map<string, string | null>();
  function depthOf(id: string): number {
    const cached = depth.get(id);
    if (cached !== undefined) return cached;
    const job = byId.get(id);
    if (!job || FINISHED.has(job.status)) {
      depth.set(id, 0);
      return 0;
    }
    let best = 1;
    let bestChild: string | null = null;
    for (const child of dependents.get(id) ?? []) {
      const childJob = byId.get(child);
      if (!childJob || FINISHED.has(childJob.status)) continue;
      const d = 1 + depthOf(child);
      if (d > best || (d === best && (bestChild === null || child < bestChild))) {
        best = d;
        bestChild = child;
      }
    }
    depth.set(id, best);
    next.set(id, bestChild);
    return best;
  }

  let bestRoot: string | null = null;
  let bestDepth = 0;
  for (const j of unfinished) {
    const d = depthOf(j.id);
    if (d > bestDepth || (d === bestDepth && (bestRoot === null || j.id < bestRoot))) {
      bestDepth = d;
      bestRoot = j.id;
    }
  }
  if (!bestRoot) return new Set();

  const out = new Set<string>();
  let cur: string | null = bestRoot;
  while (cur) {
    out.add(cur);
    cur = next.get(cur) ?? null;
  }
  return out;
}
