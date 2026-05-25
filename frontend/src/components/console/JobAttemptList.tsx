import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, ChevronDown, Circle, TriangleAlert } from "lucide-react";
import { StatusPill } from "@/components/primitives/StatusPill";
import { apiKeys } from "@/lib/api";
import { cacheOnlyQueryFn } from "@/lib/api/query-config";
import { topoSortJobs } from "@/lib/dag-topo";
import {
  ambiguousReasonExplain,
  ambiguousReasonLabel,
} from "@/components/console/run-detail/ambiguous-reason";
import {
  blockedByUpstream,
  deckUiStatus,
  jobBlockReason,
  type JobBlockReason,
  type UpstreamBlocker,
} from "@/lib/ui-derive";
import type { Deck, DeckJob, Event } from "@/lib/api-types";
import { relativeAge, shortClock } from "@/lib/format";
import { cn } from "@/lib/cn";

interface JobAttemptListProps {
  /** Run id is required so each JobRow can pull per-attempt step counts
   * from the run's event cache. */
  runId: string;
  jobs: DeckJob[];
  selectedJobId?: string;
  onSelectJob?: (id: string) => void;
  criticalPathIds?: ReadonlySet<string>;
  className?: string;
}

/**
 * Deck jobs in topological dispatch order. Latest attempt errors always
 * render; attempt history collapses unless selected or errored.
 */
export function JobAttemptList({
  runId,
  jobs,
  selectedJobId,
  onSelectJob,
  criticalPathIds,
  className,
}: JobAttemptListProps) {
  const ordered = useMemo(() => topoSortJobs(jobs), [jobs]);
  const { data: decks = [] } = useQuery<Deck[]>({
    queryKey: apiKeys.decks,
    queryFn: cacheOnlyQueryFn,
    staleTime: Infinity,
  });
  const decksById = useMemo(() => {
    const m = new Map<string, Deck>();
    for (const d of decks) m.set(d.id, d);
    return m;
  }, [decks]);
  // Events live in the run-scoped ring buffer (helpers.ts:appendEventToCache).
  // Read once here and pass the derived per-attempt step map down so each
  // JobRow doesn't re-subscribe + re-derive.
  const { data: events = [] } = useQuery<Event[]>({
    queryKey: apiKeys.eventsForRun(runId),
    queryFn: cacheOnlyQueryFn,
    staleTime: Infinity,
  });
  const stepsByAttempt = useMemo(() => {
    const m = new Map<string, number>();
    for (const e of events) {
      if (e.kind !== "JOB_STEP_COMPLETED" || !e.attempt_id) continue;
      const step = typeof e.payload?.["step"] === "number" ? (e.payload["step"] as number) : 0;
      const cur = m.get(e.attempt_id) ?? 0;
      if (step > cur) m.set(e.attempt_id, step);
    }
    return m;
  }, [events]);
  // Upstream blockers per job. Pre-computed at the parent so each row
  // doesn't re-walk the DAG.
  const upstreamBlockersByJob = useMemo(() => {
    const fakeRun = { deck_jobs: jobs } as Parameters<typeof blockedByUpstream>[0];
    const m = new Map<string, UpstreamBlocker[]>();
    for (const j of jobs) {
      const blockers = blockedByUpstream(fakeRun, j.id);
      if (blockers.length > 0) m.set(j.id, blockers);
    }
    return m;
  }, [jobs]);

  return (
    <div
      className={cn(
        "flex flex-col divide-y divide-line rounded-panel border border-line bg-surface",
        className,
      )}
    >
      {ordered.map((j) => (
        <JobRow
          key={j.id}
          job={j}
          deck={decksById.get(j.deck_id)}
          isSelected={selectedJobId === j.id}
          isOnCriticalPath={criticalPathIds?.has(j.id) ?? false}
          onSelect={onSelectJob}
          stepsByAttempt={stepsByAttempt}
          upstreamBlockers={upstreamBlockersByJob.get(j.id) ?? []}
        />
      ))}
    </div>
  );
}

interface JobRowProps {
  job: DeckJob;
  deck?: Deck;
  isSelected: boolean;
  isOnCriticalPath: boolean;
  onSelect?: (id: string) => void;
  stepsByAttempt: Map<string, number>;
  upstreamBlockers: UpstreamBlocker[];
}

