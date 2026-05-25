package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"deck-fleet/backend/internal/api"
	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/dispatch"
	"deck-fleet/backend/internal/eventlog"
	"deck-fleet/backend/internal/state"
	storegen "deck-fleet/backend/internal/store/gen"
)

var errUnknownAttempt = errors.New("handlers: unknown attempt")

// Event implements layered dedupe (ApplyByAttempt + events_attempt_kind_unique).
// ApplyByAttempt closes the C1 hole where a late COMPLETED for a superseded
// attempt could overwrite a retried job (old path only checked runs.version).
// Every "stop retrying" outcome returns 200; 409 is for operator concurrency.
func (e *ExecutorAPI) Event(w http.ResponseWriter, r *http.Request) {
	body, err := api.DecodeAndValidate[gen.ExecutorEventRequest](w, r)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	attemptID := body.AttemptId.String()

	payload, pErr := decodeExecutorPayload(body.Payload)
	if pErr != nil {
		api.WriteSimpleError(w, r, gen.ErrorCodeSCHEMAVIOLATION, pErr.Error())
		return
	}

	var (
		response    any
		retErr      error
		notifyDecks []string
	)

	txErr := e.deps.Store.WithTx(r.Context(), func(q *storegen.Queries) error {
		attempt, aErr := q.GetJobAttempt(r.Context(), attemptID)
		if errors.Is(aErr, sql.ErrNoRows) {
			retErr = errUnknownAttempt
			return nil
		}
		if aErr != nil {
			return aErr
		}
		jobRow, jErr := q.GetDeckJob(r.Context(), storegen.GetDeckJobParams{RunID: attempt.RunID, ID: attempt.JobID})
		if jErr != nil {
			return jErr
		}

		jobStatus := gen.DeckJobStatus(jobRow.Status)

		// Order matters: prior-outcome duplicate/conflict before terminal-job guard,
		// so a replayed COMPLETED on an already-COMPLETED job is duplicate, not conflict.
		if attempt.Outcome.Valid {
			if matchesOutcome(attempt.Outcome.String, string(body.Kind)) {
				response = gen.ExecutorEventDuplicate{
					Status:               gen.Duplicate,
					CurrentOutcome:       nullableAttemptOutcome(attempt.Outcome),
					CurrentOutcomeSource: nullableOutcomeSource(attempt.OutcomeSource),
				}
				return nil
			}
			if cErr := writeConflict(r.Context(), q, body, attempt, jobRow, now); cErr != nil {
				return cErr
			}
			response = gen.ExecutorEventConflictLogged{
				Status:               gen.ConflictLogged,
				YourReportedOutcome:  body.Kind,
				CurrentOutcome:       nullableAttemptOutcome(attempt.Outcome),
				CurrentOutcomeSource: nullableOutcomeSource(attempt.OutcomeSource),
			}
			return nil
		}

		// Terminal deck_job with no attempt outcome (e.g. operator cancelled
		// while still DISPATCHED).
		if jobStatus == gen.DeckJobStatusCANCELLED || state.IsTerminalJobStatus(jobStatus) {
			if cErr := writeConflict(r.Context(), q, body, attempt, jobRow, now); cErr != nil {
				return cErr
			}
			payload := gen.ExecutorEventConflictLogged{
				Status:               gen.ConflictLogged,
				YourReportedOutcome:  body.Kind,
				CurrentOutcome:       nullableAttemptOutcome(attempt.Outcome),
				CurrentOutcomeSource: nullableOutcomeSource(attempt.OutcomeSource),
			}
			if jobStatus == gen.DeckJobStatusCANCELLED {
				s := jobStatus
				payload.CurrentJobStatus = &s
			}
			response = payload
			return nil
		}

		// Progress-only; monotonic guards live in UpdateDeckJobStepProgress.
		if body.Kind == gen.ExecutorEventKindSTEPCOMPLETED {
			resp, sErr := e.applyStepCompleted(r.Context(), q, body, payload, attempt, jobRow, attemptID, now)
			if sErr != nil {
				return sErr
			}
			response = resp
			return nil
		}

		newStatus, kind := mapExecutorEventToJobStatus(body.Kind)
		if newStatus == "" {
			return fmt.Errorf("unknown executor event kind %q", body.Kind)
		}

		newError := jobRow.Error
		if body.Kind == gen.ExecutorEventKindFAILED && payload.Error != "" {
			newError = sql.NullString{String: payload.Error, Valid: true}
		}

		rows, uErr := state.ApplyByAttempt(r.Context(), state.ApplyByAttemptParams{
			Q:                 q,
			AllowedFrom:       allowedFromStatusesTyped(body.Kind),
			To:                newStatus,
			Trigger:           state.TriggerExecutorEvent,
			RunID:             jobRow.RunID,
			JobID:             jobRow.ID,
			AttemptID:         attemptID,
			NewCurrentAttempt: jobRow.CurrentAttemptID,
			NewError:          newError,
		})
		if uErr != nil {
			return fmt.Errorf("transition job: %w", uErr)
		}
		if rows == 0 {
			// Stale attempt, raced terminal, or both — record conflict so the outbox stops.
			if cErr := writeConflict(r.Context(), q, body, attempt, jobRow, now); cErr != nil {
				return cErr
			}
			response = gen.ExecutorEventConflictLogged{
				Status:               gen.ConflictLogged,
				YourReportedOutcome:  body.Kind,
				CurrentOutcome:       nullableAttemptOutcome(attempt.Outcome),
				CurrentOutcomeSource: nullableOutcomeSource(attempt.OutcomeSource),
			}
			return nil
		}

		if body.Kind == gen.ExecutorEventKindCOMPLETED || body.Kind == gen.ExecutorEventKindFAILED {
			var (
				result sql.NullString
				errStr sql.NullString
			)
			if len(payload.Result) > 0 {
				result = sql.NullString{String: string(payload.Result), Valid: true}
			}
			if payload.Error != "" {
				errStr = sql.NullString{String: payload.Error, Valid: true}
			}
			if _, sErr := q.SetAttemptOutcomeIfUnset(r.Context(), storegen.SetAttemptOutcomeIfUnsetParams{
				Outcome:       sql.NullString{String: string(body.Kind), Valid: true},
				OutcomeAt:     sql.NullInt64{Int64: now.UnixMilli(), Valid: true},
				OutcomeSource: sql.NullString{String: string(gen.EXECUTOREVENT), Valid: true},
				Result:        result,
				Error:         errStr,
				OperatorNote:  sql.NullString{},
				AttemptID:     attemptID,
			}); sErr != nil {
				return fmt.Errorf("set outcome: %w", sErr)
			}
		}

		scope := eventlog.Scope{
			RunID:     jobRow.RunID,
			JobID:     jobRow.ID,
			DeckID:    jobRow.DeckID,
			AttemptID: attemptID,
		}
		var logPayload any
		switch kind {
		case eventlog.KindJobRunning:
			logPayload = eventlog.JobRunningPayload{From: jobStatus}
		case eventlog.KindJobCompleted:
			logPayload = eventlog.JobCompletedPayload{
				From:          jobStatus,
				OutcomeSource: gen.EXECUTOREVENT,
				Result:        payload.Result,
			}
		case eventlog.KindJobFailed:
			logPayload = eventlog.JobFailedPayload{
				From:          jobStatus,
				OutcomeSource: gen.EXECUTOREVENT,
				Error:         payload.Error,
			}
		default:
		}
		if _, eErr := eventlog.Append(r.Context(), q, kind, scope, now, logPayload); eErr != nil {
			if errors.Is(eErr, eventlog.ErrDuplicate) {
				// Concurrent duplicate: unique-index violation used to 500 and retry-storm the outbox.
				e.deps.Logger.Info("executor event: concurrent duplicate detected",
					"attempt_id", attemptID, "kind", body.Kind)
				reloaded, gErr := q.GetJobAttempt(r.Context(), attemptID)
				if gErr != nil {
					return gErr
				}
				response = gen.ExecutorEventDuplicate{
					Status:               gen.Duplicate,
					CurrentOutcome:       nullableAttemptOutcome(reloaded.Outcome),
					CurrentOutcomeSource: nullableOutcomeSource(reloaded.OutcomeSource),
				}
				return nil
			}
			return eErr
		}

		if body.Kind == gen.ExecutorEventKindCOMPLETED {
			if pErr := dispatch.PromoteDownstreamReady(r.Context(), q, jobRow.RunID, now); pErr != nil {
				return pErr
			}
			dispatched, dErr := dispatch.ReadyForRun(r.Context(), q, jobRow.RunID, now)
			if dErr != nil {
				return dErr
			}
			notifyDecks = append(notifyDecks, dispatched...)
		}
		// Freed slot: dispatch next READY job across all runs for this deck.
		freed, dErr := dispatch.ReadyForDeck(r.Context(), q, jobRow.DeckID, now)
		if dErr != nil {
			return dErr
		}
		notifyDecks = append(notifyDecks, freed...)
		if _, _, mErr := state.MaterializeRunStatus(r.Context(), q, jobRow.RunID, now); mErr != nil {
			return mErr
		}

		updated, uErr := q.GetDeckJob(r.Context(), storegen.GetDeckJobParams{RunID: jobRow.RunID, ID: jobRow.ID})
		if uErr != nil {
			return uErr
		}
		response = gen.ExecutorEventApplied{
			Status:        gen.ExecutorEventAppliedStatusApplied,
			NewJobStatus:  gen.DeckJobStatus(updated.Status),
			NewJobVersion: updated.Version,
		}
		return nil
	})

	if txErr != nil {
		e.deps.Logger.Error("executor event: tx", "attempt_id", attemptID, "err", txErr)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, txErr.Error())
		return
	}

	if errors.Is(retErr, errUnknownAttempt) {
		api.WriteSimpleError(w, r, gen.ErrorCodeUNKNOWNATTEMPT, fmt.Sprintf("attempt %q does not exist", attemptID))
		return
	}
	dispatch.NotifyDecks(notifyDecks)
	api.WriteJSON(w, http.StatusOK, response)
}

