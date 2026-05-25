-- +goose Up
-- +goose StatementBegin

-- attempts is the executor's authoritative per-deck log of every dispatch it
-- has ever seen. The set-once write of `state = 'RECEIVED'` is the load-
-- bearing idempotency record from ARCHITECTURE.md §5.1: re-delivery of an
-- attempt_id is safe because INSERT OR IGNORE makes the second arrival a no-op
-- and the worker reports the stored outcome (or current progress) from this
-- table instead of running the work a second time.
--
-- last_completed_step is the per-step crash-resume cursor (C2):
-- monotonically non-decreasing within an attempt; the worker bumps it
-- inside the same SQL statement that records the step's completion, so a
-- crash mid-attempt either rolls back the bump (replay step) or has it
-- intact (skip step) -- both branches are safe.
CREATE TABLE attempts (
    attempt_id           TEXT PRIMARY KEY,
    run_id               TEXT NOT NULL,
    job_id               TEXT NOT NULL,
    deck_id              TEXT NOT NULL,
    steps                TEXT NOT NULL CHECK (json_valid(steps)),
    received_at          INTEGER NOT NULL,
    started_at           INTEGER,
    terminal_at          INTEGER,
    state                TEXT NOT NULL CHECK (state IN (
                             'RECEIVED','IN_PROGRESS','COMPLETED','FAILED'
                         )),
    result               TEXT CHECK (result IS NULL OR json_valid(result)),
    error                TEXT,
    abort_requested      INTEGER NOT NULL DEFAULT 0,
    last_completed_step  INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX attempts_state_idx ON attempts (state);

-- outbox holds executor-side state events that have not yet been delivered to
-- the orchestrator with a 2xx response. Each row is one (attempt_id, kind)
-- the orchestrator's idempotent /executor/events handler already dedupes;
-- the outbox just makes sure we never lose the *attempt* to deliver it.
CREATE TABLE outbox (
    seq             INTEGER PRIMARY KEY AUTOINCREMENT,
    attempt_id      TEXT NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN (
                        'RUNNING','COMPLETED','FAILED','STEP_COMPLETED'
                    )),
    payload         TEXT NOT NULL CHECK (json_valid(payload)),
    occurred_at     INTEGER NOT NULL,
    created_at      INTEGER NOT NULL,
    last_attempt_at INTEGER,
    retries         INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX outbox_attempt_kind_idx ON outbox (attempt_id, kind);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS outbox;
DROP TABLE IF EXISTS attempts;

-- +goose StatementEnd
