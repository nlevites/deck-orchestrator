import { useEffect, useMemo, useState } from "react";
import { AlertTriangle, Copy, RefreshCw, ScrollText, XCircle } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@/components/primitives/Button";
import { StatusPill } from "@/components/primitives/StatusPill";
import { useOperatorGate } from "@/lib/connection/operator-gate";
import { ambiguousJobs, countJobs, failedJobs } from "@/lib/run-derive";
import { shortClock } from "@/lib/format";
import { cn } from "@/lib/cn";
import { apiKeys } from "@/lib/api";
import { cacheOnlyQueryFn } from "@/lib/api/query-config";
import type { Deck, Run } from "@/lib/api-types";

interface RunHeroProps {
  run: Run;
  unreadEventCount: number;
  onResolve: () => void;
  onRetry: () => void;
  onCancel: () => void;
  onToggleActivity: () => void;
  activityOpen: boolean;
}

/**
 * Run-detail triage strip: live duration, segmented progress bar, and a
 * single primary CTA chosen by state (resolve > retry > cancel).
 */
export function RunHero({
  run,
  unreadEventCount,
  onResolve,
  onRetry,
  onCancel,
  onToggleActivity,
  activityOpen,
}: RunHeroProps) {
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
  const counts = countJobs(run.deck_jobs, decksById);
  const ambig = ambiguousJobs(run);
  const failed = failedJobs(run);
  // Terminal = the orchestrator stamped terminal_at, which only happens
  // for COMPLETED / CANCELLED. FAILED is non-terminal: the run is awaiting
  // retry or cancel. See DESIGN.md "State machine — run status".
  const terminalLike = run.status === "COMPLETED" || run.status === "CANCELLED";
  const gate = useOperatorGate();

  return (
    <header className="flex flex-col gap-4">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div className="flex min-w-0 flex-col gap-2">
          <RunIdHeading runId={run.id} />
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[13px] tracking-nav text-ink-muted">
            <StatusPill status={run.status} />
            <RunDuration
              startedAt={run.submitted_at}
              terminalAt={run.terminal_at ?? undefined}
              isTerminal={terminalLike || run.status === "AMBIGUOUS"}
            />
            <span className="font-mono text-[11px] text-ink-sub">
              submitted {shortClock(run.submitted_at)}
            </span>
            <span className="font-mono text-[11px] text-ink-sub">v{run.version}</span>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <ActivityToggle
            open={activityOpen}
            unread={unreadEventCount}
            onClick={onToggleActivity}
          />
          {ambig.length > 0 ? (
            <Button disabled={gate.disabled} title={gate.reason || undefined} onClick={onResolve}>
              <AlertTriangle size={14} />
              Resolve {ambig.length} ambiguous
            </Button>
          ) : failed.length > 0 && !terminalLike ? (
            // FAILED is non-terminal: offer Retry (resume) AND Cancel (give up).
            <>
              <Button variant="secondary" onClick={onCancel} disabled={gate.disabled}>
                <XCircle size={14} />
                Cancel run
              </Button>
              <Button disabled={gate.disabled} title={gate.reason || undefined} onClick={onRetry}>
                <RefreshCw size={14} />
                Retry {failed.length} failed
              </Button>
            </>
          ) : run.status === "PENDING" || run.status === "RUNNING" ? (
            <Button
              variant="secondary"
              disabled={gate.disabled}
              title={gate.reason || undefined}
              onClick={onCancel}
            >
              <XCircle size={14} />
              Cancel run
            </Button>
          ) : null}
        </div>
      </div>

      <ProgressStrip
        total={counts.total}
        completed={counts.completed}
        running={counts.dispatched + counts.running}
        ready={counts.ready - counts.blocked}
        blocked={counts.blocked}
        attention={counts.ambiguous + counts.failed}
        cancelled={counts.cancelled}
      />
    </header>
  );
}

function RunIdHeading({ runId }: { runId: string }) {
  const [copied, setCopied] = useState(false);
  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(runId);
      setCopied(true);
      setTimeout(() => setCopied(false), 1200);
    } catch {
      // clipboard denied; no-op so the heading doesn't throw.
    }
  };
  return (
    <div className="flex items-center gap-2 min-w-0">
      <h1
        className="truncate font-mono text-section-sm font-semibold tracking-section text-ink md:text-section"
        title={runId}
      >
        {runId}
      </h1>
      <button
        type="button"
        onClick={onCopy}
        aria-label={copied ? "copied run id" : "copy run id"}
        title={copied ? "Copied" : "Copy run id"}
        className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-ink-nav hover:bg-line/60 hover:text-ink"
      >
        <Copy size={13} strokeWidth={1.7} />
      </button>
    </div>
  );
}

interface RunDurationProps {
  startedAt: string;
  terminalAt?: string;
  isTerminal: boolean;
}