// applyStepCompleted is progress-only. Malformed steps return conflict_logged
// (not 500) so the executor outbox stops retrying.
func (e *ExecutorAPI) applyStepCompleted(
	ctx context.Context,
	q *storegen.Queries,
	body gen.ExecutorEventRequest,
	payload executorEventPayload,
	attempt storegen.JobAttempts,
	jobRow storegen.DeckJobs,
	attemptID string,
	now time.Time,
) (any, error) {
	step, total := payload.Step, payload.Total
	if step <= 0 {
		// Malformed STEP_COMPLETED → conflict (not error) to stop the outbox.
		return gen.ExecutorEventConflictLogged{
			Status:               gen.ConflictLogged,
			YourReportedOutcome:  body.Kind,
			CurrentOutcome:       nullableAttemptOutcome(attempt.Outcome),
			CurrentOutcomeSource: nullableOutcomeSource(attempt.OutcomeSource),
		}, nil
	}

	rows, uErr := q.UpdateDeckJobStepProgress(ctx, storegen.UpdateDeckJobStepProgressParams{
		Step:             int64(step),
		RunID:            jobRow.RunID,
		ID:               jobRow.ID,
		CurrentAttemptID: sql.NullString{String: attemptID, Valid: true},
	})
	if uErr != nil {
		return nil, fmt.Errorf("step progress update: %w", uErr)
	}
	if rows == 0 {
		// Monotonic guard or attempt mismatch; pre-fix returned applied and over-counted progress.
		reloaded, gErr := q.GetJobAttempt(ctx, attemptID)
		if gErr != nil {
			return nil, gErr
		}
		return gen.ExecutorEventDuplicate{
			Status:               gen.Duplicate,
			CurrentOutcome:       nullableAttemptOutcome(reloaded.Outcome),
			CurrentOutcomeSource: nullableOutcomeSource(reloaded.OutcomeSource),
		}, nil
	}

	if _, eErr := eventlog.Append(ctx, q, eventlog.KindJobStepCompleted,
		eventlog.Scope{
			RunID:     jobRow.RunID,
			JobID:     jobRow.ID,
			DeckID:    jobRow.DeckID,
			AttemptID: attemptID,
		}, now,
		eventlog.JobStepCompletedPayload{
			Step:      step,
			Total:     total,
			AttemptID: attemptID,
		}); eErr != nil {
		if errors.Is(eErr, eventlog.ErrDuplicate) {
			// Step event already written by a concurrent apply.
			e.deps.Logger.Info("executor event: concurrent step duplicate", "attempt_id", attemptID, "step", step)
		} else {
			return nil, eErr
		}
	}

	updated, gErr := q.GetDeckJob(ctx, storegen.GetDeckJobParams{RunID: jobRow.RunID, ID: jobRow.ID})
	if gErr != nil {
		return nil, gErr
	}
	return gen.ExecutorEventApplied{
		Status:        gen.ExecutorEventAppliedStatusApplied,
		NewJobStatus:  gen.DeckJobStatus(updated.Status),
		NewJobVersion: updated.Version,
	}, nil
}
