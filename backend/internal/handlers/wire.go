// Package handlers implements operator and executor HTTP endpoints.
// Run-lifecycle logic lives in internal/runs.
package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/runs"
	storegen "deck-fleet/backend/internal/store/gen"
)

func rowToRun(ctx context.Context, q *storegen.Queries, runRow storegen.Runs, includeRecent bool) (gen.Run, error) {
	return runs.RowToRun(ctx, q, runRow, includeRecent)
}

func rowToRunSummary(ctx context.Context, q *storegen.Queries, r storegen.Runs) (gen.RunSummary, error) {
	return runs.RowToRunSummary(ctx, q, r)
}

// rowToDeck stays here (not internal/runs): consumed by deck listing and live bootstrap.
func rowToDeck(ctx context.Context, q *storegen.Queries, d storegen.Decks) (gen.Deck, error) {
	deck := gen.Deck{
		Id:               d.ID,
		FirstSeenAt:      time.UnixMilli(d.FirstSeenAt),
		LastKnownHealth:  gen.DeckHealth(d.LastKnownHealth),
		LastHeartbeatAt:  nullInt64ToTimePtr(d.LastHeartbeatAt),
		EndpointUrl:      nullStringToPtr(d.EndpointUrl),
		DecommissionedAt: nullInt64ToTimePtr(d.DecommissionedAt),
	}
	if d.LastClaimedAttemptID.Valid {
		uid, err := openapiUUIDFromString(d.LastClaimedAttemptID.String)
		if err == nil {
			deck.LastClaimedAttemptId = &uid
		}
	}
	occ, err := q.GetDeckSlotOccupier(ctx, d.ID)
	switch {
	case err == nil:
		deck.CurrentJob = &gen.CurrentJob{
			RunId:  occ.RunID,
			JobId:  occ.ID,
			Status: gen.DeckJobStatus(occ.Status),
		}
	case errors.Is(err, sql.ErrNoRows):
	default:
		return gen.Deck{}, fmt.Errorf("slot occupier for %s: %w", d.ID, err)
	}
	return deck, nil
}

func nullStringToPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

func nullInt64ToTimePtr(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := time.UnixMilli(n.Int64)
	return &t
}

func openapiUUIDFromString(s string) (openapi_types.UUID, error) {
	return uuid.Parse(s)
}
