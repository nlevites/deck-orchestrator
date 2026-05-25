/**
 * deck_job event reducers. Each kind touches three caches:
 *   - apiKeys.runs (RunSummary list)        — re-tally `by_status`
 *   - apiKeys.run(run_id) (Run detail)      — flip the job's status + attempts
 *   - apiKeys.decks (Deck list)             — slot held/released by the deck_job
 *
 * All write through the helpers in ../helpers.ts so the cache-shape
 * logic stays in one place.
 */
import type { QueryClient } from "@tanstack/react-query";
import { apiKeys } from "@/lib/api/keys";
import type {
  AttemptOutcome,
  Deck,
  DeckJob,
  DeckJobStatus,
  Event,
  JobAttempt,
  OutcomeSource,
  Run,
  RunSummary,
} from "@/lib/api-types";
import {
  patchDeckJobInRun,
  setDeckCurrentJob,
  setDeckCurrentJobStatus,
  updateRunSummaryJob,
} from "@/lib/live/helpers";

// `from` in the event payload drives by_status tally when run-detail cache is cold.
function flipJobStatus(qc: QueryClient, e: Event, newStatus: DeckJobStatus): void {
  if (!e.run_id || !e.job_id) return;
  const runId = e.run_id;
  const jobId = e.job_id;
  const oldStatus = (e.payload?.["from"] ?? undefined) as DeckJobStatus | undefined;

  qc.setQueryData<Run>(apiKeys.run(runId), (prev) =>
    patchDeckJobInRun(prev, jobId, (_j) => {
      return { ..._j, status: newStatus, version: _j.version + 1 };
    }),
  );
  qc.setQueryData<RunSummary[]>(apiKeys.runs, (prev) =>
    updateRunSummaryJob(prev, runId, oldStatus, newStatus),
  );

  if (e.deck_id) {
    qc.setQueryData<Deck[]>(apiKeys.decks, (prev) =>
      setDeckCurrentJobStatus(prev, e.deck_id!, jobId, newStatus),
    );
  }
}

export function applyJobReady(qc: QueryClient, e: Event): void {
  flipJobStatus(qc, e, "READY");
}

export function applyJobDispatched(qc: QueryClient, e: Event): void {
  if (!e.run_id || !e.job_id || !e.deck_id) return;
  const runId = e.run_id;
  const jobId = e.job_id;
  const deckId = e.deck_id;
  const attemptId = (e.attempt_id ?? null) as string | null;
  const oldStatus = (e.payload?.["from"] ?? undefined) as DeckJobStatus | undefined;

  qc.setQueryData<Run>(apiKeys.run(runId), (prev) =>
    patchDeckJobInRun(prev, jobId, (j) => {
      const next = {
        ...j,
        status: "DISPATCHED" as DeckJobStatus,
        version: j.version + 1,
        current_attempt_id: attemptId ?? j.current_attempt_id,
      };
      if (
        attemptId &&
        (!j.recent_attempts || !j.recent_attempts.some((a) => a.attempt_id === attemptId))
      ) {
        const fresh: JobAttempt = {
          attempt_id: attemptId,
          dispatched_at: e.occurred_at,
        };
        next.recent_attempts = j.recent_attempts ? [fresh, ...j.recent_attempts] : [fresh];
      }
      return next;
    }),
  );

  qc.setQueryData<RunSummary[]>(apiKeys.runs, (prev) =>
    updateRunSummaryJob(prev, runId, oldStatus, "DISPATCHED"),
  );

  qc.setQueryData<Deck[]>(apiKeys.decks, (prev) =>
    setDeckCurrentJob(prev, deckId, {
      run_id: runId,
      job_id: jobId,
      status: "DISPATCHED",
    }),
  );
}

export function applyJobRunning(qc: QueryClient, e: Event): void {
  flipJobStatus(qc, e, "RUNNING");
}

