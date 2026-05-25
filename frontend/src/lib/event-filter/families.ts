/**
 * Maps the OpenAPI `EventKind` enum onto five operator-facing families
 * used by the Event-tail filter chips. The taxonomy is intentionally
 * coarser than the raw enum so the chip set stays readable at a glance
 * (Runs / Jobs / Health / Resolutions / Other).
 *
 * `JOB_RESOLVED` is split out from Jobs because Resolve is the
 * operator's own work and worth filtering to on its own. Anything not
 * matched falls into `other` so the UI never silently drops a future
 * kind the backend introduces — operators see the row by default and
 * we add it to the right family in a follow-up.
 *
 * Pure module: no React, no I/O. Reused by:
 *   - `useEventFilter()`        — initial state + persistence.
 *   - `EventFilterChips`        — chip render order + labels.
 *   - `EventTail`               — filtering predicate.
 */
import type { EventKind } from "@/lib/api-types";

export type EventFamily = "runs" | "jobs" | "health" | "resolutions" | "other";

export const FAMILIES: ReadonlyArray<EventFamily> = [
  "runs",
  "jobs",
  "health",
  "resolutions",
  "other",
] as const;

export const FAMILY_LABEL: Record<EventFamily, string> = {
  runs: "Runs",
  jobs: "Jobs",
  health: "Health",
  resolutions: "Resolutions",
  other: "Other",
};

export function familyForKind(kind: EventKind): EventFamily {
  switch (kind) {
    case "RUN_SUBMITTED":
    case "RUN_STATUS_CHANGED":
      return "runs";
    case "JOB_READY":
    case "JOB_DISPATCHED":
    case "JOB_RUNNING":
    case "JOB_COMPLETED":
    case "JOB_FAILED":
    case "JOB_AMBIGUOUS":
    case "JOB_CANCELLED":
    case "JOB_RETRIED":
    case "EXECUTOR_CONFLICT_LOGGED":
      return "jobs";
    case "DECK_HEALTH_CHANGED":
    case "DECK_REGISTERED":
      return "health";
    case "JOB_RESOLVED":
      return "resolutions";
    default:
      return "other";
  }
}

export function isKnownFamily(value: string): value is EventFamily {
  return (FAMILIES as ReadonlyArray<string>).includes(value);
}
