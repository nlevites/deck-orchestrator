package handlers_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/handlers"
	"deck-fleet/backend/internal/store"
	storegen "deck-fleet/backend/internal/store/gen"
	"deck-fleet/backend/internal/testutil"
)

func newOperator(db *store.DB) *handlers.Operator {
	return handlers.NewOperator(handlers.Deps{
		Store:          db,
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		AbortScheduler: &testutil.AbortScheduler{},
	})
}

func submitRun(t *testing.T, op *handlers.Operator, dag gen.DagSubmission) *httptest.ResponseRecorder {
	t.Helper()
	return testutil.Do(t, http.HandlerFunc(op.SubmitRun), http.MethodPost, "/api/runs", dag)
}

func listRuns(t *testing.T, op *handlers.Operator) *httptest.ResponseRecorder {
	t.Helper()
	return testutil.Do(t, http.HandlerFunc(op.ListRuns), http.MethodGet, "/api/runs", nil)
}

func getRun(t *testing.T, op *handlers.Operator, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+id, nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	op.GetRun(rec, req)
	return rec
}

func cancelRun(t *testing.T, op *handlers.Operator, id string, body gen.CancelRunRequest) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+id+"/cancel",
		marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	op.CancelRun(rec, req)
	return rec
}

func retryJob(t *testing.T, op *handlers.Operator, runID, jobID string, body gen.RetryJobRequest) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/runs/%s/jobs/%s/retry", runID, jobID),
		marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	req.SetPathValue("job_id", jobID)
	rec := httptest.NewRecorder()
	op.RetryJob(rec, req)
	return rec
}

func resolveJob(t *testing.T, op *handlers.Operator, runID, jobID string, body gen.ResolveJobRequest) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/runs/%s/jobs/%s/resolve", runID, jobID),
		marshalBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", runID)
	req.SetPathValue("job_id", jobID)
	rec := httptest.NewRecorder()
	op.ResolveJob(rec, req)
	return rec
}

func marshalBody(t *testing.T, v any) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes.NewReader(b)
}

func decodeRun(t *testing.T, rec *httptest.ResponseRecorder) gen.Run {
	t.Helper()
	var run gen.Run
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &run))
	return run
}

func TestSubmitRun_happyPath_createsRunWithDeckJobs(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q)
	})

	rec := submitRun(t, op, testutil.DAG())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	run := decodeRun(t, rec)
	require.Equal(t, testutil.DefaultRunID, run.Id)
	require.Len(t, run.DeckJobs, 1)
}

func TestSubmitRun_dagValidationFailed_returns422(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)

	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q)
	})

	rec := submitRun(t, op, testutil.DAG(testutil.WithEmptyJobs()))
	testutil.AssertDagValidationCodes(t, rec, gen.DagValidationCodeDAGHASNOJOBS)
}

func TestSubmitRun_unknownDeck_returns422(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)
	rec := submitRun(t, op, testutil.DAG())
	testutil.AssertDagValidationCodes(t, rec, gen.DagValidationCodeUNKNOWNDECK)
}

func TestSubmitRun_duplicateRunID_returns409(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)
	testutil.Tx(t, db, func(q *storegen.Queries) { testutil.SeedDeck(t, q) })

	dag := testutil.DAG(testutil.WithDAGID("run-dup"))
	rec1 := submitRun(t, op, dag)
	require.Equal(t, http.StatusCreated, rec1.Code)

	rec2 := submitRun(t, op, dag)
	testutil.AssertErrorCode(t, rec2, gen.ErrorCodeDUPLICATERESOURCE)
}

func TestListRuns_emptyDB_returnsEmptySlice(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)
	rec := listRuns(t, op)
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	runs, _ := body["runs"].([]any)
	require.Empty(t, runs)
}

func TestListRuns_returnsSeedeRuns(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q)
		testutil.SeedRun(t, q)
	})
	rec := listRuns(t, op)
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	runs, _ := body["runs"].([]any)
	require.Len(t, runs, 1)
}

func TestGetRun_notFound_returns404(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)
	rec := getRun(t, op, "does-not-exist")
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeRUNNOTFOUND)
}

func TestGetRun_found_returnsRun(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q)
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID)
	})
	rec := getRun(t, op, testutil.DefaultRunID)
	require.Equal(t, http.StatusOK, rec.Code)
	run := decodeRun(t, rec)
	require.Equal(t, testutil.DefaultRunID, run.Id)
}

func TestCancelRun_notFound_returns404(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)
	rec := cancelRun(t, op, "no-run", gen.CancelRunRequest{ExpectedVersion: 1})
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeRUNNOTFOUND)
}

func TestCancelRun_versionMismatch_returns409(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q)
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID)
	})
	rec := cancelRun(t, op, testutil.DefaultRunID, gen.CancelRunRequest{ExpectedVersion: 999})
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeVERSIONMISMATCH)
}

