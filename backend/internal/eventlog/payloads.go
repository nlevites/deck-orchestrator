package eventlog

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"deck-fleet/backend/internal/api/gen"
)

// Payload structs define on-disk JSON for events.payload. Backend-only; no OpenAPI.
//
// Job-status events include `from` so the frontend runs-list reducer can decrement
// the correct by_status bucket without a warm run-detail cache.

type RunSubmittedPayload struct {
	SubmittedAt time.Time `json:"submitted_at"`
}

type RunStatusChangedPayload struct {
	From gen.RunStatus `json:"from"`
	To   gen.RunStatus `json:"to"`
}

type JobReadyPayload struct {
	From gen.DeckJobStatus `json:"from"`
}

type JobDispatchedPayload struct {
	From gen.DeckJobStatus `json:"from"`
}

type JobRunningPayload struct {
	From gen.DeckJobStatus `json:"from"`
}

type JobCompletedPayload struct {
	From          gen.DeckJobStatus `json:"from"`
	OutcomeSource gen.OutcomeSource `json:"outcome_source"`
	// Result is the executor-reported result blob, kept as raw JSON
	// because the orchestrator never interprets its contents.
	Result json.RawMessage `json:"result,omitempty"`
}

type JobFailedPayload struct {
	From          gen.DeckJobStatus `json:"from"`
	OutcomeSource gen.OutcomeSource `json:"outcome_source"`
	Error         string            `json:"error"`
}

type JobCancelledPayload struct {
	From gen.DeckJobStatus `json:"from"`
}

type JobResolvedPayload struct {
	From         gen.DeckJobStatus  `json:"from"`
	Resolution   gen.AttemptOutcome `json:"resolution"`
	OperatorNote *string            `json:"operator_note,omitempty"`
}

type JobRetriedPayload struct {
	From              gen.DeckJobStatus `json:"from"`
	PreviousAttemptID uuid.UUID         `json:"previous_attempt_id"`
}

type DeckRegisteredPayload struct {
	EndpointURL string    `json:"endpoint_url"`
	FirstSeenAt time.Time `json:"first_seen_at"`
}

type DeckHealthChangedPayload struct {
	From            gen.DeckHealth `json:"from"`
	To              gen.DeckHealth `json:"to"`
	LastHeartbeatAt time.Time      `json:"last_heartbeat_at"`
}

type AmbiguousReason string

const (
	AmbiguousReasonDeadlineElapsed         AmbiguousReason = "DEADLINE_ELAPSED"
	AmbiguousReasonExecutorReportedUnknown AmbiguousReason = "EXECUTOR_REPORTED_UNKNOWN"
	AmbiguousReasonDeadlineExceeded        AmbiguousReason = "DEADLINE_EXCEEDED"
)

type JobAmbiguousPayload struct {
	From   gen.DeckJobStatus `json:"from"`
	Reason AmbiguousReason   `json:"reason"`
}

type ExecutorConflictLoggedPayload struct {
	ExecutorReported        gen.ExecutorEventKind `json:"executor_reported"`
	RecordedOutcome         *gen.AttemptOutcome   `json:"recorded_outcome"`
	RecordedSource          *gen.OutcomeSource    `json:"recorded_source"`
	ExecutorEventReceivedAt time.Time             `json:"executor_event_received_at"`
}

// Step is 1-indexed; Total is deck_job.total_steps.
type JobStepCompletedPayload struct {
	Step      int    `json:"step"`
	Total     int    `json:"total"`
	AttemptID string `json:"attempt_id"`
}
