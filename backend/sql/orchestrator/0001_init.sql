-- +goose Up
-- +goose StatementBegin

CREATE TABLE runs (
    id            TEXT PRIMARY KEY,
    status        TEXT NOT NULL CHECK (status IN (
                      'PENDING','RUNNING','COMPLETED','FAILED','AMBIGUOUS','CANCELLED'
                  )),
    dag           TEXT NOT NULL CHECK (json_valid(dag)),
    submitted_at  INTEGER NOT NULL,
    terminal_at   INTEGER,
    version       INTEGER NOT NULL DEFAULT 1,
    CHECK ((terminal_at IS NULL) OR (status IN ('COMPLETED','FAILED','CANCELLED')))
);

CREATE INDEX runs_status_submitted_at_idx ON runs (status, submitted_at DESC);

CREATE TABLE deck_jobs (
    run_id               TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    id                   TEXT NOT NULL,
    deck_id              TEXT NOT NULL,
    depends_on           TEXT NOT NULL CHECK (json_valid(depends_on)),
    steps                TEXT NOT NULL CHECK (json_valid(steps)),
    status               TEXT NOT NULL CHECK (status IN (
                             'PENDING','READY','DISPATCHED','RUNNING',
                             'COMPLETED','FAILED','AMBIGUOUS','CANCELLED'
                         )),
    current_attempt_id   TEXT,
    error                TEXT,
    version              INTEGER NOT NULL DEFAULT 1,
    last_completed_step  INTEGER NOT NULL DEFAULT 0,
    total_steps          INTEGER NOT NULL DEFAULT 0,
    ambiguous_reason     TEXT CHECK (ambiguous_reason IS NULL OR ambiguous_reason IN (
                             'DEADLINE_ELAPSED',
                             'EXECUTOR_REPORTED_UNKNOWN',
                             'DEADLINE_EXCEEDED'
                         )),
    PRIMARY KEY (run_id, id)
);

CREATE INDEX deck_jobs_deck_status_idx ON deck_jobs (deck_id, status);
CREATE INDEX deck_jobs_status_idx ON deck_jobs (status);

-- Defense-in-depth for the per-deck slot invariant (STATE_MACHINE §9.2):
-- at most one job per deck in {DISPATCHED, RUNNING, AMBIGUOUS} at any time.
CREATE UNIQUE INDEX deck_jobs_one_active_per_deck ON deck_jobs (deck_id)
    WHERE status IN ('DISPATCHED', 'RUNNING', 'AMBIGUOUS');

CREATE TABLE job_attempts (
    attempt_id      TEXT PRIMARY KEY,
    run_id          TEXT NOT NULL,
    job_id          TEXT NOT NULL,
    deck_id         TEXT NOT NULL,
    dispatched_at   INTEGER NOT NULL,
    outcome         TEXT CHECK (outcome IN (
                        'COMPLETED','FAILED'
                    )),
    outcome_at      INTEGER,
    outcome_source  TEXT CHECK (outcome_source IN (
                        'EXECUTOR_EVENT','RECONCILE','OPERATOR_RESOLUTION'
                    )),
    result          TEXT CHECK (result IS NULL OR json_valid(result)),
    error           TEXT,
    operator_note   TEXT,
    FOREIGN KEY (run_id, job_id) REFERENCES deck_jobs(run_id, id) ON DELETE CASCADE,
    CHECK (
        (outcome IS NULL AND outcome_at IS NULL AND outcome_source IS NULL)
        OR
        (outcome IS NOT NULL AND outcome_at IS NOT NULL AND outcome_source IS NOT NULL)
    )
);

CREATE INDEX job_attempts_run_job_idx ON job_attempts (run_id, job_id, dispatched_at DESC);
CREATE INDEX job_attempts_deck_dispatched_idx ON job_attempts (deck_id, dispatched_at DESC);

CREATE TABLE events (
    seq          INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at  INTEGER NOT NULL,
    kind         TEXT NOT NULL CHECK (kind IN (
                     'RUN_SUBMITTED',
                     'RUN_STATUS_CHANGED',
                     'JOB_READY',
                     'JOB_DISPATCHED',
                     'JOB_RUNNING',
                     'JOB_STEP_COMPLETED',
                     'JOB_COMPLETED',
                     'JOB_FAILED',
                     'JOB_AMBIGUOUS',
                     'JOB_CANCELLED',
                     'JOB_RESOLVED',
                     'JOB_RETRIED',
                     'DECK_REGISTERED',
                     'DECK_HEALTH_CHANGED',
                     'EXECUTOR_CONFLICT_LOGGED'
                 )),
    run_id       TEXT,
    job_id       TEXT,
    deck_id      TEXT,
    attempt_id   TEXT,
    payload      TEXT NOT NULL CHECK (json_valid(payload))
);

CREATE INDEX events_run_seq_idx ON events (run_id, seq);
CREATE INDEX events_attempt_kind_idx ON events (attempt_id, kind) WHERE attempt_id IS NOT NULL;

-- Defense-in-depth uniqueness for per-attempt-per-kind events.
-- EXECUTOR_CONFLICT_LOGGED is excluded so we record one conflict per
-- delivery attempt; JOB_STEP_COMPLETED is excluded because a single
-- attempt emits one row per step.
CREATE UNIQUE INDEX events_attempt_kind_unique ON events (attempt_id, kind)
    WHERE attempt_id IS NOT NULL
      AND kind != 'EXECUTOR_CONFLICT_LOGGED'
      AND kind != 'JOB_STEP_COMPLETED';

-- Decks are persistent slot identities owned by the orchestrator, seeded
-- from cfg.FleetSize at boot. Executors are transient processes that
-- attach via heartbeat. endpoint_url and last_heartbeat_at are NULL on
-- an empty slot; last_known_health is 'EMPTY' until first heartbeat.
-- decommissioned_at allows history-preserving shrink.
CREATE TABLE decks (
    id                       TEXT PRIMARY KEY,
    endpoint_url             TEXT,
    first_seen_at            INTEGER NOT NULL,
    last_heartbeat_at        INTEGER,
    last_claimed_attempt_id  TEXT,
    last_known_health        TEXT NOT NULL CHECK (last_known_health IN (
                                 'EMPTY','HEALTHY','STALE','UNREACHABLE'
                             )),
    decommissioned_at        INTEGER
);

CREATE INDEX decks_last_heartbeat_idx ON decks (last_heartbeat_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS decks;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS job_attempts;
DROP TABLE IF EXISTS deck_jobs;
DROP TABLE IF EXISTS runs;

-- +goose StatementEnd
