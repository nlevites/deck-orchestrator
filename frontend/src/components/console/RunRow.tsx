import { useMemo } from "react";
import { Link } from "react-router-dom";
import { ChevronRight } from "lucide-react";
import { StatusPill } from "@/components/primitives/StatusPill";
import type { Deck, RunSummary } from "@/lib/api-types";
import { relativeAge, shortClock } from "@/lib/format";
import { cn } from "@/lib/cn";

interface RunRowProps {
  run: RunSummary;
  /**
   * Optional deck snapshot. When provided, the row renders the
   * `current_job.deck` chips for any deck currently executing a
   * deck_job in this run (DISPATCHED or RUNNING). Pass on the
   * dashboard's active-runs panel; omit on the runs index where
   * the column would force unbounded width.
   */
  decks?: ReadonlyArray<Deck>;
}

/**
 * Active run row. Uses pre-aggregated `deck_jobs_summary`; no ETA because
 * the system has no step-duration estimates.
 */
export function RunRow({ run, decks }: RunRowProps) {
  const byStatus = run.deck_jobs_summary.by_status;
  const total = run.deck_jobs_summary.total;
  const completed = byStatus["COMPLETED"] ?? 0;
  const running = (byStatus["RUNNING"] ?? 0) + (byStatus["DISPATCHED"] ?? 0);
  const ambiguous = byStatus["AMBIGUOUS"] ?? 0;
  const failed = byStatus["FAILED"] ?? 0;
  const progressPct = total === 0 ? 0 : Math.round((completed / total) * 100);

  const executingDeckIds = useMemo(() => {
    if (!decks || decks.length === 0) return [] as string[];
    return decks
      .filter(
        (d) =>
          d.current_job?.run_id === run.id &&
          (d.current_job.status === "DISPATCHED" || d.current_job.status === "RUNNING"),
      )
      .map((d) => d.id)
      .sort();
  }, [decks, run.id]);

  const elapsedLabel = run.terminal_at
    ? `terminal ${relativeAge(run.terminal_at)}`
    : `elapsed ${relativeAge(run.submitted_at)}`;

  return (
    <Link
      to={`/runs/${run.id}`}
      aria-label={`Run ${run.id}, status ${run.status}`}
      className={cn(
        "group block border-b border-line px-5 py-3 transition-colors last:border-b-0 hover:bg-surface-subtle",
        "focus-visible:bg-surface-subtle focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-ink/20",
        // AMBIGUOUS rows need to read as "needs your eye" without a wash
        // that fails for color-blind users. A 2px left accent reads as a
        // sidebar marker rather than a tint, and pairs with the pill.
        run.status === "AMBIGUOUS" && "border-l-2 border-l-status-ambiguous pl-[18px]",
      )}
    >
      <div className="flex items-center gap-4">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex items-center gap-2">
            <StatusPill status={run.status} size="compact" />
            <span className="truncate font-mono text-[13px] font-medium tracking-sub text-ink">
              {run.id}
            </span>
          </div>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[12px] tracking-nav text-ink-muted">
            <span>submitted {relativeAge(run.submitted_at)}</span>
            <span className="font-mono text-ink-sub">{shortClock(run.submitted_at)}</span>
            <span className="font-mono text-ink-sub">{elapsedLabel}</span>
            <span className="font-mono text-ink-sub">v{run.version}</span>
          </div>
        </div>

        <div className="hidden items-center gap-3 text-[12px] tracking-nav text-ink-muted md:flex">
          <Pill tone="completed" label={`${completed}/${total} done`} />
          {running > 0 && <Pill tone="running" label={`${running} running`} />}
          {ambiguous > 0 && <Pill tone="ambiguous" label={`${ambiguous} ambiguous`} />}
          {failed > 0 && <Pill tone="failed" label={`${failed} failed`} />}
        </div>

        <ChevronRight
          size={16}
          strokeWidth={1.7}
          className="text-ink-nav transition-transform duration-150 ease-out-soft group-hover:translate-x-0.5"
        />
      </div>

      {(progressPct > 0 || executingDeckIds.length > 0) && (
        <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1.5">
          <ProgressBar pct={progressPct} />
          {executingDeckIds.length > 0 && (
            <div className="flex flex-wrap items-center gap-1">
              <span className="font-mono text-[11px] tracking-nav text-ink-sub">on</span>
              {executingDeckIds.slice(0, 4).map((id) => (
                <span
                  key={id}
                  className="inline-flex items-center rounded-full bg-[#e0effb] px-2 py-0.5 font-mono text-[11px] tracking-nav text-status-running"
                >
                  {id}
                </span>
              ))}
              {executingDeckIds.length > 4 && (
                <span className="font-mono text-[11px] tracking-nav text-ink-sub">
                  +{executingDeckIds.length - 4}
                </span>
              )}
            </div>
          )}
        </div>
      )}
    </Link>
  );
}

interface ProgressBarProps {
  pct: number;
}

function ProgressBar({ pct }: ProgressBarProps) {
  const safe = Math.max(0, Math.min(100, pct));
  return (
    <div
      role="progressbar"
      aria-label={`${safe}% complete`}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={safe}
      className="h-1 flex-1 min-w-[120px] overflow-hidden rounded-full bg-line"
    >
      <div
        className={cn("h-full bg-status-running transition-[width] duration-300 ease-out-soft")}
        style={{ width: `${safe}%` }}
      />
    </div>
  );
}

type Tone = "completed" | "running" | "ambiguous" | "failed";

const toneStyles: Record<Tone, string> = {
  completed: "bg-[#e7f1ea] text-status-completed",
  running: "bg-[#e0effb] text-status-running",
  ambiguous: "bg-[#f7eadb] text-status-ambiguous",
  failed: "bg-[#fbe7e3] text-status-failed",
};

function Pill({ tone, label }: { tone: Tone; label: string }) {
  return (
    <span
      className={
        "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[11px] font-medium tracking-nav " +
        toneStyles[tone]
      }
    >
      {label}
    </span>
  );
}
