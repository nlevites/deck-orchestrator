package runs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/dag"
	"deck-fleet/backend/internal/dispatch"
	"deck-fleet/backend/internal/eventlog"
	"deck-fleet/backend/internal/store"
	storegen "deck-fleet/backend/internal/store/gen"
)

// Submit creates a run from an operator DAG (single write tx).
// ValidationError → 422; ErrDuplicateRun → 409 with existing Run.
func Submit(ctx context.Context, db *store.DB, body gen.DagSubmission) (gen.Run, error) {
	now := time.Now().UTC()

	var (
		run         gen.Run
		retErr      error
		validate    *ValidationError
		notifyDecks []string
	)

	txErr := db.WithTx(ctx, func(q *storegen.Queries) error {
		// Deck membership inside tx — avoids stuck-READY after decommission between read and write.
		deckIDs, dErr := q.ListKnownDeckIDs(ctx)
		if dErr != nil {
			return fmt.Errorf("list known decks: %w", dErr)
		}
		decommissionedIDs, dcErr := q.ListDecommissionedDeckIDs(ctx)
		if dcErr != nil {
			return fmt.Errorf("list decommissioned decks: %w", dcErr)
		}
		if entries := dag.Validate(body, deckIDs, decommissionedIDs); len(entries) > 0 {
			validate = &ValidationError{Entries: entries}
			return nil
		}

		existing, lookupErr := q.GetRun(ctx, body.Id)
		if lookupErr == nil {
			current, cErr := RowToRun(ctx, q, existing, false)
			if cErr != nil {
				return cErr
			}
			run = current
			retErr = ErrDuplicateRun
			return nil
		}
		if !errors.Is(lookupErr, sql.ErrNoRows) {
			return fmt.Errorf("lookup existing run: %w", lookupErr)
		}

		dagBytes, mErr := json.Marshal(body)
		if mErr != nil {
			return fmt.Errorf("marshal dag: %w", mErr)
		}

		if iErr := q.InsertRun(ctx, storegen.InsertRunParams{
			ID:          body.Id,
			Status:      string(gen.PENDING),
			Dag:         string(dagBytes),
			SubmittedAt: now.UnixMilli(),
		}); iErr != nil {
			return fmt.Errorf("insert run: %w", iErr)
		}

		for _, j := range body.DeckJobs {
			deps, dmErr := json.Marshal(j.DependsOn)
			if dmErr != nil {
				return fmt.Errorf("marshal depends_on for %s: %w", j.Id, dmErr)
			}
			steps, smErr := json.Marshal(j.Steps)
			if smErr != nil {
				return fmt.Errorf("marshal steps for %s: %w", j.Id, smErr)
			}
			if jErr := q.InsertDeckJob(ctx, storegen.InsertDeckJobParams{
				RunID:      body.Id,
				ID:         j.Id,
				DeckID:     j.DeckId,
				DependsOn:  string(deps),
				Steps:      string(steps),
				Status:     string(gen.DeckJobStatusPENDING),
				TotalSteps: int64(len(j.Steps)),
			}); jErr != nil {
				return fmt.Errorf("insert deck_job %s: %w", j.Id, jErr)
			}
		}

		if _, eErr := eventlog.Append(ctx, q, eventlog.KindRunSubmitted,
			eventlog.Scope{RunID: body.Id}, now,
			eventlog.RunSubmittedPayload{SubmittedAt: now},
		); eErr != nil {
			return eErr
		}

		// Root jobs only — all jobs start PENDING.
		for _, j := range body.DeckJobs {
			if len(j.DependsOn) > 0 {
				continue
			}
			if rerr := dispatch.PromoteToReady(ctx, q, body.Id, j.Id, now); rerr != nil {
				return rerr
			}
		}

		dispatched, dispErr := dispatch.ReadyForRun(ctx, q, body.Id, now)
		if dispErr != nil {
			return dispErr
		}
		notifyDecks = append(notifyDecks, dispatched...)

		newRun, rErr := q.GetRun(ctx, body.Id)
		if rErr != nil {
			return fmt.Errorf("reload run: %w", rErr)
		}
		full, fErr := RowToRun(ctx, q, newRun, true)
		if fErr != nil {
			return fErr
		}
		run = full
		return nil
	})

	if validate != nil {
		return gen.Run{}, validate
	}
	if errors.Is(retErr, ErrDuplicateRun) {
		return run, ErrDuplicateRun
	}
	if txErr != nil {
		return gen.Run{}, txErr
	}
	// Wake long-poll handlers AFTER commit so the woken /executor/poll
	// re-query is guaranteed to see the new DISPATCHED row.
	dispatch.NotifyDecks(notifyDecks)
	return run, nil
}
