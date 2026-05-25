package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"deck-fleet/backend/internal/api"
	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/dispatch"
)

// pollFallbackInterval is the safety-net re-check cadence inside the
// long-poll loop. The dispatcher fires dispatch.notifyDeck synchronously
// on every successful READY -> DISPATCHED, so most wakes happen via the
// subscribe channel; this ticker only matters if a notify was dropped
// (e.g. handler subscribed after the notify fired but before the first
// DB query, or the broker entry was GC'd between subscribe and notify).
const pollFallbackInterval = 250 * time.Millisecond

// Poll is a long-poll: hold the connection up to PollHoldMax waiting for
// a dispatched attempt on `deck_id`. Returns 200 + DispatchPayload as
// soon as one appears; 204 on deadline. Falls back to short-poll
// (single lookup, immediate 204 on miss) when PollHoldMax is zero.
//
// Idempotency: orchestrator restart re-delivers the same attempt_id; the
// executor dedupes locally via the (attempt_id) unique index in its outbox.
// Backs S1 in analysis/inefficiencies/inefficiencies.md.
func (e *ExecutorAPI) Poll(w http.ResponseWriter, r *http.Request) {
	deckID := r.URL.Query().Get("deck_id")
	if deckID == "" {
		api.WriteSimpleError(w, r, gen.ErrorCodeSCHEMAVIOLATION, "missing deck_id query parameter")
		return
	}

	// Subscribe BEFORE the first DB query so a dispatch that lands in the
	// race window between query-miss and subscribe still wakes us up.
	wake, unsubscribe := dispatch.Subscribe(deckID)
	defer unsubscribe()

	hold := e.deps.PollHoldMax
	deadline := time.Now().Add(hold)

	for {
		dispatchPayload, found, err := e.tryFetchDispatch(r.Context(), deckID)
		if err != nil {
			e.deps.Logger.Error("executor poll: fetch", "deck_id", deckID, "err", err)
			api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, err.Error())
			return
		}
		if found {
			api.WriteJSON(w, http.StatusOK, dispatchPayload)
			return
		}
		// Short-poll fallback for tests and when long-poll is disabled.
		if hold <= 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Wait for the next wake source. Use the smaller of the fallback
		// interval and the remaining hold budget so deadline always fires
		// promptly.
		waitFor := pollFallbackInterval
		if remaining < waitFor {
			waitFor = remaining
		}
		timer := time.NewTimer(waitFor)
		select {
		case <-r.Context().Done():
			timer.Stop()
			// Client disconnected. Don't bother writing — the runtime
			// silently drops; ResponseWriter writes after ctx done are
			// no-ops on net/http.
			return
		case <-wake:
			timer.Stop()
			// Drain any additional pending notifies opportunistically.
			drain(wake)
			// Loop back to re-query.
		case <-timer.C:
			// Fallback tick — re-query in case a notify was dropped.
		}
	}
}

// drain pulls any further pending notifications off the wake channel so
// we don't do back-to-back wake-then-query-then-wake-and-query.
func drain(ch <-chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func (e *ExecutorAPI) tryFetchDispatch(ctx context.Context, deckID string) (gen.DispatchPayload, bool, error) {
	row, err := e.deps.Store.ReadQueries.GetDispatchedJobForDeck(ctx, deckID)
	if errors.Is(err, sql.ErrNoRows) {
		return gen.DispatchPayload{}, false, nil
	}
	if err != nil {
		return gen.DispatchPayload{}, false, err
	}
	var steps []gen.Step
	if err := json.Unmarshal([]byte(row.Steps), &steps); err != nil {
		return gen.DispatchPayload{}, false, fmt.Errorf("unmarshal steps: %w", err)
	}
	attemptID, err := openapiUUIDFromString(row.CurrentAttemptID.String)
	if err != nil {
		return gen.DispatchPayload{}, false, fmt.Errorf("attempt_id: %w", err)
	}
	return gen.DispatchPayload{
		AttemptId: attemptID,
		RunId:     row.RunID,
		JobId:     row.ID,
		Steps:     steps,
	}, true, nil
}
