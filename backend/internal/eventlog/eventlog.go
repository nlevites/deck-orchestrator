// Package eventlog is the orchestrator's append-only audit log (events table).
// Callers pass transaction-bound *storegen.Queries so appends commit with the
// triggering mutation.
package eventlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	storegen "deck-fleet/backend/internal/store/gen"
)

// ErrDuplicate is returned when Append hits the
// events_attempt_kind_unique partial index. It means the same
// (attempt_id, kind) pair already has an event row -- the caller's
// transaction will roll back if it doesn't catch this. Executor event
// ingestion converts ErrDuplicate to a 200 `duplicate` response so
// the executor's outbox stops retrying. Callers that don't care can
// treat any error as fatal.
var ErrDuplicate = errors.New("eventlog: duplicate (attempt_id, kind)")

type Scope struct {
	RunID     string // empty -> NULL
	JobID     string
	DeckID    string
	AttemptID string
}

// Append inserts an event row and returns seq. payload is JSON-marshalled
// (typed structs from payloads.go, or nil -> `{}`). occurredAt is stamped
// server-side per ARCHITECTURE.md §6.5 time authority.
func Append(
	ctx context.Context,
	q *storegen.Queries,
	kind Kind,
	scope Scope,
	occurredAt time.Time,
	payload any,
) (int64, error) {
	var (
		payloadBytes []byte
		err          error
	)
	if payload == nil {
		payloadBytes = []byte("{}")
	} else {
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return 0, fmt.Errorf("eventlog: marshal payload for %s: %w", kind, err)
		}
	}

	seq, err := q.InsertEvent(ctx, storegen.InsertEventParams{
		OccurredAt: occurredAt.UnixMilli(),
		Kind:       string(kind),
		RunID:      nullString(scope.RunID),
		JobID:      nullString(scope.JobID),
		DeckID:     nullString(scope.DeckID),
		AttemptID:  nullString(scope.AttemptID),
		Payload:    string(payloadBytes),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return 0, fmt.Errorf("%w: kind=%s attempt=%s: %v",
				ErrDuplicate, kind, scope.AttemptID, err)
		}
		return 0, fmt.Errorf("eventlog: insert %s: %w", kind, err)
	}
	return seq, nil
}

// isUniqueViolation detects events_attempt_kind_unique via modernc's typed
// SQLITE_CONSTRAINT_UNIQUE. Pre-fix this string-matched on the error message,
// which broke silently under driver format changes.
func isUniqueViolation(err error) bool {
	var sErr *sqlite.Error
	if !errors.As(err, &sErr) {
		return false
	}
	return sErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
