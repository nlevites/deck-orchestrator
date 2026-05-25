package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"deck-fleet/backend/internal/api"
	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/eventlog"
	storegen "deck-fleet/backend/internal/store/gen"
)

// ListDecks excludes decommissioned slots unless
// ?include_decommissioned=true.
func (o *Operator) ListDecks(w http.ResponseWriter, r *http.Request) {
	includeDecommissioned := r.URL.Query().Get("include_decommissioned") == "true"
	var (
		rows []storegen.Decks
		err  error
	)
	if includeDecommissioned {
		rows, err = o.deps.Store.ReadQueries.ListDecksIncludingDecommissioned(r.Context())
	} else {
		rows, err = o.deps.Store.ReadQueries.ListDecks(r.Context())
	}
	if err != nil {
		o.deps.Logger.Error("listDecks: query", "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, err.Error())
		return
	}
	out := make([]gen.Deck, 0, len(rows))
	for _, row := range rows {
		d, dErr := rowToDeck(r.Context(), o.deps.Store.ReadQueries, row)
		if dErr != nil {
			api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, dErr.Error())
			return
		}
		out = append(out, d)
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"decks": out})
}

// ReleaseDeck vacates a slot explicitly. Without it, supervisor detach leaves
// UNREACHABLE forever (no heartbeat to revert to EMPTY).
//
// Refuses when in-flight work exists (409). Decommissioned → 410. Unknown → 404.
// Already-EMPTY is idempotent.
func (o *Operator) ReleaseDeck(w http.ResponseWriter, r *http.Request) {
	deckID := r.PathValue("deck_id")
	if deckID == "" {
		api.WriteSimpleError(w, r, gen.ErrorCodeDECKNOTFOUND, "missing deck_id")
		return
	}
	now := time.Now().UTC()

	var (
		errInFlight     = errors.New("slot has in-flight work")
		errNotFound     = errors.New("deck not found")
		errDecommission = errors.New("deck decommissioned")
		updated         storegen.Decks
	)

	txErr := o.deps.Store.WithTx(r.Context(), func(q *storegen.Queries) error {
		row, gErr := q.GetDeck(r.Context(), deckID)
		if errors.Is(gErr, sql.ErrNoRows) {
			return errNotFound
		}
		if gErr != nil {
			return gErr
		}
		if row.DecommissionedAt.Valid {
			return errDecommission
		}
		busy, cErr := q.CountInFlightForDeck(r.Context(), deckID)
		if cErr != nil {
			return cErr
		}
		if busy > 0 {
			return errInFlight
		}

		priorHealth := gen.DeckHealth(row.LastKnownHealth)
		// Idempotent on already-EMPTY slots: the UPDATE still runs
		// (clearing scalars that may already be null) but no event
		// fires.
		if _, uErr := q.ReleaseDeckSlot(r.Context(), deckID); uErr != nil {
			return fmt.Errorf("release slot: %w", uErr)
		}
		if priorHealth != gen.EMPTY {
			if _, eErr := eventlog.Append(r.Context(), q, eventlog.KindDeckHealthChanged,
				eventlog.Scope{DeckID: deckID}, now,
				eventlog.DeckHealthChangedPayload{
					From:            priorHealth,
					To:              gen.EMPTY,
					LastHeartbeatAt: now,
				}); eErr != nil {
				return fmt.Errorf("emit DECK_HEALTH_CHANGED: %w", eErr)
			}
		}
		reloaded, rErr := q.GetDeck(r.Context(), deckID)
		if rErr != nil {
			return rErr
		}
		updated = reloaded
		return nil
	})
	switch {
	case errors.Is(txErr, errNotFound):
		api.WriteSimpleError(w, r, gen.ErrorCodeDECKNOTFOUND,
			fmt.Sprintf("deck %q not found", deckID))
		return
	case errors.Is(txErr, errDecommission):
		api.WriteSimpleError(w, r, gen.ErrorCodeDECKDECOMMISSIONED,
			fmt.Sprintf("deck %q is decommissioned", deckID))
		return
	case errors.Is(txErr, errInFlight):
		api.WriteSimpleError(w, r, gen.ErrorCodeSLOTHASINFLIGHTWORK,
			fmt.Sprintf("deck %q has in-flight work; cancel or resolve before release", deckID))
		return
	case txErr != nil:
		o.deps.Logger.Error("releaseDeck: tx", "deck_id", deckID, "err", txErr)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, txErr.Error())
		return
	}
	out, dErr := rowToDeck(r.Context(), o.deps.Store.ReadQueries, updated)
	if dErr != nil {
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, dErr.Error())
		return
	}
	api.WriteJSON(w, http.StatusOK, out)
}
