import { useEffect, useRef } from "react";
import { createPortal } from "react-dom";
import { StatusPill } from "@/components/primitives/StatusPill";
import { TimeAgo } from "@/components/primitives/TimeAgo";
import { cn } from "@/lib/cn";
import { ProcessStatePill } from "./ProcessStatePill";
import { SlotActions } from "./SlotActions";
import type { SlotRow } from "./slot-state";

interface SlotPopoverProps {
  slot: SlotRow;
  anchor: DOMRect;
  supervisorReachable: boolean;
  onClose: () => void;
  onRequestAttach: (deckId: string) => void;
  onRequestDetach: (deckId: string) => void;
  onRequestRelease: (deckId: string) => void;
}

const POPOVER_WIDTH = 280;
const POPOVER_MARGIN = 8;
const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function SlotPopover({
  slot,
  anchor,
  supervisorReachable,
  onClose,
  onRequestAttach,
  onRequestDetach,
  onRequestRelease,
}: SlotPopoverProps) {
  const ref = useRef<HTMLDivElement>(null);

  // Focus-trap + outside dismiss. Portal to body avoids clip-overflow;
  // mousedown (not click) fires before the cell's onClick so switching
  // cells doesn't close-then-reopen on the same anchor.
  useEffect(() => {
    const focusFrame = requestAnimationFrame(() => {
      if (!ref.current) return;
      const first = ref.current.querySelector<HTMLElement>(FOCUSABLE_SELECTOR);
      first?.focus();
    });

    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
        return;
      }
      if (e.key === "Tab" && ref.current) {
        const focusables = ref.current.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR);
        if (focusables.length === 0) {
          e.preventDefault();
          return;
        }
        const first = focusables[0]!;
        const last = focusables[focusables.length - 1]!;
        const active = document.activeElement as HTMLElement | null;
        if (e.shiftKey && active === first) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && active === last) {
          e.preventDefault();
          first.focus();
        }
      }
    };
    const onClick = (e: MouseEvent) => {
      if (!ref.current) return;
      if (ref.current.contains(e.target as Node)) return;
      onClose();
    };
    document.addEventListener("keydown", onKey);
    document.addEventListener("mousedown", onClick);
    return () => {
      cancelAnimationFrame(focusFrame);
      document.removeEventListener("keydown", onKey);
      document.removeEventListener("mousedown", onClick);
    };
  }, [onClose]);

  const pos = computePopoverPosition(anchor);

  return createPortal(
    <div
      ref={ref}
      role="dialog"
      aria-label={`${slot.deckId} controls`}
      className={cn(
        "fixed z-[90] w-[280px] overflow-hidden rounded-card border border-line bg-surface shadow-card-hover",
        "animate-fade-up",
      )}
      style={{ top: pos.top, left: pos.left }}
    >
      <div className="flex items-center justify-between gap-2 border-b border-line px-3 py-2">
        <span className="font-mono text-[12.5px] font-semibold tracking-sub text-ink">
          {slot.deckId}
        </span>
        {slot.process && !slot.deck ? (
          <span className="text-[10px] uppercase tracking-[0.08em] text-ink-sub">Unmanaged</span>
        ) : null}
      </div>
      <div className="flex flex-col gap-2 px-3 py-3">
        <div className="flex flex-wrap items-center gap-2 text-[11px] tracking-nav text-ink-muted">
          <span className="text-ink-sub">Process</span>
          {slot.process ? (
            <ProcessStatePill state={slot.process.state} />
          ) : (
            <span className="font-mono text-ink-sub">—</span>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2 text-[11px] tracking-nav text-ink-muted">
          <span className="text-ink-sub">Health</span>
          {slot.deck ? (
            <>
              <StatusPill status={slot.deck.last_known_health} />
              {slot.deck.last_heartbeat_at && slot.deck.last_known_health !== "EMPTY" ? (
                <span className="font-mono text-[10px] text-ink-sub">
                  <TimeAgo timestamp={slot.deck.last_heartbeat_at} />
                </span>
              ) : null}
            </>
          ) : (
            <span className="font-mono text-ink-sub">—</span>
          )}
        </div>
        {slot.process?.pid || slot.process?.port ? (
          <div className="flex flex-wrap items-center gap-x-2 font-mono text-[10px] text-ink-sub">
            {slot.process?.port ? <span>:{slot.process.port}</span> : null}
            {slot.process?.pid ? <span>PID {slot.process.pid}</span> : null}
          </div>
        ) : null}

        <div className="mt-1 flex flex-wrap items-center gap-1.5">
          <SlotActions
            slot={slot}
            supervisorReachable={supervisorReachable}
            onAttach={() => onRequestAttach(slot.deckId)}
            onDetach={() => onRequestDetach(slot.deckId)}
            onRelease={() => onRequestRelease(slot.deckId)}
          />
        </div>

        <ContextualClarities slot={slot} />
      </div>
    </div>,
    document.body,
  );
}

function computePopoverPosition(anchor: DOMRect): { top: number; left: number } {
  const viewportWidth = window.innerWidth;
  const viewportHeight = window.innerHeight;
  const preferredLeft = anchor.right + POPOVER_MARGIN;
  const overflowsRight = preferredLeft + POPOVER_WIDTH + POPOVER_MARGIN > viewportWidth;
  const left = overflowsRight
    ? Math.max(POPOVER_MARGIN, anchor.left - POPOVER_WIDTH - POPOVER_MARGIN)
    : preferredLeft;
  const popoverEstimatedHeight = 240;
  const maxTop = viewportHeight - popoverEstimatedHeight - POPOVER_MARGIN;
  const top = Math.max(POPOVER_MARGIN, Math.min(anchor.top, maxTop));
  return { top, left };
}

function ContextualClarities({ slot }: { slot: SlotRow }) {
  const state = slot.process?.state;
  const notes: string[] = [];
  if (state === "Running" || state === "Starting") {
    notes.push("Stopping leaves the slot in the fleet (ages to UNREACHABLE).");
  }
  if (slot.process) {
    notes.push("Detaching stops the executor and releases the slot to EMPTY.");
  }
  if (
    !slot.process &&
    slot.deck &&
    slot.deck.last_known_health !== "EMPTY" &&
    !slot.deck.decommissioned_at
  ) {
    notes.push("Slot is non-EMPTY but no executor is managed. Use Release if it's gone for good.");
  }
  if (notes.length === 0) return null;
  return (
    <ul className="mt-1 flex flex-col gap-0.5 text-[10.5px] leading-4 text-ink-sub">
      {notes.map((n, i) => (
        <li key={i}>{n}</li>
      ))}
    </ul>
  );
}
