import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { X } from "lucide-react";
import { EventTail } from "@/components/console/EventTail";
import { apiKeys } from "@/lib/api";
import { cacheOnlyQueryFn } from "@/lib/api/query-config";
import type { Event } from "@/lib/api-types";
import { cn } from "@/lib/cn";

interface ActivityDrawerProps {
  runId: string;
  open: boolean;
  onClose: () => void;
}

/**
 * Slide-in event tail (replaces `/runs/:id/events`). Non-modal so the
 * operator can watch events while resolving jobs on the page.
 */
export function ActivityDrawer({ runId, open, onClose }: ActivityDrawerProps) {
  const { data: events = [] } = useQuery<Event[]>({
    queryKey: apiKeys.eventsForRun(runId),
    queryFn: cacheOnlyQueryFn,
    staleTime: Infinity,
    enabled: open,
  });

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  return (
    <>
      {/* pointer-events-none when closed so the page beneath stays interactive */}
      <div
        className={cn(
          "fixed inset-0 z-30 bg-ink/15 transition-opacity duration-200",
          open ? "opacity-100" : "pointer-events-none opacity-0",
        )}
        onClick={onClose}
        aria-hidden
      />
      <aside
        role="dialog"
        aria-modal="false"
        aria-label="Run activity"
        className={cn(
          "fixed right-0 top-0 z-40 flex h-screen w-full max-w-[440px] flex-col border-l border-line bg-surface shadow-2xl",
          "transition-transform duration-200 ease-out-soft",
          open ? "translate-x-0" : "translate-x-full",
        )}
      >
        <header className="flex items-center justify-between gap-3 border-b border-line px-4 py-3">
          <div className="flex flex-col gap-0.5">
            <span className="font-mono text-[10px] uppercase tracking-[0.14em] text-ink-sub">
              Activity
            </span>
            <span className="text-[14px] font-semibold tracking-sub text-ink">
              {events.length} event{events.length === 1 ? "" : "s"}
            </span>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close activity drawer"
            className="inline-flex h-8 w-8 items-center justify-center rounded-md text-ink-nav hover:bg-line/60 hover:text-ink"
          >
            <X size={16} strokeWidth={1.7} />
          </button>
        </header>
        <div className="flex-1 overflow-y-auto">
          {open && <EventTail events={events} density="compact" />}
        </div>
      </aside>
    </>
  );
}
