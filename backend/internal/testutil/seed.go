package testutil

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"deck-fleet/backend/internal/api/gen"
	storegen "deck-fleet/backend/internal/store/gen"
)

const (
	DefaultRunID  = "run-test"
	DefaultJobID  = "job-1"
	DefaultDeckID = "deck-test"
)

type RunOpt func(*runSeed)

type runSeed struct {
	ID          string
	Status      gen.RunStatus
	DAG         gen.DagSubmission
	SubmittedAt time.Time
	TerminalAt  *time.Time
}

func WithRunID(id string) RunOpt { return func(s *runSeed) { s.ID = id } }

func WithRunStatus(st gen.RunStatus) RunOpt { return func(s *runSeed) { s.Status = st } }

func WithRunDAG(d gen.DagSubmission) RunOpt { return func(s *runSeed) { s.DAG = d } }

func WithRunSubmittedAt(t time.Time) RunOpt { return func(s *runSeed) { s.SubmittedAt = t } }

// SeedRun inserts a run row (default: PENDING, DefaultRunID, 1-job DAG).
func SeedRun(t *testing.T, q *storegen.Queries, opts ...RunOpt) storegen.Runs {
	t.Helper()
	s := runSeed{
		ID:          DefaultRunID,
		Status:      gen.PENDING,
		DAG:         DAG(),
		SubmittedAt: Epoch,
	}
	for _, opt := range opts {
		opt(&s)
	}
	if isTerminalRunStatus(s.Status) && s.TerminalAt == nil {
		t := s.SubmittedAt
		s.TerminalAt = &t
	}
	dagBytes, err := json.Marshal(s.DAG)
	if err != nil {
		t.Fatalf("testutil: marshal dag: %v", err)
	}
	if err := q.InsertRun(context.Background(), storegen.InsertRunParams{
		ID:          s.ID,
		Status:      string(s.Status),
		Dag:         string(dagBytes),
		SubmittedAt: s.SubmittedAt.UnixMilli(),
	}); err != nil {
		t.Fatalf("testutil: insert run: %v", err)
	}
	if s.TerminalAt != nil {
		if _, err := q.UpdateRunStatusUnchecked(context.Background(), storegen.UpdateRunStatusUncheckedParams{
			Status:     string(s.Status),
			TerminalAt: sql.NullInt64{Int64: s.TerminalAt.UnixMilli(), Valid: true},
			ID:         s.ID,
		}); err != nil {
			t.Fatalf("testutil: stamp terminal_at: %v", err)
		}
	}
	row, err := q.GetRun(context.Background(), s.ID)
	if err != nil {
		t.Fatalf("testutil: reload run: %v", err)
	}
	return row
}

type JobOpt func(*jobSeed)

type jobSeed struct {
	ID        string
	DeckID    string
	Status    gen.DeckJobStatus
	DependsOn []string
	Steps     []gen.Step
}

func WithJobID(id string) JobOpt { return func(s *jobSeed) { s.ID = id } }

func WithJobDeck(deckID string) JobOpt { return func(s *jobSeed) { s.DeckID = deckID } }

// WithJobStatus: at most one DISPATCHED/RUNNING/AMBIGUOUS row per deck_id.
func WithJobStatus(st gen.DeckJobStatus) JobOpt { return func(s *jobSeed) { s.Status = st } }

// WithJobDeps sets depends_on; SeedRun's stored DAG is not auto-updated.
func WithJobDeps(deps ...string) JobOpt {
	cp := append([]string(nil), deps...)
	return func(s *jobSeed) { s.DependsOn = cp }
}

// WithJobSteps replaces the default 1-step seed; total_steps is derived
// from the slice length so per-step deadline math works end-to-end.
func WithJobSteps(steps ...gen.Step) JobOpt {
	cp := append([]gen.Step(nil), steps...)
	return func(s *jobSeed) { s.Steps = cp }
}

// SeedDeckJob inserts a deck_job row. Attach current_attempt_id via SeedAttempt
// then UpdateDeckJobStatusVersioned (FK: job_attempts → deck_jobs).
func SeedDeckJob(t *testing.T, q *storegen.Queries, runID string, opts ...JobOpt) storegen.DeckJobs {
	t.Helper()
	s := jobSeed{
		ID:        DefaultJobID,
		DeckID:    DefaultDeckID,
		Status:    gen.DeckJobStatusPENDING,
		DependsOn: []string{},
		Steps:     []gen.Step{{Type: "noop", Description: "test"}},
	}
	for _, opt := range opts {
		opt(&s)
	}
	depsBytes, err := json.Marshal(s.DependsOn)
	if err != nil {
		t.Fatalf("testutil: marshal deps: %v", err)
	}
	stepsBytes, err := json.Marshal(s.Steps)
	if err != nil {
		t.Fatalf("testutil: marshal steps: %v", err)
	}
	if err := q.InsertDeckJob(context.Background(), storegen.InsertDeckJobParams{
		RunID:      runID,
		ID:         s.ID,
		DeckID:     s.DeckID,
		DependsOn:  string(depsBytes),
		Steps:      string(stepsBytes),
		Status:     string(s.Status),
		TotalSteps: int64(len(s.Steps)),
	}); err != nil {
		t.Fatalf("testutil: insert deck_job: %v", err)
	}
	row, err := q.GetDeckJob(context.Background(), storegen.GetDeckJobParams{RunID: runID, ID: s.ID})
	if err != nil {
		t.Fatalf("testutil: reload deck_job: %v", err)
	}
	return row
}

