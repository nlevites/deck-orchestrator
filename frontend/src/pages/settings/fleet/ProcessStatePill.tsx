import type { ProcessState } from "@/lib/api/supervisor";
import { cn } from "@/lib/cn";

const PROCESS_STATE_VISUAL: Record<
  ProcessState,
  { label: string; className: string; dotClassName: string }
> = {
  Running: {
    label: "Running",
    className: "bg-[#e7f1ea] text-status-healthy",
    dotClassName: "bg-status-healthy",
  },
  Starting: {
    label: "Starting",
    className: "bg-[#e7eef9] text-status-dispatched",
    dotClassName: "bg-status-dispatched",
  },
  Stopped: {
    label: "Stopped",
    className: "bg-line text-ink-nav",
    dotClassName: "bg-ink-nav",
  },
  Crashing: {
    label: "Crashing",
    className: "bg-[#fbe7e3] text-status-failed",
    dotClassName: "bg-status-failed",
  },
  FatalConfig: {
    label: "FatalConfig",
    className: "bg-[#fbe7e3] text-status-failed",
    dotClassName: "bg-status-failed",
  },
};

export function ProcessStatePill({ state }: { state: ProcessState }) {
  const visual = PROCESS_STATE_VISUAL[state];
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10.5px] font-medium tracking-nav",
        visual.className,
      )}
    >
      <span className={cn("h-1.5 w-1.5 rounded-full", visual.dotClassName)} />
      {visual.label}
    </span>
  );
}
