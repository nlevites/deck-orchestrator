package eventlog_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/eventlog"
	storegen "deck-fleet/backend/internal/store/gen"
	"deck-fleet/backend/internal/testutil"
)

func TestKind_constants_matchExpectedStrings(t *testing.T) {
	cases := []struct {
		name string
		got  eventlog.Kind
		want string
	}{
		{"KindRunSubmitted", eventlog.KindRunSubmitted, "RUN_SUBMITTED"},
		{"KindRunStatusChanged", eventlog.KindRunStatusChanged, "RUN_STATUS_CHANGED"},
		{"KindJobReady", eventlog.KindJobReady, "JOB_READY"},
		{"KindJobDispatched", eventlog.KindJobDispatched, "JOB_DISPATCHED"},
		{"KindJobRunning", eventlog.KindJobRunning, "JOB_RUNNING"},
		{"KindJobCompleted", eventlog.KindJobCompleted, "JOB_COMPLETED"},
		{"KindJobFailed", eventlog.KindJobFailed, "JOB_FAILED"},
		{"KindJobAmbiguous", eventlog.KindJobAmbiguous, "JOB_AMBIGUOUS"},
		{"KindJobCancelled", eventlog.KindJobCancelled, "JOB_CANCELLED"},
		{"KindJobResolved", eventlog.KindJobResolved, "JOB_RESOLVED"},
		{"KindJobRetried", eventlog.KindJobRetried, "JOB_RETRIED"},
		{"KindDeckRegistered", eventlog.KindDeckRegistered, "DECK_REGISTERED"},
		{"KindDeckHealthChanged", eventlog.KindDeckHealthChanged, "DECK_HEALTH_CHANGED"},
		{"KindExecutorConflictLogged", eventlog.KindExecutorConflictLogged, "EXECUTOR_CONFLICT_LOGGED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, string(tc.got))
		})
	}
}

func TestPayloads_jsonRoundTrip(t *testing.T) {
	ts := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	prevAttempt := uuid.MustParse("018f4e3a-1234-7000-8000-000000000001")
	result := json.RawMessage(`{"key":"value"}`)
	note := "resolved by operator"
	outcome := gen.AttemptOutcomeCOMPLETED
	source := gen.EXECUTOREVENT

	tests := []struct {
		name    string
		payload any
		fresh   func() any
	}{
		{
			name:    "RunSubmittedPayload",
			payload: eventlog.RunSubmittedPayload{SubmittedAt: ts},
			fresh:   func() any { return &eventlog.RunSubmittedPayload{} },
		},
		{
			name:    "RunStatusChangedPayload",
			payload: eventlog.RunStatusChangedPayload{From: gen.PENDING, To: gen.RUNNING},
			fresh:   func() any { return &eventlog.RunStatusChangedPayload{} },
		},
		{
			name:    "JobCompletedPayload_with_result",
			payload: eventlog.JobCompletedPayload{OutcomeSource: gen.EXECUTOREVENT, Result: result},
			fresh:   func() any { return &eventlog.JobCompletedPayload{} },
		},
		{
			name:    "JobCompletedPayload_without_result",
			payload: eventlog.JobCompletedPayload{OutcomeSource: gen.RECONCILE, Result: nil},
			fresh:   func() any { return &eventlog.JobCompletedPayload{} },
		},
		{
			name:    "JobFailedPayload",
			payload: eventlog.JobFailedPayload{OutcomeSource: gen.EXECUTOREVENT, Error: "connection refused"},
			fresh:   func() any { return &eventlog.JobFailedPayload{} },
		},
		{
			name:    "JobResolvedPayload_with_note",
			payload: eventlog.JobResolvedPayload{Resolution: gen.AttemptOutcomeCOMPLETED, OperatorNote: &note},
			fresh:   func() any { return &eventlog.JobResolvedPayload{} },
		},
		{
			name:    "JobResolvedPayload_without_note",
			payload: eventlog.JobResolvedPayload{Resolution: gen.AttemptOutcomeFAILED, OperatorNote: nil},
			fresh:   func() any { return &eventlog.JobResolvedPayload{} },
		},
		{
			name:    "JobRetriedPayload",
			payload: eventlog.JobRetriedPayload{PreviousAttemptID: prevAttempt},
			fresh:   func() any { return &eventlog.JobRetriedPayload{} },
		},
		{
			name:    "DeckRegisteredPayload",
			payload: eventlog.DeckRegisteredPayload{EndpointURL: "http://deck.example.com:9000", FirstSeenAt: ts},
			fresh:   func() any { return &eventlog.DeckRegisteredPayload{} },
		},
		{
			name:    "DeckHealthChangedPayload",
			payload: eventlog.DeckHealthChangedPayload{From: gen.HEALTHY, To: gen.STALE, LastHeartbeatAt: ts},
			fresh:   func() any { return &eventlog.DeckHealthChangedPayload{} },
		},
		{
			name:    "JobAmbiguousPayload",
			payload: eventlog.JobAmbiguousPayload{Reason: eventlog.AmbiguousReasonDeadlineElapsed},
			fresh:   func() any { return &eventlog.JobAmbiguousPayload{} },
		},
		{
			name: "ExecutorConflictLoggedPayload",
			payload: eventlog.ExecutorConflictLoggedPayload{
				ExecutorReported:        gen.ExecutorEventKindCOMPLETED,
				RecordedOutcome:         &outcome,
				RecordedSource:          &source,
				ExecutorEventReceivedAt: ts,
			},
			fresh: func() any { return &eventlog.ExecutorConflictLoggedPayload{} },
		},
		{
			name: "ExecutorConflictLoggedPayload_nil_pointers",
			payload: eventlog.ExecutorConflictLoggedPayload{
				ExecutorReported:        gen.ExecutorEventKindFAILED,
				RecordedOutcome:         nil,
				RecordedSource:          nil,
				ExecutorEventReceivedAt: ts,
			},
			fresh: func() any { return &eventlog.ExecutorConflictLoggedPayload{} },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.payload)
			require.NoError(t, err, "marshal")

			dst := tc.fresh()
			require.NoError(t, json.Unmarshal(b, dst), "unmarshal")

			b2, err := json.Marshal(dst)
			require.NoError(t, err, "re-marshal")
			require.JSONEq(t, string(b), string(b2))
		})
	}
}

