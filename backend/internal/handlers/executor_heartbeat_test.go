package handlers_test

import (
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/handlers"
	"deck-fleet/backend/internal/store"
	storegen "deck-fleet/backend/internal/store/gen"
	"deck-fleet/backend/internal/testutil"
)

func newHeartbeatHandler(db *store.DB) http.HandlerFunc {
	api := handlers.NewExecutorAPI(handlers.ExecutorDeps{
		Store:  db,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return api.Heartbeat
}

// TestHeartbeat_freshHeartbeatAfterStale_recoversToHealthy asserts STALE → HEALTHY
// on heartbeat and exactly one DECK_HEALTH_CHANGED (from liveness/monitor_test.go).
func TestHeartbeat_freshHeartbeatAfterStale_recoversToHealthy(t *testing.T) {
	db := testutil.OpenDB(t)

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q,
			testutil.WithDeckHeartbeat(testutil.Epoch),
			testutil.WithDeckHealth(gen.STALE),
		)
	})
	require.Equal(t, gen.STALE, gen.DeckHealth(testutil.DeckHealthFromDB(t, db, testutil.DefaultDeckID)))
	require.Zero(t, testutil.CountEventsOfKindForDeck(t, db, "DECK_HEALTH_CHANGED", testutil.DefaultDeckID),
		"no health-changed events expected before the heartbeat arrives")

	rec := testutil.Do(t, newHeartbeatHandler(db), http.MethodPost, "/executor/heartbeat",
		gen.Heartbeat{
			DeckId:      testutil.DefaultDeckID,
			EndpointUrl: "http://127.0.0.1:0",
		})
	require.Equal(t, http.StatusNoContent, rec.Code,
		"successful heartbeat returns 204 (body=%s)", rec.Body.String())

	require.Equal(t, gen.HEALTHY, gen.DeckHealth(testutil.DeckHealthFromDB(t, db, testutil.DefaultDeckID)),
		"deck must transition STALE → HEALTHY on heartbeat write")
	require.Equal(t, 1, testutil.CountEventsOfKindForDeck(t, db, "DECK_HEALTH_CHANGED", testutil.DefaultDeckID),
		"exactly one DECK_HEALTH_CHANGED event must be emitted on recovery")
}

func TestHeartbeat_alreadyHealthy_emitsNoEvent(t *testing.T) {
	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q) // defaults: HEALTHY
	})

	rec := testutil.Do(t, newHeartbeatHandler(db), http.MethodPost, "/executor/heartbeat",
		gen.Heartbeat{
			DeckId:      testutil.DefaultDeckID,
			EndpointUrl: "http://127.0.0.1:0",
		})
	require.Equal(t, http.StatusNoContent, rec.Code)

	require.Equal(t, gen.HEALTHY, gen.DeckHealth(testutil.DeckHealthFromDB(t, db, testutil.DefaultDeckID)))
	require.Zero(t, testutil.CountEventsOfKindForDeck(t, db, "DECK_HEALTH_CHANGED", testutil.DefaultDeckID),
		"no DECK_HEALTH_CHANGED event for HEALTHY → HEALTHY heartbeat")
}
