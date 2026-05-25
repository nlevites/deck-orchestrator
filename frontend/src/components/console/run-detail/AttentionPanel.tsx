import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, CheckCircle2, RefreshCw } from "lucide-react";
import { Button } from "@/components/primitives/Button";
import { Card } from "@/components/primitives/Card";
import { StatusPill } from "@/components/primitives/StatusPill";
import { AmbiguousResolutionModal } from "@/components/console/AmbiguousResolutionModal";
import { RetryConfirmModal } from "@/components/console/RetryConfirmModal";
import {
  ambiguousReasonExplain,
  ambiguousReasonLabel,
} from "@/components/console/run-detail/ambiguous-reason";
import { apiKeys } from "@/lib/api";
import { cacheOnlyQueryFn } from "@/lib/api/query-config";
import { useOperatorGate } from "@/lib/connection/operator-gate";
import { ambiguousJobs, failedJobs } from "@/lib/run-derive";
import { relativeAge } from "@/lib/format";
import { cn } from "@/lib/cn";
import type { Deck, DeckJob, Run } from "@/lib/api-types";

interface AttentionPanelProps {
  run: Run;
  resolveJobIdToOpen?: string | null;
  retryJobIdToOpen?: string | null;
  onModalsConsumed?: () => void;
}

/**
 * Inline resolve/retry panel; hidden when nothing needs action.
 * AMBIGUOUS before FAILED; deck heartbeat inlined so operators don't
 * context-switch to check liveness.
 */
export function AttentionPanel({
  run,
  resolveJobIdToOpen,
  retryJobIdToOpen,
  onModalsConsumed,
}: AttentionPanelProps) {
  const ambig = ambiguousJobs(run);
  const failed = failedJobs(run);

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

  // Row-click sources of truth (user clicked a "Resolve"/"Retry" CTA
  // inside this panel). External CTA targets (from RunHero) are derived
  // from props each render — mirroring them into state via useEffect
  // would trip react-hooks/set-state-in-effect and isn't necessary
  // because the resolve/retry id IS already the source of truth.
  const [clickedResolveJob, setClickedResolveJob] = useState<DeckJob | null>(null);
  const [clickedRetryJob, setClickedRetryJob] = useState<DeckJob | null>(null);
  const externalResolveJob = useMemo<DeckJob | null>(() => {
    if (!resolveJobIdToOpen) return null;
    return ambig.find((j) => j.id === resolveJobIdToOpen) ?? ambig[0] ?? null;
  }, [resolveJobIdToOpen, ambig]);
  const externalRetryJob = useMemo<DeckJob | null>(() => {
    if (!retryJobIdToOpen) return null;
    return failed.find((j) => j.id === retryJobIdToOpen) ?? failed[0] ?? null;
  }, [retryJobIdToOpen, failed]);
  const resolveJob = clickedResolveJob ?? externalResolveJob;
  const retryJob = clickedRetryJob ?? externalRetryJob;
  const gate = useOperatorGate();

  if (ambig.length === 0 && failed.length === 0) return null;

  return (
    <section
      className="flex flex-col gap-4 rounded-panel border border-status-ambiguous/25 bg-[#fffaf2] p-4"
      aria-label="Needs operator attention"
    >
      <header className="flex items-center gap-2 text-status-ambiguous">
        <AlertTriangle size={16} strokeWidth={1.8} />
        <h2 className="text-[14px] font-semibold tracking-sub">Needs your attention</h2>
        <span className="font-mono text-[11px] uppercase tracking-[0.12em] text-ink-sub">
          {ambig.length + failed.length} job{ambig.length + failed.length === 1 ? "" : "s"}
        </span>
      </header>

      {ambig.length > 0 && (
        <div className="flex flex-col gap-3">
          <Subhead text="Ambiguous — declare the physical outcome before the deck slot can release." />
          {ambig.map((j) => (
            <AttentionRow
              key={j.id}
              job={j}
              deck={decksById.get(j.deck_id)}
              kind="ambiguous"
              cta={
                <Button
                  disabled={gate.disabled}
                  title={gate.reason || undefined}
                  onClick={() => setClickedResolveJob(j)}
                >
                  <CheckCircle2 size={14} />
                  Resolve
                </Button>
              }
            />
          ))}
        </div>
      )}

      {failed.length > 0 && (
        <div className="flex flex-col gap-3">
          <Subhead text="Failed — confirm the deck is in a known-good state, then retry." />
          {failed.map((j) => (
            <AttentionRow
              key={j.id}
              job={j}
              deck={decksById.get(j.deck_id)}
              kind="failed"
              cta={
                <Button
                  variant="secondary"
                  disabled={gate.disabled}
                  title={gate.reason || undefined}
                  onClick={() => setClickedRetryJob(j)}
                >
                  <RefreshCw size={14} />
                  Retry
                </Button>
              }
            />
          ))}
        </div>
      )}

      {resolveJob && (
        <AmbiguousResolutionModal
          open={true}
          onClose={() => {
            setClickedResolveJob(null);
            // Tell the parent to drop resolveJobIdToOpen if it was driving us.
            onModalsConsumed?.();
          }}
          run={run}
          job={resolveJob}
        />
      )}
      {retryJob && (
        <RetryConfirmModal
          open={true}
          onClose={() => {
            setClickedRetryJob(null);
            onModalsConsumed?.();
          }}
          run={run}
          job={retryJob}
        />
      )}
    </section>
  );
}

