import { Link, useNavigate } from "react-router-dom";
import { PulseSegmentBar } from "@/components/console/fleet/PulseSegmentBar";
import {
  pulseBucketFor,
  type PulseBucket,
  type PulseCounts,
  type PulseFilter,
} from "@/components/console/fleet/pulse-helpers";
import { deckUiStatus, isDeckHeldByAmbiguous } from "@/lib/ui-derive";
import type { Deck } from "@/lib/api-types";
import { cn } from "@/lib/cn";

interface FleetPulseStripProps {
  decks: ReadonlyArray<Deck>;
}

/**
 * Dashboard fleet snapshot; segments/buttons deep-link to `/fleet/grid?filter=`.
 */
export function FleetPulseStrip({ decks }: FleetPulseStripProps) {
  const navigate = useNavigate();
  const counts = bucketCounts(decks);
  const total = decks.length;

  const handleBucket = (bucket: PulseFilter) => {
    if (bucket === "all" || total === 0) return;
    navigate(`/fleet/grid?filter=${bucket}`);
  };

  if (total === 0) {
    return (
      <section
        aria-label="Fleet pulse"
        className="flex flex-col gap-2 rounded-panel border border-dashed border-line bg-surface p-4"
      >
        <span className="font-mono text-eyebrow uppercase text-ink-sub">Fleet pulse</span>
        <p className="text-[13px] leading-5 text-ink-muted">
          No executors attached.{" "}
          <Link
            to="/settings/fleet"
            className="text-accent-link hover:text-accent-linkAlt hover:underline"
          >
            Open Settings &gt; Fleet Management
          </Link>{" "}
          to attach one.
        </p>
      </section>
    );
  }

  return (
    <section
      aria-label="Fleet pulse"
      className="flex flex-col gap-3 rounded-panel border border-line bg-surface p-4"
    >
      <div className="flex items-center justify-between gap-3">
        <span className="font-mono text-eyebrow uppercase text-ink-sub">Fleet pulse</span>
        <span className="font-mono text-[11px] tracking-nav text-ink-sub">
          {total} {total === 1 ? "deck attached" : "decks attached"}
        </span>
      </div>
      <PulseSegmentBar counts={counts} total={total} active="all" onChange={handleBucket} />
      <div className="grid grid-cols-3 gap-2">
        <BucketButton
          tone="attention"
          label="Needs attention"
          count={counts.attention}
          onClick={() => handleBucket("attention")}
        />
        <BucketButton
          tone="active"
          label="Active"
          count={counts.active}
          onClick={() => handleBucket("active")}
        />
        <BucketButton
          tone="idle"
          label="Idle"
          count={counts.idle}
          onClick={() => handleBucket("idle")}
        />
      </div>
    </section>
  );
}

function bucketCounts(decks: ReadonlyArray<Deck>): PulseCounts {
  let attention = 0;
  let active = 0;
  let idle = 0;
  for (const d of decks) {
    const bucket = pulseBucketFor(deckUiStatus(d), isDeckHeldByAmbiguous(d));
    if (bucket === "attention") attention += 1;
    else if (bucket === "active") active += 1;
    else idle += 1;
  }
  return { attention, active, idle };
}

const TONE_DOT: Record<PulseBucket, string> = {
  attention: "bg-status-unreachable",
  active: "bg-status-running",
  idle: "bg-status-healthy",
};

interface BucketButtonProps {
  tone: PulseBucket;
  label: string;
  count: number;
  onClick: () => void;
}

function BucketButton({ tone, label, count, onClick }: BucketButtonProps) {
  const interactive = count > 0;
  return (
    <button
      type="button"
      disabled={!interactive}
      onClick={onClick}
      aria-label={`${label}: ${count}. Open in /fleet/grid filtered by ${tone}.`}
      className={cn(
        "flex flex-col items-start gap-1 rounded-lg border border-line bg-surface px-3 py-2 text-left transition-colors duration-150 ease-out-soft",
        interactive ? "hover:border-ink/30 hover:bg-surface-warm" : "cursor-default opacity-60",
      )}
    >
      <span className="flex items-center gap-1.5 font-mono text-[11px] uppercase tracking-nav text-ink-sub">
        <span className={cn("inline-block h-1.5 w-1.5 rounded-full", TONE_DOT[tone])} />
        {label}
      </span>
      <span className="font-mono text-[22px] font-semibold leading-none text-ink">{count}</span>
    </button>
  );
}
