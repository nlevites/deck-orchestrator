package runs

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/dispatch"
	"deck-fleet/backend/internal/eventlog"
	"deck-fleet/backend/internal/state"
	"deck-fleet/backend/internal/store"
	storegen "deck-fleet/backend/internal/store/gen"
)

// Retry handles FAILED deck_job operator retry (API.md §8.5).
// Resets to READY (clears attempt + error), emits JOB_RETRIED, runs ReadyForRun.
func Retry(ctx context.Context, db *store.DB, runID, jobID string, expectedVersion int64) (gen.Run, error) {
	now := time.Now().UTC()

	var (
		run         gen.Run
		retErr      error
		notifyDecks []string
	)

	txErr := db.WithTx(ctx, func(q *storegen.Queries) error {
		runRow, rErr := q.GetRun(ctx, runID)
		if errors.Is(rErr, sql.ErrNoRows) {
			retErr = ErrRunNotFound
			return nil
		}
		if rErr != nil {
			return rErr
		}
		jobRow, jErr := q.GetDeckJob(ctx, storegen.GetDeckJobParams{RunID: runID, ID: jobID})
		if errors.Is(jErr, sql.ErrNoRows) {
			retErr = ErrJobNotFound
			return nil
		}
		if jErr != nil {
			return jErr
		}

		if state.IsTerminalRunStatus(gen.RunStatus(runRow.Status)) {
			current, cErr := RowToRun(ctx, q, runRow, false)
			if cErr != nil {
				return cErr
			}
			run = current
			retErr = ErrAlreadyTerminalRun
			return nil
		}
		if gen.DeckJobStatus(jobRow.Status) != gen.DeckJobStatusFAILED {
			current, cErr := RowToRun(ctx, q, runRow, false)
			if cErr != nil {
				return cErr
			}
			run = current
			retErr = ErrInvalidTransition
			return nil
		}
		if jobRow.Version != expectedVersion {
			current, cErr := RowToRun(ctx, q, runRow, false)
			if cErr != nil {
				return cErr
			}
			run = current
			retErr = ErrVersionMismatch
			return nil
		}

		prev := jobRow.CurrentAttemptID
		rows, uErr := state.ApplyVersioned(ctx, state.ApplyVersionedParams{
			Q:                 q,
			From:              gen.DeckJobStatusFAILED,
			To:                gen.DeckJobStatusREADY,
			Trigger:           state.TriggerOperatorRetry,
			RunID:             runID,
			JobID:             jobID,
			Version:           jobRow.Version,
			NewCurrentAttempt: sql.NullString{},
			NewError:          sql.NullString{},
		})
		if uErr != nil {
			return uErr
		}
		if rows == 0 {
			current, cErr := RowToRun(ctx, q, runRow, false)
			if cErr != nil {
				return cErr
			}
			run = current
			retErr = ErrVersionMismatch
			return nil
		}

		retriedPayload := eventlog.JobRetriedPayload{From: gen.DeckJobStatusFAILED}
		if prev.Valid {
			if u, pErr := uuid.Parse(prev.String); pErr == nil {
				retriedPayload.PreviousAttemptID = u
			}
		}
		if _, eErr := eventlog.Append(ctx, q, eventlog.KindJobRetried,
			eventlog.Scope{RunID: runID, JobID: jobID, DeckID: jobRow.DeckID},
			now, retriedPayload,
		); eErr != nil {
			return eErr
		}

		dispatched, dErr := dispatch.ReadyForRun(ctx, q, runID, now)
		if dErr != nil {
			return dErr
		}
		notifyDecks = append(notifyDecks, dispatched...)

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
	dispatch.NotifyDecks(notifyDecks)
	return run, nil
}
