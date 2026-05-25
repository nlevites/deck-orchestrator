import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { AlertTriangle, ArrowUpRight, Activity, Wand2, XCircle } from "lucide-react";
import type { Deck, Event, RunSummary } from "@/lib/api-types";
import { deckUiStatus } from "@/lib/ui-derive";
import type { DeckHealth } from "@/components/primitives/StatusPill";
import { relativeAge } from "@/lib/format";
import { cn } from "@/lib/cn";

interface NeedsAttentionPanelProps {
  runs: ReadonlyArray<RunSummary>;
  decks: ReadonlyArray<Deck>;
  events: ReadonlyArray<Event>;
  className?: string;
}

/**
 * Dashboard triage list; renders nothing when empty. Priority: ambiguous
 * runs, unhealthy decks, flapping decks (excluding unhealthy), open
 * FAILED runs awaiting operator decision.
 *
 * Open-FAILED runs (status FAILED + terminal_at null) are non-terminal
 * per DESIGN.md: a run with FAILED jobs sits awaiting retry or cancel
 * indefinitely, not a 30-minute window. The row drops the moment the
 * operator retries (run resumes) or cancels (run terminalizes).
 */
const FLAP_WINDOW_MS = 5 * 60_000;
const FLAP_MIN_TRANSITIONS = 3;
const NOW_TICK_MS = 30_000;
const UNHEALTHY: ReadonlySet<DeckHealth> = new Set<DeckHealth>([
  "STALE",
  "UNREACHABLE",
  "RECOVERING",
  "UNKNOWN",
]);

export function NeedsAttentionPanel({ runs, decks, events, className }: NeedsAttentionPanelProps) {
  const now = useNowTick(NOW_TICK_MS);

  const ambiguousRuns = useMemo(() => runs.filter((r) => r.status === "AMBIGUOUS"), [runs]);

  const unhealthyDecks = useMemo(() => {
    const out = decks.filter((d) => UNHEALTHY.has(deckUiStatus(d)));
    return out.sort((a, b) => heartbeatMs(a) - heartbeatMs(b));
  }, [decks]);

  const flappingDecks = useMemo(
    () => detectFlappingDecks(events, unhealthyDecks, now),
    [events, unhealthyDecks, now],
  );

  // Open FAILED runs: status FAILED + terminal_at null (the run is
  // awaiting operator retry or cancel). See DESIGN.md.
  const openFailedRuns = useMemo(
    () => runs.filter((r) => r.status === "FAILED" && !r.terminal_at),
    [runs],
  );

  const total =
    ambiguousRuns.length + unhealthyDecks.length + flappingDecks.length + openFailedRuns.length;

  if (total === 0) return null;

  return (
    <section
      aria-label="Needs attention"
      className={cn("rounded-panel border border-[#f7eadb] bg-[#fff7ec] p-4", className)}
    >
      <header className="flex items-center gap-2 px-1 pb-3">
        <span className="inline-flex h-7 w-7 items-center justify-center rounded-full bg-[#f4dec4] text-status-ambiguous">
          <AlertTriangle size={14} strokeWidth={1.8} />
        </span>
        <h2 className="text-[15px] font-semibold tracking-sub text-ink">Needs attention</h2>
        <span className="font-mono text-[11px] uppercase tracking-nav text-ink-sub">{total}</span>
      </header>

      <div className="flex flex-col gap-1.5">
        {ambiguousRuns.length > 0 && (
          <ul aria-label="Runs needing operator resolution" className="flex flex-col gap-1.5">
            {ambiguousRuns.map((run) => (
              <AmbiguousRow key={`amb-${run.id}`} run={run} />
            ))}
          </ul>
        )}

        {unhealthyDecks.map((deck) => (
          <UnhealthyDeckRow key={`deck-${deck.id}`} deck={deck} />
        ))}

        {flappingDecks.map((flap) => (
          <FlapRow
            key={`flap-${flap.deck_id}`}
            deckId={flap.deck_id}
            transitions={flap.transitions}
            firstAt={flap.first_at}
          />
        ))}

        {openFailedRuns.map((run) => (
          <OpenFailedRow key={`fail-${run.id}`} run={run} />
        ))}
      </div>
    </section>
  );
}

interface RowShellProps {
  toneDot: string;
  icon: React.ReactNode;
  label: string;
  subject: React.ReactNode;
  meta?: React.ReactNode;
  cta: { to: string; label: string };
}

function RowShell({ toneDot, icon, label, subject, meta, cta }: RowShellProps) {
  return (
    <div className="flex items-center gap-3 rounded-md border border-line bg-surface px-3 py-2">
      <span className={cn("inline-block h-1.5 w-1.5 rounded-full", toneDot)} aria-hidden />
      <span className="inline-flex h-6 w-6 items-center justify-center text-ink-sub">{icon}</span>
      <span className="font-mono text-[10px] uppercase tracking-nav text-ink-sub">{label}</span>
      <div className="min-w-0 flex-1 text-[13px] text-ink">{subject}</div>
      {meta && (
        <div className="hidden font-mono text-[11px] tracking-nav text-ink-sub md:block">
          {meta}
        </div>
      )}
      <Link
        to={cta.to}
        className="inline-flex items-center gap-1 rounded-pill bg-surface-ink px-3 py-1 text-[12px] font-medium tracking-nav text-white transition-colors hover:bg-black"
      >
        {cta.label}
        <ArrowUpRight size={12} strokeWidth={2} />
      </Link>
    </div>
  );
}

