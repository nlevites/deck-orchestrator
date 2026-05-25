import { useEffect, useState } from "react";
import { CloudOff, RefreshCw, AlertOctagon } from "lucide-react";
import type { ConnectionState } from "@/lib/connection/connection-ctx";
import { useConnection } from "@/lib/connection/use-connection";
import { cn } from "@/lib/cn";

interface VisualSpec {
  bg: string;
  text: string;
  dot: string;
  icon: typeof CloudOff;
  title: string;
  body: string;
}

const visuals: Record<Exclude<ConnectionState, "OK">, VisualSpec> = {
  OFFLINE: {
    bg: "bg-status-failed/10 border-b-status-failed/30",
    text: "text-status-failed",
    dot: "bg-status-failed",
    icon: CloudOff,
    title: "Offline",
    body: "Showing last known state. Operator actions are disabled until the browser reconnects.",
  },
  LIVE_PAUSED: {
    bg: "bg-accent-link/10 border-b-accent-link/30",
    text: "text-accent-link",
    dot: "bg-accent-link",
    icon: RefreshCw,
    title: "Live updates paused",
    body: "The orchestrator isn't responding to polls. Your view may be stale and operator actions are disabled until the connection recovers.",
  },
  DEGRADED_MODE: {
    bg: "bg-status-ambiguous/10 border-b-status-ambiguous/30",
    text: "text-status-ambiguous",
    dot: "bg-status-ambiguous",
    icon: AlertOctagon,
    title: "Orchestrator reconciling",
    body: "The orchestrator is reconciling in-flight work after a restart. Mutations are temporarily refused; reads continue.",
  },
};

/**
 * Tri-state connection banner per ARCHITECTURE.md §7.1.
 *
 * Hidden when state === "OK". Distinct visuals per state — DO NOT collapse
 * into a single "connection issue" banner.
 */
export function ConnectionBanner() {
  const { state, lastSyncAt } = useConnection();
  // Track "now" in state with a 1s tick so the banner's "last sync Ns
  // ago" copy stays accurate without calling Date.now() during render
  // (which would be impure and produce stale UI between re-renders).
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  if (state === "OK") return null;
  const v = visuals[state];
  const Icon = v.icon;
  const ageSec = Math.max(0, Math.round((now - new Date(lastSyncAt).getTime()) / 1000));

  return (
    <div
      role="status"
      aria-live="polite"
      aria-label={`Connection ${state}`}
      className={cn("sticky top-0 z-50 border-b backdrop-blur-cta", v.bg)}
    >
      <div className="mx-auto flex max-w-container-content items-center gap-3 page-x py-2">
        <span
          className={cn(
            "inline-flex h-6 w-6 items-center justify-center rounded-full",
            "bg-white/70",
            v.text,
          )}
          aria-hidden
        >
          <Icon size={13} strokeWidth={2} />
        </span>
        <div className="flex flex-1 flex-wrap items-baseline gap-x-3 gap-y-0.5">
          <span className={cn("text-[13px] font-semibold tracking-nav", v.text)}>{v.title}</span>
          <span className="text-[13px] tracking-sub text-ink">{v.body}</span>
        </div>
        <span className="hidden font-mono text-[11px] tracking-nav text-ink-muted md:inline">
          last sync {ageSec}s ago
        </span>
      </div>
    </div>
  );
}
