import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, Bug } from "lucide-react";
import { Card } from "@/components/primitives/Card";
import { StatusPill } from "@/components/primitives/StatusPill";
import { TimeAgo } from "@/components/primitives/TimeAgo";
import type { Deck } from "@/lib/api-types";
import { deckUiStatus } from "@/lib/ui-derive";
import { apiKeys, getDeckChaos, isChaosActive } from "@/lib/api";
import { DeckChaosModal } from "./DeckChaosModal";

interface DeckCardProps {
  deck: Deck;
  /** True when the deck slot is held by an AMBIGUOUS deck_job (review finding #1). */
  heldByAmbiguous?: boolean;
}

/**
 * Compact fleet grid cell (200×110). Whole card links to deck detail;
 * chaos/ambiguous badges must read distinctly from busy (STATE_MACHINE.md §5).
 */
export function DeckCard({ deck, heldByAmbiguous = false }: DeckCardProps) {
  const [chaosOpen, setChaosOpen] = useState(false);

  // Poll the chaos state at a slow cadence so the CHAOS badge is up to
  // date without spamming the orchestrator. Errors are swallowed —
  // executors that are down won't have chaos state and that's fine.
  const chaosQuery = useQuery({
    queryKey: apiKeys.chaos(deck.id),
    queryFn: () => getDeckChaos(deck.id),
    refetchInterval: 5000,
    retry: false,
    staleTime: 4000,
  });

  const chaosActive = isChaosActive(chaosQuery.data);
  const uiStatus = deckUiStatus(deck);

  return (
    <>
      {/*
        Stretched-link: chaos button is a sibling of <Link> — see ActiveDeckCard.
      */}
      <div className="relative">
        <Link
          to={`/decks/${encodeURIComponent(deck.id)}`}
          aria-label={`Deck ${deck.id}, status ${uiStatus}`}
          className="block focus:outline-none focus-visible:ring-2 focus-visible:ring-ink/20 rounded-panel"
        >
          <Card interactive className="flex h-full flex-col gap-2 p-3">
            <div className="flex flex-col gap-2">
              <div className="flex items-center justify-between gap-2">
                <span className="font-mono text-[11px] tracking-nav text-ink-sub">{deck.id}</span>
                <div className="flex items-center gap-1.5">
                  <StatusPill status={uiStatus} />
                  <span aria-hidden className="inline-block h-5 w-5" />
                </div>
              </div>

              <div className="flex flex-col gap-2">
                <div className="flex items-baseline justify-between gap-2">
                  <span className="truncate font-mono text-[12px] font-semibold tracking-sub text-ink">
                    {deck.id}
                  </span>
                  {/*
                    TimeAgo only when health is degraded — a 100-card grid
                    ticking every second on HEALTHY decks is noise + cost.
                  */}
                  {uiStatus !== "HEALTHY" &&
                    uiStatus !== "HEALTHY_IDLE" &&
                    uiStatus !== "HEALTHY_BUSY" &&
                    deck.last_heartbeat_at && (
                      <TimeAgo
                        timestamp={deck.last_heartbeat_at}
                        className="font-mono text-[10px] tracking-nav text-ink-sub"
                      />
                    )}
                </div>

                {deck.current_job && (
                  <div className="flex items-center justify-between gap-2 text-[11px] tracking-nav text-ink-muted">
                    <span className="truncate font-mono text-[10px] text-ink-sub">
                      {deck.current_job.job_id}
                    </span>
                  </div>
                )}

                <div className="flex flex-wrap items-center gap-1">
                  {heldByAmbiguous && (
                    <div className="inline-flex items-center gap-1 rounded-full bg-[#fff7ec] px-1.5 py-0.5 text-[10px] font-medium tracking-nav text-status-ambiguous">
                      <AlertTriangle size={10} strokeWidth={2} />
                      slot held — needs resolve
                    </div>
                  )}
                  {chaosActive && (
                    <div className="inline-flex items-center gap-1 rounded-full bg-[#fff7ec] px-1.5 py-0.5 text-[10px] font-medium tracking-nav text-status-failed">
                      <Bug size={10} strokeWidth={2} />
                      chaos active
                    </div>
                  )}
                </div>
              </div>
            </div>
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
