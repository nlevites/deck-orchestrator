import { useMemo, useRef } from "react";
import { Link } from "react-router-dom";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { Deck } from "@/lib/api-types";
import { deckUiStatus } from "@/lib/ui-derive";
import { relativeAge } from "@/lib/format";
import { cn } from "@/lib/cn";

interface IdleDeckPillProps {
  deck: Deck;
}

/**
 * Single-line idle deck pill. Satisfies e2e `aria-label^="Deck deck-1,"`
 * without competing visually with attention cards.
 */
export function IdleDeckPill({ deck }: IdleDeckPillProps) {
  const uiStatus = deckUiStatus(deck);
  const heartbeat = deck.last_heartbeat_at ? relativeAge(deck.last_heartbeat_at) : "never";
  return (
    <Link
      to={`/decks/${encodeURIComponent(deck.id)}`}
      aria-label={`Deck ${deck.id}, status ${uiStatus}`}
      title={`${deck.id} · idle · last heartbeat ${heartbeat}`}
      className={cn(
        "group inline-flex h-7 items-center gap-2 rounded-pill border border-line bg-surface px-2.5",
        "text-[12px] tracking-nav text-ink",
        "transition-colors duration-150 ease-out-soft",
        "hover:border-ink/30 hover:bg-surface-warm",
        "focus:outline-none focus-visible:ring-2 focus-visible:ring-ink/20",
      )}
    >
      <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-status-healthy" aria-hidden="true" />
      <span className="font-mono text-[11.5px] text-ink">{deck.id}</span>
    </Link>
  );
}

interface IdleHeatmapProps {
  decks: Deck[];
}

const COLUMNS = 8;
const ROW_HEIGHT_PX = 36;
const ROW_GAP_PX = 8;

/**
 * Row-virtualized idle pill grid; renders eagerly below ~24 decks.
 */
export function IdleHeatmap({ decks }: IdleHeatmapProps) {
  const scrollRef = useRef<HTMLDivElement | null>(null);

  const rows = useMemo(() => {
    const out: Deck[][] = [];
    for (let i = 0; i < decks.length; i += COLUMNS) {
      out.push(decks.slice(i, i + COLUMNS));
    }
    return out;
  }, [decks]);

  const shouldVirtualize = rows.length > 4;

  // eslint-disable-next-line react-hooks/incompatible-library
  const virt = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT_PX + ROW_GAP_PX,
    overscan: 4,
    enabled: shouldVirtualize,
  });

  if (decks.length === 0) {
    return (
      <div className="rounded-panel border border-dashed border-line bg-surface-subtle px-5 py-6 text-center text-[13px] text-ink-muted">
        No idle decks.
      </div>
    );
  }

  if (!shouldVirtualize) {
    return (
      <div
        className="grid gap-2"
        style={{ gridTemplateColumns: `repeat(${COLUMNS}, minmax(0, 1fr))` }}
      >
        {decks.map((d) => (
          <IdleDeckPill key={d.id} deck={d} />
        ))}
      </div>
    );
  }

  return (
    <div
      ref={scrollRef}
      className="max-h-[420px] overflow-auto rounded-panel border border-line bg-surface px-3 py-3"
    >
      <div className="relative w-full" style={{ height: virt.getTotalSize() }}>
        {virt.getVirtualItems().map((vrow) => {
          const row = rows[vrow.index];
          if (!row) return null;
          return (
            <div
              key={vrow.key}
              className="absolute left-0 right-0 grid gap-2"
              style={{
                top: vrow.start,
                height: ROW_HEIGHT_PX,
                gridTemplateColumns: `repeat(${COLUMNS}, minmax(0, 1fr))`,
              }}
            >
              {row.map((deck) => (
                <IdleDeckPill key={deck.id} deck={deck} />
              ))}
            </div>
          );
        })}
      </div>
    </div>
  );
}
