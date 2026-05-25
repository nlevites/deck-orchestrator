import { useId } from "react";
import { cn } from "@/lib/cn";
import type { PulseBucket, PulseCounts, PulseFilter } from "./pulse-helpers";

interface PulseSegmentBarProps {
  counts: PulseCounts;
  total: number;
  active: PulseFilter;
  onChange: (next: PulseFilter) => void;
  className?: string;
}

/**
 * Proportional fleet bucket bar; each segment click-filters the grid.
 * Hides on zero-deck fleet.
 */
export function PulseSegmentBar({
  counts,
  total,
  active,
  onChange,
  className,
}: PulseSegmentBarProps) {
  const groupId = useId();
  if (total === 0) return null;

  return (
    <div
      id={groupId}
      role="group"
      aria-label="Fleet bucket distribution"
      className={cn("flex h-2 w-full overflow-hidden rounded-pill bg-line", className)}
    >
      <PulseSegment
        bucket="attention"
        count={counts.attention}
        total={total}
        active={active}
        onChange={onChange}
        tone="attention"
      />
      <PulseSegment
        bucket="active"
        count={counts.active}
        total={total}
        active={active}
        onChange={onChange}
        tone="active"
      />
      <PulseSegment
        bucket="idle"
        count={counts.idle}
        total={total}
        active={active}
        onChange={onChange}
        tone="idle"
      />
    </div>
  );
}

type Tone = "attention" | "active" | "idle";

const SEGMENT_TONE: Record<Tone, string> = {
  attention: "bg-status-unreachable",
  active: "bg-status-running",
  idle: "bg-status-healthy",
};

const SEGMENT_TOOLTIP: Record<Tone, string> = {
  attention: "Needs attention — Stale, Unreachable, Recovering, or holding an AMBIGUOUS job",
  active: "Active — HEALTHY_BUSY (DISPATCHED or RUNNING deck_job)",
  idle: "Idle — HEALTHY_IDLE (no job currently pinned to the deck)",
};

interface PulseSegmentProps {
  bucket: PulseBucket;
  count: number;
  total: number;
  active: PulseFilter;
  onChange: (next: PulseFilter) => void;
  tone: Tone;
}

function PulseSegment({ bucket, count, total, active, onChange, tone }: PulseSegmentProps) {
  if (count === 0) return null;
  const pct = (count / total) * 100;
  const isActive = active === bucket;
  const tip = `${SEGMENT_TOOLTIP[tone]} — ${count} ${count === 1 ? "deck" : "decks"}`;
  return (
    <button
      type="button"
      onClick={() => onChange(isActive ? "all" : bucket)}
      aria-label={tip}
      aria-pressed={isActive}
      title={tip}
      style={{ flexBasis: `${pct}%` }}
      className={cn(
        "h-full transition-opacity duration-150 ease-out-soft",
        SEGMENT_TONE[tone],
        active === "all" || isActive ? "opacity-100" : "opacity-30",
      )}
    />
  );
}
