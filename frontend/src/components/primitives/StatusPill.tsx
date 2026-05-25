import { forwardRef, type HTMLAttributes } from "react";
import { cn } from "@/lib/cn";
import { statusMap } from "./status-helpers";

/**
 * HMR-pure re-export surface; statusMap and unions live in status-helpers.ts.
 * HEALTHY_IDLE/BUSY are derived UI states (STATE_MACHINE.md §5 deck_jobs lookup).
 */
export type DeckJobStatus =
  | "PENDING"
  | "READY"
  | "DISPATCHED"
  | "RUNNING"
  | "COMPLETED"
  | "FAILED"
  | "AMBIGUOUS"
  | "CANCELLED";

export type RunStatus = "PENDING" | "RUNNING" | "COMPLETED" | "FAILED" | "CANCELLED" | "AMBIGUOUS";

export type DeckHealth =
  // Raw spec values from STATE_MACHINE.md §5 + decks-as-slots EMPTY.
  | "EMPTY"
  | "HEALTHY"
  | "STALE"
  | "UNREACHABLE"
  // Derived display values; HEALTHY split by deck_jobs lookup.
  | "HEALTHY_IDLE"
  | "HEALTHY_BUSY"
  // Transitional UI state used while a reconciliation pull is in flight.
  | "RECOVERING"
  // Catch-all for pre-first-heartbeat decks; spec rejects this in practice,
  // but the union accepts it so we can render the case if it ever leaks.
  | "UNKNOWN";

export type AnyStatus = DeckJobStatus | RunStatus | DeckHealth;

interface StatusPillProps extends HTMLAttributes<HTMLSpanElement> {
  status: AnyStatus;
  /** Render an animated dot for live states (RUNNING, DISPATCHED, HEALTHY_BUSY, AMBIGUOUS). */
  dot?: boolean;
  /**
   * `"full"` (default) renders dot + label, as on detail pages and alerts.
   * `"compact"` renders the dot only with the tinted background; the literal
   * status word is surfaced via aria-label + title so the pill stays
   * accessible. Use in dense lists where 50 repeating labels are noise.
   */
  size?: "full" | "compact";
}

export const StatusPill = forwardRef<HTMLSpanElement, StatusPillProps>(
  ({ status, dot = true, size = "full", className, children, ...rest }, ref) => {
    const visual = statusMap[status];
    const compact = size === "compact";
    const ariaLabel = rest["aria-label"] ?? (compact ? visual.label : undefined);
    const titleAttr = rest.title ?? (compact ? visual.label : undefined);
    return (
      <span
        ref={ref}
        {...rest}
        aria-label={ariaLabel}
        title={titleAttr}
        className={cn(
          "inline-flex items-center rounded-full font-medium tracking-nav",
          compact ? "h-4 w-4 justify-center p-0" : "gap-1 px-2 py-0.5 text-[11px]",
          visual.className,
          className,
        )}
      >
        {dot && (
          <span
            className={cn(
              "h-1.5 w-1.5 rounded-full",
              visual.dotClassName,
              visual.pulse && "animate-pulse-slow",
            )}
          />
        )}
        {!compact && (children ?? visual.label)}
      </span>
    );
  },
);
StatusPill.displayName = "StatusPill";