function applyJobTerminal(
  qc: QueryClient,
  e: Event,
  status: DeckJobStatus,
  outcome: AttemptOutcome,
): void {
  if (!e.run_id || !e.job_id) return;
  const runId = e.run_id;
  const jobId = e.job_id;
  const attemptId = (e.attempt_id ?? null) as string | null;
  const oldStatus = (e.payload?.["from"] ?? undefined) as DeckJobStatus | undefined;
  const outcomeSource = (e.payload?.["outcome_source"] ?? null) as OutcomeSource | null;
  const errorMessage =
    typeof e.payload?.["error"] === "string" ? (e.payload["error"] as string) : undefined;

  qc.setQueryData<Run>(apiKeys.run(runId), (prev) =>
    patchDeckJobInRun(prev, jobId, (j) => {
      const next = {
        ...j,
        status,
        version: j.version + 1,
        error: errorMessage ?? j.error,
      };
      // Mirror the backend's UpdateDeckJobStatus* CASE: on COMPLETED, stamp
      // last_completed_step = total_steps so a tab watching live still
      // shows the right count when a buffered STEP event lost the race
      // against the terminal transition.
      if (status === "COMPLETED" && j.total_steps != null) {
        next.last_completed_step = j.total_steps;
      }
      if (attemptId && j.recent_attempts) {
        next.recent_attempts = j.recent_attempts.map((a) =>
          a.attempt_id === attemptId
            ? {
                ...a,
                outcome,
                outcome_at: e.occurred_at,
                outcome_source: outcomeSource ?? a.outcome_source,
                error: errorMessage ?? a.error,
              }
            : a,
        );
      }
      return next;
    }),
  );

  qc.setQueryData<RunSummary[]>(apiKeys.runs, (prev) =>
    updateRunSummaryJob(prev, runId, oldStatus, status),
  );

  if (e.deck_id) {
    qc.setQueryData<Deck[]>(apiKeys.decks, (prev) => setDeckCurrentJob(prev, e.deck_id!, null));
  }
}

export function applyJobCompleted(qc: QueryClient, e: Event): void {
  applyJobTerminal(qc, e, "COMPLETED", "COMPLETED");
}

export function applyJobFailed(qc: QueryClient, e: Event): void {
  applyJobTerminal(qc, e, "FAILED", "FAILED");
}

export function applyJobAmbiguous(qc: QueryClient, e: Event): void {
  // AMBIGUOUS holds the deck slot until operator resolution. We do
  // NOT clear current_job; only flip the slot's status.
  if (!e.run_id || !e.job_id) return;
  const runId = e.run_id;
  const jobId = e.job_id;
  const oldStatus = (e.payload?.["from"] ?? undefined) as DeckJobStatus | undefined;
  const reason = (e.payload?.["reason"] ?? null) as DeckJob["ambiguous_reason"];

  qc.setQueryData<Run>(apiKeys.run(runId), (prev) =>
    patchDeckJobInRun(prev, jobId, (j) => {
      return { ...j, status: "AMBIGUOUS", ambiguous_reason: reason, version: j.version + 1 };
    }),
  );
  qc.setQueryData<RunSummary[]>(apiKeys.runs, (prev) =>
    updateRunSummaryJob(prev, runId, oldStatus, "AMBIGUOUS"),
  );
  if (e.deck_id) {
    qc.setQueryData<Deck[]>(apiKeys.decks, (prev) =>
      setDeckCurrentJobStatus(prev, e.deck_id!, jobId, "AMBIGUOUS"),
    );
  }
}

export function applyJobCancelled(qc: QueryClient, e: Event): void {
  if (!e.run_id || !e.job_id) return;
  const runId = e.run_id;
  const jobId = e.job_id;
  const oldStatus = (e.payload?.["from"] ?? undefined) as DeckJobStatus | undefined;

  qc.setQueryData<Run>(apiKeys.run(runId), (prev) =>
    patchDeckJobInRun(prev, jobId, (j) => {
      return { ...j, status: "CANCELLED", version: j.version + 1 };
    }),
  );
  qc.setQueryData<RunSummary[]>(apiKeys.runs, (prev) =>
    updateRunSummaryJob(prev, runId, oldStatus, "CANCELLED"),
  );
  if (e.deck_id) {
    qc.setQueryData<Deck[]>(apiKeys.decks, (prev) => setDeckCurrentJob(prev, e.deck_id!, null));
  }
}

