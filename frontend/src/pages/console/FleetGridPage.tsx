import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import { Search } from "lucide-react";
import { PageHeader } from "@/components/console/PageHeader";
import { ActiveDeckCard } from "@/components/console/fleet/ActiveDeckCard";
import { AttentionDeckCard } from "@/components/console/fleet/AttentionDeckCard";
import { PulseSegmentBar } from "@/components/console/fleet/PulseSegmentBar";
import { PulseFilterChips } from "@/components/console/fleet/PulseFilterChips";
import {
  pulseBucketFor,
  type PulseCounts,
  type PulseFilter,
} from "@/components/console/fleet/pulse-helpers";
import { IdleHeatmap } from "@/components/console/fleet/IdleDeckPill";
import { VirtualDeckGrid } from "@/components/console/fleet/VirtualDeckGrid";
import { useFleetChaos } from "@/components/console/fleet/use-fleet-chaos";
import { apiKeys } from "@/lib/api";
import { cacheOnlyQueryFn } from "@/lib/api/query-config";
import { deckUiStatus, isDeckHeldByAmbiguous, partitionDecksForFleetPage } from "@/lib/ui-derive";
import type { Deck } from "@/lib/api-types";
import { cn } from "@/lib/cn";

/**
 * `/decks` — the all-decks view.
 *
 * Layered, signal-first layout:
 *   1. Pulse bar           — proportional fleet snapshot + segmented filter.
 *   2. Needs attention     — full triage cards for unhealthy / held decks.
 *   3. Sticky toolbar      — search + section anchors.
 *   4. Active work         — medium cards for HEALTHY_BUSY decks.
 *   5. Idle fleet          — virtualized heatmap of compact pills.
 *
 * Sections are derived from `partitionDecksForFleetPage`, which is a
 * pure projection of the live cache (`apiKeys.decks`). Live updates
 * land via `useLiveState`'s 1Hz tick — same as before. No new
 * endpoints, no fabricated metrics.
 *
 * The active and attention sections both use VirtualDeckGrid: at
 * fleet sizes ≥ ~12 each section's render cost is bounded by the
 * viewport, not the busy/attention count. At <12 they render
 * eagerly to avoid the scroll container's layout cost. Cards are
 * memoized so an unchanged deck doesn't re-render on every 1Hz poll
 * (the global hook replaces the decks slice wholesale every tick).
 */

// Hand-tuned row heights for the virtualized active/attention grids.
// Both cards have a few stable rows of content (status pill, run/job
// id, heartbeat); a small over-estimate is harmless (extra empty
// space at the row boundary) but an under-estimate causes overlap.
const ACTIVE_ROW_HEIGHT_PX = 132;
const ATTENTION_ROW_HEIGHT_PX = 168;
export function FleetGridPage() {
  const decksQ = useQuery<Deck[]>({
    queryKey: apiKeys.decks,
    queryFn: cacheOnlyQueryFn,
    staleTime: Infinity,
  });

  // Hide EMPTY slots from this operational view. Empty slots are
  // visible only on /settings/fleet (the management surface).
  const decks = useMemo(
    () => (decksQ.data ?? []).filter((d) => d.last_known_health !== "EMPTY"),
    [decksQ.data],
  );

  // The dashboard pulse strip deep-links here with `?filter=attention`
  // (etc). We read the param once on mount as the initial filter so the
  // landing matches the operator's intent — and then drop URL coupling
  // so the in-page chips can toggle freely without rewriting history.
  const [searchParams, setSearchParams] = useSearchParams();
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<PulseFilter>(() =>
    parsePulseFilter(searchParams.get("filter")),
  );

  useEffect(() => {
    if (searchParams.has("filter")) {
      const next = new URLSearchParams(searchParams);
      next.delete("filter");
      setSearchParams(next, { replace: true });
    }
  }, [searchParams, setSearchParams]);

  const filteredDecks = useMemo(() => filterDecks(decks, query), [decks, query]);
  const partition = useMemo(() => partitionDecksForFleetPage(filteredDecks), [filteredDecks]);

  // Pulse counts derive from the FULL fleet (search-aware, but not
  // bucket-aware). Otherwise selecting "Idle" would show "0 active"
  // and the bar would lose meaning.
  const pulseCounts = useMemo(() => {
    let attention = 0;
    let active = 0;
    let idle = 0;
    for (const d of filteredDecks) {
      const bucket = pulseBucketFor(deckUiStatus(d), isDeckHeldByAmbiguous(d));
      if (bucket === "attention") attention += 1;
      else if (bucket === "active") active += 1;
      else idle += 1;
    }
    return { attention, active, idle };
  }, [filteredDecks]);

  // Lift chaos polling to the page so we hit the executors once per
  // visible attention/active deck rather than once per card mount.
  const chaosTargets = useMemo(
    () => [...partition.attention, ...partition.active].map((d) => d.id),
    [partition.attention, partition.active],
  );
  const chaosFlags = useFleetChaos(chaosTargets);

  // Card renderers: stable callbacks so virtualized rows don't re-create each poll.
  const renderActiveCard = useCallback(
    (deck: Deck) => (
      <ActiveDeckCard key={deck.id} deck={deck} chaosActive={chaosFlags.get(deck.id) ?? false} />
    ),
    [chaosFlags],
  );
  const renderAttentionCard = useCallback(
    (deck: Deck) => (
      <AttentionDeckCard key={deck.id} deck={deck} chaosActive={chaosFlags.get(deck.id) ?? false} />
    ),
    [chaosFlags],
  );

  const showAttention =
    (filter === "all" || filter === "attention") && partition.attention.length > 0;
  const showActive = (filter === "all" || filter === "active") && partition.active.length > 0;
  const showIdle = (filter === "all" || filter === "idle") && partition.idle.length > 0;

  return (
    <div className="mx-auto max-w-container-content page-x py-8 lg:py-10">
      <PageHeader
        title="All decks"
        body="Trouble surfaces first. Active work shows what each busy deck is doing. Idle decks compress so you can read the fleet at a glance."
      />

      {showAttention && (
        <Section
          id="attention"
          title="Needs attention"
          subtitle={`${partition.attention.length} ${partition.attention.length === 1 ? "deck" : "decks"} require operator action.`}
          tone="attention"
        >
          <VirtualDeckGrid
            decks={partition.attention}
            cols={3}
            rowHeightPx={ATTENTION_ROW_HEIGHT_PX}
            renderCard={renderAttentionCard}
            eagerGridClassName="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3"
          />
        </Section>
      )}

      <Toolbar
        query={query}
        onQuery={setQuery}
        matched={filteredDecks.length}
        total={decks.length}
        filter={filter}
        onFilter={setFilter}
        counts={pulseCounts}
      />

      {showActive && (
        <Section
          id="active"
          title="Active work"
          subtitle={`${partition.active.length} ${partition.active.length === 1 ? "deck" : "decks"} running deck_jobs.`}
          tone="active"
        >
          <VirtualDeckGrid
            decks={partition.active}
            cols={4}
            rowHeightPx={ACTIVE_ROW_HEIGHT_PX}
            renderCard={renderActiveCard}
            eagerGridClassName="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4"
          />
        </Section>
      )}

      {showIdle && (
        <Section
          id="idle"
          title="Idle fleet"
          subtitle={`${partition.idle.length} ${partition.idle.length === 1 ? "deck" : "decks"} ready for new jobs.`}
          tone="idle"
        >
          <IdleHeatmap decks={partition.idle} />
        </Section>
      )}

      {filteredDecks.length === 0 && (
        <div className="mt-10 rounded-panel border border-dashed border-line bg-surface-subtle px-6 py-10 text-center text-[14px] text-ink-muted">
          No decks match the current search.
        </div>
      )}
    </div>
  );
}

