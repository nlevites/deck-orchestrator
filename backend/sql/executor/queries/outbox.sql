-- name: EnqueueEvent :exec
INSERT INTO outbox (attempt_id, kind, payload, occurred_at, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: NextOutbox :one
SELECT seq, attempt_id, kind, payload, occurred_at, retries, last_attempt_at
FROM outbox
ORDER BY seq ASC
LIMIT 1;

-- name: DeleteOutbox :exec
DELETE FROM outbox WHERE seq = ?;

-- name: BumpOutboxRetry :exec
UPDATE outbox
SET retries = retries + 1, last_attempt_at = ?
WHERE seq = ?;
