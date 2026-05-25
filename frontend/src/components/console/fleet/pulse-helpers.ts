import type { DeckHealth } from "@/components/primitives/StatusPill";

export type PulseBucket = "idle" | "active" | "attention";

export interface PulseCounts {
  idle: number;
  active: number;
  attention: number;
}

export type PulseFilter = PulseBucket | "all";

/**
 * Maps a `DeckHealth` to the pulse bucket it lives in. Single source
 * of truth for "what color row does this deck contribute to" — the
 * bar segments, the chip counts, and the section partition all
 * derive from this same predicate.
 *
 * AMBIGUOUS-held wins regardless of underlying health: a HEALTHY deck
 * holding an AMBIGUOUS job is operator-actionable in exactly the
 * same way as a STALE deck.
 */
export function pulseBucketFor(health: DeckHealth, heldByAmbiguous: boolean): PulseBucket {
  if (heldByAmbiguous) return "attention";
  switch (health) {
    case "STALE":
    case "UNREACHABLE":
    case "RECOVERING":
    case "UNKNOWN":
      return "attention";
    case "HEALTHY_BUSY":
      return "active";
    default:
      return "idle";
  }
}
