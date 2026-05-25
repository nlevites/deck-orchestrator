import { memo, useState } from "react";
import { Link } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { Bug } from "lucide-react";
import { Card } from "@/components/primitives/Card";
import { StatusPill } from "@/components/primitives/StatusPill";
import { TimeAgo } from "@/components/primitives/TimeAgo";
import type { Deck, Run } from "@/lib/api-types";
import { apiKeys } from "@/lib/api";
import { deckUiStatus } from "@/lib/ui-derive";
import { DeckChaosModal } from "@/components/console/DeckChaosModal";

interface ActiveDeckCardProps {
  deck: Deck;
  chaosActive: boolean;
}

/**
 * Shallow-prop equality is enough for the memo guard: the parent
 * (FleetGridPage) holds a stable per-poll Deck reference until the
 * deck's wire shape actually changes, and chaosActive is a boolean.
 * Without memo, a 1Hz live poll re-renders every active card on
 * every tick — at 100 busy decks that's the dominant cost on the
 * fleet view. The two fields-that-matter are deck identity and
 * chaosActive; everything else is derived from `deck`.
 */
function activePropsEqual(prev: ActiveDeckCardProps, next: ActiveDeckCardProps): boolean {
  if (prev.chaosActive !== next.chaosActive) return false;
  if (prev.deck === next.deck) return true;
  return (
    prev.deck.id === next.deck.id &&
    prev.deck.last_known_health === next.deck.last_known_health &&
    prev.deck.last_heartbeat_at === next.deck.last_heartbeat_at &&
    prev.deck.current_job?.run_id === next.deck.current_job?.run_id &&
    prev.deck.current_job?.job_id === next.deck.current_job?.job_id &&
    prev.deck.current_job?.status === next.deck.current_job?.status
  );
}

/**
 * HEALTHY_BUSY deck card. Elapsed-since-dispatch comes from cached run
 * detail only — the list endpoint doesn't carry it, and we won't fetch here.
 */
function ActiveDeckCardImpl({ deck, chaosActive }: ActiveDeckCardProps) {
  const [chaosOpen, setChaosOpen] = useState(false);
  const qc = useQueryClient();
  const uiStatus = deckUiStatus(deck);
  const job = deck.current_job;

  const dispatchedAt = job ? readDispatchedAtFromCache(qc, job.run_id, job.job_id) : undefined;

  return (
    <>
      {/*
        Stretched-link: chaos button is a sibling of <Link> because
        interactive content inside <a> is invalid HTML.
      */}
      <div className="relative">
        <Link
          to={`/decks/${encodeURIComponent(deck.id)}`}
          aria-label={`Deck ${deck.id}, status ${uiStatus}`}
          className="block focus:outline-none focus-visible:ring-2 focus-visible:ring-ink/20 rounded-panel"
        >
          <Card interactive className="flex h-full flex-col gap-2 p-3">
            <div className="flex items-center justify-between gap-2">
              <span className="font-mono text-[11px] tracking-nav text-ink-sub">{deck.id}</span>
              <div className="flex items-center gap-1.5">
                <StatusPill status={uiStatus} />
                <span aria-hidden className="inline-block h-5 w-5" />
              </div>
            </div>

            {job ? (
              <>
                <div className="flex flex-col gap-0.5">
                  <span className="truncate font-mono text-[12.5px] font-semibold tracking-tight text-ink">
                    {job.job_id}
                  </span>
                  <span className="truncate font-mono text-[11px] tracking-nav text-ink-sub">
                    run {job.run_id}
                  </span>
                </div>
                <div className="mt-auto flex items-center justify-between text-[11px] tracking-nav">
                  <StatusPill status={job.status} dot={false} className="text-[10px]" />
                  {dispatchedAt ? (
                    <span className="font-mono text-ink-sub">
                      <TimeAgo timestamp={dispatchedAt} />
                    </span>
                  ) : deck.last_heartbeat_at ? (
                    <span className="font-mono text-ink-sub">
                      hb <TimeAgo timestamp={deck.last_heartbeat_at} />
                    </span>
                  ) : null}
                </div>
              </>
            ) : (
              // Partition can briefly show a busy deck without current_job.
              <div className="text-[12px] text-ink-muted">slot occupied</div>
            )}

            {chaosActive && (
              <div className="inline-flex w-fit items-center gap-1 rounded-full bg-[#fff7ec] px-1.5 py-0.5 text-[10px] font-medium tracking-nav text-status-failed">
                <Bug size={10} strokeWidth={2} />
                chaos active
              </div>
            )}
          </Card>
        </Link>
        <button
          type="button"
          onClick={() => setChaosOpen(true)}
          aria-label={`Chaos controls for ${deck.id}`}
          title="Chaos controls"
          className="absolute right-3 top-3 z-10 inline-flex h-5 w-5 items-center justify-center rounded text-ink-sub transition-colors hover:bg-surface-subtle hover:text-ink"
        >
          <Bug size={12} />
        </button>
      </div>
      <DeckChaosModal open={chaosOpen} onClose={() => setChaosOpen(false)} deckId={deck.id} />
    </>
  );
}

/**
 * Cache-only read of dispatched_at; no fetch — advisory when run detail
 * is already warm.
 */
function readDispatchedAtFromCache(
  qc: ReturnType<typeof useQueryClient>,
  runId: string,
  jobId: string,
): string | undefined {
  const run = qc.getQueryData<Run>(apiKeys.run(runId));
  if (!run) return undefined;
  const job = run.deck_jobs.find((j) => j.id === jobId);
  if (!job) return undefined;
  const attempt = job.recent_attempts?.[0];
  return attempt?.dispatched_at;
}

export const ActiveDeckCard = memo(ActiveDeckCardImpl, activePropsEqual);
