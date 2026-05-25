// Package dispatch is the inline dispatcher: callers invoke it inside the
// same transaction that produced READY work or freed a deck slot (submit,
// retry, resolve, terminal events, cancel, reconcile apply, heartbeat recovery).
//
// ReadyForRun — one run's READY jobs after submit/retry/resolve.
// ReadyForDeck — first eligible READY job for a deck after a slot frees.
// PromoteDownstreamReady — PENDING → READY when deps are COMPLETED.
//
// Dispatch checks: slot free, deck HEALTHY and not decommissioned, then
// allocates attempt_id, transitions READY→DISPATCHED, emits JOB_DISPATCHED.
// ReadyFor* materializes affected run status before return.
package dispatch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/eventlog"
	"deck-fleet/backend/internal/state"
	storegen "deck-fleet/backend/internal/store/gen"
)

// ReadyForRun returns the deck IDs that were just dispatched and need a
// post-commit NotifyDeck wake. Caller MUST fire NotifyDecks(...) after
// the enclosing tx commits.
func ReadyForRun(ctx context.Context, q *storegen.Queries, runID string, now time.Time) ([]string, error) {
	jobs, err := q.ListDeckJobsByRun(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("dispatch.ReadyForRun: list jobs: %w", err)
	}
	var notify []string
	for _, j := range jobs {
		if gen.DeckJobStatus(j.Status) != gen.DeckJobStatusREADY {
			continue
		}
		ok, err := tryDispatch(ctx, q, j, now)
		if err != nil {
			return nil, err
		}
		if ok {
			notify = append(notify, j.DeckID)
		}
	}
	if len(notify) > 0 {
		if _, _, err := state.MaterializeRunStatus(ctx, q, runID, now); err != nil {
			return nil, fmt.Errorf("dispatch.ReadyForRun: materialize: %w", err)
		}
	}
	return notify, nil
}

// ReadyForDeck returns the (single) deck ID that was dispatched, if
// any. Caller MUST fire NotifyDecks(...) after the enclosing tx commits.
func ReadyForDeck(ctx context.Context, q *storegen.Queries, deckID string, now time.Time) ([]string, error) {
	jobs, err := q.ListReadyJobsForDeck(ctx, deckID)
	if err != nil {
		return nil, fmt.Errorf("dispatch.ReadyForDeck: list ready jobs: %w", err)
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	for _, j := range jobs {
		ok, err := tryDispatch(ctx, q, j, now)
		if err != nil {
			return nil, err
		}
		if ok {
			if _, _, mErr := state.MaterializeRunStatus(ctx, q, j.RunID, now); mErr != nil {
				return nil, fmt.Errorf("dispatch.ReadyForDeck: materialize: %w", mErr)
			}
			// One dispatch per slot-free event; other READY jobs wait.
			return []string{j.DeckID}, nil
		}
	}
	return nil, nil
}

func PromoteDownstreamReady(ctx context.Context, q *storegen.Queries, runID string, now time.Time) error {
	jobs, err := q.ListDeckJobsByRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("dispatch.PromoteDownstreamReady: list jobs: %w", err)
	}
	statusByID := make(map[string]gen.DeckJobStatus, len(jobs))
	for _, j := range jobs {
		statusByID[j.ID] = gen.DeckJobStatus(j.Status)
	}
	for _, j := range jobs {
		if gen.DeckJobStatus(j.Status) != gen.DeckJobStatusPENDING {
			continue
		}
		var deps []string
		if err := json.Unmarshal([]byte(j.DependsOn), &deps); err != nil {
			return fmt.Errorf("dispatch.PromoteDownstreamReady: parse depends_on for %s: %w", j.ID, err)
		}
		allOK := true
		for _, d := range deps {
			if statusByID[d] != gen.DeckJobStatusCOMPLETED {
				allOK = false
				break
			}
		}
		if !allOK {
			continue
		}
		if err := promoteToReady(ctx, q, runID, j.ID, now); err != nil {
			return err
		}
	}
	return nil
}

