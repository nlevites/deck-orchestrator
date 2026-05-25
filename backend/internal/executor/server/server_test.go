package server_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/executor/localstore"
	"deck-fleet/backend/internal/executor/server"
	"deck-fleet/backend/internal/testutil"
)

func newServer(t *testing.T) (http.Handler, *localstore.Store) {
	t.Helper()
	s := testutil.OpenLocalStore(t)
	srv := server.New("deck-test", s, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return srv.Handler(), s
}

func do(t *testing.T, h http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestHealth_returns200(t *testing.T) {
	h, _ := newServer(t)
	rec := do(t, h, http.MethodGet, "/health")
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestGetState_noAttemptID_returnsOverall(t *testing.T) {
	h, _ := newServer(t)
	rec := do(t, h, http.MethodGet, "/executor/state")
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "deck-test", body["deck_id"])
	require.NotNil(t, body["recent_attempts"])
}

func TestGetState_withAttemptID_knownAttempt(t *testing.T) {
	h, s := newServer(t)
	ctx := context.Background()
	a := localstore.Attempt{
		AttemptID: "att-1", RunID: "r1", JobID: "j1", DeckID: "deck-test",
		StepsJSON: `[]`, ReceivedAt: time.Now().UTC(),
	}
	_, err := s.InsertReceived(ctx, a)
	require.NoError(t, err)

	rec := do(t, h, http.MethodGet, "/executor/state?attempt_id=att-1")
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "att-1", body["attempt_id"])
	require.Equal(t, "RECEIVED", body["state"])
}

func TestGetState_withAttemptID_unknownAttempt(t *testing.T) {
	h, _ := newServer(t)
	rec := do(t, h, http.MethodGet, "/executor/state?attempt_id=no-such")
	require.Equal(t, http.StatusNotFound, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "UNKNOWN_ATTEMPT", body["code"])
}

func TestPostAbort_unknownAttempt(t *testing.T) {
	h, _ := newServer(t)
	rec := do(t, h, http.MethodPost, "/executor/abort/no-such")
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPostAbort_setsAbortFlag(t *testing.T) {
	h, s := newServer(t)
	ctx := context.Background()
	a := localstore.Attempt{
		AttemptID: "att-2", RunID: "r1", JobID: "j1", DeckID: "deck-test",
		StepsJSON: `[]`, ReceivedAt: time.Now().UTC(),
	}
	_, err := s.InsertReceived(ctx, a)
	require.NoError(t, err)

	rec := do(t, h, http.MethodPost, "/executor/abort/att-2")
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "abort_requested", body["status"])

	loaded, err := s.GetAttempt(ctx, "att-2")
	require.NoError(t, err)
	require.True(t, loaded.AbortRequested)
}

func TestPostAbort_alreadyTerminal(t *testing.T) {
	h, s := newServer(t)
	ctx := context.Background()
	a := localstore.Attempt{
		AttemptID: "att-3", RunID: "r1", JobID: "j1", DeckID: "deck-test",
		StepsJSON: `[]`, ReceivedAt: time.Now().UTC(),
	}
	_, err := s.InsertReceived(ctx, a)
	require.NoError(t, err)
	require.NoError(t, s.MarkInProgress(ctx, "att-3", time.Now().UTC()))
	require.NoError(t, s.MarkTerminal(ctx, "att-3", localstore.StateCompleted, "", "", time.Now().UTC()))

	rec := do(t, h, http.MethodPost, "/executor/abort/att-3")
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "already_terminal", body["status"])
}
