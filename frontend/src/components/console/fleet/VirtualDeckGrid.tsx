/**
 * Row-virtualized deck grid for active/attention sections. Below
 * `threshold`, renders eagerly — virtualizer overhead isn't worth it
 * and a scroll container changes layout. Fixed `cols` in the virtualized
 * branch keeps row heights predictable.
 */
import { useMemo, useRef } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { Deck } from "@/lib/api-types";

interface VirtualDeckGridProps {
  decks: Deck[];
  cols: number;
  rowHeightPx: number;
  rowGapPx?: number;
  threshold?: number;
  renderCard: (deck: Deck) => React.ReactNode;
  eagerGridClassName: string;
}

export function VirtualDeckGrid({
  decks,
  cols,
  rowHeightPx,
  rowGapPx = 12,
  threshold = 12,
  renderCard,
  eagerGridClassName,
}: VirtualDeckGridProps) {
  const scrollRef = useRef<HTMLDivElement | null>(null);

  const rows = useMemo(() => {
    const out: Deck[][] = [];
    for (let i = 0; i < decks.length; i += cols) {
      out.push(decks.slice(i, i + cols));
    }
    return out;
  }, [decks, cols]);

  const shouldVirtualize = decks.length > threshold;

  // eslint-disable-next-line react-hooks/incompatible-library
  const virt = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeightPx + rowGapPx,
    overscan: 2,
    enabled: shouldVirtualize,
  });

  if (!shouldVirtualize) {
    return <div className={eagerGridClassName}>{decks.map((d) => renderCard(d))}</div>;
  }

  return (
    <div
      ref={scrollRef}
      // ~5 rows before scroll; tuned for active card height.
      className="max-h-[640px] overflow-auto rounded-panel border border-line bg-surface px-3 py-3"
    >
      <div className="relative w-full" style={{ height: virt.getTotalSize() }}>
        {virt.getVirtualItems().map((vrow) => {
          const row = rows[vrow.index];
          if (!row) return null;
          return (
            <div
              key={vrow.key}
              className="absolute left-0 right-0 grid gap-3"
              style={{
                top: vrow.start,
                height: rowHeightPx,
                gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`,
              }}
            >
              {row.map((deck) => renderCard(deck))}
            </div>
          );
        })}
      </div>
    </div>
  );
}
