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

// Resolve handles AMBIGUOUS deck_job operator resolution (API.md §8.6).
// resolution must be COMPLETED or FAILED. Same sentinels as Retry plus ErrInvalidResolution.
// COMPLETED unblocks downstream jobs; either outcome frees the deck slot (ReadyForDeck).
//
// scheduler dials /executor/abort after the tx commits, but only when the
// operator declared FAILED: COMPLETED means the operator already accepted
// the physical outcome, so a still-alive executor finishing the work is
// consistent with that decision. FAILED is the case where a still-alive
// executor could duplicate physical work — we ask it to stop. Best-effort.
func Resolve(ctx context.Context, db *store.DB, scheduler AbortScheduler, runID, jobID string, resolution gen.AttemptOutcome, operatorNote string, expectedVersion int64) (gen.Run, error) {
	if resolution != gen.AttemptOutcomeCOMPLETED && resolution != gen.AttemptOutcomeFAILED {
		return gen.Run{}, ErrInvalidResolution
	}
	now := time.Now().UTC()

	var (
		run         gen.Run
		retErr      error
		abortTarget *AbortTarget
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
		if gen.DeckJobStatus(jobRow.Status) != gen.DeckJobStatusAMBIGUOUS {
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

		var note sql.NullString
		if operatorNote != "" {
			note = sql.NullString{String: operatorNote, Valid: true}
		}

		if jobRow.CurrentAttemptID.Valid {
			if _, soErr := q.SetAttemptOutcomeIfUnset(ctx, storegen.SetAttemptOutcomeIfUnsetParams{
				Outcome:       sql.NullString{String: string(resolution), Valid: true},
				OutcomeAt:     sql.NullInt64{Int64: now.UnixMilli(), Valid: true},
				OutcomeSource: sql.NullString{String: string(gen.OPERATORRESOLUTION), Valid: true},
				Result:        sql.NullString{},
				Error:         sql.NullString{},
				OperatorNote:  note,
				AttemptID:     jobRow.CurrentAttemptID.String,
			}); soErr != nil {
				return fmt.Errorf("set outcome: %w", soErr)
			}
		}

		rows, uErr := state.ApplyVersioned(ctx, state.ApplyVersionedParams{
			Q:                 q,
			From:              gen.DeckJobStatusAMBIGUOUS,
			To:                gen.DeckJobStatus(resolution),
			Trigger:           state.TriggerOperatorResolution,
			RunID:             runID,
			JobID:             jobID,
			Version:           jobRow.Version,
			NewCurrentAttempt: jobRow.CurrentAttemptID,
			NewError:          jobRow.Error,
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

		scope := eventlog.Scope{RunID: runID, JobID: jobID, DeckID: jobRow.DeckID}
		if jobRow.CurrentAttemptID.Valid {
			scope.AttemptID = jobRow.CurrentAttemptID.String
		}
		resolvedPayload := eventlog.JobResolvedPayload{
			From:       gen.DeckJobStatusAMBIGUOUS,
			Resolution: resolution,
		}
		if operatorNote != "" {
			n := operatorNote
			resolvedPayload.OperatorNote = &n
		}
		if _, eErr := eventlog.Append(ctx, q, eventlog.KindJobResolved,
			scope, now, resolvedPayload,
		); eErr != nil {
			return eErr
		}

		// COMPLETED: promote downstream + dispatch this run; both outcomes free the deck.
		if resolution == gen.AttemptOutcomeCOMPLETED {
			if pErr := dispatch.PromoteDownstreamReady(ctx, q, runID, now); pErr != nil {
				return pErr
			}
			dispatched, dErr := dispatch.ReadyForRun(ctx, q, runID, now)
			if dErr != nil {
				return dErr
			}
			notifyDecks = append(notifyDecks, dispatched...)
		}
		freed, dErr := dispatch.ReadyForDeck(ctx, q, jobRow.DeckID, now)
		if dErr != nil {
			return dErr
		}
		notifyDecks = append(notifyDecks, freed...)
		if _, _, mErr := state.MaterializeRunStatus(ctx, q, runID, now); mErr != nil {
			return mErr
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

		if resolution == gen.AttemptOutcomeFAILED && jobRow.CurrentAttemptID.Valid {
			abortTarget = &AbortTarget{
				DeckID:    jobRow.DeckID,
				AttemptID: jobRow.CurrentAttemptID.String,
			}
		}
		return nil
	})

	if txErr != nil {
		return gen.Run{}, txErr
	}
	if retErr != nil {
		return run, retErr
	}

	if scheduler != nil && abortTarget != nil {
		scheduler.Schedule(context.Background(), abortTarget.DeckID, abortTarget.AttemptID)
	}
	dispatch.NotifyDecks(notifyDecks)
	return run, nil
}