func TestAmbiguousReason_constants(t *testing.T) {
	cases := []struct {
		name string
		got  eventlog.AmbiguousReason
		want string
	}{
		{"AmbiguousReasonDeadlineElapsed", eventlog.AmbiguousReasonDeadlineElapsed, "DEADLINE_ELAPSED"},
		{"AmbiguousReasonExecutorReportedUnknown", eventlog.AmbiguousReasonExecutorReportedUnknown, "EXECUTOR_REPORTED_UNKNOWN"},
		{"AmbiguousReasonDeadlineExceeded", eventlog.AmbiguousReasonDeadlineExceeded, "DEADLINE_EXCEEDED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, string(tc.got))
		})
	}
}

type eventRow struct {
	Seq       int64
	Kind      string
	RunID     sql.NullString
	JobID     sql.NullString
	DeckID    sql.NullString
	AttemptID sql.NullString
	Payload   string
}

func readEventBySeq(t *testing.T, db *sql.DB, seq int64) eventRow {
	t.Helper()
	var row eventRow
	err := db.QueryRowContext(
		context.Background(),
		`SELECT seq, kind, run_id, job_id, deck_id, attempt_id, payload FROM events WHERE seq = ?`,
		seq,
	).Scan(&row.Seq, &row.Kind, &row.RunID, &row.JobID, &row.DeckID, &row.AttemptID, &row.Payload)
	require.NoError(t, err, "read event row seq=%d", seq)
	return row
}

func appendInTx(
	t *testing.T,
	db interface {
		WithTx(context.Context, func(*storegen.Queries) error) error
	},
	kind eventlog.Kind,
	scope eventlog.Scope,
	payload any,
) int64 {
	t.Helper()
	var seq int64
	err := db.WithTx(context.Background(), func(q *storegen.Queries) error {
		var innerErr error
		seq, innerErr = eventlog.Append(context.Background(), q, kind, scope, testutil.Epoch, payload)
		return innerErr
	})
	require.NoError(t, err)
	return seq
}