function Subhead({ text }: { text: string }) {
  return <p className="text-[12px] leading-5 tracking-nav text-ink-muted">{text}</p>;
}

interface AttentionRowProps {
  job: DeckJob;
  deck?: Deck;
  kind: "ambiguous" | "failed";
  cta: React.ReactNode;
}

function AttentionRow({ job, deck, kind, cta }: AttentionRowProps) {
  const lastError = job.recent_attempts?.[0]?.error ?? job.error ?? undefined;
  const showReason = kind === "ambiguous" && job.ambiguous_reason;
  return (
    <Card className="p-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 flex flex-col gap-1">
          <span className="font-mono text-[11px] uppercase tracking-[0.12em] text-ink-sub">
            {job.id}
          </span>
          <span className="truncate text-[15px] font-semibold tracking-sub text-ink">
            {job.steps[0]?.description ?? "(no steps)"}
          </span>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[12px] tracking-nav text-ink-muted">
            <DeckInline deck={deck} deckId={job.deck_id} />
            <span>
              {job.recent_attempts?.length ?? 0} attempt
              {(job.recent_attempts?.length ?? 0) === 1 ? "" : "s"}
            </span>
            {kind === "ambiguous" && (
              <span className="font-medium text-status-ambiguous">slot held</span>
            )}
          </div>
        </div>
        <StatusPill status={job.status} />
      </div>
      {showReason && (
        <div className="mt-3 rounded-md border border-[#f7eadb] bg-[#fff7ec] px-3 py-2 text-[12px] leading-5 text-status-ambiguous">
          <div className="font-mono text-[10px] uppercase tracking-[0.12em] text-status-ambiguous/80">
            {ambiguousReasonLabel(job.ambiguous_reason!)}
          </div>
          <div className="text-ink">{ambiguousReasonExplain(job)}</div>
        </div>
      )}
      {lastError && (
        <div
          className={cn(
            "mt-3 rounded-md border px-3 py-2 text-[12px] leading-5",
            kind === "ambiguous"
              ? "border-[#f7eadb] bg-[#fff7ec] text-status-ambiguous"
              : "border-[#fbe7e3] bg-[#fff3f1] text-status-failed",
          )}
        >
          {lastError}
        </div>
      )}
      <div className="mt-3 flex flex-wrap items-center gap-2">{cta}</div>
    </Card>
  );
}

function DeckInline({ deck, deckId }: { deck?: Deck; deckId: string }) {
  const heartbeat = deck?.last_heartbeat_at
    ? `last heartbeat ${relativeAge(deck.last_heartbeat_at)}`
    : "no heartbeat yet";
  const live =
    deck?.last_known_health === "HEALTHY" ||
    deck?.last_known_health === "STALE" ||
    deck?.last_known_health === "UNREACHABLE";
  const dotClass = (() => {
    switch (deck?.last_known_health) {
      case "HEALTHY":
        return "bg-status-completed";
      case "STALE":
        return "bg-status-ambiguous";
      case "UNREACHABLE":
        return "bg-status-failed";
      default:
        return "bg-line-strong";
    }
  })();
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={cn("h-1.5 w-1.5 rounded-full", dotClass)} aria-hidden />
      <span className="font-mono text-ink-nav">{deckId}</span>
      {live && <span className="text-ink-sub">· {heartbeat}</span>}
    </span>
  );
}
