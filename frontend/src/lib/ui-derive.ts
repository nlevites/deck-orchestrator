/**
 * UI-derived helpers over the raw backend shapes.
 *
 * The backend's `Deck.last_known_health` is the narrow HEALTHY / STALE /
 * UNREACHABLE union — sufficient for state-machine reasoning, but the
 * operator console needs a richer surface (IDLE vs BUSY, slot-held-by-
 * AMBIGUOUS) for the fleet view. These helpers compute the display
 * status without polluting the canonical domain types.
 *
 * Anything in this file is a pure projection of a snapshot. Nothing
 * stateful, nothing async. If you find yourself reaching for the
 * query client here, the helper belongs in `lib/live/` instead.
 */
import type { Deck, DeckJob, DeckJobStatus, Run, RunStatus, RunSummary } from "@/lib/api-types";
import type { DeckHealth } from "@/components/primitives/StatusPill";

export function deckUiStatus(deck: Deck): DeckHealth {
  if (deck.last_known_health === "HEALTHY") {
    return deck.current_job ? "HEALTHY_BUSY" : "HEALTHY_IDLE";
  }
  return deck.last_known_health as DeckHealth;
}

export function isDeckHeldByAmbiguous(deck: Deck): boolean {
  return deck.current_job?.status === "AMBIGUOUS";
}

/**
 * deck_job statuses that pin the deck's slot. Per STATE_MACHINE.md §5,
 * a deck is "busy" while any deck_job for it is DISPATCHED, RUNNING,
 * or AMBIGUOUS — AMBIGUOUS holds the slot until operator resolution.
 */
export const OCCUPYING_STATUSES: ReadonlySet<DeckJobStatus> = new Set<DeckJobStatus>([
  "DISPATCHED",
  "RUNNING",
  "AMBIGUOUS",
]);

/**
 * The reason a READY job is not being dispatched. A non-null result
 * means the orchestrator is intentionally holding the job because the
 * target deck cannot accept work right now. The DAG contract binds jobs
 * to specific decks at submit time; the orchestrator never reassigns.
 *
 * Separate from upstream-dependency blocking (FAILED/AMBIGUOUS upstream)
 * which is handled by `blockedSet` in DagViewer — that's a DAG-structure
 * concern; this is a fleet-health concern.
 */
export type JobBlockReason =
  | { kind: "deck-unhealthy"; health: DeckHealth }
  | { kind: "slot-held"; holderStatus: DeckJobStatus }
  | { kind: "deck-decommissioned" }
  | null;

export function jobBlockReason(job: DeckJob, deck: Deck | undefined): JobBlockReason {
  if (job.status !== "READY") return null;
  if (!deck) return null;
  if (deck.decommissioned_at) return { kind: "deck-decommissioned" };
  if (deck.current_job && OCCUPYING_STATUSES.has(deck.current_job.status)) {
    return { kind: "slot-held", holderStatus: deck.current_job.status };
  }
  if (deck.last_known_health !== "HEALTHY") {
    return { kind: "deck-unhealthy", health: deckUiStatus(deck) };
  }
  return null;
}

/**
 * Upstream blockers for a job: parent IDs in a non-success terminal-ish
 * state (FAILED, AMBIGUOUS, CANCELLED) that prevent the orchestrator
 * from promoting this job PENDING -> READY (see backend's
 * dispatch.PromoteDownstreamReady: requires every dep to be COMPLETED).
 *
 * Empty array if nothing upstream is holding the job (or if the job has
 * already moved past PENDING / is itself terminal). Caller decides UI.
 */
export interface UpstreamBlocker {
  jobId: string;
  status: DeckJobStatus;
}

export function blockedByUpstream(run: Run, jobId: string): UpstreamBlocker[] {
  const job = run.deck_jobs.find((j) => j.id === jobId);
  if (!job) return [];
  // Only PENDING jobs can be held by upstream state — once a job has
  // already advanced (READY/DISPATCHED/RUNNING/terminal) its dispatch
  // decision is on a different code path.
  if (job.status !== "PENDING") return [];
  const byId = new Map(run.deck_jobs.map((j) => [j.id, j]));
  const out: UpstreamBlocker[] = [];
  for (const dep of job.depends_on) {
    const u = byId.get(dep);
    if (!u) continue;
    if (u.status === "FAILED" || u.status === "AMBIGUOUS" || u.status === "CANCELLED") {
      out.push({ jobId: u.id, status: u.status });
    }
  }
  return out;
}

