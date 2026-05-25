package localstore_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/executor/localstore"
	"deck-fleet/backend/internal/testutil"
)

func baseAttempt(id string) localstore.Attempt {
	return localstore.Attempt{
		AttemptID:  id,
		RunID:      "run-1",
		JobID:      "job-1",
		DeckID:     "deck-1",
		StepsJSON:  `[{"type":"noop","description":"test"}]`,
		ReceivedAt: time.Now().UTC(),
	}
}

func TestOpen_emptyPath_returnsError(t *testing.T) {
	_, err := localstore.Open(context.Background(), "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.Error(t, err)
}

func TestOpen_validPath_migratesAndReturns(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	require.NotNil(t, s)
}

func TestInsertReceived_insertsThenDedupes(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	ctx := context.Background()

	inserted, err := s.InsertReceived(ctx, baseAttempt("a-1"))
	require.NoError(t, err)
	require.True(t, inserted, "first insert should return inserted=true")

	inserted2, err := s.InsertReceived(ctx, baseAttempt("a-1"))
	require.NoError(t, err)
	require.False(t, inserted2, "duplicate insert should return inserted=false")
}

func TestGetAttempt_returnsReceivedState(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	ctx := context.Background()

	_, err := s.InsertReceived(ctx, baseAttempt("a-2"))
	require.NoError(t, err)

	a, err := s.GetAttempt(ctx, "a-2")
	require.NoError(t, err)
	require.Equal(t, "a-2", a.AttemptID)
	require.Equal(t, localstore.StateReceived, a.State)
	require.False(t, a.AbortRequested)
}

func TestCurrentInFlight_returnsNilWhenEmpty(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	_, ok, err := s.CurrentInFlight(context.Background())
	require.NoError(t, err)
	require.False(t, ok)
}

func TestCurrentInFlight_returnsInProgressAttempt(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	ctx := context.Background()
	_, err := s.InsertReceived(ctx, baseAttempt("a-3"))
	require.NoError(t, err)
	require.NoError(t, s.MarkInProgress(ctx, "a-3", time.Now().UTC()))

	a, ok, err := s.CurrentInFlight(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, localstore.StateInProgress, a.State)
}

func TestMarkInProgress_transitionsState(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	ctx := context.Background()
	_, err := s.InsertReceived(ctx, baseAttempt("a-4"))
	require.NoError(t, err)
	now := time.Now().UTC()
	require.NoError(t, s.MarkInProgress(ctx, "a-4", now))

	a, err := s.GetAttempt(ctx, "a-4")
	require.NoError(t, err)
	require.Equal(t, localstore.StateInProgress, a.State)
	require.NotNil(t, a.StartedAt)
}

func TestMarkTerminal_completedState(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	ctx := context.Background()
	_, err := s.InsertReceived(ctx, baseAttempt("a-5"))
	require.NoError(t, err)
	require.NoError(t, s.MarkInProgress(ctx, "a-5", time.Now().UTC()))
	now := time.Now().UTC()
	require.NoError(t, s.MarkTerminal(ctx, "a-5", localstore.StateCompleted, `{"ok":true}`, "", now))

	a, err := s.GetAttempt(ctx, "a-5")
	require.NoError(t, err)
	require.Equal(t, localstore.StateCompleted, a.State)
	require.NotNil(t, a.TerminalAt)
}

func TestMarkTerminal_failedState(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	ctx := context.Background()
	_, err := s.InsertReceived(ctx, baseAttempt("a-6"))
	require.NoError(t, err)
	require.NoError(t, s.MarkInProgress(ctx, "a-6", time.Now().UTC()))
	require.NoError(t, s.MarkTerminal(ctx, "a-6", localstore.StateFailed, "", "boom", time.Now().UTC()))

	a, err := s.GetAttempt(ctx, "a-6")
	require.NoError(t, err)
	require.Equal(t, localstore.StateFailed, a.State)
	require.NotNil(t, a.Error)
	require.Equal(t, "boom", *a.Error)
}

func TestMarkTerminal_badState_returnsError(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	err := s.MarkTerminal(context.Background(), "x", "RUNNING", "", "", time.Now().UTC())
	require.Error(t, err)
}

func TestSetAbortRequested_setsFlag(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	ctx := context.Background()
	_, err := s.InsertReceived(ctx, baseAttempt("a-7"))
	require.NoError(t, err)
	require.NoError(t, s.SetAbortRequested(ctx, "a-7"))

	a, err := s.GetAttempt(ctx, "a-7")
	require.NoError(t, err)
	require.True(t, a.AbortRequested)
}

func TestEnqueueEvent_andNextOutbox(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	ctx := context.Background()
	_, err := s.InsertReceived(ctx, baseAttempt("a-8"))
	require.NoError(t, err)
	now := time.Now().UTC()
	require.NoError(t, s.EnqueueEvent(ctx, "a-8", localstore.EventRunning, map[string]any{}, now))

	row, ok, err := s.NextOutbox(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "a-8", row.AttemptID)
	require.Equal(t, localstore.EventRunning, row.Kind)
}

func TestDeleteOutbox_removesRow(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	ctx := context.Background()
	_, err := s.InsertReceived(ctx, baseAttempt("a-9"))
	require.NoError(t, err)
	require.NoError(t, s.EnqueueEvent(ctx, "a-9", localstore.EventCompleted, nil, time.Now().UTC()))

	row, ok, err := s.NextOutbox(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, s.DeleteOutbox(ctx, row.Seq))

	_, ok2, err := s.NextOutbox(ctx)
	require.NoError(t, err)
	require.False(t, ok2)
}

func TestBumpOutboxRetry_incrementsRetries(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	ctx := context.Background()
	_, err := s.InsertReceived(ctx, baseAttempt("a-10"))
	require.NoError(t, err)
	require.NoError(t, s.EnqueueEvent(ctx, "a-10", localstore.EventFailed, nil, time.Now().UTC()))

	row1, ok, err := s.NextOutbox(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	now := time.Now().UTC()
	require.NoError(t, s.BumpOutboxRetry(ctx, row1.Seq, now))

	row2, ok, err := s.NextOutbox(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.Greater(t, row2.Retries, row1.Retries, "retries should have incremented")
}

func TestListRecentAttempts_orderedByRecency(t *testing.T) {
	s := testutil.OpenLocalStore(t)
	ctx := context.Background()
	for _, id := range []string{"b-1", "b-2", "b-3"} {
		a := baseAttempt(id)
		a.ReceivedAt = time.Now().UTC()
		_, err := s.InsertReceived(ctx, a)
		require.NoError(t, err)
	}
	list, err := s.ListRecentAttempts(ctx, 10)
	require.NoError(t, err)
	require.Len(t, list, 3)
}
