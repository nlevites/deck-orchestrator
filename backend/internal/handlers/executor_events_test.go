package handlers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/handlers"
	"deck-fleet/backend/internal/store"
	storegen "deck-fleet/backend/internal/store/gen"
	"deck-fleet/backend/internal/testutil"
)

func newExecutorAPI(db *store.DB) *handlers.ExecutorAPI {
	return handlers.NewExecutorAPI(handlers.ExecutorDeps{
		Store:  db,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func doEvent(t *testing.T, api *handlers.ExecutorAPI, body gen.ExecutorEventRequest) *httptest.ResponseRecorder {
	t.Helper()
	return testutil.Do(t, http.HandlerFunc(api.Event), http.MethodPost, "/executor/events", body)
}

func doPoll(t *testing.T, api *handlers.ExecutorAPI, deckID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/executor/poll?deck_id="+deckID, nil)
	rec := httptest.NewRecorder()
	api.Poll(rec, req)
	return rec
}

func seedDispatchedAttempt(t *testing.T, db *store.DB) string {
	t.Helper()
	var attemptID string
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q)
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusDISPATCHED))
		attemptID = testutil.SeedAttempt(t, q,
			testutil.DefaultRunID, testutil.DefaultJobID, testutil.DefaultDeckID,
			testutil.WithAttemptDispatchedAt(time.Now().UTC()))
		_, err := q.UpdateDeckJobStatusVersioned(context.Background(),
			storegen.UpdateDeckJobStatusVersionedParams{
				Status:           string(gen.DeckJobStatusDISPATCHED),
				CurrentAttemptID: sql.NullString{String: attemptID, Valid: true},
				RunID:            testutil.DefaultRunID,
				ID:               testutil.DefaultJobID,
				Version:          1,
			})
		require.NoError(t, err)
	})
	return attemptID
}

func mustParseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	u, err := uuid.Parse(s)
	require.NoError(t, err)
	return u
}

func TestPoll_missingDeckID_returns400(t *testing.T) {
	db := testutil.OpenDB(t)
	api := newExecutorAPI(db)
	req := httptest.NewRequest(http.MethodGet, "/executor/poll", nil)
	rec := httptest.NewRecorder()
	api.Poll(rec, req)
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeSCHEMAVIOLATION)
}

func TestPoll_noPendingWork_returns204(t *testing.T) {
	db := testutil.OpenDB(t)
	api := newExecutorAPI(db)
	rec := doPoll(t, api, "deck-1")
	require.Equal(t, http.StatusNoContent, rec.Code)
}

func TestPoll_dispatchedJob_returns200WithPayload(t *testing.T) {
	db := testutil.OpenDB(t)
	attemptID := seedDispatchedAttempt(t, db)
	api := newExecutorAPI(db)

	rec := doPoll(t, api, testutil.DefaultDeckID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var payload gen.DispatchPayload
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, attemptID, payload.AttemptId.String())
	require.Equal(t, testutil.DefaultJobID, payload.JobId)
}

func TestEvent_unknownAttempt_returns404(t *testing.T) {
	db := testutil.OpenDB(t)
	api := newExecutorAPI(db)

	fakeUUID := mustParseUUID(t, "01967c4a-6060-7000-8000-000000000001")
	rec := doEvent(t, api, gen.ExecutorEventRequest{
		AttemptId: fakeUUID,
		Kind:      gen.ExecutorEventKindCOMPLETED,
	})
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeUNKNOWNATTEMPT)
}