type AttemptOpt func(*attemptSeed)

type attemptSeed struct {
	AttemptID    string
	DispatchedAt time.Time
}

func WithAttemptID(id string) AttemptOpt { return func(s *attemptSeed) { s.AttemptID = id } }

func WithAttemptDispatchedAt(at time.Time) AttemptOpt {
	return func(s *attemptSeed) { s.DispatchedAt = at }
}

func SeedAttempt(t *testing.T, q *storegen.Queries, runID, jobID, deckID string, opts ...AttemptOpt) string {
	t.Helper()
	s := attemptSeed{
		DispatchedAt: Epoch,
	}
	for _, opt := range opts {
		opt(&s)
	}
	if s.AttemptID == "" {
		u, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("testutil: uuid: %v", err)
		}
		s.AttemptID = u.String()
	}
	if err := q.InsertJobAttempt(context.Background(), storegen.InsertJobAttemptParams{
		AttemptID:    s.AttemptID,
		RunID:        runID,
		JobID:        jobID,
		DeckID:       deckID,
		DispatchedAt: s.DispatchedAt.UnixMilli(),
	}); err != nil {
		t.Fatalf("testutil: insert attempt: %v", err)
	}
	return s.AttemptID
}

type DeckOpt func(*deckSeed)

type deckSeed struct {
	ID          string
	EndpointURL string
	FirstSeenAt time.Time
	Heartbeat   time.Time
	Health      gen.DeckHealth
}

func WithDeckID(id string) DeckOpt { return func(s *deckSeed) { s.ID = id } }

func WithDeckEndpoint(url string) DeckOpt { return func(s *deckSeed) { s.EndpointURL = url } }

func WithDeckHeartbeat(at time.Time) DeckOpt { return func(s *deckSeed) { s.Heartbeat = at } }

func WithDeckHealth(h gen.DeckHealth) DeckOpt { return func(s *deckSeed) { s.Health = h } }

// SeedDeck upserts a deck slot (EMPTY first, then heartbeat). Non-HEALTHY
// health requires a follow-up UpdateDeckHealth.
func SeedDeck(t *testing.T, q *storegen.Queries, opts ...DeckOpt) storegen.Decks {
	t.Helper()
	s := deckSeed{
		ID:          DefaultDeckID,
		EndpointURL: "http://127.0.0.1:0",
		FirstSeenAt: Epoch,
		Heartbeat:   Epoch,
		Health:      "HEALTHY",
	}
	for _, opt := range opts {
		opt(&s)
	}
	if _, err := q.UpsertEmptyDeckIfAbsent(context.Background(), storegen.UpsertEmptyDeckIfAbsentParams{
		ID:          s.ID,
		FirstSeenAt: s.FirstSeenAt.UnixMilli(),
	}); err != nil {
		t.Fatalf("testutil: seed empty deck: %v", err)
	}
	if _, err := q.UpsertDeckHeartbeat(context.Background(), storegen.UpsertDeckHeartbeatParams{
		ID:                   s.ID,
		EndpointUrl:          sql.NullString{String: s.EndpointURL, Valid: true},
		FirstSeenAt:          s.FirstSeenAt.UnixMilli(),
		LastHeartbeatAt:      sql.NullInt64{Int64: s.Heartbeat.UnixMilli(), Valid: true},
		LastClaimedAttemptID: sql.NullString{},
	}); err != nil {
		t.Fatalf("testutil: upsert deck: %v", err)
	}
	if s.Health != "HEALTHY" {
		if _, err := q.UpdateDeckHealth(context.Background(), storegen.UpdateDeckHealthParams{
			LastKnownHealth: string(s.Health),
			ID:              s.ID,
			ExpectedHealth:  "HEALTHY",
		}); err != nil {
			t.Fatalf("testutil: update health: %v", err)
		}
	}
	row, err := q.GetDeck(context.Background(), s.ID)
	if err != nil {
		t.Fatalf("testutil: reload deck: %v", err)
	}
	return row
}

func SeedEmptySlot(t *testing.T, q *storegen.Queries, deckID string) storegen.Decks {
	t.Helper()
	if _, err := q.UpsertEmptyDeckIfAbsent(context.Background(), storegen.UpsertEmptyDeckIfAbsentParams{
		ID:          deckID,
		FirstSeenAt: Epoch.UnixMilli(),
	}); err != nil {
		t.Fatalf("testutil: seed empty slot %s: %v", deckID, err)
	}
	row, err := q.GetDeck(context.Background(), deckID)
	if err != nil {
		t.Fatalf("testutil: reload empty slot %s: %v", deckID, err)
	}
	return row
}

func isTerminalRunStatus(s gen.RunStatus) bool {
	switch s {
	case gen.COMPLETED, gen.FAILED, gen.CANCELLED:
		return true
	case gen.AMBIGUOUS, gen.PENDING, gen.RUNNING:
		return false
	}
	return false
}
