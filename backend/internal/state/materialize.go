package state

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/eventlog"
	storegen "deck-fleet/backend/internal/store/gen"
)

// MaterializeRunStatus recomputes and persists derived run status in the
// same tx. terminal_at is stamped only on terminal statuses (COMPLETED,
// CANCELLED) per IsTerminalRunStatus; FAILED and AMBIGUOUS leave it null
// because they're awaiting an operator decision (see DESIGN.md).
// Status changes bump runs.version so concurrent operator writes get
// VERSION_MISMATCH.
func MaterializeRunStatus(ctx context.Context, q *storegen.Queries, runID string, now time.Time) (gen.RunStatus, bool, error) {
	jobs, err := q.ListDeckJobsByRun(ctx, runID)
	if err != nil {
		return "", false, fmt.Errorf("state: load jobs for run %s: %w", runID, err)
	}
	run, err := q.GetRun(ctx, runID)
	if err != nil {
		return "", false, fmt.Errorf("state: load run %s: %w", runID, err)
	}

	newStatus := DeriveRunStatus(jobs)
	if gen.RunStatus(run.Status) == newStatus {
		return newStatus, false, nil
	}

	var terminalAt sql.NullInt64
	if IsTerminalRunStatus(newStatus) {
		terminalAt = sql.NullInt64{Int64: now.UnixMilli(), Valid: true}
	}

	rows, err := q.UpdateRunStatusUnchecked(ctx, storegen.UpdateRunStatusUncheckedParams{
		Status:     string(newStatus),
		TerminalAt: terminalAt,
		ID:         runID,
	})
	if err != nil {
		return "", false, fmt.Errorf("state: update run status %s: %w", runID, err)
	}
	if rows == 0 {
		return "", false, fmt.Errorf("state: run %s vanished mid-transition", runID)
	}

	if _, err := eventlog.Append(ctx, q, eventlog.KindRunStatusChanged,
		eventlog.Scope{RunID: runID}, now,
		eventlog.RunStatusChangedPayload{
			From: gen.RunStatus(run.Status),
			To:   newStatus,
		},
	); err != nil {
		return "", false, fmt.Errorf("state: append RUN_STATUS_CHANGED: %w", err)
	}

	return newStatus, true, nil
}
