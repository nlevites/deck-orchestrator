// Package state implements deck_job transitions (STATE_MACHINE.md §3.2).
// transitions_data.go mirrors the table row-for-row for review diffs.
package state

import (
	"context"
	"database/sql"
	"fmt"

	"deck-fleet/backend/internal/api/gen"
	storegen "deck-fleet/backend/internal/store/gen"
)

type Trigger string

const (
	TriggerSchedulerReady     Trigger = "scheduler_ready"
	TriggerDispatcherClaim    Trigger = "dispatcher_claim"
	TriggerExecutorEvent      Trigger = "executor_event"
	TriggerReconciler         Trigger = "reconciler"
	TriggerLivenessMonitor    Trigger = "liveness_monitor"
	TriggerOperatorCancel     Trigger = "operator_cancel"
	TriggerOperatorRetry      Trigger = "operator_retry"
	TriggerOperatorResolution Trigger = "operator_resolution"
)

type transition struct {
	From    gen.DeckJobStatus
	To      gen.DeckJobStatus
	Trigger Trigger
}

// CanTransition reports whether (from, to, trigger) is in §3.2.
func CanTransition(from, to gen.DeckJobStatus, trigger Trigger) bool {
	for _, t := range deckJobTransitions {
		if t.From == from && t.To == to && t.Trigger == trigger {
			return true
		}
	}
	return false
}

func AllowedNextStates(from gen.DeckJobStatus) []gen.DeckJobStatus {
	var out []gen.DeckJobStatus
	seen := make(map[gen.DeckJobStatus]struct{})
	for _, t := range deckJobTransitions {
		if t.From != from {
			continue
		}
		if _, dup := seen[t.To]; dup {
			continue
		}
		seen[t.To] = struct{}{}
		out = append(out, t.To)
	}
	return out
}

// IsTerminalJobStatus: {COMPLETED, CANCELLED} per §3.1. FAILED is retryable.
func IsTerminalJobStatus(s gen.DeckJobStatus) bool {
	switch s {
	case gen.DeckJobStatusCOMPLETED, gen.DeckJobStatusCANCELLED:
		return true
	default:
		return false
	}
}

type ApplyVersionedParams struct {
	Q       *storegen.Queries
	From    gen.DeckJobStatus
	To      gen.DeckJobStatus
	Trigger Trigger

	RunID              string
	JobID              string
	Version            int64
	NewCurrentAttempt  sql.NullString
	NewError           sql.NullString
	NewAmbiguousReason sql.NullString // set on AMBIGUOUS, NULL on transitions out
}

// ApplyVersioned runs version-checked UPDATE; 0 rows = version moved.
// CanTransition false is a programming error (handlers validate from-state first).
func ApplyVersioned(ctx context.Context, p ApplyVersionedParams) (int64, error) {
	if !CanTransition(p.From, p.To, p.Trigger) {
		return 0, fmt.Errorf("state.ApplyVersioned: forbidden transition %s -> %s via %s",
			p.From, p.To, p.Trigger)
	}
	return p.Q.UpdateDeckJobStatusVersioned(ctx, storegen.UpdateDeckJobStatusVersionedParams{
		Status:           string(p.To),
		CurrentAttemptID: p.NewCurrentAttempt,
		Error:            p.NewError,
		AmbiguousReason:  p.NewAmbiguousReason,
		RunID:            p.RunID,
		ID:               p.JobID,
		Version:          p.Version,
	})
}

// ApplyByAttemptParams: attempt-token authority (no version OCC).
// Operator retries that swap current_attempt_id yield rows=0 → conflict_logged.
type ApplyByAttemptParams struct {
	Q           *storegen.Queries
	AllowedFrom []gen.DeckJobStatus
	To          gen.DeckJobStatus
	Trigger     Trigger

	RunID              string
	JobID              string
	AttemptID          string // required; stale events filtered at SQL layer (C1)
	NewCurrentAttempt  sql.NullString
	NewError           sql.NullString
	NewAmbiguousReason sql.NullString // set on AMBIGUOUS, NULL on transitions out
}

// ApplyByAttempt: guards on current_attempt_id + status-in-AllowedFrom; 0 rows = conflict_logged.
func ApplyByAttempt(ctx context.Context, p ApplyByAttemptParams) (int64, error) {
	if p.AttemptID == "" {
		return 0, fmt.Errorf("state.ApplyByAttempt: AttemptID required")
	}
	if len(p.AllowedFrom) == 0 {
		return 0, fmt.Errorf("state.ApplyByAttempt: AllowedFrom must be non-empty")
	}
	for _, f := range p.AllowedFrom {
		if !CanTransition(f, p.To, p.Trigger) {
			return 0, fmt.Errorf("state.ApplyByAttempt: forbidden transition %s -> %s via %s",
				f, p.To, p.Trigger)
		}
	}
	allowed := make([]string, 0, len(p.AllowedFrom))
	for _, f := range p.AllowedFrom {
		allowed = append(allowed, string(f))
	}
	return p.Q.UpdateDeckJobStatusByCurrentStatuses(ctx, storegen.UpdateDeckJobStatusByCurrentStatusesParams{
		Status:           string(p.To),
		CurrentAttemptID: p.NewCurrentAttempt,
		Error:            p.NewError,
		AmbiguousReason:  p.NewAmbiguousReason,
		RunID:            p.RunID,
		ID:               p.JobID,
		PrevAttemptID:    sql.NullString{String: p.AttemptID, Valid: true},
		AllowedStatuses:  allowed,
	})
}