function JobRow({
  job,
  deck,
  isSelected,
  isOnCriticalPath,
  onSelect,
  stepsByAttempt,
  upstreamBlockers,
}: JobRowProps) {
  const attempts = job.recent_attempts ?? [];
  const lastAttempt = attempts[0];
  const [expanded, setExpanded] = useState(false);
  const hasAttempts = attempts.length > 0;
  // Auto-expand when the operator selects this row OR when there's an
  // error to surface. Keeps the default list compact but never hides
  // the answer to "what just went wrong".
  const showAttempts = expanded || isSelected || !!lastAttempt?.error;
  const blockReason = jobBlockReason(job, deck);

  return (
    <div
      className={cn(
        "relative flex flex-col gap-2 px-5 py-4 text-left transition-colors",
        isSelected && "bg-surface-warm",
        !isSelected && "hover:bg-surface-warm/60",
      )}
    >
      {isOnCriticalPath && (
        <span
          aria-hidden
          className="absolute inset-y-2 left-0 w-[3px] rounded-r bg-status-running/60"
          title="critical path"
        />
      )}
      <button
        type="button"
        onClick={() => onSelect?.(job.id)}
        className="flex items-start justify-between gap-4 text-left"
      >
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 font-mono text-[11px] uppercase tracking-[0.12em] text-ink-sub">
            <span>{job.id}</span>
            {isOnCriticalPath && (
              <span className="rounded-pill bg-line/60 px-1.5 text-[9px] tracking-[0.14em] text-ink-nav">
                CRITICAL
              </span>
            )}
          </div>
          <div className="mt-0.5 truncate text-[14px] font-semibold tracking-sub text-ink">
            <StepTitle job={job} />
          </div>
          {(job.status === "RUNNING" || job.status === "AMBIGUOUS") &&
            (job.last_completed_step ?? 0) > 0 &&
            (job.total_steps ?? 0) > 1 && (
              <StepProgressBar
                completed={job.last_completed_step ?? 0}
                total={job.total_steps ?? 0}
                steps={job.steps}
                frozen={job.status === "AMBIGUOUS"}
              />
            )}
          <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[12px] tracking-nav text-ink-muted">
            <DeckBadge deck={deck} deckId={job.deck_id} />
            {job.depends_on.length > 0 && (
              <>
                <span aria-hidden>·</span>
                <span>after {job.depends_on.join(", ")}</span>
              </>
            )}
            {hasAttempts && (
              <>
                <span aria-hidden>·</span>
                <span>
                  {attempts.length} attempt{attempts.length === 1 ? "" : "s"}
                </span>
              </>
            )}
          </div>
          {blockReason && (
            <div className="flex items-center gap-1.5 text-[11.5px] text-status-stale">
              <TriangleAlert size={12} strokeWidth={2} />
              <span>{blockReasonLabel(blockReason)}</span>
            </div>
          )}
          {upstreamBlockers.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5 text-[11.5px] text-status-failed">
              <TriangleAlert size={12} strokeWidth={2} />
              <span>blocked by upstream:</span>
              {upstreamBlockers.map((b) => (
                <span
                  key={b.jobId}
                  className="inline-flex items-center gap-1 rounded-pill border border-status-failed/30 bg-[#fff3f1] px-1.5 py-px font-mono text-[10.5px] tracking-[0.06em] text-status-failed"
                  title={`${b.jobId} is ${b.status}; resolve or retry it to unblock this job.`}
                >
                  {b.jobId} · {b.status.toLowerCase()}
                </span>
              ))}
            </div>
          )}
        </div>
        <StatusPill status={job.status} />
      </button>

      {/* Protocol section: the static job definition with overall progress.
          Sits above attempts so the eye doesn't read steps as belonging to
          attempt 1. Always visible for multi-step jobs. */}
      {job.steps.length > 1 && (
        <section className="ml-1 mt-1">
          <div className="mb-1 flex items-baseline gap-2 font-mono text-[10px] uppercase tracking-[0.12em] text-ink-sub">
            <span>Protocol</span>
            <span className="normal-case tracking-nav text-ink-muted">
              {job.last_completed_step ?? 0}/{job.total_steps ?? job.steps.length}
              {job.status === "COMPLETED" ? " complete" : ""}
            </span>
          </div>
          <StepList job={job} />
        </section>
      )}

      {hasAttempts && (
        <div className="ml-1">
          {!showAttempts ? (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                setExpanded(true);
              }}
              className="inline-flex items-center gap-1 text-[11px] font-medium tracking-nav text-ink-nav hover:text-ink"
            >
              <ChevronDown size={12} />
              Show attempts
            </button>
          ) : (
            <>
              <div className="mb-1 font-mono text-[10px] uppercase tracking-[0.12em] text-ink-sub">
                Attempts
              </div>
              <ol className="flex flex-col gap-1.5 border-l border-line pl-3">
                {attempts.map((a, idx) => {
                  const stepCount = stepsByAttempt.get(a.attempt_id) ?? 0;
                  const total = job.total_steps ?? job.steps.length;
                  return (
                    <li
                      key={a.attempt_id}
                      className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-[12px] tracking-nav text-ink-muted"
                    >
                      <span className="font-mono text-[11px] text-ink-sub">
                        attempt {attempts.length - idx}
                      </span>
                      <span className="font-mono text-[10px] text-ink-sub">{a.attempt_id}</span>
                      <span>
                        dispatched {shortClock(a.dispatched_at)} · {relativeAge(a.dispatched_at)}
                      </span>
                      {total > 0 && (
                        <span className="font-mono text-[11px] text-ink-sub">
                          · {stepCount}/{total} step{total === 1 ? "" : "s"}
                        </span>
                      )}
                      {a.outcome && (
                        <span
                          className={cn(
                            "font-medium",
                            a.outcome === "COMPLETED"
                              ? "text-status-completed"
                              : "text-status-failed",
                          )}
                        >
                          → {a.outcome.toLowerCase()}
                        </span>
                      )}
                      {a.outcome_source && (
                        <span className="font-mono text-[10px] text-ink-sub">
                          ({a.outcome_source.toLowerCase()})
                        </span>
                      )}
                    </li>
                  );
                })}
              </ol>
            </>
          )}
        </div>
      )}

      {job.status === "AMBIGUOUS" && job.ambiguous_reason && (
        <div className="rounded-md border border-[#f7eadb] bg-[#fff7ec] px-3 py-2 text-[12px] leading-5 text-status-ambiguous">
          <div className="font-mono text-[10px] uppercase tracking-[0.12em] text-status-ambiguous/80">
            {ambiguousReasonLabel(job.ambiguous_reason)}
          </div>
          <div className="text-ink">{ambiguousReasonExplain(job)}</div>
        </div>
      )}

      {lastAttempt?.error && (
        <div className="rounded-md border border-[#fbe7e3] bg-[#fff3f1] px-3 py-2 text-[12px] leading-5 text-status-failed">
          {lastAttempt.error}
        </div>
      )}
    </div>
  );
}

