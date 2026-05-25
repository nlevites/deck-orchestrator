import { cn } from "@/lib/cn";
import type { PulseBucket, PulseCounts, PulseFilter } from "./pulse-helpers";

interface PulseFilterChipsProps {
  counts: PulseCounts;
  active: PulseFilter;
  onChange: (next: PulseFilter) => void;
  className?: string;
}

/**
 * Sticky toolbar bucket filter; clicking an active chip clears back to "all".
 */
export function PulseFilterChips({ counts, active, onChange, className }: PulseFilterChipsProps) {
  return (
    <div
      role="group"
      aria-label="Filter decks by bucket"
      className={cn("flex flex-wrap items-center gap-1.5", className)}
    >
      <Chip
        bucket="attention"
        label="Needs attention"
        count={counts.attention}
        active={active}
        onChange={onChange}
        tone="attention"
      />
      <Chip
        bucket="active"
        label="Active"
        count={counts.active}
        active={active}
        onChange={onChange}
        tone="active"
      />
      <Chip
        bucket="idle"
        label="Idle"
        count={counts.idle}
        active={active}
        onChange={onChange}
        tone="idle"
      />
    </div>
  );
}

type Tone = "attention" | "active" | "idle";

const DOT_TONE: Record<Tone, string> = {
  attention: "bg-status-unreachable",
  active: "bg-status-running",
  idle: "bg-status-healthy",
};

interface ChipProps {
  bucket: PulseBucket;
  label: string;
  count: number;
  active: PulseFilter;
  onChange: (next: PulseFilter) => void;
  tone: Tone;
}

function Chip({ bucket, label, count, active, onChange, tone }: ChipProps) {
  const isActive = active === bucket;
  const isMuted = active !== "all" && !isActive;
  const interactive = count > 0 || isActive;
  return (
    <button
      type="button"
      disabled={!interactive}
      onClick={() => onChange(isActive ? "all" : bucket)}
      aria-pressed={isActive}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-pill border px-3 py-1 text-[12px] font-medium tracking-nav transition-colors",
        isActive ? "border-ink/60 bg-surface-ink text-white" : "border-line bg-surface text-ink",
        isMuted && "opacity-60",
        !interactive && "cursor-default opacity-50",
        interactive && !isActive && "hover:border-ink/30 hover:bg-surface-warm",
      )}
    >
      <span className={cn("h-1.5 w-1.5 rounded-full", DOT_TONE[tone])} />
      <span>{label}</span>
      <span className={cn("font-mono text-[11px]", isActive ? "text-white/80" : "text-ink-sub")}>
        {count}
      </span>
    </button>
  );
}
