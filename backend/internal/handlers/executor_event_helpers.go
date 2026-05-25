package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/eventlog"
	storegen "deck-fleet/backend/internal/store/gen"
)

// executorEventPayload decodes ExecutorEventRequest.payload once from the
// OpenAPI map type. Per-kind: COMPLETED {result}, FAILED {error},
// RUNNING {}, STEP_COMPLETED {step, total}. Result is json.RawMessage
// because the orchestrator round-trips it verbatim.
type executorEventPayload struct {
	Step   int             `json:"step,omitempty"`
	Total  int             `json:"total,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

func decodeExecutorPayload(p *map[string]any) (executorEventPayload, error) {
	var out executorEventPayload
	if p == nil || len(*p) == 0 {
		return out, nil
	}
	b, err := json.Marshal(*p)
	if err != nil {
		return out, fmt.Errorf("re-marshal payload: %w", err)
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("decode payload: %w", err)
	}
	return out, nil
}

func mapExecutorEventToJobStatus(k gen.ExecutorEventKind) (gen.DeckJobStatus, eventlog.Kind) {
	switch k {
	case gen.ExecutorEventKindRUNNING:
		return gen.DeckJobStatusRUNNING, eventlog.KindJobRunning
	case gen.ExecutorEventKindCOMPLETED:
		return gen.DeckJobStatusCOMPLETED, eventlog.KindJobCompleted
	case gen.ExecutorEventKindFAILED:
		return gen.DeckJobStatusFAILED, eventlog.KindJobFailed
	default:
		return "", ""
	}
}

// allowedFromStatusesTyped is the §3.2 allowed-from set for executor transitions.
func allowedFromStatusesTyped(k gen.ExecutorEventKind) []gen.DeckJobStatus {
	switch k {
	case gen.ExecutorEventKindRUNNING:
		return []gen.DeckJobStatus{gen.DeckJobStatusDISPATCHED}
	case gen.ExecutorEventKindCOMPLETED, gen.ExecutorEventKindFAILED:
		return []gen.DeckJobStatus{
			gen.DeckJobStatusDISPATCHED,
			gen.DeckJobStatusRUNNING,
		}
	default:
		return nil
	}
}

func matchesOutcome(stored, incoming string) bool {
	return stored == incoming
}

func nullableAttemptOutcome(ns sql.NullString) *gen.AttemptOutcome {
	if !ns.Valid {
		return nil
	}
	o := gen.AttemptOutcome(ns.String)
	return &o
}

func nullableOutcomeSource(ns sql.NullString) *gen.OutcomeSource {
	if !ns.Valid {
		return nil
	}
	s := gen.OutcomeSource(ns.String)
	return &s
}

func writeConflict(ctx context.Context, q *storegen.Queries, body gen.ExecutorEventRequest, attempt storegen.JobAttempts, jobRow storegen.DeckJobs, now time.Time) error {
	payload := eventlog.ExecutorConflictLoggedPayload{
		ExecutorReported:        body.Kind,
		ExecutorEventReceivedAt: now,
		RecordedOutcome:         nullableAttemptOutcome(attempt.Outcome),
		RecordedSource:          nullableOutcomeSource(attempt.OutcomeSource),
	}
	scope := eventlog.Scope{
		RunID:     jobRow.RunID,
		JobID:     jobRow.ID,
		DeckID:    jobRow.DeckID,
		AttemptID: attempt.AttemptID,
	}
	_, err := eventlog.Append(ctx, q, eventlog.KindExecutorConflictLogged, scope, now, payload)
	return err
}