interface DeckBadgeProps {
  deck?: Deck;
  deckId: string;
}

function DeckBadge({ deck, deckId }: DeckBadgeProps) {
  const ui = deck ? deckUiStatus(deck) : "UNKNOWN";
  // The fleet view's full pill surface is overkill inline; render a
  // plain dot + deck id with status colour by class. Falls back to a
  // muted dot if the deck isn't in the cache yet.
  const dotClass = (() => {
    switch (ui) {
      case "HEALTHY_BUSY":
      case "HEALTHY":
      case "HEALTHY_IDLE":
        return "bg-status-completed";
      case "STALE":
      case "RECOVERING":
        return "bg-status-ambiguous";
      case "UNREACHABLE":
        return "bg-status-failed";
      case "EMPTY":
        return "bg-line-strong";
      default:
        return "bg-ink-sub";
    }
  })();
  const heartbeatLabel = deck?.last_heartbeat_at
    ? `heartbeat ${relativeAge(deck.last_heartbeat_at)}`
    : "no heartbeat";
  return (
    <span className="inline-flex items-center gap-1.5" title={`${deckId} · ${heartbeatLabel}`}>
      <span className={cn("h-1.5 w-1.5 rounded-full", dotClass)} aria-hidden />
      <span className="font-mono text-ink-nav">{deckId}</span>
    </span>
  );
}