func TestAppend_assignsMonotonicSeq(t *testing.T) {
	db := testutil.OpenDB(t)

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
	})

	scope := eventlog.Scope{RunID: testutil.DefaultRunID}

	seq1 := appendInTx(t, db, eventlog.KindRunSubmitted, scope, nil)
	seq2 := appendInTx(t, db, eventlog.KindRunStatusChanged, scope,
		eventlog.RunStatusChangedPayload{From: gen.PENDING, To: gen.RUNNING})

	require.Greater(t, seq2, seq1, "second append should have a higher seq than the first")
	require.Equal(t, seq1+1, seq2, "seq numbers should be strictly consecutive")
}

func TestAppend_nilPayload_storesEmptyObject(t *testing.T) {
	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
	})

	seq := appendInTx(t, db, eventlog.KindJobReady, eventlog.Scope{RunID: testutil.DefaultRunID}, nil)

	row := readEventBySeq(t, db.Write, seq)
	require.Equal(t, "{}", row.Payload, "nil payload should be stored as {}")
}

func TestAppend_typedPayload_storesJSON(t *testing.T) {
	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
	})

	want := eventlog.RunStatusChangedPayload{From: gen.PENDING, To: gen.RUNNING}
	seq := appendInTx(t, db, eventlog.KindRunStatusChanged, eventlog.Scope{RunID: testutil.DefaultRunID}, want)

	row := readEventBySeq(t, db.Write, seq)

	var got eventlog.RunStatusChangedPayload
	require.NoError(t, json.Unmarshal([]byte(row.Payload), &got))
	require.Equal(t, want.From, got.From)
	require.Equal(t, want.To, got.To)
}

func TestAppend_scopeNullables(t *testing.T) {
	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
	})

	seq := appendInTx(t, db, eventlog.KindRunSubmitted,
		eventlog.Scope{RunID: testutil.DefaultRunID},
		nil)

	row := readEventBySeq(t, db.Write, seq)
	require.True(t, row.RunID.Valid, "run_id should be set")
	require.Equal(t, testutil.DefaultRunID, row.RunID.String)
	require.False(t, row.JobID.Valid, "job_id should be NULL")
	require.False(t, row.DeckID.Valid, "deck_id should be NULL")
	require.False(t, row.AttemptID.Valid, "attempt_id should be NULL")
}

func TestAppend_allScopeFields(t *testing.T) {
	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
	})

	scope := eventlog.Scope{
		RunID:     testutil.DefaultRunID,
		JobID:     testutil.DefaultJobID,
		DeckID:    testutil.DefaultDeckID,
		AttemptID: "attempt-abc",
	}
	seq := appendInTx(t, db, eventlog.KindJobRunning, scope, nil)

	row := readEventBySeq(t, db.Write, seq)
	require.True(t, row.RunID.Valid && row.RunID.String == testutil.DefaultRunID)
	require.True(t, row.JobID.Valid && row.JobID.String == testutil.DefaultJobID)
	require.True(t, row.DeckID.Valid && row.DeckID.String == testutil.DefaultDeckID)
	require.True(t, row.AttemptID.Valid && row.AttemptID.String == "attempt-abc")
}

func TestAppend_duplicateAttemptKind_violatesUniqueIndex(t *testing.T) {
	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedRun(t, q)
	})

	// events.attempt_id has no FK — a synthetic UUID suffices for the unique-index test.
	attemptID := uuid.New().String()
	scope := eventlog.Scope{
		RunID:     testutil.DefaultRunID,
		AttemptID: attemptID,
	}

	err1 := db.WithTx(context.Background(), func(q *storegen.Queries) error {
		_, err := eventlog.Append(context.Background(), q,
			eventlog.KindJobRunning, scope, testutil.Epoch, nil)
		return err
	})
	require.NoError(t, err1, "first append should succeed")

	err2 := db.WithTx(context.Background(), func(q *storegen.Queries) error {
		_, err := eventlog.Append(context.Background(), q,
			eventlog.KindJobRunning, scope, testutil.Epoch.Add(time.Second), nil)
		return err
	})
	require.Error(t, err2, "second append with same (attempt_id, kind) should violate unique index")
	require.ErrorIs(t, err2, eventlog.ErrDuplicate,
		"unique-index violation should map to eventlog.ErrDuplicate (driver-typed check)")
}
