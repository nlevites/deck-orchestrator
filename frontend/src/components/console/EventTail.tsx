import { useMemo } from "react";
import {
  Activity,
  AlertTriangle,
  CheckCircle2,
  CircleDot,
  Play,
  Wand2,
  Workflow,
  XCircle,
} from "lucide-react";
import { EventFilterChips } from "@/components/console/EventFilterChips";
import type { Event, EventKind } from "@/lib/api-types";
import { familyForKind } from "@/lib/event-filter/families";
import { useEventFilter } from "@/lib/event-filter/use-event-filter";
import { shortClockWithSeconds } from "@/lib/format";
import { cn } from "@/lib/cn";

interface EventTailProps {
  events: Event[];
  density?: "compact" | "comfortable";
}

/**
 * Live event log (newest first). Adjacent DECK_HEALTH_CHANGED rows for
 * the same deck coalesce into one flap row so a bouncing deck doesn't
 * flood the tail. Filter before coalescing so hidden flaps don't leave
 * phantom stubs. Chip counts use the unfiltered slice.
 */
export function EventTail({ events, density = "comfortable" }: EventTailProps) {
  const { active, toggle, reset } = useEventFilter();

  // Filter BEFORE coalescing: a flap run that's hidden shouldn't
  // show as a phantom "0 transitions" stub.
  const filtered = useMemo(
    () => events.filter((e) => active.has(familyForKind(e.kind))),
    [events, active],
  );
  const items = useMemo(() => coalesceEvents(filtered), [filtered]);

  return (
    <div>
      <div
        className={cn("border-b border-line", density === "compact" ? "px-3 py-2" : "px-4 py-3")}
      >
        <EventFilterChips events={events} active={active} onToggle={toggle} onReset={reset} />
      </div>
      {events.length === 0 ? (
        <div className="px-5 py-8 text-center text-[13px] text-ink-muted">No events yet.</div>
      ) : items.length === 0 ? (
        <div className="flex flex-wrap items-center justify-center gap-2 px-5 py-8 text-[13px] text-ink-muted">
          <span>No events match the active filter.</span>
          <button
            type="button"
            onClick={reset}
            className="text-[13px] font-medium tracking-nav text-accent-link hover:text-accent-linkAlt hover:underline"
          >
            Show all
          </button>
        </div>
      ) : (
        <ul className="divide-y divide-line" aria-label="Event log">
          {items.map((item, i) =>
            item.kind === "single" ? (
              <SingleRow
                key={`s-${item.event.seq}`}
                event={item.event}
                density={density}
                isFresh={i === 0}
              />
            ) : (
              <FlapRow key={`f-${item.head.seq}`} item={item} density={density} isFresh={i === 0} />
            ),
          )}
        </ul>
      )}
    </div>
  );
}

interface SingleRowProps {
  event: Event;
  density: "compact" | "comfortable";
  isFresh: boolean;
}

function SingleRow({ event: e, density, isFresh }: SingleRowProps) {
  return (
    <li
      className={cn(
        "border-l-2 transition-colors",
        density === "compact" ? "px-4 py-2" : "px-5 py-3",
        borderForKind(e.kind),
        isFresh && "animate-fade-up",
      )}
      aria-label={`Event ${e.kind} seq ${e.seq}`}
    >
      <div className="flex items-center gap-2">
        <KindIcon kind={e.kind} className={cn("shrink-0", kindLabelTone(e.kind))} />
        <span className="font-mono text-[10px] uppercase tracking-[0.12em] text-ink-sub">
          #{e.seq}
        </span>
        <span className="font-mono text-[10px] tracking-nav text-ink-sub">
          {shortClockWithSeconds(e.occurred_at)}
        </span>
        <span
          className={cn("font-mono text-[10px] font-medium tracking-nav", kindLabelTone(e.kind))}
        >
          {e.kind}
        </span>
      </div>
      <div
        className={cn("mt-0.5 text-ink", density === "compact" ? "text-[12.5px]" : "text-[13px]")}
      >
        {describeEvent(e)}
      </div>
    </li>
  );
}

interface FlapItem {
  kind: "flap";
  head: Event;
  tail: Event;
  count: number;
  deck_id: string;
}

interface FlapRowProps {
  item: FlapItem;
  density: "compact" | "comfortable";
  isFresh: boolean;
}

function FlapRow({ item, density, isFresh }: FlapRowProps) {
  const headTs = new Date(item.head.occurred_at).getTime();
  const tailTs = new Date(item.tail.occurred_at).getTime();
  const spanMs = Math.max(0, headTs - tailTs);
  return (
    <li
      className={cn(
        "border-l-2 border-l-status-ambiguous bg-surface-subtle/40 transition-colors",
        density === "compact" ? "px-4 py-2" : "px-5 py-3",
        isFresh && "animate-fade-up",
      )}
      aria-label={`Event flap deck ${item.deck_id} ${item.count} transitions`}
    >
      <div className="flex items-center gap-2">
        <Activity size={12} strokeWidth={1.8} className="shrink-0 text-status-ambiguous" />
        <span className="font-mono text-[10px] uppercase tracking-[0.12em] text-ink-sub">
          #{item.head.seq}–#{item.tail.seq}
        </span>
        <span className="font-mono text-[10px] tracking-nav text-ink-sub">
          {shortClockWithSeconds(item.head.occurred_at)}
        </span>
        <span className="font-mono text-[10px] font-medium tracking-nav text-status-ambiguous">
          DECK_HEALTH_FLAPPING
        </span>
      </div>
      <div
        className={cn("mt-0.5 text-ink", density === "compact" ? "text-[12.5px]" : "text-[13px]")}
      >
        deck={item.deck_id} : {item.count} health transitions in {formatSpan(spanMs)}
      </div>
    </li>
  );
}