const SECTION_DOT: Record<"attention" | "active" | "idle", string> = {
  attention: "bg-status-unreachable",
  active: "bg-status-running",
  idle: "bg-status-healthy",
};

interface SectionProps {
  id: string;
  title: string;
  subtitle: string;
  tone: "attention" | "active" | "idle";
  children: React.ReactNode;
}

function Section({ id, title, subtitle, tone, children }: SectionProps) {
  return (
    <section id={id} aria-labelledby={`${id}-h`} className="mt-8 flex flex-col gap-3">
      <div className="flex items-baseline justify-between gap-3">
        <div className="flex items-baseline gap-2">
          <span
            className={cn("inline-block h-2 w-2 rounded-full", SECTION_DOT[tone])}
            aria-hidden="true"
          />
          <h2 id={`${id}-h`} className="text-[18px] font-semibold tracking-sub text-ink">
            {title}
          </h2>
        </div>
        <span className="text-[12px] tracking-nav text-ink-sub">{subtitle}</span>
      </div>
      {children}
    </section>
  );
}

interface ToolbarProps {
  query: string;
  onQuery: (v: string) => void;
  matched: number;
  total: number;
  filter: PulseFilter;
  onFilter: (next: PulseFilter) => void;
  counts: PulseCounts;
}

function Toolbar({ query, onQuery, matched, total, filter, onFilter, counts }: ToolbarProps) {
  return (
    <div className="sticky top-0 z-10 mt-6 -mx-2 flex flex-col gap-3 rounded-panel bg-surface/90 px-2 py-2 backdrop-blur supports-[backdrop-filter]:bg-surface/70">
      <div className="flex flex-wrap items-center gap-3">
        <label className="relative flex min-w-[240px] flex-1 items-center md:max-w-md">
          <Search
            size={14}
            strokeWidth={1.8}
            className="pointer-events-none absolute left-3 text-ink-nav"
          />
          <input
            value={query}
            onChange={(e) => onQuery(e.target.value)}
            placeholder="Search deck id, current job, or run…"
            className={cn(
              "h-9 w-full rounded-pill border border-line bg-surface pl-9 pr-3 text-[13px] tracking-nav text-ink",
              "placeholder:text-ink-sub focus:border-ink/30 focus:outline-none",
            )}
          />
        </label>
        <PulseFilterChips counts={counts} active={filter} onChange={onFilter} />
        {filter !== "all" && (
          <button
            type="button"
            onClick={() => onFilter("all")}
            className="text-[12px] font-medium tracking-nav text-ink-nav hover:text-ink"
          >
            Clear filter
          </button>
        )}
        <span className="ml-auto font-mono text-[11px] tracking-nav text-ink-sub">
          {matched} of {total}
        </span>
      </div>
      <PulseSegmentBar
        counts={counts}
        total={matched}
        active={filter}
        onChange={onFilter}
        className="mx-1"
      />
    </div>
  );
}

function filterDecks(decks: ReadonlyArray<Deck>, query: string): Deck[] {
  const q = query.trim().toLowerCase();
  if (!q) return [...decks];
  return decks.filter(
    (d) =>
      d.id.toLowerCase().includes(q) ||
      d.current_job?.job_id.toLowerCase().includes(q) ||
      d.current_job?.run_id.toLowerCase().includes(q),
  );
}

function parsePulseFilter(raw: string | null): PulseFilter {
  if (raw === "attention" || raw === "active" || raw === "idle") return raw;
  return "all";
}