func tryDispatch(ctx context.Context, q *storegen.Queries, j storegen.DeckJobs, now time.Time) (bool, error) {
	busy, err := q.CountDeckSlotOccupiers(ctx, j.DeckID)
	if err != nil {
		return false, fmt.Errorf("dispatch.tryDispatch: slot check %s: %w", j.DeckID, err)
	}
	if busy > 0 {
		return false, nil
	}
	deck, err := q.GetDeck(ctx, j.DeckID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("dispatch.tryDispatch: get deck %s: %w", j.DeckID, err)
	}
	// Decommissioned between submit validation and dispatch — runtime guard.
	if deck.DecommissionedAt.Valid {
		return false, nil
	}
	if gen.DeckHealth(deck.LastKnownHealth) != gen.HEALTHY {
		return false, nil
	}

	attemptUUID, uErr := uuid.NewV7()
	if uErr != nil {
		attemptUUID = uuid.New()
	}
	attemptID := attemptUUID.String()

	if iErr := q.InsertJobAttempt(ctx, storegen.InsertJobAttemptParams{
		AttemptID:    attemptID,
		RunID:        j.RunID,
		JobID:        j.ID,
		DeckID:       j.DeckID,
		DispatchedAt: now.UnixMilli(),
	}); iErr != nil {
		return false, fmt.Errorf("dispatch.tryDispatch: insert attempt: %w", iErr)
	}

	rows, tErr := state.ApplyVersioned(ctx, state.ApplyVersionedParams{
		Q:                 q,
		From:              gen.DeckJobStatusREADY,
		To:                gen.DeckJobStatusDISPATCHED,
		Trigger:           state.TriggerDispatcherClaim,
		RunID:             j.RunID,
		JobID:             j.ID,
		Version:           j.Version,
		NewCurrentAttempt: sql.NullString{String: attemptID, Valid: true},
		NewError:          j.Error,
	})
	if tErr != nil {
		return false, fmt.Errorf("dispatch.tryDispatch: transition: %w", tErr)
	}
	if rows == 0 {
		// Lost READY→DISPATCHED race: skip instead of aborting the tx
		// (pre-fix returned an error and could discard unrelated commits).
		return false, nil
	}

	if _, err := eventlog.Append(ctx, q, eventlog.KindJobDispatched,
		eventlog.Scope{RunID: j.RunID, JobID: j.ID, DeckID: j.DeckID, AttemptID: attemptID},
		now, eventlog.JobDispatchedPayload{From: gen.DeckJobStatusREADY},
	); err != nil {
		return false, err
	}
	// Caller is responsible for calling NotifyDeck(j.DeckID) AFTER the
	// enclosing tx commits — see ReadyForRun / ReadyForDeck return values.
	return true, nil
}

func promoteToReady(ctx context.Context, q *storegen.Queries, runID, jobID string, now time.Time) error {
	jobRow, err := q.GetDeckJob(ctx, storegen.GetDeckJobParams{RunID: runID, ID: jobID})
	if err != nil {
		return fmt.Errorf("dispatch.promoteToReady: get %s/%s: %w", runID, jobID, err)
	}
	if gen.DeckJobStatus(jobRow.Status) != gen.DeckJobStatusPENDING {
		return nil
	}
	rows, err := state.ApplyVersioned(ctx, state.ApplyVersionedParams{
		Q:                 q,
		From:              gen.DeckJobStatusPENDING,
		To:                gen.DeckJobStatusREADY,
		Trigger:           state.TriggerSchedulerReady,
		RunID:             runID,
		JobID:             jobID,
		Version:           jobRow.Version,
		NewCurrentAttempt: jobRow.CurrentAttemptID,
		NewError:          jobRow.Error,
	})
	if err != nil {
		return fmt.Errorf("dispatch.promoteToReady: %s/%s: %w", runID, jobID, err)
	}
	if rows == 0 {
		return fmt.Errorf("dispatch.promoteToReady: race on %s/%s", runID, jobID)
	}
	_, err = eventlog.Append(ctx, q, eventlog.KindJobReady,
		eventlog.Scope{RunID: runID, JobID: jobID, DeckID: jobRow.DeckID},
		now, eventlog.JobReadyPayload{From: gen.DeckJobStatusPENDING})
	return err
}

func PromoteToReady(ctx context.Context, q *storegen.Queries, runID, jobID string, now time.Time) error {
	return promoteToReady(ctx, q, runID, jobID, now)
}
