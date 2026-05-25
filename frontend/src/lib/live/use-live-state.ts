/**
 * useLiveState — global console live stream.
 *
 * Mount once at the top of the console tree (AppShell). The hook polls
 * `/api/state` on a 1-second cadence and projects each response onto
 * the TanStack Query cache.
 *
 * Two distinct cache update mechanisms cooperate, matching the
 * backend's split between "semantic transitions" and "continuous
 * scalars":
 *
 *   1. Events (semantic transitions: RUN_SUBMITTED, JOB_COMPLETED,
 *      DECK_HEALTH_CHANGED, etc) are applied via reducers per
 *      apply-event.ts. These reconcile run/job state in place.
 *
 *   2. The decks slice is replaced wholesale on EVERY response.
 *      Heartbeat advances are continuous scalars that don't merit
 *      events (~4/sec/deck would drown the audit log), so the only
 *      way the cache stays coherent on `last_heartbeat_at` is to
 *      receive an authoritative snapshot every poll. Cost is
 *      bounded by fleet size (~30 bytes/deck × N × 1 Hz). See the
 *      backend handler comment in handlers/state.go for details.
 *
 * Drift safety nets — gap detection, unknown event kind, and a 5min
 * periodic re-bootstrap — all converge on the same fallback: set
 * `seqRef = 0` so the next tick re-bootstraps. The 5min interval is
 * defense in depth for events-cache drift on long-lived tabs; it is
 * NOT the primary heartbeat-freshness mechanism (the always-applied
 * decks slice is).
 *
 * Implementation note: we keep the polling state in refs (not React
 * state) so re-renders don't interfere with the polling loop. The
 * loop is driven by `setInterval` and runs while the component is
 * mounted; on unmount we clear the interval cleanly.
 *
 * Visibility gating: `tick()` short-circuits while the tab is hidden,
 * and a `visibilitychange` listener fires a single catch-up tick on
 * return. A forgotten tab costs ~0 req/s instead of ~1; quantified in
 * analysis/inefficiencies/inefficiencies.md as finding C1.
 */
import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { apiKeys } from "@/lib/api/keys";
import type { Deck } from "@/lib/api-types";
import { emitSignal } from "@/lib/connection/signals";
import { applyEvent } from "@/lib/live/apply-event";
import { fetchState } from "@/lib/live/fetch-state";
import { setEventsCache } from "@/lib/live/helpers";

const POLL_INTERVAL_MS = 1_000;
// Long-tail safety net only. The decks slice now arrives on every
// poll (see module doc), so heartbeat freshness no longer depends on
// this interval. Kept as cheap defense in depth for events-cache
// drift on long-lived tabs.
const REBOOTSTRAP_INTERVAL_MS = 5 * 60_000;

interface UseLiveStateOptions {
  enabled?: boolean;
}

export function useLiveState({ enabled = true }: UseLiveStateOptions = {}): void {
  const qc = useQueryClient();
  const seqRef = useRef(0);
  const lastBootstrapAtRef = useRef(0);
  const inFlightRef = useRef(false);

  useEffect(() => {
    if (!enabled) return undefined;

    // One controller per mount: the cleanup below aborts any in-flight
    // fetch so a slow response after unmount can't write to the cache
    // (or fire emitSignal) for an audience that's gone.
    const ctrl = new AbortController();

    const tick = async () => {
      // Skip if a prior tick hasn't finished — avoids piling up
      // simultaneous fetches when the network is slow.
      if (inFlightRef.current) return;
      if (ctrl.signal.aborted) return;
      // Skip while the tab is backgrounded. A forgotten tab otherwise costs
      // 1 req/s to /api/state forever; the visibilitychange listener below
      // fires an immediate catch-up tick on return so freshness is
      // recovered without waiting for the next setInterval.
      if (typeof document !== "undefined" && document.visibilityState === "hidden") {
        return;
      }
      inFlightRef.current = true;
      try {
        const now = Date.now();
        const dueForBootstrap =
          seqRef.current === 0 || now - lastBootstrapAtRef.current > REBOOTSTRAP_INTERVAL_MS;
        const since = dueForBootstrap ? 0 : seqRef.current;

        const snap = await fetchState(since, ctrl.signal);
        if (ctrl.signal.aborted) return;
        emitSignal("poll-ok");

        // Decks come in two shapes:
        //   - bootstrap: full `decks` slice -> replace cache wholesale.
        //   - delta: `decks_delta` carries only touched-since rows ->
        //     merge by id into the existing cache. Backs S3.
        if (snap.decks) {
          qc.setQueryData(apiKeys.decks, snap.decks);
        } else if (snap.decks_delta && snap.decks_delta.length > 0) {
          const delta = snap.decks_delta;
          qc.setQueryData<Deck[]>(apiKeys.decks, (prev) => {
            if (!prev) return delta;
            const byId = new Map(prev.map((d) => [d.id, d]));
            for (const d of delta) byId.set(d.id, d);
            return Array.from(byId.values());
          });
        }

        // server_seq regression: the orchestrator restarted with a
        // fresh DB (or someone wiped events). Our cursor is now ahead
        // of the server, so delta polls would silently return zero
        // events and we'd never re-anchor until the 5min periodic
        // re-bootstrap fires. Force a re-bootstrap immediately.
        if (snap.server_seq < seqRef.current) {
          seqRef.current = 0;
          return;
        }

        if (since === 0) {
          if (snap.runs) qc.setQueryData(apiKeys.runs, snap.runs);
          setEventsCache(qc, snap.events);
          lastBootstrapAtRef.current = now;
          seqRef.current = snap.server_seq;
          return;
        }

        let cursor = seqRef.current;
        for (const event of snap.events) {
          if (event.seq !== cursor + 1) {
            seqRef.current = 0;
            return;
          }
          const handled = applyEvent(qc, event);
          if (!handled) {
            seqRef.current = 0;
            return;
          }
          cursor = event.seq;
        }
        seqRef.current = Math.max(cursor, snap.server_seq);
      } catch {
        // Network blip, transient error, or AbortError on unmount:
        // leave seqRef alone; next tick (or next mount) retries with
        // the same `since_seq`. If the orchestrator restarts and
        // rotates the event log, the next successful poll will
        // likely trigger a gap → re-bootstrap path, which is correct.
      } finally {
        inFlightRef.current = false;
      }
    };

    void tick();
    const id = setInterval(() => {
      void tick();
    }, POLL_INTERVAL_MS);

    // Catch-up tick on tab return so the cache doesn't sit ~1s stale
    // after a long hidden window.
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
  }, [qc, enabled]);
}
