/**
 * Non-component exports split out for HMR (react-refresh/only-export-components).
 * Keep statusMap, StatusPill unions, and tailwind colors.status in sync.
 */
import type { DeckJobStatus, RunStatus, DeckHealth, AnyStatus } from "./StatusPill";

export interface StatusVisual {
  label: string;
  className: string;
  dotClassName: string;
  pulse: boolean;
}

export const statusMap: Record<AnyStatus, StatusVisual> = {
  PENDING: {
    label: "Pending",
    className: "bg-line text-ink-nav",
    dotClassName: "bg-ink-nav",
    pulse: false,
  },
  READY: {
    label: "Ready",
    className: "bg-[#f4ece1] text-status-ready",
    dotClassName: "bg-status-ready",
    pulse: false,
  },
  DISPATCHED: {
    label: "Dispatched",
    className: "bg-[#e7eef9] text-status-dispatched",
    dotClassName: "bg-status-dispatched",
    pulse: true,
  },
  RUNNING: {
    label: "Running",
    className: "bg-[#e0effb] text-status-running",
    dotClassName: "bg-status-running",
    pulse: true,
  },
  COMPLETED: {
    label: "Completed",
    className: "bg-[#e7f1ea] text-status-completed",
    dotClassName: "bg-status-completed",
    pulse: false,
  },
  FAILED: {
    label: "Failed",
    className: "bg-[#fbe7e3] text-status-failed",
    dotClassName: "bg-status-failed",
    pulse: false,
  },
  AMBIGUOUS: {
    label: "Ambiguous",
    className: "bg-[#f7eadb] text-status-ambiguous",
    dotClassName: "bg-status-ambiguous",
    pulse: true,
  },
  CANCELLED: {
    label: "Cancelled",
    className: "bg-line text-status-cancelled",
    dotClassName: "bg-status-cancelled",
    pulse: false,
  },
  EMPTY: {
    label: "Empty",
    className: "bg-line/40 text-ink-sub",
    dotClassName: "bg-ink-sub",
    pulse: false,
  },
  HEALTHY: {
    label: "Healthy",
    className: "bg-[#e7f1ea] text-status-healthy",
    dotClassName: "bg-status-healthy",
    pulse: false,
  },
  HEALTHY_IDLE: {
    label: "Idle",
    className: "bg-[#e7f1ea] text-status-healthy",
    dotClassName: "bg-status-healthy",
    pulse: false,
  },
  HEALTHY_BUSY: {
    label: "Busy",
    className: "bg-[#e0effb] text-status-busy",
    dotClassName: "bg-status-busy",
    pulse: true,
  },
  STALE: {
    label: "Stale",
    className: "bg-[#f7eadb] text-status-stale",
    dotClassName: "bg-status-stale",
    pulse: false,
  },
  UNREACHABLE: {
    label: "Unreachable",
    className: "bg-[#fbe7e3] text-status-unreachable",
    dotClassName: "bg-status-unreachable",
    pulse: false,
  },
  RECOVERING: {
    label: "Recovering",
    className: "bg-[#f7eadb] text-status-recovering",
    dotClassName: "bg-status-recovering",
    pulse: true,
  },
  UNKNOWN: {
    label: "Unknown",
    className: "bg-line text-ink-nav",
    dotClassName: "bg-ink-nav",
    pulse: false,
  },
};

export function statusLabel(status: AnyStatus): string {
  return statusMap[status].label;
}

export const ALL_DECK_JOB_STATUSES: DeckJobStatus[] = [
  "PENDING",
  "READY",
  "DISPATCHED",
  "RUNNING",
  "COMPLETED",
  "FAILED",
  "AMBIGUOUS",
  "CANCELLED",
];

export const ALL_RUN_STATUSES: RunStatus[] = [
  "PENDING",
  "RUNNING",
  "COMPLETED",
  "FAILED",
  "CANCELLED",
  "AMBIGUOUS",
];

export const ALL_DECK_HEALTHS: DeckHealth[] = [
  "EMPTY",
  "HEALTHY",
  "HEALTHY_IDLE",
  "HEALTHY_BUSY",
  "STALE",
  "UNREACHABLE",
  "RECOVERING",
  "UNKNOWN",
];
