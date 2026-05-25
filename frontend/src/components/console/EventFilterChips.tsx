import type { Event } from "@/lib/api-types";
import {
  familyForKind,
  FAMILIES,
  FAMILY_LABEL,
  type EventFamily,
} from "@/lib/event-filter/families";
import { cn } from "@/lib/cn";

interface EventFilterChipsProps {
  events: ReadonlyArray<Event>;
  active: ReadonlySet<EventFamily>;
  onToggle: (family: EventFamily) => void;
  onReset: () => void;
  className?: string;
}

/**
 * Family filter chips; counts come from the unfiltered slice so the
 * operator sees what they're hiding. "All" re-enables every family.
 */
export function EventFilterChips({
  events,
  active,
  onToggle,
  onReset,
  className,
}: EventFilterChipsProps) {
  const counts = familyCounts(events);
  const isAll = active.size === FAMILIES.length;

  return (
    <div
      role="group"
      aria-label="Event family filter"
      className={cn("flex flex-wrap items-center gap-1.5", className)}
    >
      <button
        type="button"
        onClick={onReset}
        aria-pressed={isAll}
        className={cn(
          "inline-flex items-center gap-1.5 rounded-pill border px-3 py-1 text-[12px] font-medium tracking-nav transition-colors",
          isAll
            ? "border-ink/60 bg-surface-ink text-white"
            : "border-line bg-surface text-ink hover:border-ink/30 hover:bg-surface-warm",
        )}
      >
        <span>All</span>
        <span className={cn("font-mono text-[11px]", isAll ? "text-white/80" : "text-ink-sub")}>
          {events.length}
        </span>
      </button>
      {FAMILIES.map((family) => (
        <Chip
          key={family}
          family={family}
          label={FAMILY_LABEL[family]}
          count={counts[family]}
          isActive={active.has(family)}
          isAllOn={isAll}
          onToggle={() => onToggle(family)}
        />
      ))}
    </div>
  );
}

const DOT_TONE: Record<EventFamily, string> = {
  runs: "bg-status-running",
  jobs: "bg-status-completed",
  health: "bg-status-ambiguous",
  resolutions: "bg-accent-link",
  other: "bg-line-strong",
};

interface ChipProps {
  family: EventFamily;
  label: string;
  count: number;
  isActive: boolean;
  isAllOn: boolean;
  onToggle: () => void;
}

function Chip({ family, label, count, isActive, isAllOn, onToggle }: ChipProps) {
  // When every family is enabled (the default), no chip is "muted" —
  // it's the resting state, not a filter. Once the operator narrows
  // the set, disabled families dim so the active subset stands out.
  const isMuted = !isAllOn && !isActive;
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-pressed={isActive}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-pill border px-3 py-1 text-[12px] font-medium tracking-nav transition-colors",
        isActive && !isAllOn
          ? "border-ink/60 bg-surface-ink text-white"
          : "border-line bg-surface text-ink hover:border-ink/30 hover:bg-surface-warm",
        isMuted && "opacity-60",
      )}
    >
      <span className={cn("h-1.5 w-1.5 rounded-full", DOT_TONE[family])} />
      <span>{label}</span>
      <span
        className={cn(
          "font-mono text-[11px]",
          isActive && !isAllOn ? "text-white/80" : "text-ink-sub",
        )}
      >
        {count}
      </span>
    </button>
  );
}

function familyCounts(events: ReadonlyArray<Event>): Record<EventFamily, number> {
  const out: Record<EventFamily, number> = {
    runs: 0,
    jobs: 0,
    health: 0,
    resolutions: 0,
    other: 0,
  };
  for (const e of events) {
    out[familyForKind(e.kind)] += 1;
  }
  return out;
}
