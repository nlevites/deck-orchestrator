package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"deck-fleet/backend/internal/api"
	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/dispatch"
	"deck-fleet/backend/internal/eventlog"
	storegen "deck-fleet/backend/internal/store/gen"
)

// Heartbeat rejects unknown deck_id (404) and decommissioned slots (410).
// The executor treats both as fatal config errors (exit 78).
//
// Metadata, health-change events, and recovery dispatch share one tx so
// a crash cannot leave a deck HEALTHY without dispatch attempted.
// ReadyForDeck is indexed by (deck_id, status='READY'), not a full-run scan.
func (e *ExecutorAPI) Heartbeat(w http.ResponseWriter, r *http.Request) {
	body, err := api.DecodeAndValidate[gen.Heartbeat](w, r)
	if err != nil {
		return
	}
	now := time.Now().UTC()

	var (
		notInFleet     bool
		decommissioned bool
		notifyDecks    []string
	)

	txErr := e.deps.Store.WithTx(r.Context(), func(q *storegen.Queries) error {
		prior, lookupErr := q.GetDeck(r.Context(), body.DeckId)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			notInFleet = true
			return nil
		}
		if lookupErr != nil {
			return lookupErr
		}
		if prior.DecommissionedAt.Valid {
			decommissioned = true
			return nil
		}

		// EMPTY → HEALTHY: first attach or post-EMPTY recovery; emits DECK_REGISTERED.
		priorHealth := gen.DeckHealth(prior.LastKnownHealth)
		firstAttach := priorHealth == gen.EMPTY

		var claimed sql.NullString
		if body.CurrentAttemptId != nil {
			claimed = sql.NullString{String: body.CurrentAttemptId.String(), Valid: true}
		}
		if _, err := q.UpsertDeckHeartbeat(r.Context(), storegen.UpsertDeckHeartbeatParams{
			ID:                   body.DeckId,
			EndpointUrl:          sql.NullString{String: body.EndpointUrl, Valid: true},
			FirstSeenAt:          prior.FirstSeenAt,
			LastHeartbeatAt:      sql.NullInt64{Int64: now.UnixMilli(), Valid: true},
			LastClaimedAttemptID: claimed,
		}); err != nil {
			return fmt.Errorf("upsert deck: %w", err)
		}

		if firstAttach {
			if _, eErr := eventlog.Append(r.Context(), q, eventlog.KindDeckRegistered,
				eventlog.Scope{DeckID: body.DeckId}, now,
				eventlog.DeckRegisteredPayload{EndpointURL: body.EndpointUrl, FirstSeenAt: now},
			); eErr != nil {
				return eErr
			}
		} else if priorHealth != gen.HEALTHY {
			if _, eErr := eventlog.Append(r.Context(), q, eventlog.KindDeckHealthChanged,
				eventlog.Scope{DeckID: body.DeckId}, now,
				eventlog.DeckHealthChangedPayload{
					From:            priorHealth,
					To:              gen.HEALTHY,
					LastHeartbeatAt: now,
				},
			); eErr != nil {
				return eErr
			}
		}

		if firstAttach || priorHealth != gen.HEALTHY {
			dispatched, err := dispatch.ReadyForDeck(r.Context(), q, body.DeckId, now)
			if err != nil {
				return fmt.Errorf("dispatch on heartbeat recovery: %w", err)
			}
			notifyDecks = append(notifyDecks, dispatched...)
		}

		return nil
	})
	if txErr != nil {
		e.deps.Logger.Error("heartbeat: tx", "deck_id", body.DeckId, "err", txErr)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, txErr.Error())
		return
	}
	if notInFleet {
		api.WriteSimpleError(w, r, gen.ErrorCodeDECKNOTFOUND,
			fmt.Sprintf("deck %q is not in this orchestrator's fleet", body.DeckId))
		return
	}
	if decommissioned {
		api.WriteSimpleError(w, r, gen.ErrorCodeDECKDECOMMISSIONED,
			fmt.Sprintf("deck %q has been decommissioned and no longer accepts heartbeats", body.DeckId))
		return
	}
	dispatch.NotifyDecks(notifyDecks)
	w.WriteHeader(http.StatusNoContent)
}
