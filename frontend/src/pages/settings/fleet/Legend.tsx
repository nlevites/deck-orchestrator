import { cn } from "@/lib/cn";
import { CELL_TONE, LEGEND_ORDER, type CellTone } from "./slot-state";

export function Legend({ counts }: { counts: Record<CellTone, number> }) {
  return (
    <ul className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
      {LEGEND_ORDER.map((tone) => {
        const v = CELL_TONE[tone];
        return (
          <li
            key={tone}
            className="inline-flex items-center gap-1.5 text-[11.5px] tracking-nav text-ink-muted"
          >
            <span
              className={cn("inline-block h-3.5 w-3.5 rounded-sm border", v.bg, v.border)}
              aria-hidden
            />
            <span>{v.label}</span>
            <span className="font-mono text-ink-sub">{counts[tone]}</span>
          </li>
        );
      })}
    </ul>
  );
}
