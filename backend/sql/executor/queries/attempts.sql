-- name: InsertReceived :execrows
-- Idempotent receipt of an attempt dispatch. INSERT OR IGNORE makes
-- redelivery of an attempt_id a no-op. RowsAffected = 1 means this
-- delivery created the row; 0 means a prior delivery already did.
INSERT OR IGNORE INTO attempts (
    attempt_id, run_id, job_id, deck_id, steps, received_at, state
) VALUES (?, ?, ?, ?, ?, ?, 'RECEIVED');

-- name: GetAttempt :one
SELECT attempt_id, run_id, job_id, deck_id, steps,
       received_at, started_at, terminal_at, state, result, error,
       abort_requested, last_completed_step
FROM attempts
WHERE attempt_id = ?;

-- name: ListRecentAttempts :many
SELECT attempt_id, run_id, job_id, deck_id, steps,
       received_at, started_at, terminal_at, state, result, error,
       abort_requested, last_completed_step
FROM attempts
ORDER BY received_at DESC
LIMIT ?;

-- name: GetCurrentInFlight :one
-- Returns the most-recent non-terminal attempt, if any. The per-deck
-- slot invariant means there is at most one row in RECEIVED or
-- IN_PROGRESS at a time.
SELECT attempt_id, run_id, job_id, deck_id, steps,
       received_at, started_at, terminal_at, state, result, error,
       abort_requested, last_completed_step
FROM attempts
WHERE state IN ('RECEIVED','IN_PROGRESS')
ORDER BY received_at DESC
LIMIT 1;

-- name: BumpStepCursor :exec
-- Monotonically advance the per-attempt step cursor; out-of-order replay
-- is a no-op via the `last_completed_step < step` guard.
UPDATE attempts
SET last_completed_step = sqlc.arg(step)
WHERE attempt_id = sqlc.arg(attempt_id)
  AND last_completed_step < sqlc.arg(step)
  AND state = 'IN_PROGRESS';

-- name: MarkInProgress :exec
UPDATE attempts
SET state = 'IN_PROGRESS', started_at = ?
WHERE attempt_id = ? AND state = 'RECEIVED';

-- name: MarkTerminal :exec
-- Set-once terminal transition. result/error use COALESCE so callers
-- can pass NULL to leave any existing values untouched.
UPDATE attempts
SET state = ?, terminal_at = ?,
    result = COALESCE(?, result),
    error  = COALESCE(?, error)
WHERE attempt_id = ? AND state IN ('RECEIVED','IN_PROGRESS');

-- name: SetAbortRequested :exec
UPDATE attempts SET abort_requested = 1 WHERE attempt_id = ?;