func TestCancelRun_alreadyTerminal_returns409AlreadyTerminal(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q)
		testutil.SeedRun(t, q, testutil.WithRunStatus(gen.CANCELLED))
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID, testutil.WithJobStatus(gen.DeckJobStatusCANCELLED))
	})
	// version = 2 after the UpdateRunStatusUnchecked bump in SeedRun for terminal
	run, err := db.ReadQueries.GetRun(context.Background(), testutil.DefaultRunID)
	require.NoError(t, err)
	rec := cancelRun(t, op, testutil.DefaultRunID, gen.CancelRunRequest{ExpectedVersion: run.Version})
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeALREADYTERMINAL)
}

func TestCancelRun_happy_cancelsJobsAndReturns200(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q)
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID)
	})
	run, err := db.ReadQueries.GetRun(context.Background(), testutil.DefaultRunID)
	require.NoError(t, err)

	rec := cancelRun(t, op, testutil.DefaultRunID, gen.CancelRunRequest{ExpectedVersion: run.Version})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	job, err := db.ReadQueries.GetDeckJob(context.Background(), storegen.GetDeckJobParams{
		RunID: testutil.DefaultRunID, ID: testutil.DefaultJobID,
	})
	require.NoError(t, err)
	require.Equal(t, string(gen.DeckJobStatusCANCELLED), job.Status)
}

func TestRetryJob_notFound_returns404(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)
	rec := retryJob(t, op, "no-run", "no-job", gen.RetryJobRequest{ExpectedVersion: 1})
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeRUNNOTFOUND)
}

func TestRetryJob_jobNotFailed_returns409InvalidTransition(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q)
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusPENDING))
	})
	job, _ := db.ReadQueries.GetDeckJob(context.Background(), storegen.GetDeckJobParams{
		RunID: testutil.DefaultRunID, ID: testutil.DefaultJobID,
	})
	rec := retryJob(t, op, testutil.DefaultRunID, testutil.DefaultJobID,
		gen.RetryJobRequest{ExpectedVersion: job.Version})
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeINVALIDTRANSITION)
}

func TestRetryJob_happyPath_resetsJobToReady(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q)
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusFAILED))
	})
	job, _ := db.ReadQueries.GetDeckJob(context.Background(), storegen.GetDeckJobParams{
		RunID: testutil.DefaultRunID, ID: testutil.DefaultJobID,
	})
	rec := retryJob(t, op, testutil.DefaultRunID, testutil.DefaultJobID,
		gen.RetryJobRequest{ExpectedVersion: job.Version})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	updated, _ := db.ReadQueries.GetDeckJob(context.Background(), storegen.GetDeckJobParams{
		RunID: testutil.DefaultRunID, ID: testutil.DefaultJobID,
	})
	// READY or DISPATCHED (if deck was free and healthy, the dispatcher fires immediately).
	require.True(t,
		updated.Status == string(gen.DeckJobStatusREADY) ||
			updated.Status == string(gen.DeckJobStatusDISPATCHED),
		"job must be READY or DISPATCHED after retry, got %s", updated.Status)
}

func TestResolveJob_notAmbiguous_returns409InvalidTransition(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q)
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusFAILED))
	})
	job, _ := db.ReadQueries.GetDeckJob(context.Background(), storegen.GetDeckJobParams{
		RunID: testutil.DefaultRunID, ID: testutil.DefaultJobID,
	})
	res := gen.AttemptOutcomeCOMPLETED
	rec := resolveJob(t, op, testutil.DefaultRunID, testutil.DefaultJobID,
		gen.ResolveJobRequest{Resolution: res, ExpectedVersion: job.Version})
	testutil.AssertErrorCode(t, rec, gen.ErrorCodeINVALIDTRANSITION)
}

func TestResolveJob_happyPath_resolvesToCompleted(t *testing.T) {
	db := testutil.OpenDB(t)
	op := newOperator(db)
	var attemptID string
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q)
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusAMBIGUOUS))
		attemptID = testutil.SeedAttempt(t, q,
			testutil.DefaultRunID, testutil.DefaultJobID, testutil.DefaultDeckID)
		_, err := q.UpdateDeckJobStatusVersioned(context.Background(),
			storegen.UpdateDeckJobStatusVersionedParams{
				Status:           string(gen.DeckJobStatusAMBIGUOUS),
				CurrentAttemptID: sql.NullString{String: attemptID, Valid: true},
				RunID:            testutil.DefaultRunID,
				ID:               testutil.DefaultJobID,
				Version:          1,
			})
		require.NoError(t, err)
	})
	job, _ := db.ReadQueries.GetDeckJob(context.Background(), storegen.GetDeckJobParams{
		RunID: testutil.DefaultRunID, ID: testutil.DefaultJobID,
	})
	res := gen.AttemptOutcomeCOMPLETED
	rec := resolveJob(t, op, testutil.DefaultRunID, testutil.DefaultJobID,
		gen.ResolveJobRequest{Resolution: res, ExpectedVersion: job.Version})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	updated, _ := db.ReadQueries.GetDeckJob(context.Background(), storegen.GetDeckJobParams{
		RunID: testutil.DefaultRunID, ID: testutil.DefaultJobID,
	})
	require.Equal(t, string(gen.DeckJobStatusCOMPLETED), updated.Status)
}
