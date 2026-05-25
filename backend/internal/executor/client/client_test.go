package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/executor/client"
)

func newClient(serverURL string) *client.Client {
	c := client.New(serverURL)
	return c
}

func TestPoll_204_returnsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	c := newClient(srv.URL)
	_, ok, err := c.Poll(context.Background(), "deck-1")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestPoll_200_returnsDispatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"attempt_id": "att-xyz",
			"run_id":     "run-1",
			"job_id":     "job-1",
			"steps":      []map[string]string{{"type": "noop", "description": "x"}},
		})
	}))
	t.Cleanup(srv.Close)
	c := newClient(srv.URL)
	d, ok, err := c.Poll(context.Background(), "deck-1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "att-xyz", d.AttemptID)
	require.Equal(t, "job-1", d.JobID)
}

func TestPoll_500_returnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := newClient(srv.URL)
	_, _, err := c.Poll(context.Background(), "deck-1")
	require.Error(t, err)
}

func TestHeartbeat_204_returnsNil(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	c := newClient(srv.URL)
	err := c.Heartbeat(context.Background(), "deck-1", "http://localhost:9090", "")
	require.NoError(t, err)
	require.Equal(t, "deck-1", received["deck_id"])
	require.Equal(t, "http://localhost:9090", received["endpoint_url"])
	_, hasAttempt := received["current_attempt_id"]
	require.False(t, hasAttempt, "empty attempt_id should not be in body")
}

func TestHeartbeat_withAttemptID(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	c := newClient(srv.URL)
	err := c.Heartbeat(context.Background(), "deck-1", "http://x", "att-99")
	require.NoError(t, err)
	require.Equal(t, "att-99", received["current_attempt_id"])
}

func TestHeartbeat_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	c := newClient(srv.URL)
	err := c.Heartbeat(context.Background(), "d", "http://x", "")
	require.Error(t, err)
}

func TestPostEvent_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	c := newClient(srv.URL)
	ok, err := c.PostEvent(context.Background(), "att-1", "COMPLETED", []byte(`{}`), time.Now().UTC())
	require.NoError(t, err)
	require.True(t, ok)
}

// C3: 404 must not delete the outbox row (delivered=false keeps retrying).
func TestPostEvent_404_isRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	c := newClient(srv.URL)
	ok, err := c.PostEvent(context.Background(), "att-1", "COMPLETED", nil, time.Now().UTC())
	require.False(t, ok)
	require.Error(t, err)
}

func TestPostEvent_500_isRetryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := newClient(srv.URL)
	ok, err := c.PostEvent(context.Background(), "att-1", "RUNNING", nil, time.Now().UTC())
	require.False(t, ok)
	require.Error(t, err)
}
