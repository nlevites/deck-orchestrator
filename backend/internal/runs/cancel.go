package runs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/dispatch"
	"deck-fleet/backend/internal/eventlog"
	"deck-fleet/backend/internal/state"
	"deck-fleet/backend/internal/store"
	storegen "deck-fleet/backend/internal/store/gen"
)

// AbortScheduler dials /executor/abort after Cancel commits. Nil skips delivery.
type AbortScheduler interface {
	Schedule(ctx context.Context, deckID, attemptID string)
}

// AbortTarget is one (deck_id, attempt_id) pair scheduled for post-commit abort.
type AbortTarget struct {
	DeckID    string
	AttemptID string
}

// Cancel handles operator cancellation (API.md §8.4).
// Sentinel errors: ErrRunNotFound, ErrAlreadyTerminalRun, ErrVersionMismatch.
// FAILED jobs are cancelled too (FAILED→CANCELLED via TriggerOperatorCancel)
// so a FAILED-non-terminal run can be terminalized by the operator. See
// DESIGN.md "State machine — run status".
// AbortScheduler runs after the tx commits, never inside.
func Cancel(ctx context.Context, db *store.DB, scheduler AbortScheduler, runID string, expectedVersion int64) (gen.Run, error) {
	now := time.Now().UTC()

	var (
		run              gen.Run
		retErr           error
		collectedTargets []AbortTarget
		freedDeckIDs     []string
		notifyDecks      []string
	)

	txErr := db.WithTx(ctx, func(q *storegen.Queries) error {
		runRow, lookupErr := q.GetRun(ctx, runID)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			retErr = ErrRunNotFound
			return nil
		}
		if lookupErr != nil {
			return lookupErr
		}

		// Terminal before version check (API.md §8.4).
		if state.IsTerminalRunStatus(gen.RunStatus(runRow.Status)) {
			current, cErr := RowToRun(ctx, q, runRow, false)
			if cErr != nil {
				return cErr
			}
			run = current
			retErr = ErrAlreadyTerminalRun
			return nil
		}
		if runRow.Version != expectedVersion {
			current, cErr := RowToRun(ctx, q, runRow, false)
			if cErr != nil {
				return cErr
			}
			run = current
			retErr = ErrVersionMismatch
			return nil
		}

		jobs, lErr := q.ListDeckJobsByRun(ctx, runID)
		if lErr != nil {
			return fmt.Errorf("list jobs: %w", lErr)
		}
		// FAILED→CANCELLED is permitted (operator give-up). Skip only
		// jobs that are already terminal (COMPLETED / CANCELLED).
		freedDecks := make(map[string]struct{})
		for _, j := range jobs {
			jobStatus := gen.DeckJobStatus(j.Status)
			if state.IsTerminalJobStatus(jobStatus) {
				continue
			}
			// Include AMBIGUOUS: executor may still be running after DEADLINE_EXCEEDED.
			if j.CurrentAttemptID.Valid && (jobStatus == gen.DeckJobStatusDISPATCHED ||
				jobStatus == gen.DeckJobStatusRUNNING ||
				jobStatus == gen.DeckJobStatusAMBIGUOUS) {
				collectedTargets = append(collectedTargets, AbortTarget{
					DeckID:    j.DeckID,
					AttemptID: j.CurrentAttemptID.String,
				})
			}
			// DISPATCHED/RUNNING/AMBIGUOUS occupy a deck slot.
			if jobStatus == gen.DeckJobStatusDISPATCHED ||
				jobStatus == gen.DeckJobStatusRUNNING ||
				jobStatus == gen.DeckJobStatusAMBIGUOUS {
				freedDecks[j.DeckID] = struct{}{}
			}
			rows, err := state.ApplyVersioned(ctx, state.ApplyVersionedParams{
				Q:                 q,
				From:              jobStatus,
				To:                gen.DeckJobStatusCANCELLED,
				Trigger:           state.TriggerOperatorCancel,
				RunID:             j.RunID,
				JobID:             j.ID,
				Version:           j.Version,
				NewCurrentAttempt: j.CurrentAttemptID,
				NewError:          j.Error,
			})
			if err != nil {
				return fmt.Errorf("cancel job %s: %w", j.ID, err)
			}
			if rows == 0 {
				return fmt.Errorf("cancel race on job %s/%s", j.RunID, j.ID)
			}
			scope := eventlog.Scope{RunID: runID, JobID: j.ID, DeckID: j.DeckID}
			if j.CurrentAttemptID.Valid {
				scope.AttemptID = j.CurrentAttemptID.String
			}
			if _, eErr := eventlog.Append(ctx, q, eventlog.KindJobCancelled,
				scope, now, eventlog.JobCancelledPayload{From: jobStatus},
			); eErr != nil {
				return eErr
			}
		}
		for d := range freedDecks {
			freedDeckIDs = append(freedDeckIDs, d)
		}

		if _, _, mErr := state.MaterializeRunStatus(ctx, q, runID, now); mErr != nil {
			return mErr
		}

		// Re-dispatch other runs' READY jobs on freed decks (same tx).
		for _, deckID := range freedDeckIDs {
			dispatched, dErr := dispatch.ReadyForDeck(ctx, q, deckID, now)
			if dErr != nil {
				return fmt.Errorf("post-cancel dispatch for %s: %w", deckID, dErr)
			}
			notifyDecks = append(notifyDecks, dispatched...)
		}

		newRunRow, gErr := q.GetRun(ctx, runID)
		if gErr != nil {
			return gErr
		}
		full, fErr := RowToRun(ctx, q, newRunRow, true)
		if fErr != nil {
			return fErr
		}
		run = full
		return nil
	})

	if txErr != nil {
		return gen.Run{}, txErr
	}
	if retErr != nil {
		return run, retErr
	}

	// Post-commit abort dials use context.Background(); scheduler owns cancellation.
	if scheduler != nil {
		for _, t := range collectedTargets {
			scheduler.Schedule(context.Background(), t.DeckID, t.AttemptID)
		}
	}
	dispatch.NotifyDecks(notifyDecks)
	return run, nil
}
