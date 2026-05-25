import { cn } from "@/lib/cn";
import { CELL_TONE, describeCell, shortLabel, toneFor, type SlotRow } from "./slot-state";

interface HeatmapCellProps {
  slot: SlotRow;
  selected: boolean;
  onSelect: (anchor: DOMRect) => void;
}

export function HeatmapCell({ slot, selected, onSelect }: HeatmapCellProps) {
  const tone = toneFor(slot);
  const visual = CELL_TONE[tone];
  const label = shortLabel(slot.deckId);
  // The sub-second TimeAgo on the cell isn't worth the re-render cost
  // for ~100 cells. The popover gets the live heartbeat ticker.
  const ariaLabel = describeCell(slot, visual.label);

  return (
    <button
      type="button"
      role="gridcell"
      aria-label={ariaLabel}
      title={ariaLabel}
      onClick={(e) => onSelect(e.currentTarget.getBoundingClientRect())}
      className={cn(
        "relative h-10 w-10 rounded-md border font-mono text-[12px] leading-none",
        "flex items-center justify-center",
        "transition-shadow duration-150 ease-out-soft",
        "focus:outline-none focus-visible:ring-2 focus-visible:ring-ink/40 focus-visible:ring-offset-1 focus-visible:ring-offset-surface-subtle",
        visual.bg,
        visual.fg,
        visual.border,
        visual.pulse && "animate-pulse-slow",
        selected && "ring-2 ring-ink ring-offset-1 ring-offset-surface-subtle",
      )}
    >
      {label}
    </button>
  );
}
