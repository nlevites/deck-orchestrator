/**
 * Slot-state derivation for the dev-only Fleet Management page. Pure
 * data: cross-joins the orchestrator deck list with the supervisor
 * process list, and collapses the (process, health) pair into the
 * single visual tone the heatmap renders.
 *
 * Lives next to its only consumer (Settings > Fleet Management) rather
 * than in `lib/ui-derive.ts` because the supervisor is dev tooling —
 * no operator-facing surface mixes these two sources.
 */
import type { Deck } from "@/lib/api-types";
import type { ProcessEntry } from "@/lib/api/supervisor";
import { compareDeckIds } from "@/lib/ui-derive";

export interface SlotRow {
  deckId: string;
  deck?: Deck;
  process?: ProcessEntry;
}

export function joinSlots(decks: Deck[], processes: ProcessEntry[]): SlotRow[] {
  const byId = new Map<string, SlotRow>();
  for (const d of decks) {
    byId.set(d.id, { deckId: d.id, deck: d });
  }
  for (const p of processes) {
    if (!p.deck_id) continue;
    const existing = byId.get(p.deck_id);
    if (existing) {
      existing.process = p;
    } else {
      byId.set(p.deck_id, { deckId: p.deck_id, process: p });
    }
  }
  return Array.from(byId.values()).sort((a, b) => compareDeckIds(a.deckId, b.deckId));
}

/**
 * Visual states for heatmap cells, collapsed from supervisor process
 * state + orchestrator health. Popover splits them back for diagnosis.
 */
export type CellTone =
  | "empty"
  | "starting"
  | "running"
  | "stale"
  | "unreachable"
  | "stopped"
  | "fatal";

export interface ToneVisual {
  label: string;
  bg: string;
  fg: string;
  border?: string;
  pulse?: boolean;
}

export const CELL_TONE: Record<CellTone, ToneVisual> = {
  empty: {
    label: "Empty",
    bg: "bg-line/50",
    fg: "text-ink-sub",
    border: "border-line",
  },
  starting: {
    label: "Starting",
    bg: "bg-status-dispatched/70",
    fg: "text-white",
    border: "border-status-dispatched",
    pulse: true,
  },
  running: {
    label: "Running",
    bg: "bg-status-healthy",
    fg: "text-white",
    border: "border-status-healthy",
  },
  stale: {
    label: "Stale",
    bg: "bg-status-stale",
    fg: "text-white",
    border: "border-status-stale",
  },
  unreachable: {
    label: "Unreachable",
    bg: "bg-status-unreachable",
    fg: "text-white",
    border: "border-status-unreachable",
  },
  stopped: {
    label: "Stopped",
    bg: "bg-ink-nav/30",
    fg: "text-ink-nav",
    border: "border-ink-nav/50",
  },
  fatal: {
    label: "Fatal",
    bg: "bg-status-failed",
    fg: "text-white",
    border: "border-status-failed border-dashed",
    pulse: false,
  },
};

export const LEGEND_ORDER: CellTone[] = [
  "empty",
  "starting",
  "running",
  "stale",
  "unreachable",
  "stopped",
  "fatal",
];

/**
 * Collapse the (process, health) pair to a single tone. Stopped /
 * FatalConfig / Crashing always win — the operator needs to see them
 * regardless of what the orchestrator's last health snapshot says
 * (the supervisor's view is authoritative once it has a verdict).
 */
export function toneFor(slot: SlotRow): CellTone {
  const processState = slot.process?.state;
  if (processState === "FatalConfig") return "fatal";
  if (processState === "Crashing") return "fatal";
  if (processState === "Stopped") return "stopped";
  if (processState === "Starting") return "starting";

  if (!slot.process && !slot.deck) return "empty";

  if (slot.deck) {
    switch (slot.deck.last_known_health) {
      case "EMPTY":
        // If a process is Running but the orchestrator hasn't received
        // a heartbeat yet, present as Starting so the cell isn't
        // misleadingly green.
        return processState === "Running" ? "starting" : "empty";
      case "HEALTHY":
        return "running";
      case "STALE":
        return "stale";
      case "UNREACHABLE":
        return "unreachable";
    }
  }

  // No deck row but a process exists: still treat as starting so the
  // operator sees something. Should be transient.
  return "starting";
}

export function shortLabel(deckId: string): string {
  const m = /^deck-(\d+)$/.exec(deckId);
  return m ? m[1]! : deckId;
}

export function describeCell(slot: SlotRow, toneLabel: string): string {
  const parts: string[] = [slot.deckId, toneLabel];
  if (slot.process?.state) parts.push(`process ${slot.process.state}`);
  if (slot.deck?.last_known_health) parts.push(`health ${slot.deck.last_known_health}`);
  return parts.join(" · ");
}
