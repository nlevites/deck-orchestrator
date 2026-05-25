-- name: InsertRun :exec
INSERT INTO runs (id, status, dag, submitted_at, terminal_at, version)
VALUES (?, ?, ?, ?, NULL, 1);

-- name: GetRun :one
SELECT id, status, dag, submitted_at, terminal_at, version
FROM runs
WHERE id = ?;

-- name: ListRuns :many
SELECT id, status, dag, submitted_at, terminal_at, version
FROM runs
ORDER BY submitted_at DESC
LIMIT ?;

-- name: ListRunsByStatus :many
SELECT id, status, dag, submitted_at, terminal_at, version
FROM runs
WHERE status = ?
ORDER BY submitted_at DESC
LIMIT ?;

-- name: UpdateRunStatusVersioned :execrows
UPDATE runs
SET status = ?, terminal_at = ?, version = version + 1
WHERE id = ? AND version = ?;

-- name: UpdateRunStatusUnchecked :execrows
-- Used by MaterializeRunStatus (derived-status writes; no version check because the
-- caller already holds the runs row in the current transaction). Returns rows affected
-- so callers can confirm the row existed.
UPDATE runs
SET status = ?, terminal_at = ?, version = version + 1
WHERE id = ?;