function AmbiguousRow({ run }: { run: RunSummary }) {
  const count = run.deck_jobs_summary.by_status["AMBIGUOUS"] ?? 0;
  return (
    <li>
      <RowShell
        toneDot="bg-status-ambiguous"
        icon={<Wand2 size={14} strokeWidth={1.8} />}
        label="Ambiguous"
        subject={
          <span className="flex items-center gap-2">
            <span className="font-mono text-[12.5px] font-medium text-ink">{run.id}</span>
            <span className="text-ink-muted">
              {count} job{count === 1 ? "" : "s"} awaiting decision
            </span>
          </span>
        }
        cta={{ to: `/runs/${encodeURIComponent(run.id)}/resolve`, label: "Resolve" }}
      />
    </li>
  );
}

function UnhealthyDeckRow({ deck }: { deck: Deck }) {
  const ui = deckUiStatus(deck);
  const tone = ui === "UNREACHABLE" ? "bg-status-failed" : "bg-status-ambiguous";
  return (
    <RowShell
      toneDot={tone}
      icon={<Activity size={14} strokeWidth={1.8} />}
      label={ui}
      subject={
        <span className="flex items-center gap-2">
          <span className="font-mono text-[12.5px] font-medium text-ink">{deck.id}</span>
          <span className="text-ink-muted">
            last heartbeat {deck.last_heartbeat_at ? relativeAge(deck.last_heartbeat_at) : "never"}
          </span>
        </span>
      }
      cta={{ to: `/decks/${encodeURIComponent(deck.id)}`, label: "Open deck" }}
    />
  );
}

// Empty heartbeat sorts first — decks with no signal are most urgent.
function heartbeatMs(d: Deck): number {
  if (!d.last_heartbeat_at) return 0;
  return new Date(d.last_heartbeat_at).getTime();
}

function FlapRow({
  deckId,
  transitions,
  firstAt,
}: {
  deckId: string;
  transitions: number;
  firstAt: string;
}) {
  return (
    <RowShell
      toneDot="bg-status-ambiguous"
      icon={<Activity size={14} strokeWidth={1.8} />}
      label="Flapping"
      subject={
        <span className="flex items-center gap-2">
          <span className="font-mono text-[12.5px] font-medium text-ink">{deckId}</span>
          <span className="text-ink-muted">
            {transitions} health transitions since {relativeAge(firstAt)}
          </span>
        </span>
      }
      cta={{ to: `/decks/${encodeURIComponent(deckId)}`, label: "Open deck" }}
    />
  );
}

function OpenFailedRow({ run }: { run: RunSummary }) {
  const failedCount = run.deck_jobs_summary.by_status["FAILED"] ?? 0;
  return (
    <RowShell
      toneDot="bg-status-failed"
      icon={<XCircle size={14} strokeWidth={1.8} />}
      label="Failed"
      subject={
        <span className="flex items-center gap-2">
          <span className="font-mono text-[12.5px] font-medium text-ink">{run.id}</span>
          <span className="text-ink-muted">
            {failedCount} job{failedCount === 1 ? "" : "s"} failed · awaiting retry or cancel
          </span>
        </span>
      }
      cta={{ to: `/runs/${encodeURIComponent(run.id)}`, label: "Open run" }}
    />
  );
}

function useNowTick(intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

interface FlapEntry {
  deck_id: string;
  transitions: number;
  first_at: string;
}

/**
 * Counts DECK_HEALTH_CHANGED events per deck within the rolling
 * `FLAP_WINDOW_MS`. Decks that already appear in the unhealthy list
 * are excluded — flapping is the surface for "passed-through churn"
 * not "currently broken."
 */
function detectFlappingDecks(
  events: ReadonlyArray<Event>,
  unhealthyDecks: ReadonlyArray<Deck>,
  now: number,
): FlapEntry[] {
  const cutoff = now - FLAP_WINDOW_MS;
  const exclude = new Set(unhealthyDecks.map((d) => d.id));
  const counts = new Map<string, { count: number; first: number }>();
  for (const e of events) {
    if (e.kind !== "DECK_HEALTH_CHANGED") continue;
    if (!e.deck_id || exclude.has(e.deck_id)) continue;
    const ts = new Date(e.occurred_at).getTime();
    if (ts < cutoff) continue;
    const cur = counts.get(e.deck_id);
    if (cur) {
      cur.count += 1;
      cur.first = Math.min(cur.first, ts);
    } else {
      counts.set(e.deck_id, { count: 1, first: ts });
    }
  }
  const out: FlapEntry[] = [];
  for (const [deck_id, { count, first }] of counts) {
    if (count >= FLAP_MIN_TRANSITIONS) {
      out.push({
        deck_id,
        transitions: count,
        first_at: new Date(first).toISOString(),
      });
    }
  }
  return out.sort((a, b) => b.transitions - a.transitions);
}
