-- name: UpsertEmptyDeckIfAbsent :execrows
-- Idempotent seed used at orchestrator boot to materialize the fleet
-- from cfg.FleetSize. Existing rows (with their history) are left
-- untouched; only new ids land here in the EMPTY state with all
-- heartbeat fields NULL.
INSERT INTO decks (id, endpoint_url, first_seen_at, last_heartbeat_at,
                   last_claimed_attempt_id, last_known_health, decommissioned_at)
VALUES (?, NULL, ?, NULL, NULL, 'EMPTY', NULL)
ON CONFLICT(id) DO NOTHING;

-- name: UpsertDeckHeartbeat :execrows
-- Heartbeat-driven update. The orchestrator pre-checks that the deck
-- row exists and is not decommissioned (handlers/executor.go), so this
-- statement is only reached on a known live slot. Transitions
-- last_known_health to HEALTHY and populates endpoint_url + heartbeat.
INSERT INTO decks (id, endpoint_url, first_seen_at, last_heartbeat_at,
                   last_claimed_attempt_id, last_known_health, decommissioned_at)
VALUES (?, ?, ?, ?, ?, 'HEALTHY', NULL)
ON CONFLICT(id) DO UPDATE SET
    endpoint_url = excluded.endpoint_url,
    last_heartbeat_at = excluded.last_heartbeat_at,
    last_claimed_attempt_id = excluded.last_claimed_attempt_id,
    last_known_health = 'HEALTHY';

-- name: GetDeck :one
SELECT id, endpoint_url, first_seen_at, last_heartbeat_at,
       last_claimed_attempt_id, last_known_health, decommissioned_at
FROM decks
WHERE id = ?;

-- name: ListDecks :many
-- Operator-facing fleet view. Excludes decommissioned slots by default.
SELECT id, endpoint_url, first_seen_at, last_heartbeat_at,
       last_claimed_attempt_id, last_known_health, decommissioned_at
FROM decks
WHERE decommissioned_at IS NULL
ORDER BY first_seen_at ASC;

-- name: ListDecksIncludingDecommissioned :many
-- Audit / history view. Returns every deck row including decommissioned ones.
SELECT id, endpoint_url, first_seen_at, last_heartbeat_at,
       last_claimed_attempt_id, last_known_health, decommissioned_at
FROM decks
ORDER BY first_seen_at ASC;

-- name: ListKnownDeckIDs :many
-- Used by DAG submission to validate UNKNOWN_DECK at semantic-validation
-- time. Decommissioned ids are excluded so submissions targeting them
-- surface as DECK_DECOMMISSIONED via a separate lookup.
SELECT id FROM decks WHERE decommissioned_at IS NULL;

-- name: ListDecommissionedDeckIDs :many
-- Used by DAG submission to distinguish a decommissioned target from a
-- truly unknown one. The validator emits DECK_DECOMMISSIONED for matches.
SELECT id FROM decks WHERE decommissioned_at IS NOT NULL;

-- name: MaxActiveDeckNumber :one
-- Highest numeric suffix of any non-decommissioned deck_id matching
-- deck-N. Used by the boot seed shrink guard. Returns 0 when empty.
SELECT CAST(COALESCE(MAX(CAST(SUBSTR(id, 6) AS INTEGER)), 0) AS INTEGER) AS max_n
FROM decks
WHERE decommissioned_at IS NULL
  AND id LIKE 'deck-%'
  AND SUBSTR(id, 6) GLOB '[0-9]*';

-- name: ListDecksHeartbeatSince :many
-- Delta path for GET /api/state. Returns the rows whose last_heartbeat_at
-- advanced inside the freshness window (cutoff = request_time - ~5s).
-- The handler unions this with decks implicated by event rows since
-- since_seq. Excludes decommissioned slots; empty slots are filtered out
-- via the IS NOT NULL guard since they have no heartbeat to advance.
-- Backs S3 in analysis/inefficiencies/inefficiencies.md.
SELECT id, endpoint_url, first_seen_at, last_heartbeat_at,
       last_claimed_attempt_id, last_known_health, decommissioned_at
FROM decks
WHERE decommissioned_at IS NULL
  AND last_heartbeat_at IS NOT NULL
  AND last_heartbeat_at >= sqlc.arg(cutoff)
ORDER BY first_seen_at ASC;

-- name: ListDecksWithStaleHeartbeat :many
-- Liveness sweep. Empty slots (last_heartbeat_at IS NULL) are skipped:
-- the param is bound twice so sqlc emits an int64 signature even though
-- the column is nullable.
SELECT id, endpoint_url, first_seen_at, last_heartbeat_at,
       last_claimed_attempt_id, last_known_health, decommissioned_at
FROM decks
WHERE last_heartbeat_at IS NOT NULL
  AND last_heartbeat_at < sqlc.arg(cutoff);

-- name: ReleaseDeckSlot :execrows
-- Operator-deliberate detach: clears endpoint_url + last_heartbeat_at
-- and forces last_known_health back to EMPTY. Idempotent on already-EMPTY
-- slots. Refused (rows=0) for decommissioned slots since those have
-- their own retired-state semantics.
UPDATE decks
SET endpoint_url = NULL,
    last_heartbeat_at = NULL,
    last_known_health = 'EMPTY'
WHERE id = ? AND decommissioned_at IS NULL;

-- name: CountInFlightForDeck :one
-- Used by ReleaseDeckSlot's pre-check: refuse to release a slot while
-- DISPATCHED/RUNNING/AMBIGUOUS work references it. AMBIGUOUS counts
-- because the operator must resolve it before the slot can be vacated.
SELECT COUNT(*) FROM deck_jobs
WHERE deck_id = ? AND status IN ('DISPATCHED','RUNNING','AMBIGUOUS');

-- name: UpdateDeckHealth :execrows
-- Move a deck between EMPTY/HEALTHY/STALE/UNREACHABLE. Used by the
-- Liveness Monitor and the Reconciler. Compare-and-swap on the prior
-- health value so a heartbeat-driven HEALTHY upsert that interleaves
-- with a sweep's STALE/UNREACHABLE write doesn't race: the loser's
-- UPDATE matches zero rows and the caller observes that via the
-- :execrows return to skip its event emission. Without the CAS the
-- emitted DECK_HEALTH_CHANGED.from could lie about the prior state.
UPDATE decks
SET last_known_health = ?
WHERE id = ? AND last_known_health = sqlc.arg(expected_health);
