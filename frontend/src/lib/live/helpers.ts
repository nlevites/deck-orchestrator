import type { QueryClient } from "@tanstack/react-query";
import { apiKeys } from "@/lib/api/keys";
import type {
  CurrentJob,
  Deck,
  DeckJob,
  DeckJobStatus,
  Event,
  Run,
  RunStatus,
  RunSummary,
} from "@/lib/api-types";

export function updateRunSummaryJob(
  runs: RunSummary[] | undefined,
  runId: string,
  oldStatus: DeckJobStatus | undefined,
  newStatus: DeckJobStatus | undefined,
): RunSummary[] | undefined {
  if (!runs) return runs;
  return runs.map((r) => {
    if (r.id !== runId) return r;
    const byStatus = { ...r.deck_jobs_summary.by_status };
    if (oldStatus && (byStatus[oldStatus] ?? 0) > 0) {
      byStatus[oldStatus] = (byStatus[oldStatus] ?? 0) - 1;
      if (byStatus[oldStatus] === 0) delete byStatus[oldStatus];
    }
    if (newStatus) {
      byStatus[newStatus] = (byStatus[newStatus] ?? 0) + 1;
    }
    return {
      ...r,
      deck_jobs_summary: {
        total: r.deck_jobs_summary.total,
        by_status: byStatus,
      },
    };
  });
}

export function setRunSummaryStatus(
  runs: RunSummary[] | undefined,
  runId: string,
  status: RunStatus,
  terminalAt?: string,
): RunSummary[] | undefined {
  if (!runs) return runs;
  // Mirror the backend: terminal_at tracks the destination status. If
  // the caller passes terminalAt we set it; otherwise we clear it so a
  // FAILED→RUNNING transition (operator retry) drops a stale stamp.
  return runs.map((r) =>
    r.id === runId
      ? {
          ...r,
          status,
          version: r.version + 1,
          terminal_at: terminalAt,
        }
      : r,
  );
}

export function patchDeckJobInRun(
  run: Run | undefined,
  jobId: string,
  patcher: (j: DeckJob) => DeckJob,
): Run | undefined {
  if (!run) return run;
  return {
    ...run,
    deck_jobs: run.deck_jobs.map((j) => (j.id === jobId ? patcher(j) : j)),
  };
}

export function setRunStatus(
  run: Run | undefined,
  status: RunStatus,
  terminalAt?: string,
): Run | undefined {
  if (!run) return run;
  // See setRunSummaryStatus comment — clear terminal_at on non-terminal
  // transitions so e.g. FAILED→RUNNING (retry) drops a stale stamp.
  return {
    ...run,
    status,
    version: run.version + 1,
    terminal_at: terminalAt,
  };
}

export function setDeckCurrentJob(
  decks: Deck[] | undefined,
  deckId: string,
  next: CurrentJob | null,
): Deck[] | undefined {
  if (!decks) return decks;
  return decks.map((d) => (d.id === deckId ? { ...d, current_job: next ?? undefined } : d));
}

export function setDeckCurrentJobStatus(
  decks: Deck[] | undefined,
  deckId: string,
  jobId: string,
  status: DeckJobStatus,
): Deck[] | undefined {
  if (!decks) return decks;
  return decks.map((d) => {
    if (d.id !== deckId || !d.current_job || d.current_job.job_id !== jobId) {
      return d;
    }
    return { ...d, current_job: { ...d.current_job, status } };
  });
}

export function setDeckHealth(
  decks: Deck[] | undefined,
  deckId: string,
  health: Deck["last_known_health"],
  occurredAt: string,
): Deck[] | undefined {
  if (!decks) return decks;
  return decks.map((d) =>
    d.id === deckId
      ? {
          ...d,
          last_known_health: health,
          last_heartbeat_at: occurredAt,
        }
      : d,
  );
}

/**
 * Ring buffer for apiKeys.events / eventsForRun; reducers prepend newest-first.
 */
const EVENTS_CACHE_CAP = 500;

export function appendEventToCache(qc: QueryClient, event: Event): void {
  qc.setQueryData<Event[]>(apiKeys.events, (prev) => {
    const next = prev ? [event, ...prev] : [event];
    return next.length > EVENTS_CACHE_CAP ? next.slice(0, EVENTS_CACHE_CAP) : next;
  });
  if (event.run_id) {
    qc.setQueryData<Event[]>(apiKeys.eventsForRun(event.run_id), (prev) => {
      const next = prev ? [event, ...prev] : [event];
      return next.length > EVENTS_CACHE_CAP ? next.slice(0, EVENTS_CACHE_CAP) : next;
    });
  }
}

export function setEventsCache(qc: QueryClient, events: Event[]): void {
  const sorted = [...events].sort((a, b) => b.seq - a.seq);
  qc.setQueryData<Event[]>(apiKeys.events, sorted);
}

export function setRunEventsCache(qc: QueryClient, runId: string, events: Event[]): void {
  const sorted = [...events].sort((a, b) => b.seq - a.seq);
  qc.setQueryData<Event[]>(apiKeys.eventsForRun(runId), sorted);
}
