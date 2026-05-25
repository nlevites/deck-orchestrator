import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { ArrowUpRight } from "lucide-react";
import { Button } from "@/components/primitives/Button";
import { Card } from "@/components/primitives/Card";
import { EventTail } from "@/components/console/EventTail";
import { FleetPulseStrip } from "@/components/console/FleetPulseStrip";
import { LiveIndicator } from "@/components/console/LiveIndicator";
import { NeedsAttentionPanel } from "@/components/console/NeedsAttentionPanel";
import { PageHeader } from "@/components/console/PageHeader";
import { RunRow } from "@/components/console/RunRow";
import { apiKeys } from "@/lib/api";
import { cacheOnlyQueryFn } from "@/lib/api/query-config";
import type { Deck, Event, RunSummary } from "@/lib/api-types";

/**
 * The /fleet dashboard. Ordered by operator urgency:
 *
 *   1. Pulse strip          — fleet snapshot, three buckets, deep-link.
 *   2. Needs attention      — AMBIGUOUS runs, slow / silent decks,
 *                             flapping decks, recently FAILED runs.
 *   3. Active runs          — what's progressing right now.
 *   4. Recent events        — coalesced live tail.
 *
 * AMBIGUOUS rows in (2) keep the same `aria-label` ("Runs needing
 * operator resolution") that the original banner used so e2e
 * selectors stay stable.
 */
export function FleetDashboardPage() {
  const runsQ = useQuery<RunSummary[]>({
    queryKey: apiKeys.runs,
    queryFn: cacheOnlyQueryFn,
    staleTime: Infinity,
  });
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

  // Memoize the unwrap-with-fallback so downstream useMemo deps are stable
  // across renders (the bare `data ?? []` allocates a fresh array each time
  // when data is undefined, which churns every memo that lists it as a dep).
  const runs = useMemo(() => runsQ.data ?? [], [runsQ.data]);
  const decks = useMemo(() => decksQ.data ?? [], [decksQ.data]);
  const events = useMemo(() => eventsQ.data ?? [], [eventsQ.data]);
  const compactEvents = useMemo(() => events.slice(0, 8), [events]);

  // Operational views hide EMPTY slots: an empty slot has no executor
  // attached and nothing to action from this surface. The full 100-slot
  // inventory is visible at /settings/fleet, which is its job.
  const attachedDecks = useMemo(
    () => decks.filter((d) => d.last_known_health !== "EMPTY"),
    [decks],
  );

  const activeRuns = useMemo(() => runs.filter((r) => r.status === "RUNNING"), [runs]);

  return (
    <div className="mx-auto max-w-container-content page-x py-8 lg:py-10">
      <PageHeader
        title="Fleet"
        actions={
          <>
            <LiveIndicator />
            <Link to="/submit">
              <Button variant="primary" size="md">
                New run
              </Button>
            </Link>
          </>
        }
      />

      <div className="mt-6">
        <FleetPulseStrip decks={attachedDecks} />
      </div>

      <NeedsAttentionPanel runs={runs} decks={attachedDecks} events={events} className="mt-6" />

      <section className="mt-8 grid grid-cols-1 gap-6 xl:grid-cols-3">
        <div className="xl:col-span-2 flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <h2 className="text-[18px] font-semibold tracking-sub text-ink">Active runs</h2>
            <Link
              to="/runs"
              className="inline-flex items-center gap-1 text-[13px] font-medium tracking-nav text-ink-nav hover:text-ink"
            >
              See all
              <ArrowUpRight size={12} strokeWidth={2} />
            </Link>
          </div>
          {activeRuns.length === 0 ? (
            <div className="flex flex-wrap items-center gap-3 rounded-panel border border-dashed border-line bg-surface px-4 py-3 text-[13px] tracking-nav text-ink-muted">
              <span>No runs are active.</span>
              <Link
                to="/submit"
                className="inline-flex items-center gap-1 text-[13px] font-medium text-accent-link hover:text-accent-linkAlt hover:underline"
              >
                Submit a DAG
                <ArrowUpRight size={12} strokeWidth={2} />
              </Link>
            </div>
          ) : (
            <Card className="overflow-hidden p-0">
              {activeRuns.map((r) => (
                <RunRow key={r.id} run={r} decks={decks} />
              ))}
            </Card>
          )}
        </div>

        <div className="flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <h2 className="text-[18px] font-semibold tracking-sub text-ink">Recent events</h2>
          </div>
          <Card className="overflow-hidden p-0">
            <EventTail events={compactEvents} density="compact" />
          </Card>
        </div>
      </section>
    </div>
  );
}