interface StepTitleProps {
  job: DeckJob;
}

function StepTitle({ job }: StepTitleProps) {
  if (job.status === "RUNNING" && (job.last_completed_step ?? 0) < job.steps.length) {
    const currentIdx = job.last_completed_step ?? 0;
    return <>{job.steps[currentIdx]?.description ?? job.steps[0]?.description ?? "(no steps)"}</>;
  }
  return <>{job.steps[0]?.description ?? "(no description)"}</>;
}

interface StepProgressBarProps {
  completed: number;
  total: number;
  steps: DeckJob["steps"];
  /** When true, render in the ambiguous palette and suppress "current step"
   * language — the executor isn't moving, the operator is. */
  frozen?: boolean;
}

function StepProgressBar({ completed, total, steps, frozen }: StepProgressBarProps) {
  const pct = total > 0 ? Math.round((completed / total) * 100) : 0;
  const currentDesc = frozen ? "" : (steps[completed]?.description ?? "");
  const numberClass = frozen ? "text-status-ambiguous" : "text-status-running";
  const fillClass = frozen ? "bg-status-ambiguous/70" : "bg-status-running/70";
  return (
    <div className="mt-1 flex flex-col gap-0.5">
      <div className="flex items-center gap-2 text-[11px] text-ink-muted">
        <span className={cn("font-mono tabular-nums", numberClass)}>
          {frozen ? "stalled at " : ""}step {completed}/{total}
        </span>
        {currentDesc && (
          <>
            <span aria-hidden>—</span>
            <span className="truncate">{currentDesc}</span>
          </>
        )}
      </div>
      <div className="h-0.5 w-full overflow-hidden rounded-pill bg-line">
        <div
          className={cn("h-full rounded-pill transition-[width] duration-500 ease-out", fillClass)}
          style={{ width: `${pct}%` }}
          role="progressbar"
          aria-valuenow={pct}
          aria-valuemin={0}
          aria-valuemax={100}
          aria-label={`${pct}% of steps complete`}
        />
      </div>
    </div>
  );
}

interface StepListProps {
  job: DeckJob;
}

function StepList({ job }: StepListProps) {
  const completedN = job.last_completed_step ?? 0;
  return (
    <ol className="ml-1 flex flex-col gap-0.5 border-l border-line pl-3">
      {job.steps.map((step, i) => {
        const stepNum = i + 1;
        const isDone = stepNum <= completedN;
        const isCurrent = job.status === "RUNNING" && stepNum === completedN + 1;
        return (
          <li
            key={i}
            className={cn(
              "flex items-center gap-2 text-[11.5px] tracking-nav",
              isDone ? "text-ink-sub" : isCurrent ? "text-ink" : "text-ink-sub/50",
            )}
          >
            {isDone ? (
              <Check size={11} strokeWidth={2} className="shrink-0 text-status-completed" />
            ) : isCurrent ? (
              <Circle
                size={10}
                strokeWidth={2}
                className="shrink-0 animate-pulse-slow text-status-running"
              />
            ) : (
              <span className="inline-block h-2 w-2 shrink-0 rounded-full bg-line-strong" />
            )}
            <span className="font-mono text-[10px] text-ink-sub/60">{stepNum}.</span>
            <span>{step.description}</span>
          </li>
        );
      })}
    </ol>
  );
}

function blockReasonLabel(reason: NonNullable<JobBlockReason>): string {
  switch (reason.kind) {
    case "deck-decommissioned":
      return "Blocked — deck is decommissioned. Cancel this run and resubmit targeting an active deck.";
    case "slot-held":
      return `Blocked — deck slot is held by a ${reason.holderStatus.toLowerCase()} job. Resolve it to free the slot.`;
    case "deck-unhealthy": {
      const h = reason.health.toLowerCase().replace("_", " ");
      return `Blocked — deck is ${h}. The run will proceed once the deck recovers or is reattached.`;
    }
  }
}
