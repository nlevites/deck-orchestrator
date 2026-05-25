-- name: InsertEvent :one
INSERT INTO events (occurred_at, kind, run_id, job_id, deck_id, attempt_id, payload)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING seq;

-- name: EventsSince :many
-- Live polling surface: returns events with seq > since_seq, capped at
-- `limit`. The window is the *tail* (highest seqs) -- selecting the
-- most-recent N first via the inner ORDER BY DESC + LIMIT, then
-- re-sorting ASC for the client's apply-in-order loop. Critical for
-- bootstrap (since_seq=0): the runs/decks slice arriving alongside
-- reflects current entity state, so handing the client ancient events
-- would force its event-cache to drift. With a naive ASC + LIMIT the
-- tail is silently dropped and clients gap-detect into a re-bootstrap
-- loop on long-running sessions.
SELECT seq, occurred_at, kind, run_id, job_id, deck_id, attempt_id, payload
FROM (
    SELECT seq, occurred_at, kind, run_id, job_id, deck_id, attempt_id, payload
    FROM events
    WHERE seq > ?
    ORDER BY seq DESC
    LIMIT ?
)
ORDER BY seq ASC;

-- name: EventsSinceForRun :many
-- Run-scoped variant for the run-detail screen: same monotonic
-- delivery, narrowed to events whose run_id matches. Same tail-window
-- shape as EventsSince so a long-lived run's bootstrap doesn't return
-- the oldest N events and lose recent transitions.
SELECT seq, occurred_at, kind, run_id, job_id, deck_id, attempt_id, payload
FROM (
    SELECT seq, occurred_at, kind, run_id, job_id, deck_id, attempt_id, payload
    FROM events
    WHERE run_id = ? AND seq > ?
    ORDER BY seq DESC
    LIMIT ?
)
ORDER BY seq ASC;

-- name: MaxEventSeq :one
-- Returns the highest event seq present in the events table, or 0 if
-- the table is empty. Used as the `server_seq` watermark on the live
-- polling response so clients can re-anchor across reconnects without
-- guessing.
SELECT CAST(COALESCE(MAX(seq), 0) AS INTEGER) AS server_seq FROM events;