func TestEvent_running_transitionsDISPATCHEDtoRUNNING(t *testing.T) {
	db := testutil.OpenDB(t)
	attemptID := seedDispatchedAttempt(t, db)
	api := newExecutorAPI(db)

	rec := doEvent(t, api, gen.ExecutorEventRequest{
		AttemptId: mustParseUUID(t, attemptID),
		Kind:      gen.ExecutorEventKindRUNNING,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	job, err := db.ReadQueries.GetDeckJob(context.Background(), storegen.GetDeckJobParams{
		RunID: testutil.DefaultRunID, ID: testutil.DefaultJobID,
	})
	require.NoError(t, err)
	require.Equal(t, string(gen.DeckJobStatusRUNNING), job.Status)
}

func TestEvent_completed_transitionsRUNNINGtoCOMPLETED(t *testing.T) {
	db := testutil.OpenDB(t)
	attemptID := seedDispatchedAttempt(t, db)
	api := newExecutorAPI(db)

	doEvent(t, api, gen.ExecutorEventRequest{
		AttemptId: mustParseUUID(t, attemptID),
		Kind:      gen.ExecutorEventKindRUNNING,
	})
	rec := doEvent(t, api, gen.ExecutorEventRequest{
		AttemptId: mustParseUUID(t, attemptID),
		Kind:      gen.ExecutorEventKindCOMPLETED,
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	job, _ := db.ReadQueries.GetDeckJob(context.Background(), storegen.GetDeckJobParams{
		RunID: testutil.DefaultRunID, ID: testutil.DefaultJobID,
	})
	require.Equal(t, string(gen.DeckJobStatusCOMPLETED), job.Status)
}

func TestEvent_failed_transitionsToFAILED(t *testing.T) {
	db := testutil.OpenDB(t)
	attemptID := seedDispatchedAttempt(t, db)
	api := newExecutorAPI(db)

	payload := map[string]any{"error": "something blew up"}
	rec := doEvent(t, api, gen.ExecutorEventRequest{
		AttemptId: mustParseUUID(t, attemptID),
		Kind:      gen.ExecutorEventKindFAILED,
		Payload:   &payload,
	})
	require.Equal(t, http.StatusOK, rec.Code)

	job, _ := db.ReadQueries.GetDeckJob(context.Background(), storegen.GetDeckJobParams{
		RunID: testutil.DefaultRunID, ID: testutil.DefaultJobID,
	})
	require.Equal(t, string(gen.DeckJobStatusFAILED), job.Status)
}

func TestEvent_duplicate_returnsDuplicateStatus(t *testing.T) {
	db := testutil.OpenDB(t)
	attemptID := seedDispatchedAttempt(t, db)
	api := newExecutorAPI(db)

	req := gen.ExecutorEventRequest{
		AttemptId: mustParseUUID(t, attemptID),
		Kind:      gen.ExecutorEventKindCOMPLETED,
	}
	doEvent(t, api, req)
	rec2 := doEvent(t, api, req)
	require.Equal(t, http.StatusOK, rec2.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &body))
	require.Equal(t, "duplicate", fmt.Sprintf("%v", body["status"]))
}

func TestEvent_conflict_logsConflict(t *testing.T) {
	db := testutil.OpenDB(t)
	var attemptID string
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q)
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusCANCELLED))
		attemptID = testutil.SeedAttempt(t, q,
			testutil.DefaultRunID, testutil.DefaultJobID, testutil.DefaultDeckID)
		// Bind attempt but leave job CANCELLED.
		rows, err := q.UpdateDeckJobStatusVersioned(context.Background(),
			storegen.UpdateDeckJobStatusVersionedParams{
				Status:           string(gen.DeckJobStatusCANCELLED),
				CurrentAttemptID: sql.NullString{String: attemptID, Valid: true},
				RunID:            testutil.DefaultRunID,
				ID:               testutil.DefaultJobID,
				Version:          1,
			})
		require.NoError(t, err)
		require.Equal(t, int64(1), rows)
	})
	api := newExecutorAPI(db)
	rec := doEvent(t, api, gen.ExecutorEventRequest{
		AttemptId: mustParseUUID(t, attemptID),
		Kind:      gen.ExecutorEventKindCOMPLETED,
	})
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "conflict_logged", fmt.Sprintf("%v", body["status"]))
}

func TestListDecks_returnsSeededDeck(t *testing.T) {
	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) { testutil.SeedDeck(t, q) })

	op := newOperator(db)
	req := httptest.NewRequest(http.MethodGet, "/api/decks", nil)
	rec := httptest.NewRecorder()
	op.ListDecks(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	decks, _ := body["decks"].([]any)
	require.Len(t, decks, 1)
}
