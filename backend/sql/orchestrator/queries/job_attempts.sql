-- name: InsertJobAttempt :exec
INSERT INTO job_attempts (
    attempt_id, run_id, job_id, deck_id, dispatched_at,
    outcome, outcome_at, outcome_source, result, error, operator_note
) VALUES (?, ?, ?, ?, ?, NULL, NULL, NULL, NULL, NULL, NULL);

-- name: GetJobAttempt :one
SELECT attempt_id, run_id, job_id, deck_id, dispatched_at,
       outcome, outcome_at, outcome_source, result, error, operator_note
FROM job_attempts
WHERE attempt_id = ?;

-- name: ListAttemptsForJob :many
SELECT attempt_id, run_id, job_id, deck_id, dispatched_at,
       outcome, outcome_at, outcome_source, result, error, operator_note
FROM job_attempts
WHERE run_id = ? AND job_id = ?
ORDER BY dispatched_at DESC;

-- name: ListOverdueAttempts :many
-- Liveness sweep: attempts whose effective deadline has elapsed and are
-- still attached to a non-terminal deck_job. The effective per-attempt
-- deadline scales with workload size:
--   effective = AttemptDeadlineBase + total_steps * AttemptDeadlinePerStep
-- The caller passes the durations as milliseconds plus now (also ms).
-- Args: base_ms, per_step_ms, now_ms.
SELECT a.attempt_id, a.run_id, a.job_id, a.deck_id, a.dispatched_at,
       a.outcome, a.outcome_at, a.outcome_source, a.result, a.error, a.operator_note
FROM job_attempts a
JOIN deck_jobs j ON j.run_id = a.run_id AND j.id = a.job_id
WHERE a.dispatched_at + sqlc.arg(base_ms) + j.total_steps * sqlc.arg(per_step_ms) < sqlc.arg(now_ms)
  AND j.status IN ('DISPATCHED','RUNNING')
  AND j.current_attempt_id = a.attempt_id;

-- name: HasJobRunningEventForAttempt :one
-- Prior-evidence half-A: did we ever see a JOB_RUNNING for this attempt?
SELECT EXISTS(
    SELECT 1 FROM events
    WHERE attempt_id = ?1 AND kind = 'JOB_RUNNING'
) AS has_running;

-- name: HasClaimedHeartbeatForAttempt :one
-- Prior-evidence half-B: did any deck heartbeat claim this attempt id?
SELECT EXISTS(
    SELECT 1 FROM decks
    WHERE last_claimed_attempt_id = ?1
) AS has_claimed;

-- name: SetAttemptOutcomeIfUnset :execrows
-- Set-once invariant: outcome stays NULL until the first authoritative source
-- determines it. The WHERE outcome IS NULL guard makes the write conditional;
-- rows-affected = 0 means another path won the race.
UPDATE job_attempts
SET outcome = ?, outcome_at = ?, outcome_source = ?,
    result = COALESCE(?, result),
    error = COALESCE(?, error),
    operator_note = COALESCE(?, operator_note)
WHERE attempt_id = ? AND outcome IS NULL;
