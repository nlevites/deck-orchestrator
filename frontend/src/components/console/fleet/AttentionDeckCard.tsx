import { memo, useState } from "react";
import { Link } from "react-router-dom";
import { AlertTriangle, ArrowUpRight, Bug } from "lucide-react";
import { Card } from "@/components/primitives/Card";
import { StatusPill } from "@/components/primitives/StatusPill";
import { TimeAgo } from "@/components/primitives/TimeAgo";
import type { Deck } from "@/lib/api-types";
import { deckUiStatus, isDeckHeldByAmbiguous } from "@/lib/ui-derive";
import { DeckChaosModal } from "@/components/console/DeckChaosModal";

interface AttentionDeckCardProps {
  deck: Deck;
  chaosActive: boolean;
}

/** Memo guard — same rationale as ActiveDeckCard; job status matters for ambiguous-held. */
function attentionPropsEqual(prev: AttentionDeckCardProps, next: AttentionDeckCardProps): boolean {
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
 * Triage card for the attention strip: status, why it's flagged, and
 * direct actions (open deck / resolve ambiguous).
 */
function AttentionDeckCardImpl({ deck, chaosActive }: AttentionDeckCardProps) {
  const [chaosOpen, setChaosOpen] = useState(false);
  const uiStatus = deckUiStatus(deck);
  const heldByAmbiguous = isDeckHeldByAmbiguous(deck);
  const reason = describeAttentionReason(deck, chaosActive);

  return (
    <>
      {/*
        No stretched-link — footer CTAs are the primary navigation.
        See ActiveDeckCard for the clickable-card pattern.
      */}
      <Card className="flex flex-col gap-3 p-4" aria-label={`Deck ${deck.id}, status ${uiStatus}`}>
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 flex-col gap-0.5">
            <span className="font-mono text-eyebrow uppercase tracking-[0.12em] text-ink-sub">
              Attention
            </span>
            <span className="truncate font-mono text-[15px] font-semibold tracking-tight text-ink">
              {deck.id}
            </span>
          </div>
          <div className="flex items-center gap-1.5">
            <StatusPill status={uiStatus} />
            <button
              type="button"
              onClick={() => setChaosOpen(true)}
              aria-label={`Chaos controls for ${deck.id}`}
              title="Chaos controls"
              className="inline-flex h-6 w-6 items-center justify-center rounded text-ink-sub transition-colors hover:bg-surface-warm hover:text-ink"
            >
              <Bug size={13} />
            </button>
          </div>
        </div>

        <p className="text-[13px] leading-5 text-ink-muted">{reason}</p>

        <dl className="grid grid-cols-2 gap-y-1.5 text-[12px] tracking-nav">
          <dt className="text-ink-sub">Last heartbeat</dt>
          <dd className="text-right font-mono text-ink">
            {deck.last_heartbeat_at ? <TimeAgo timestamp={deck.last_heartbeat_at} /> : "never"}
          </dd>
          {deck.current_job && (
            <>
              <dt className="text-ink-sub">Holding</dt>
              <dd className="text-right">
                <span className="font-mono text-[12px] text-ink">{deck.current_job.job_id}</span>
                <span className="ml-1 font-mono text-[11px] text-ink-sub">
                  ({deck.current_job.status})
                </span>
              </dd>
              <dt className="text-ink-sub">Run</dt>
              <dd className="text-right font-mono text-ink">{deck.current_job.run_id}</dd>
            </>
          )}
        </dl>

        {(heldByAmbiguous || chaosActive) && (
          <div className="flex flex-wrap gap-1.5">
            {heldByAmbiguous && (
              <span className="inline-flex items-center gap-1 rounded-full bg-[#fff7ec] px-2 py-0.5 text-[11px] font-medium tracking-nav text-status-ambiguous">
                <AlertTriangle size={11} strokeWidth={2} />
                slot held — needs resolve
              </span>
            )}
            {chaosActive && (
              <span className="inline-flex items-center gap-1 rounded-full bg-[#fff7ec] px-2 py-0.5 text-[11px] font-medium tracking-nav text-status-failed">
                <Bug size={11} strokeWidth={2} />
                chaos active
              </span>
            )}
          </div>
        )}

        <div className="mt-auto flex flex-wrap items-center gap-2 pt-1">
          <Link
            to={`/decks/${encodeURIComponent(deck.id)}`}
            className="inline-flex items-center gap-1 rounded-pill bg-surface-ink px-3 py-1.5 text-[12px] font-medium tracking-nav text-white transition-colors hover:bg-ink"
          >
            Open deck
            <ArrowUpRight size={12} strokeWidth={2} />
          </Link>
          {heldByAmbiguous && deck.current_job && (
            <Link
              to={`/runs/${encodeURIComponent(deck.current_job.run_id)}/resolve`}
              className="inline-flex items-center gap-1 rounded-pill border border-line bg-surface px-3 py-1.5 text-[12px] font-medium tracking-nav text-ink transition-colors hover:bg-surface-warm"
            >
              Resolve ambiguous
              <ArrowUpRight size={12} strokeWidth={2} />
            </Link>
          )}
        </div>
      </Card>
      <DeckChaosModal open={chaosOpen} onClose={() => setChaosOpen(false)} deckId={deck.id} />
    </>
  );
}

/** Ambiguous-held wins over unreachable — resolving the run is the concrete next step. */
function describeAttentionReason(deck: Deck, chaosActive: boolean): string {
  if (isDeckHeldByAmbiguous(deck)) {
    return "Slot held by an AMBIGUOUS job. The orchestrator can't reuse this deck until you mark the job Completed or Failed.";
  }
  switch (deck.last_known_health) {
    case "UNREACHABLE":
      return "Heartbeats stopped reaching the orchestrator. The deck may have crashed or been partitioned.";
    case "STALE":
      return "Heartbeats are arriving slower than the stale threshold. Could be a flaky network or a hung worker.";
    default:
      break;
  }
  if (chaosActive) {
    return "A chaos / test-control flag is set on this executor. Clear it once the scenario is done.";
  }
  return "Deck health is degraded. Open the detail page for the latest events.";
}

export const AttentionDeckCard = memo(AttentionDeckCardImpl, attentionPropsEqual);
