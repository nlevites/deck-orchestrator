import { useConnection } from "@/lib/connection/use-connection";
import { cn } from "@/lib/cn";

/**
 * Calm connection chip for page headers; ConnectionBanner is the loud surface.
 */
export function LiveIndicator() {
  const { state } = useConnection();
  const meta = STATE_META[state];
  return (
    <span
      className="inline-flex items-center gap-1.5 rounded-pill border border-line bg-surface px-2.5 py-1 font-mono text-[11px] uppercase tracking-nav text-ink-sub"
      aria-label={`Live state ${state}`}
      title={meta.title}
    >
      <span
        className={cn(
          "inline-flex h-2 w-2 rounded-full",
          meta.dot,
          meta.pulse && "animate-pulse-slow",
        )}
      />
      {meta.label}
    </span>
  );
}

const STATE_META = {
  OK: {
    label: "Live",
    title: "Live updates streaming",
    dot: "bg-status-healthy",
    pulse: false,
  },
  LIVE_PAUSED: {
    label: "Paused",
    title: "No fresh polls — see banner",
    dot: "bg-status-ambiguous",
    pulse: true,
  },
  DEGRADED_MODE: {
    label: "Degraded",
    title: "Orchestrator reconciling — see banner",
    dot: "bg-status-ambiguous",
    pulse: true,
  },
  OFFLINE: {
    label: "Offline",
    title: "Browser is offline — see banner",
    dot: "bg-status-failed",
    pulse: true,
  },
} as const;
