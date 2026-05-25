import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ArrowUpRight, Bug } from "lucide-react";
import { Button } from "@/components/primitives/Button";
import { Card } from "@/components/primitives/Card";
import { StatusPill } from "@/components/primitives/StatusPill";
import { TimeAgo } from "@/components/primitives/TimeAgo";
import { DeckChaosModal } from "@/components/console/DeckChaosModal";
import { EventTail } from "@/components/console/EventTail";
import { StatTile } from "@/components/console/StatTile";
import { apiKeys, getDeckChaos, isChaosActive } from "@/lib/api";
import { cacheOnlyQueryFn } from "@/lib/api/query-config";
import { deckUiStatus } from "@/lib/ui-derive";
import { shortClock } from "@/lib/format";
import type { ChaosState, Deck, Event } from "@/lib/api-types";

/**
 * Operator view of a single deck. Pairs with /runs/:id structurally.
 *
 * No backend endpoint of its own: the deck row comes from the fleet
 * cache that `useLiveState` (mounted by AppShell) already polls at 1s,
 * the chaos state comes from the per-deck chaos query (shared with
 * DeckCard via apiKeys.chaos(id)), and recent activity is the events
 * cache filtered to this deck.
 *
 * Limitations:
 *   - Recent activity is rolling-window only (the events cache caps
 *     at 500 entries). A full deck history would need a backend
 *     endpoint; out of scope.
 *   - Chaos toggles are reachable through the existing modal, not
 *     inline — keeps parity with the grid affordance.
 */