export function applyJobResolved(qc: QueryClient, e: Event): void {
  // Resolution records the operator's outcome on the in-flight
  // attempt; the deck_job itself is then transitioned to COMPLETED
  // or FAILED on the same tx, but the RESOLVED event only carries
  // the resolution payload — we stamp it onto the attempt and leave
  // the status flip to the subsequent state change (the orchestrator
  // emits a separate JOB_STATUS_CHANGED / cascading event).
  if (!e.run_id || !e.job_id) return;
  const runId = e.run_id;
  const jobId = e.job_id;
  const attemptId = (e.attempt_id ?? null) as string | null;
  const resolution = (e.payload?.["resolution"] ?? null) as AttemptOutcome | null;
  const note =
    typeof e.payload?.["operator_note"] === "string"
      ? (e.payload["operator_note"] as string)
      : undefined;

  if (!resolution) return;
  const targetStatus: DeckJobStatus = resolution;
  // JOB_RESOLVED uses "resolution" as the status transition; `from` is AMBIGUOUS.
  const oldStatus = (e.payload?.["from"] ?? undefined) as DeckJobStatus | undefined;

  qc.setQueryData<Run>(apiKeys.run(runId), (prev) =>
    patchDeckJobInRun(prev, jobId, (j) => {
      const next = {
        ...j,
        status: targetStatus,
        ambiguous_reason: null,
        version: j.version + 1,
      };
      // Same UI-counter guarantee as applyJobTerminal: operator-declared
      // COMPLETED stamps last_completed_step = total_steps.
      if (targetStatus === "COMPLETED" && j.total_steps != null) {
        next.last_completed_step = j.total_steps;
      }
      if (attemptId && j.recent_attempts) {
        next.recent_attempts = j.recent_attempts.map((a) =>
          a.attempt_id === attemptId
            ? {
                ...a,
                outcome: resolution,
                outcome_at: e.occurred_at,
                outcome_source: "OPERATOR_RESOLUTION" as OutcomeSource,
                operator_note: note ?? a.operator_note,
              }
            : a,
        );
      }
      return next;
    }),
  );
  qc.setQueryData<RunSummary[]>(apiKeys.runs, (prev) =>
    updateRunSummaryJob(prev, runId, oldStatus, targetStatus),
  );
  if (e.deck_id) {
    qc.setQueryData<Deck[]>(apiKeys.decks, (prev) => setDeckCurrentJob(prev, e.deck_id!, null));
  }
}

/**
 * JOB_STEP_COMPLETED: progress-only update. No status change. Patches
 * last_completed_step to max(existing, step) so duplicate / out-of-order
 * events from the outbox are safe no-ops. Bumps version to stay consistent
 * with the server-side version increment.
 *
 * The `last_completed_step` field is a bootstrap denorm on the deck_job row
 * (set at start from total_steps; advanced by each STEP_COMPLETED). The event
 * gives us a live delta path so operators see progress immediately without
 * waiting for a bootstrap re-fetch.
 */
export function applyJobStepCompleted(qc: QueryClient, e: Event): void {
  if (!e.run_id || !e.job_id) return;
  const runId = e.run_id;
  const jobId = e.job_id;
  const step = typeof e.payload?.["step"] === "number" ? (e.payload["step"] as number) : undefined;
  if (step === undefined) return;

  qc.setQueryData<Run>(apiKeys.run(runId), (prev) =>
    patchDeckJobInRun(prev, jobId, (j) => {
      const current = j.last_completed_step ?? 0;
      if (step <= current) return j; // monotonic guard: no-op if not advancing
      return {
        ...j,
        last_completed_step: step,
        version: j.version + 1,
      };
    }),
  );
}

export function applyJobRetried(qc: QueryClient, e: Event): void {
  if (!e.run_id || !e.job_id) return;
  const runId = e.run_id;
  const jobId = e.job_id;
  const oldStatus = (e.payload?.["from"] ?? undefined) as DeckJobStatus | undefined;

  qc.setQueryData<Run>(apiKeys.run(runId), (prev) =>
    patchDeckJobInRun(prev, jobId, (j) => {
      return {
        ...j,
        status: "READY",
        version: j.version + 1,
        current_attempt_id: undefined,
        error: undefined,
        ambiguous_reason: null,
      };
    }),
  );
  qc.setQueryData<RunSummary[]>(apiKeys.runs, (prev) =>
    updateRunSummaryJob(prev, runId, oldStatus, "READY"),
  );
}
