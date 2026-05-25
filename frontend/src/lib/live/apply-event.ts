/**
 * Event dispatcher. Maps each `EventKind` to the reducer that knows
 * how to project that event onto the TanStack Query cache.
 *
 * Return contract for `applyEvent`:
 *   - `true`  — event was handled; reducer reconciled cache state.
 *   - `false` — either (a) no reducer is registered for the kind, or
 *               (b) a registered reducer explicitly returned `false`
 *               to signal "I cannot fully reconcile cache state from
 *               this event's payload; please rebootstrap." The polling
 *               hooks treat both cases the same way: set `seqRef = 0`
 *               so the next tick takes the bootstrap branch.
 *
 * The `false`-from-reducer path exists for events whose payload doesn't
 * carry enough information to synthesize a faithful cache row (e.g.
 * `RUN_SUBMITTED` — RunSummary needs `deck_jobs_summary` + version, not
 * in the event payload). Rebootstrap on the next 1s tick still beats the
 * 60s periodic rebootstrap floor.
 */
import type { QueryClient } from "@tanstack/react-query";
import type { Event, EventKind } from "@/lib/api-types";
import { appendEventToCache } from "@/lib/live/helpers";
import {
  applyJobAmbiguous,
  applyJobCancelled,
  applyJobCompleted,
  applyJobDispatched,
  applyJobFailed,
  applyJobReady,
  applyJobResolved,
  applyJobRetried,
  applyJobRunning,
  applyJobStepCompleted,
} from "@/lib/live/reducers/job";
import {
  applyDeckHealthChanged,
  applyDeckRegistered,
  applyExecutorConflictLogged,
} from "@/lib/live/reducers/deck";
import { applyRunStatusChanged, applyRunSubmitted } from "@/lib/live/reducers/run";

type Reducer = (qc: QueryClient, e: Event) => void | boolean;

const REDUCERS: Record<EventKind, Reducer> = {
  RUN_SUBMITTED: applyRunSubmitted,
  RUN_STATUS_CHANGED: applyRunStatusChanged,
  JOB_READY: applyJobReady,
  JOB_DISPATCHED: applyJobDispatched,
  JOB_RUNNING: applyJobRunning,
  JOB_STEP_COMPLETED: applyJobStepCompleted,
  JOB_COMPLETED: applyJobCompleted,
  JOB_FAILED: applyJobFailed,
  JOB_AMBIGUOUS: applyJobAmbiguous,
  JOB_CANCELLED: applyJobCancelled,
  JOB_RESOLVED: applyJobResolved,
  JOB_RETRIED: applyJobRetried,
  DECK_REGISTERED: applyDeckRegistered,
  DECK_HEALTH_CHANGED: applyDeckHealthChanged,
  EXECUTOR_CONFLICT_LOGGED: applyExecutorConflictLogged,
};

export function applyEvent(qc: QueryClient, e: Event): boolean {
  // Every event lands in the events caches first; even kinds with
  // advisory reducers (DECK_REGISTERED, EXECUTOR_CONFLICT_LOGGED)
  // should show up in EventTail so operators see audit traffic.
  appendEventToCache(qc, e);

  const reducer = REDUCERS[e.kind];
  if (!reducer) {
    return false;
  }
  const result = reducer(qc, e);
  return result !== false;
}
