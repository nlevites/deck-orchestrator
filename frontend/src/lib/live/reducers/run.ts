/**
 * Run-scoped event reducers.
 *
 * RUN_SUBMITTED can't be reconciled into the list cache from the event
 * payload alone (RunSummary needs `deck_jobs_summary` + version, neither
 * carried by the event), so the reducer returns `false` to ask the
 * dispatcher to rebootstrap on the next tick. See apply-event.ts for
 * the contract.
 *
 * RUN_STATUS_CHANGED carries the new terminal/non-terminal status in
 * its payload and reconciles in place.
 */
import type { QueryClient } from "@tanstack/react-query";
import { apiKeys } from "@/lib/api/keys";
import type { Event, Run, RunStatus, RunSummary } from "@/lib/api-types";
import { setRunStatus, setRunSummaryStatus } from "@/lib/live/helpers";

export function applyRunSubmitted(_qc: QueryClient, _e: Event): boolean {
  // Signal rebootstrap: the next /api/state poll will include the new
  // run in its bootstrap snapshot, which is the only way to populate
  // a faithful RunSummary row. Latency: one poll tick (~1s) plus one
  // bootstrap (~50ms), versus the 60s periodic-rebootstrap floor if
  // we returned void here.
  return false;
}

// Only COMPLETED and CANCELLED stamp terminal_at. FAILED and AMBIGUOUS
// are non-terminal — the run is awaiting an operator decision. Mirrors
// the backend's IsTerminalRunStatus rule (DESIGN.md).
function isTerminalStatus(s: RunStatus): boolean {
  return s === "COMPLETED" || s === "CANCELLED";
}

export function applyRunStatusChanged(qc: QueryClient, e: Event): void {
  if (!e.run_id) return;
  const to = (e.payload?.["to"] ?? null) as RunStatus | null;
  if (!to) return;
  const terminalAt = isTerminalStatus(to) ? e.occurred_at : undefined;
  qc.setQueryData<RunSummary[]>(apiKeys.runs, (prev) =>
    setRunSummaryStatus(prev, e.run_id!, to, terminalAt),
  );
  qc.setQueryData<Run>(apiKeys.run(e.run_id), (prev) => setRunStatus(prev, to, terminalAt));
}
