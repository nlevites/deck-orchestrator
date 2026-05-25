-- name: InsertDeckJob :exec
INSERT INTO deck_jobs (
    run_id, id, deck_id, depends_on, steps, status,
    current_attempt_id, error, version, last_completed_step, total_steps, ambiguous_reason
) VALUES (?, ?, ?, ?, ?, ?, NULL, NULL, 1, 0, ?, NULL);

-- name: GetDeckJob :one
SELECT run_id, id, deck_id, depends_on, steps, status,
       current_attempt_id, error, version, last_completed_step, total_steps, ambiguous_reason
FROM deck_jobs
WHERE run_id = ? AND id = ?;

-- name: ListDeckJobsByRun :many
SELECT run_id, id, deck_id, depends_on, steps, status,
       current_attempt_id, error, version, last_completed_step, total_steps, ambiguous_reason
FROM deck_jobs
WHERE run_id = ?
ORDER BY id ASC;

-- name: CountDeckSlotOccupiers :one
-- Per-deck slot invariant check used by the Dispatcher pre-check.
SELECT COUNT(*) FROM deck_jobs
WHERE deck_id = ? AND status IN ('DISPATCHED', 'RUNNING', 'AMBIGUOUS');

-- name: GetDeckSlotOccupier :one
-- Returns the (run_id, id) of the active deck_job for this deck, if any.
-- Per-deck slot invariant guarantees at most one row.
SELECT run_id, id, status, current_attempt_id
FROM deck_jobs
WHERE deck_id = ? AND status IN ('DISPATCHED', 'RUNNING', 'AMBIGUOUS')
LIMIT 1;

-- name: GetDispatchedJobForDeck :one
-- Returns the currently-DISPATCHED job for this deck (long-poll target).
SELECT run_id, id, deck_id, steps, current_attempt_id
FROM deck_jobs
WHERE deck_id = ? AND status = 'DISPATCHED'
LIMIT 1;

-- name: ListJobsByStatus :many
SELECT run_id, id, deck_id, depends_on, steps, status,
       current_attempt_id, error, version, last_completed_step, total_steps, ambiguous_reason
FROM deck_jobs
WHERE status = ?;

-- name: ListReadyJobsForDeck :many
-- Used by the per-deck dispatcher. Covered by deck_jobs_deck_status_idx.
-- Sole READY-jobs-for-this-deck reader; lets the dispatcher run in
-- O(jobs_for_this_deck) instead of scanning all runs to find work
-- waiting on a freshly-freed slot.
SELECT run_id, id, deck_id, depends_on, steps, status,
       current_attempt_id, error, version, last_completed_step, total_steps, ambiguous_reason
FROM deck_jobs
WHERE deck_id = ? AND status = 'READY'
ORDER BY run_id ASC, id ASC;

-- name: UpdateDeckJobStatusVersioned :execrows
-- ambiguous_reason: set on AMBIGUOUS, NULLed on transitions out (resolve, retry).
UPDATE deck_jobs
SET status              = sqlc.arg(status),
    current_attempt_id  = sqlc.arg(current_attempt_id),
    error               = sqlc.arg(error),
    ambiguous_reason    = sqlc.arg(ambiguous_reason),
    version             = version + 1
WHERE run_id  = sqlc.arg(run_id)
  AND id      = sqlc.arg(id)
  AND version = sqlc.arg(version);

-- name: UpdateDeckJobStatusByCurrentStatuses :execrows
-- Used by the executor-event path: transition to a new status only if currently
-- in one of the allowed statuses, and the attempt_id matches.
-- Returns rows affected so the handler can detect duplicate-vs-fresh.
-- ambiguous_reason: set on AMBIGUOUS, NULLed on transitions out.
UPDATE deck_jobs
SET status             = sqlc.arg(status),
    current_attempt_id = sqlc.arg(current_attempt_id),
    error              = sqlc.arg(error),
    ambiguous_reason   = sqlc.arg(ambiguous_reason),
    version            = version + 1
WHERE run_id  = sqlc.arg(run_id)
  AND id      = sqlc.arg(id)
  AND current_attempt_id = sqlc.arg(prev_attempt_id)
  AND status IN (sqlc.slice('allowed_statuses'));

-- name: ListRunDeckIDs :many
-- Distinct deck_ids referenced by a run (used for "did the operator just unblock
-- this deck?" event-driven dispatch readiness eval).
SELECT DISTINCT deck_id FROM deck_jobs WHERE run_id = ?;

-- name: ListInFlightJobsForDeck :many
-- Reconciler / Liveness Monitor probe: deck_jobs currently in
-- DISPATCHED or RUNNING on a given deck. Per the per-deck slot
-- invariant there is at most one.
SELECT run_id, id, deck_id, depends_on, steps, status,
       current_attempt_id, error, version, last_completed_step, total_steps, ambiguous_reason
FROM deck_jobs
WHERE deck_id = ?1 AND status IN ('DISPATCHED', 'RUNNING');

-- name: ListInFlightDeckJobs :many
-- Startup reconciliation: every deck_job the orchestrator was
-- previously committed to (DISPATCHED, RUNNING, or AMBIGUOUS).
SELECT run_id, id, deck_id, depends_on, steps, status,
       current_attempt_id, error, version, last_completed_step, total_steps, ambiguous_reason
FROM deck_jobs
WHERE status IN ('DISPATCHED', 'RUNNING', 'AMBIGUOUS');

-- name: UpdateDeckJobStepProgress :execrows
-- Monotonic step-progress update. Advances last_completed_step to `step`
-- only when it strictly exceeds the current value (so out-of-order replays
-- are no-ops), bumps version for OCC, and refuses to apply on stale
-- attempts or jobs that have left the active-execution window. AMBIGUOUS
-- is intentionally excluded -- once a job is AMBIGUOUS the operator owns
-- resolution and a late step bump would race ResolveJob's version check.
UPDATE deck_jobs
SET last_completed_step = max(last_completed_step, CAST(sqlc.arg(step) AS INTEGER)),
    version = version + 1
WHERE run_id = sqlc.arg(run_id)
  AND id = sqlc.arg(id)
  AND current_attempt_id = sqlc.arg(current_attempt_id)
  AND status IN ('DISPATCHED', 'RUNNING')
  AND last_completed_step < CAST(sqlc.arg(step) AS INTEGER);