export function runCountsByStatus(
  runs: ReadonlyArray<Run | RunSummary>,
): Record<RunStatus, number> {
  const counts: Record<RunStatus, number> = {
    PENDING: 0,
    RUNNING: 0,
    COMPLETED: 0,
    FAILED: 0,
    AMBIGUOUS: 0,
    CANCELLED: 0,
  };
  for (const r of runs) counts[r.status] += 1;
  return counts;
}

export function deckCountsByStatus(decks: ReadonlyArray<Deck>): Record<DeckHealth, number> {
  const counts: Record<DeckHealth, number> = {
    EMPTY: 0,
    HEALTHY: 0,
    HEALTHY_IDLE: 0,
    HEALTHY_BUSY: 0,
    STALE: 0,
    UNREACHABLE: 0,
    RECOVERING: 0,
    UNKNOWN: 0,
  };
  for (const d of decks) {
    counts[deckUiStatus(d)] += 1;
  }
  return counts;
}

/**
 * Newest first across all attempts on a deck_job. Backend rows are
 * already returned in this order; the helper exists so consumers can
 * defensively re-sort if they pull attempts from a different surface.
 */
export function attemptsNewestFirst(job: DeckJob) {
  const a = job.recent_attempts ?? [];
  return [...a].sort(
    (x, y) => new Date(y.dispatched_at).getTime() - new Date(x.dispatched_at).getTime(),
  );
}

const ATTENTION_HEALTH: ReadonlySet<DeckHealth> = new Set<DeckHealth>([
  "STALE",
  "RECOVERING",
  "UNREACHABLE",
  "UNKNOWN",
]);

const ATTENTION_RANK: Record<DeckHealth, number> = {
  UNREACHABLE: 0,
  STALE: 1,
  RECOVERING: 2,
  UNKNOWN: 3,
  HEALTHY_BUSY: 4,
  HEALTHY_IDLE: 5,
  HEALTHY: 6,
  EMPTY: 7,
};

export interface FleetPartition {
  attention: Deck[];
  active: Deck[];
  idle: Deck[];
}

/**
 * Buckets the fleet into the three sections rendered on `/decks`. A
 * single deck is placed into exactly one bucket: attention wins over
 * active wins over idle so an unhealthy busy deck doesn't get hidden
 * in the "Active work" strip.
 *
 * Sort order inside each bucket:
 * - attention: by ATTENTION_RANK (unreachable first), then by id.
 * - active: ambiguous-held first (review finding #1), then by id so the
 *   list is stable across live-state polls.
 * - idle: by id, ascending. Keeps the heatmap visually anchored.
 */
export function partitionDecksForFleetPage(decks: ReadonlyArray<Deck>): FleetPartition {
  const attention: Deck[] = [];
  const active: Deck[] = [];
  const idle: Deck[] = [];

  for (const d of decks) {
    const ui = deckUiStatus(d);
    if (ATTENTION_HEALTH.has(ui) || isDeckHeldByAmbiguous(d)) {
      attention.push(d);
    } else if (ui === "HEALTHY_BUSY") {
      active.push(d);
    } else {
      idle.push(d);
    }
  }

  attention.sort((a, b) => {
    const ra = ATTENTION_RANK[deckUiStatus(a)] ?? 99;
    const rb = ATTENTION_RANK[deckUiStatus(b)] ?? 99;
    if (ra !== rb) return ra - rb;
    return compareDeckIds(a.id, b.id);
  });
  active.sort((a, b) => {
    const ha = isDeckHeldByAmbiguous(a) ? 0 : 1;
    const hb = isDeckHeldByAmbiguous(b) ? 0 : 1;
    if (ha !== hb) return ha - hb;
    return compareDeckIds(a.id, b.id);
  });
  idle.sort((a, b) => compareDeckIds(a.id, b.id));

  return { attention, active, idle };
}

/**
 * Natural-ordering comparator for deck ids of the form `deck-N`. Falls
 * back to lexicographic compare for non-conforming ids so the sort is
 * stable in mixed environments.
 */
export function compareDeckIds(a: string, b: string): number {
  const ra = /^deck-(\d+)$/.exec(a);
  const rb = /^deck-(\d+)$/.exec(b);
  if (ra && rb) return Number(ra[1]) - Number(rb[1]);
  return a.localeCompare(b);
}