const FLAP_MIN_TRANSITIONS = 3;

interface SingleItem {
  kind: "single";
  event: Event;
}

type EventItem = SingleItem | FlapItem;

/**
 * Walks the event list (already newest-first) and collapses runs of
 * adjacent `DECK_HEALTH_CHANGED` events for the same deck into a
 * single FlapItem when the run length meets `FLAP_MIN_TRANSITIONS`.
 * Shorter runs and unrelated events pass through untouched.
 */
function coalesceEvents(events: ReadonlyArray<Event>): EventItem[] {
  const out: EventItem[] = [];
  let i = 0;
  while (i < events.length) {
    const e = events[i];
    if (e.kind === "DECK_HEALTH_CHANGED" && e.deck_id) {
      let j = i + 1;
      while (
        j < events.length &&
        events[j].kind === "DECK_HEALTH_CHANGED" &&
        events[j].deck_id === e.deck_id
      ) {
        j += 1;
      }
      const runLen = j - i;
      if (runLen >= FLAP_MIN_TRANSITIONS) {
        out.push({
          kind: "flap",
          head: events[i],
          tail: events[j - 1],
          count: runLen,
          deck_id: e.deck_id,
        });
        i = j;
        continue;
      }
    }
    out.push({ kind: "single", event: e });
    i += 1;
  }
  return out;
}

function formatSpan(ms: number): string {
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return `${sec}s`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m`;
  const hr = Math.floor(min / 60);
  return `${hr}h`;
}

function describeEvent(e: Event): string {
  const parts: string[] = [];
  if (e.run_id) parts.push(`run=${e.run_id}`);
  if (e.job_id) parts.push(`job=${e.job_id}`);
  if (e.deck_id) parts.push(`deck=${e.deck_id}`);
  const scope = parts.join(" · ");

  switch (e.kind) {
    case "RUN_STATUS_CHANGED": {
      const from = payloadString(e.payload, "from");
      const to = payloadString(e.payload, "to");
      return `${scope} : ${from} → ${to}`;
    }
    case "DECK_HEALTH_CHANGED": {
      const from = payloadString(e.payload, "from");
      const to = payloadString(e.payload, "to");
      return `${scope} : ${from} → ${to}`;
    }
    case "JOB_FAILED": {
      const err = payloadString(e.payload, "error");
      return err ? `${scope} : ${err}` : scope;
    }
    case "JOB_AMBIGUOUS": {
      const reason = payloadString(e.payload, "reason");
      return reason ? `${scope} : ${reason.toLowerCase().replace(/_/g, " ")}` : scope;
    }
    case "JOB_RESOLVED": {
      const resolution = payloadString(e.payload, "resolution");
      return resolution ? `${scope} : resolved as ${resolution.toLowerCase()}` : scope;
    }
    case "EXECUTOR_CONFLICT_LOGGED": {
      const reported = payloadString(e.payload, "executor_reported");
      return reported ? `${scope} : executor reported ${reported}` : scope;
    }
    default:
      return scope || e.kind;
  }
}

function payloadString(payload: Record<string, unknown> | undefined, key: string): string {
  if (!payload) return "";
  const v = payload[key];
  return typeof v === "string" ? v : "";
}

function borderForKind(kind: EventKind): string {
  switch (kind) {
    case "JOB_AMBIGUOUS":
      return "border-l-status-ambiguous";
    case "JOB_FAILED":
    case "EXECUTOR_CONFLICT_LOGGED":
      return "border-l-status-failed";
    case "JOB_COMPLETED":
      return "border-l-status-completed";
    case "JOB_RUNNING":
    case "JOB_DISPATCHED":
      return "border-l-status-running";
    case "DECK_HEALTH_CHANGED":
      return "border-l-status-ambiguous";
    default:
      return "border-l-transparent";
  }
}

function kindLabelTone(kind: EventKind): string {
  switch (kind) {
    case "JOB_AMBIGUOUS":
      return "text-status-ambiguous";
    case "JOB_FAILED":
    case "EXECUTOR_CONFLICT_LOGGED":
      return "text-status-failed";
    case "JOB_COMPLETED":
      return "text-status-completed";
    case "JOB_RUNNING":
    case "JOB_DISPATCHED":
      return "text-status-running";
    case "DECK_HEALTH_CHANGED":
      return "text-status-ambiguous";
    default:
      return "text-ink-sub";
  }
}

/**
 * Family icon per event kind. Pairs with the left-border tint so a
 * colorblind operator can still tell the families apart by shape.
 */
function KindIcon({ kind, className }: { kind: EventKind; className?: string }) {
  const props = { size: 12, strokeWidth: 1.8, className } as const;
  switch (kind) {
    case "JOB_AMBIGUOUS":
      return <AlertTriangle {...props} />;
    case "JOB_FAILED":
    case "EXECUTOR_CONFLICT_LOGGED":
      return <XCircle {...props} />;
    case "JOB_COMPLETED":
      return <CheckCircle2 {...props} />;
    case "JOB_RUNNING":
    case "JOB_DISPATCHED":
      return <Play {...props} />;
    case "JOB_RESOLVED":
      return <Wand2 {...props} />;
    case "DECK_HEALTH_CHANGED":
      return <Activity {...props} />;
    case "RUN_STATUS_CHANGED":
      return <Workflow {...props} />;
    default:
      return <CircleDot {...props} />;
  }
}
