/**
 * useLiveRunState — run-detail screen live bootstrap.
 *
 * Mount inside `RunDetailPage`. The hook polls
 * `/api/runs/{id}/state` on a 1s cadence with `since_seq=0` and
 * replaces the run-detail + run-scoped events caches wholesale on
 * every response.
 *
 * Single-writer rule (C4 fix). The global `useLiveState` mounted in
 * `AppShell` is the *only* writer for shared caches via
 * `applyEvent`: it owns `apiKeys.runs[]`, `apiKeys.decks`,
 * `apiKeys.events`, and per-event mutations to `apiKeys.run(id)` and
 * `apiKeys.eventsForRun(id)`.
 *
 * Pre-fix, this hook also called `applyEvent` for shared caches —
 * which meant on every `/runs/:id/*` route, two independent cursors
 * applied the same JOB_RUNNING etc. event twice against the same
 * cache key. `job.version` and `run.version` inflated by 2 instead
 * of 1, modal `expected_version` snapshots went stale, operator
 * actions returned spurious 409 VERSION_MISMATCH even with no other
 * operator. This hook now provides the *bootstrap* (the run-detail
 * snapshot the global hook can't synthesize from the fleet-scoped
 * payload) and lets the global hook keep the run fresh via its
 * normal event reducers.
 *
 * The forced bootstrap on every tick is intentional: the global
 * `useLiveState` may not always reach this run's events soon enough
 * for the operator UI on a fresh page load (its bootstrap floor is
 * 5 minutes), and re-bootstrapping the run-scoped slice every second
 * is cheap (one indexed query + ~20 recent events). It's the
 * cache-as-projection model: we have ONE writer per cache key, and
 * we accept the redundant snapshot fetch in exchange for never
 * having to reconcile two writers.
 */
import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { apiKeys } from "@/lib/api/keys";
import { emitSignal } from "@/lib/connection/signals";
import { fetchRunState } from "@/lib/live/fetch-state";
import { setRunEventsCache } from "@/lib/live/helpers";

const POLL_INTERVAL_MS = 1_000;

export function useLiveRunState(runId: string | undefined): void {
  const qc = useQueryClient();
  const inFlightRef = useRef(false);
  // Track which run the refs are bound to so a route change resets the in-flight flag.
  const boundRunIdRef = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (!runId) return undefined;

    if (boundRunIdRef.current !== runId) {
      boundRunIdRef.current = runId;
      inFlightRef.current = false;
    }

    // One controller per (mount × runId): aborting on cleanup or on a
    // route change ensures a slow response for the previous run can't
    // overwrite the cache for a different run the operator just
    // navigated to.
    const ctrl = new AbortController();

    const tick = async () => {
      if (inFlightRef.current) return;
      if (ctrl.signal.aborted) return;
      // Skip when the tab is hidden — the visibilitychange handler fires
      // a catch-up tick on return. Matches the global useLiveState gate
      // so a backgrounded run-detail tab issues 0 req/s.
      if (typeof document !== "undefined" && document.visibilityState === "hidden") {
        return;
      }
      inFlightRef.current = true;
      try {
        // Always bootstrap (since=0); global useLiveState owns applyEvent.
        const snap = await fetchRunState(runId, 0, ctrl.signal);
        if (ctrl.signal.aborted) return;
        // Run-scoped poll uses its own signal so a successful run-detail
        // fetch can't mask a failing global /api/state poll. The
        // banner / LIVE_PAUSED state is driven by global poll-ok only;
        // otherwise navigating to /runs/:id during a fleet-wide outage
        // would clear the banner while the decks/runs caches are stale.
        emitSignal("run-poll-ok");
        if (snap.run) qc.setQueryData(apiKeys.run(runId), snap.run);
        setRunEventsCache(qc, runId, snap.events);
      } catch {
        // Network blip or AbortError on unmount/route change: leave
        // the cache alone; next mount or next tick retries. The global
        // hook's connection-state signals already surface the
        // disconnect to the operator.
      } finally {
        inFlightRef.current = false;
      }
    };

    void tick();
    const id = setInterval(() => {
      void tick();
    }, POLL_INTERVAL_MS);
    const onVisibility = () => {
      if (typeof document !== "undefined" && document.visibilityState === "visible") {
        void tick();
      }
    };
    if (typeof document !== "undefined") {
      document.addEventListener("visibilitychange", onVisibility);
    }
    return () => {
      clearInterval(id);
      ctrl.abort();
      if (typeof document !== "undefined") {
        document.removeEventListener("visibilitychange", onVisibility);
      }
    };
  }, [qc, runId]);
}
