package testutil_test

import (
	"context"
	"database/sql"
	"net/http"
	"testing"
	"time"

	"deck-fleet/backend/internal/api/gen"
	storegen "deck-fleet/backend/internal/store/gen"
	"deck-fleet/backend/internal/testutil"
)

// Self-tests for testutil; regressions here break downstream packages.
func TestFakeClock_AdvanceAndSet(t *testing.T) {
	c := testutil.NewClock(testutil.Epoch)
	if got := c.Now(); !got.Equal(testutil.Epoch) {
		t.Fatalf("Now=%v want %v", got, testutil.Epoch)
	}
	c.Advance(2 * time.Second)
	if got := c.Now(); !got.Equal(testutil.Epoch.Add(2 * time.Second)) {
		t.Fatalf("after advance Now=%v", got)
	}
	jump := testutil.Epoch.Add(time.Hour)
	c.Set(jump)
	if got := c.Now(); !got.Equal(jump) {
		t.Fatalf("after set Now=%v", got)
	}
}

func TestOpenDB_OpensAndMigrates(t *testing.T) {
	db := testutil.OpenDB(t)
	rows, err := db.ReadQueries.ListRuns(context.Background(), 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("fresh db has %d runs", len(rows))
	}
}

func TestSeedRun_DefaultsAreValidPendingRun(t *testing.T) {
	db := testutil.OpenDB(t)
	var got storegen.Runs
	testutil.Tx(t, db, func(q *storegen.Queries) {
		got = testutil.SeedRun(t, q)
	})
	if got.ID != testutil.DefaultRunID {
		t.Fatalf("id=%q", got.ID)
	}
	if got.Status != string(gen.PENDING) {
		t.Fatalf("status=%q", got.Status)
	}
	if got.TerminalAt.Valid {
		t.Fatalf("terminal_at should be NULL on PENDING run")
	}
}

func TestSeedRun_TerminalStatusStampsTerminalAt(t *testing.T) {
	db := testutil.OpenDB(t)
	var got storegen.Runs
	testutil.Tx(t, db, func(q *storegen.Queries) {
		got = testutil.SeedRun(t, q, testutil.WithRunStatus(gen.COMPLETED))
	})
	if !got.TerminalAt.Valid {
		t.Fatalf("terminal_at should be set on COMPLETED run")
	}
}

func TestSeedDeckJobAttempt_LinkRow(t *testing.T) {
	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		testutil.SeedDeck(t, q)
		testutil.SeedRun(t, q)
		testutil.SeedDeckJob(t, q, testutil.DefaultRunID,
			testutil.WithJobStatus(gen.DeckJobStatusPENDING),
		)
		attemptID := testutil.SeedAttempt(t, q,
			testutil.DefaultRunID, testutil.DefaultJobID, testutil.DefaultDeckID)
		if _, err := q.UpdateDeckJobStatusVersioned(context.Background(),
			storegen.UpdateDeckJobStatusVersionedParams{
				Status:           string(gen.DeckJobStatusDISPATCHED),
				CurrentAttemptID: sqlNullStr(attemptID),
				RunID:            testutil.DefaultRunID,
				ID:               testutil.DefaultJobID,
				Version:          1,
			}); err != nil {
			t.Fatalf("attach: %v", err)
		}
		// Schema allows at most one active job per deck slot.
		got, err := q.GetDeckSlotOccupier(context.Background(), testutil.DefaultDeckID)
		if err != nil {
			t.Fatalf("slot occupier: %v", err)
		}
		if got.Status != string(gen.DeckJobStatusDISPATCHED) {
			t.Fatalf("status=%q", got.Status)
		}
		if !got.CurrentAttemptID.Valid || got.CurrentAttemptID.String != attemptID {
			t.Fatalf("attempt_id=%v", got.CurrentAttemptID)
		}
	})
}

func sqlNullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

func TestSeedDeck_StaleHealthOverride(t *testing.T) {
	db := testutil.OpenDB(t)
	testutil.Tx(t, db, func(q *storegen.Queries) {
		row := testutil.SeedDeck(t, q, testutil.WithDeckHealth("STALE"))
		if row.LastKnownHealth != "STALE" {
			t.Fatalf("health=%q", row.LastKnownHealth)
		}
	})
}

func TestDAG_BuildersComposeIntoExpectedShapes(t *testing.T) {
	d := testutil.DAG()
	if len(d.DeckJobs) != 1 || d.DeckJobs[0].Id != testutil.DefaultJobID {
		t.Fatalf("default DAG shape wrong: %+v", d)
	}
	d2 := testutil.DAG(
		testutil.WithDAGID("run-2"),
		testutil.WithJob("job-2", "deck-2", testutil.DefaultJobID),
		testutil.WithNoSteps("job-2"),
	)
	if d2.Id != "run-2" {
		t.Fatalf("id=%q", d2.Id)
	}
	if len(d2.DeckJobs) != 2 {
		t.Fatalf("len jobs=%d", len(d2.DeckJobs))
	}
	if len(d2.DeckJobs[1].Steps) != 0 {
		t.Fatalf("WithNoSteps did not strip steps")
	}
	if got := d2.DeckJobs[1].DependsOn; len(got) != 1 || got[0] != testutil.DefaultJobID {
		t.Fatalf("deps=%v", got)
	}
}

func TestDo_AndAssertErrorCode(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"INVALID_JSON","message":"x","request_id":""}}`))
	})
	rec := testutil.Do(t, h, http.MethodPost, "/x", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
	got := testutil.AssertErrorCode(t, rec, gen.ErrorCodeINVALIDJSON)
	if got["message"] != "x" {
		t.Fatalf("message=%v", got["message"])
	}
}

func TestAbortScheduler_RecordsCalls(t *testing.T) {
	var s testutil.AbortScheduler
	s.Schedule(context.Background(), "d1", "a1")
	s.Schedule(context.Background(), "d1", "a2")
	calls := s.Snapshot()
	if len(calls) != 2 || calls[1].AttemptID != "a2" {
		t.Fatalf("calls=%+v", calls)
	}
}

func TestRecordingTransport_DefaultAndCustom(t *testing.T) {
	rt := &testutil.RecordingTransport{}
	c := rt.Client()
	resp, err := c.Get("http://example.invalid/x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	_ = resp.Body.Close()
	if got := rt.Requests(); len(got) != 1 || got[0].URL.Path != "/x" {
		t.Fatalf("requests=%v", got)
	}
}

func TestExecutorServer_RoutesByPath(t *testing.T) {
	es := testutil.NewExecutorServer(t)
	es.AbortFn = func(attemptID string) (int, []byte) {
		if attemptID != "a-1" {
			t.Errorf("attemptID=%q", attemptID)
		}
		return http.StatusOK, []byte(`{"status":"abort_requested"}`)
	}
	resp, err := http.Post(es.URL+"/executor/abort/a-1", "application/json", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if hits := es.Hits(); len(hits) != 1 || hits[0].Method != http.MethodPost {
		t.Fatalf("hits=%+v", hits)
	}
}
