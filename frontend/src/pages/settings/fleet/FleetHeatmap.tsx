import { useMemo, useState } from "react";
import { cn } from "@/lib/cn";
import { AttachModal } from "./AttachModal";
import { DetachModal } from "./DetachModal";
import { HeatmapCell } from "./HeatmapCell";
import { Legend } from "./Legend";
import { ReleaseModal } from "./ReleaseModal";
import { SlotPopover } from "./SlotPopover";
import { toneFor, type CellTone, type SlotRow } from "./slot-state";

interface FleetHeatmapProps {
  slots: SlotRow[];
  supervisorReachable: boolean;
}

export function FleetHeatmap({ slots, supervisorReachable }: FleetHeatmapProps) {
  const counts = useMemo(() => {
    const by: Record<CellTone, number> = {
      empty: 0,
      starting: 0,
      running: 0,
      stale: 0,
      unreachable: 0,
      stopped: 0,
      fatal: 0,
    };
    for (const s of slots) by[toneFor(s)] += 1;
    return by;
  }, [slots]);
  const attached = slots.length - counts.empty;
  const unhealthy = counts.stale + counts.unreachable + counts.fatal;

  const [selected, setSelected] = useState<{
    slot: SlotRow;
    anchor: DOMRect;
  } | null>(null);
  // Modal state is held HERE (the heatmap parent), not inside
  // SlotPopover, because the modals render via createPortal and live
  // in a sibling DOM tree. If they were children of the popover,
  // the popover's outside-click handler (which checks `ref.contains`)
  // would close the popover on any modal click — which unmounts the
  // modal before the click can fire. Lifting them keeps the modal
  // alive independent of the popover's lifecycle.
  const [attachFor, setAttachFor] = useState<string | null>(null);
  const [detachFor, setDetachFor] = useState<string | null>(null);
  const [releaseFor, setReleaseFor] = useState<string | null>(null);

  return (
    <div className="flex flex-col gap-3">
      <header className="flex flex-wrap items-baseline justify-between gap-3">
        <h2 className="text-[15px] font-semibold tracking-sub text-ink">Fleet</h2>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 font-mono text-[12px] tracking-nav text-ink-muted">
          <span>
            {slots.length} {slots.length === 1 ? "slot" : "slots"}
          </span>
          <span aria-hidden>·</span>
          <span>{attached} attached</span>
          <span aria-hidden>·</span>
          <span className={cn(unhealthy > 0 && "text-status-failed")}>{unhealthy} unhealthy</span>
        </div>
      </header>

      <Legend counts={counts} />

      <div
        role="grid"
        aria-label={`Fleet of ${slots.length} deck slots`}
        className="grid gap-1 rounded-panel border border-line bg-surface-subtle p-3"
        style={{ gridTemplateColumns: "repeat(auto-fill, 40px)" }}
      >
        {slots.map((slot) => (
          <HeatmapCell
            key={slot.deckId}
            slot={slot}
            selected={selected?.slot.deckId === slot.deckId}
            onSelect={(anchor) => setSelected({ slot, anchor })}
          />
        ))}
      </div>

      <p className="text-[11.5px] text-ink-sub">
        Process state from the supervisor sidecar; health from the orchestrator. Click any cell to
        inspect or act.
      </p>

      {selected ? (
        <SlotPopover
          slot={selected.slot}
          anchor={selected.anchor}
          supervisorReachable={supervisorReachable}
          onClose={() => setSelected(null)}
          onRequestAttach={(deckId) => {
            setSelected(null);
            setAttachFor(deckId);
          }}
          onRequestDetach={(deckId) => {
            setSelected(null);
            setDetachFor(deckId);
          }}
          onRequestRelease={(deckId) => {
            setSelected(null);
            setReleaseFor(deckId);
          }}
        />
      ) : null}

      {/* Modals are conditionally mounted so their internal state
          (Fresh state checkbox) resets between opens. */}
      {attachFor !== null && <AttachModal deckId={attachFor} onClose={() => setAttachFor(null)} />}
      {detachFor !== null && <DetachModal deckId={detachFor} onClose={() => setDetachFor(null)} />}
      {releaseFor !== null && (
        <ReleaseModal deckId={releaseFor} onClose={() => setReleaseFor(null)} />
      )}
    </div>
  );
}
