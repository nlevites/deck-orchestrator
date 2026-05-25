package runs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"deck-fleet/backend/internal/api/gen"
	storegen "deck-fleet/backend/internal/store/gen"
)

// RowToRun assembles gen.Run from store rows. includeRecent adds per-job attempt history.
func RowToRun(ctx context.Context, q *storegen.Queries, runRow storegen.Runs, includeRecent bool) (gen.Run, error) {
	var dag gen.DagSubmission
	if err := json.Unmarshal([]byte(runRow.Dag), &dag); err != nil {
		return gen.Run{}, fmt.Errorf("unmarshal stored dag: %w", err)
	}

	jobRows, err := q.ListDeckJobsByRun(ctx, runRow.ID)
	if err != nil {
		return gen.Run{}, fmt.Errorf("list deck_jobs for %s: %w", runRow.ID, err)
	}

	jobs := make([]gen.DeckJob, 0, len(jobRows))
	for _, jr := range jobRows {
		j, jerr := RowToDeckJob(ctx, q, jr, includeRecent)
		if jerr != nil {
			return gen.Run{}, jerr
		}
		jobs = append(jobs, j)
	}

	return gen.Run{
		Id:          runRow.ID,
		Status:      gen.RunStatus(runRow.Status),
		SubmittedAt: time.UnixMilli(runRow.SubmittedAt),
		TerminalAt:  nullInt64ToTimePtr(runRow.TerminalAt),
		Version:     runRow.Version,
		Dag:         dag,
		DeckJobs:    jobs,
	}, nil
}

func RowToDeckJob(ctx context.Context, q *storegen.Queries, jr storegen.DeckJobs, includeRecent bool) (gen.DeckJob, error) {
	var depends []string
	if err := json.Unmarshal([]byte(jr.DependsOn), &depends); err != nil {
		return gen.DeckJob{}, fmt.Errorf("unmarshal depends_on for %s: %w", jr.ID, err)
	}
	var steps []gen.Step
	if err := json.Unmarshal([]byte(jr.Steps), &steps); err != nil {
		return gen.DeckJob{}, fmt.Errorf("unmarshal steps for %s: %w", jr.ID, err)
	}

	job := gen.DeckJob{
		Id:                jr.ID,
		DeckId:            jr.DeckID,
		DependsOn:         depends,
		Steps:             steps,
		Status:            gen.DeckJobStatus(jr.Status),
		Version:           jr.Version,
		Error:             nullStringToPtr(jr.Error),
		CurrentAttemptId:  nullStringToUUIDPtr(jr.CurrentAttemptID),
		LastCompletedStep: &jr.LastCompletedStep,
		TotalSteps:        &jr.TotalSteps,
		AmbiguousReason:   nullStringToAmbiguousReasonPtr(jr.AmbiguousReason),
	}
	if includeRecent {
		atRows, err := q.ListAttemptsForJob(ctx, storegen.ListAttemptsForJobParams{
			RunID: jr.RunID,
			JobID: jr.ID,
		})
		if err != nil {
			return gen.DeckJob{}, fmt.Errorf("list attempts for %s/%s: %w", jr.RunID, jr.ID, err)
		}
		attempts := make([]gen.JobAttempt, 0, len(atRows))
		for _, ar := range atRows {
			attempts = append(attempts, AttemptRowToAttempt(ar))
		}
		job.RecentAttempts = &attempts
	}
	return job, nil
}

func AttemptRowToAttempt(a storegen.JobAttempts) gen.JobAttempt {
	out := gen.JobAttempt{
		AttemptId:    mustUUID(a.AttemptID),
		DispatchedAt: time.UnixMilli(a.DispatchedAt),
		Error:        nullStringToPtr(a.Error),
		OperatorNote: nullStringToPtr(a.OperatorNote),
	}
	if a.Outcome.Valid {
		o := gen.AttemptOutcome(a.Outcome.String)
		out.Outcome = &o
	}
	if a.OutcomeAt.Valid {
		t := time.UnixMilli(a.OutcomeAt.Int64)
		out.OutcomeAt = &t
	}
	if a.OutcomeSource.Valid {
		s := gen.OutcomeSource(a.OutcomeSource.String)
		out.OutcomeSource = &s
	}
	if a.Result.Valid {
		var m map[string]any
		if err := json.Unmarshal([]byte(a.Result.String), &m); err == nil {
			out.Result = &m
		}
	}
	return out
}

func RowToRunSummary(ctx context.Context, q *storegen.Queries, r storegen.Runs) (gen.RunSummary, error) {
	jobs, err := q.ListDeckJobsByRun(ctx, r.ID)
	if err != nil {
		return gen.RunSummary{}, fmt.Errorf("list deck_jobs for %s: %w", r.ID, err)
	}
	byStatus := make(map[string]int, len(jobs))
	for _, j := range jobs {
		byStatus[j.Status]++
	}
	return gen.RunSummary{
		Id:          r.ID,
		Status:      gen.RunStatus(r.Status),
		SubmittedAt: time.UnixMilli(r.SubmittedAt),
		TerminalAt:  nullInt64ToTimePtr(r.TerminalAt),
		Version:     r.Version,
		DeckJobsSummary: gen.DeckJobsSummary{
			Total:    len(jobs),
			ByStatus: byStatus,
		},
	}, nil
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

func nullStringToUUIDPtr(ns sql.NullString) *openapi_types.UUID {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	u, err := uuid.Parse(ns.String)
	if err != nil {
		return nil
	}
	return &u
}

func mustUUID(s string) openapi_types.UUID {
	u, _ := uuid.Parse(s)
	return u
}

func nullStringToAmbiguousReasonPtr(ns sql.NullString) *gen.DeckJobAmbiguousReason {
	if !ns.Valid {
		return nil
	}
	r := gen.DeckJobAmbiguousReason(ns.String)
	return &r
}