function RunDuration({ startedAt, terminalAt, isTerminal }: RunDurationProps) {
  // Tick once per second so the wall-clock duration on a live run
  // advances without a parent re-render. Terminal runs freeze.
  // We track `now` as state (initialized lazily and refreshed by the
  // interval) so render stays pure — calling Date.now() during render
  // would trip react-hooks/purity.
  const [now, setNow] = useState<number>(() => Date.now());
  useEffect(() => {
    if (isTerminal) return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [isTerminal]);

  const start = new Date(startedAt).getTime();
  const end = terminalAt ? new Date(terminalAt).getTime() : now;
  const durMs = Math.max(0, end - start);
  const sec = Math.floor(durMs / 1000) % 60;
  const min = Math.floor(durMs / 60_000) % 60;
  const hr = Math.floor(durMs / 3_600_000);

  let text: string;
  if (hr > 0) text = `${hr}h ${min}m`;
  else if (min > 0) text = `${min}m ${String(sec).padStart(2, "0")}s`;
  else text = `${sec}s`;

  return (
    <span
      className={cn("font-mono text-[12px]", isTerminal ? "text-ink-sub" : "text-ink-muted")}
      aria-label={isTerminal ? "total run duration" : "elapsed run duration (live)"}
    >
      {isTerminal ? "" : "running for "}
      {text}
      {!isTerminal && (
        <span
          aria-hidden
          className="ml-1 inline-block h-1.5 w-1.5 animate-pulse-slow rounded-full bg-status-running align-middle"
        />
      )}
    </span>
  );
}

interface ProgressStripProps {
  total: number;
  completed: number;
  running: number;
  ready: number;
  blocked: number;
  attention: number;
  cancelled: number;
}

/**
 * Segmented progress bar; attention segments sit at the right end so
 * they read as "stuck" rather than forward progress.
 */
function ProgressStrip({
  total,
  completed,
  running,
  ready,
  blocked,
  attention,
  cancelled,
}: ProgressStripProps) {
  if (total === 0) return null;
  const pending = Math.max(
    0,
    total - completed - running - ready - blocked - attention - cancelled,
  );
  const segments: Array<{ key: string; count: number; className: string; label: string }> = [
    { key: "completed", count: completed, className: "bg-status-completed/85", label: "completed" },
    { key: "running", count: running, className: "bg-status-running/85", label: "in flight" },
    { key: "ready", count: ready, className: "bg-status-ready/70", label: "ready" },
    // Blocked = READY but deck cannot accept work. Rendered in amber so the
    // operator can see it's stuck without it being as alarming as failed/ambiguous.
    { key: "blocked", count: blocked, className: "bg-status-stale/80", label: "blocked" },
    { key: "pending", count: pending, className: "bg-line-strong", label: "pending" },
    { key: "cancelled", count: cancelled, className: "bg-status-cancelled/40", label: "cancelled" },
    {
      key: "attention",
      count: attention,
      className: "bg-status-failed/85",
      label: "needs attention",
    },
  ];
  return (
    <div className="flex flex-col gap-2">
      <div
        className="flex h-2 w-full overflow-hidden rounded-pill bg-line"
        role="img"
        aria-label={`${completed} of ${total} jobs completed, ${attention} need attention${blocked > 0 ? `, ${blocked} blocked` : ""}`}
      >
        {segments.map((s) =>
          s.count > 0 ? (
            <span
              key={s.key}
              className={cn("block h-full", s.className)}
              style={{ width: `${(s.count / total) * 100}%` }}
              title={`${s.count} ${s.label}`}
            />
          ) : null,
        )}
      </div>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 font-mono text-[11px] uppercase tracking-[0.12em] text-ink-sub">
        <span>{total} jobs</span>
        <Legend className="bg-status-completed/85" label={`${completed} completed`} />
        {running > 0 && <Legend className="bg-status-running/85" label={`${running} in flight`} />}
        {ready > 0 && <Legend className="bg-status-ready/70" label={`${ready} ready`} />}
        {blocked > 0 && <Legend className="bg-status-stale/80" label={`${blocked} blocked`} />}
        {pending > 0 && <Legend className="bg-line-strong" label={`${pending} pending`} />}
        {attention > 0 && (
          <Legend className="bg-status-failed/85" label={`${attention} attention`} />
        )}
        {cancelled > 0 && (
          <Legend className="bg-status-cancelled/40" label={`${cancelled} cancelled`} />
        )}
      </div>
    </div>
  );
}

function Legend({ className, label }: { className: string; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={cn("h-1.5 w-3 rounded-pill", className)} aria-hidden />
      <span>{label}</span>
    </span>
  );
}

interface ActivityToggleProps {
  open: boolean;
  unread: number;
  onClick: () => void;
}

function ActivityToggle({ open, unread, onClick }: ActivityToggleProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={open}
      className={cn(
        "inline-flex h-9 items-center gap-2 rounded-pill border px-3 text-[13px] font-medium tracking-nav transition-colors duration-150 ease-out-soft",
        open
          ? "border-ink/30 bg-surface text-ink"
          : "border-line bg-transparent text-ink-nav hover:border-ink/30 hover:text-ink",
      )}
    >
      <ScrollText size={14} strokeWidth={1.7} />
      Activity
      {unread > 0 && !open && (
        <span className="inline-flex h-4 min-w-[16px] items-center justify-center rounded-full bg-status-running/15 px-1.5 font-mono text-[10px] text-status-running">
          {unread > 99 ? "99+" : unread}
        </span>
      )}
    </button>
  );
}