export function DeckDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [chaosOpen, setChaosOpen] = useState(false);

  const decksQ = useQuery<Deck[]>({
    queryKey: apiKeys.decks,
    queryFn: cacheOnlyQueryFn,
    staleTime: Infinity,
  });
  const eventsQ = useQuery<Event[]>({
    queryKey: apiKeys.events,
    queryFn: cacheOnlyQueryFn,
    staleTime: Infinity,
  });
  // Read the cached chaos state at the same cadence DeckCard uses, so
  // the StatTile reflects what an operator just saw on the grid.
  const chaosQ = useQuery<ChaosState>({
    queryKey: apiKeys.chaos(id ?? ""),
    queryFn: () => getDeckChaos(id!),
    enabled: !!id,
    refetchInterval: 5000,
    retry: false,
    staleTime: 4000,
  });

  const deck = useMemo(
    () => (id ? decksQ.data?.find((d) => d.id === id) : undefined),
    [id, decksQ.data],
  );

  // Defensive: some events carry deck_id only in payload.
  const deckEvents = useMemo(() => {
    if (!id || !eventsQ.data) return [];
    return eventsQ.data.filter((e) => {
      if (e.deck_id === id) return true;
      const fromPayload =
        e.payload && typeof e.payload === "object"
          ? (e.payload as Record<string, unknown>)["deck_id"]
          : undefined;
      return fromPayload === id;
    });
  }, [id, eventsQ.data]);

  if (!id || decksQ.isLoading) {
    return (
      <div className="mx-auto max-w-container-content page-x py-10">
        <div className="text-[14px] text-ink-muted">Loading deck…</div>
      </div>
    );
  }

  if (!deck) {
    return (
      <div className="mx-auto max-w-container-content page-x py-10">
        <p className="text-[14px] text-ink-muted">
          Deck <span className="font-mono">{id}</span> not found.
        </p>
      </div>
    );
  }

  const uiStatus = deckUiStatus(deck);
  const chaosFlags = chaosQ.data ? countFlags(chaosQ.data) : 0;
  const chaosOn = isChaosActive(chaosQ.data);

  return (
    <div className="mx-auto max-w-container-content page-x py-8 lg:py-10">
      <header className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
        <div className="flex min-w-0 flex-col gap-1.5">
          <h1 className="truncate font-mono text-section-sm font-semibold tracking-section text-ink md:text-section">
            {deck.id}
          </h1>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[13px] tracking-nav text-ink-muted">
            <StatusPill status={uiStatus} />
            <span>
              last heartbeat{" "}
              {deck.last_heartbeat_at ? <TimeAgo timestamp={deck.last_heartbeat_at} /> : "never"}
            </span>
            <span className="font-mono text-[11px] text-ink-sub">
              first seen {shortClock(deck.first_seen_at)}
            </span>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Button variant="secondary" onClick={() => setChaosOpen(true)}>
            <Bug size={14} />
            Chaos controls
          </Button>
        </div>
      </header>

      <DeckChaosModal open={chaosOpen} onClose={() => setChaosOpen(false)} deckId={deck.id} />

      <section className="mt-6 grid grid-cols-2 gap-3 md:grid-cols-4">
        <StatTile label="Status" value={statusValue(uiStatus)} status={uiStatus} />
        <StatTile
          label="Current job"
          value={deck.current_job ? "1" : "—"}
          status={deck.current_job ? "RUNNING" : undefined}
        />
        <StatTile
          label="Last heartbeat"
          value={deck.last_heartbeat_at ? <TimeAgo timestamp={deck.last_heartbeat_at} /> : "never"}
        />
        <StatTile
          label="Chaos flags"
          value={chaosFlags}
          status={chaosOn ? "AMBIGUOUS" : undefined}
          hint={chaosOn ? "tampered" : undefined}
        />
      </section>

      {deck.current_job && (
        <section className="mt-8 flex flex-col gap-3">
          <h2 className="text-[18px] font-semibold tracking-sub text-ink">Current job</h2>
          <Card className="p-4">
            <Link
              to={`/runs/${encodeURIComponent(deck.current_job.run_id)}`}
              className="flex flex-col gap-1.5"
            >
              <div className="flex items-baseline justify-between gap-2">
                <span className="font-mono text-[13px] font-semibold tracking-sub text-ink">
                  {deck.current_job.job_id}
                </span>
                <span className="inline-flex items-center gap-1 text-[12px] font-medium tracking-nav text-ink-nav hover:text-ink">
                  Open run
                  <ArrowUpRight size={12} strokeWidth={2} />
                </span>
              </div>
              <div className="flex items-center gap-2 text-[12px] text-ink-muted">
                <span className="font-mono">{deck.current_job.run_id}</span>
                <StatusPill status={deck.current_job.status} />
              </div>
            </Link>
          </Card>
        </section>
      )}

      <section className="mt-8 flex flex-col gap-3">
        <div className="flex items-center justify-between">
          <h2 className="text-[18px] font-semibold tracking-sub text-ink">Recent activity</h2>
          <span className="font-mono text-[11px] tracking-nav text-ink-sub">
            {deckEvents.length === 0
              ? "no events in window"
              : `${deckEvents.length} event${deckEvents.length === 1 ? "" : "s"}`}
          </span>
        </div>
        <Card className="overflow-hidden p-0">
          <EventTail events={deckEvents} />
        </Card>
      </section>
    </div>
  );
}

function statusValue(s: ReturnType<typeof deckUiStatus>): string {
  // StatTile renders the label as the big number. Use a short human form
  // for deck health so the tile reads clean on a single line.
  switch (s) {
    case "HEALTHY_IDLE":
      return "Idle";
    case "HEALTHY_BUSY":
      return "Busy";
    case "HEALTHY":
      return "Healthy";
    case "STALE":
      return "Stale";
    case "UNREACHABLE":
      return "Unreachable";
    case "RECOVERING":
      return "Recovering";
    case "EMPTY":
      return "Empty";
    case "UNKNOWN":
      return "Unknown";
  }
}

function countFlags(c: ChaosState): number {
  // Mirror isChaosActive (chaos.ts): treats both booleans and the two
  // numeric counters as "non-default if non-zero".
  let n = 0;
  if (c.hang) n++;
  if (c.silent) n++;
  if (c.drop_events) n++;
  if (c.pause_egress) n++;
  if (c.pause_ingress) n++;
  if (c.hang_after_step > 0) n++;
  if (c.crash_after_step > 0) n++;
  return n;
}
