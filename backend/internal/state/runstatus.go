package state

import (
	"deck-fleet/backend/internal/api/gen"
	storegen "deck-fleet/backend/internal/store/gen"
)

// DeriveRunStatus picks a run status from its deck_jobs (first match wins).
// Terminalization rules: see IsTerminalRunStatus and DESIGN.md.
//
//  1. COMPLETED  — every deck_job is COMPLETED.
//  2. AMBIGUOUS  — any deck_job is AMBIGUOUS.
//  3. RUNNING    — any deck_job is DISPATCHED or RUNNING.
//  4. FAILED     — any FAILED, none in {READY, DISPATCHED, RUNNING, AMBIGUOUS}.
//  5. CANCELLED  — any CANCELLED, none in {READY, DISPATCHED, RUNNING, AMBIGUOUS, FAILED}.
//  6. PENDING    — every deck_job is PENDING or READY.
func DeriveRunStatus(jobs []storegen.DeckJobs) gen.RunStatus {
	if len(jobs) == 0 {
		return gen.PENDING
	}

	var (
		hasPending, hasReady, hasDispatched, hasRunning     bool
		hasAmbiguous, hasCompleted, hasFailed, hasCancelled bool
	)
	for _, j := range jobs {
		switch gen.DeckJobStatus(j.Status) {
		case gen.DeckJobStatusPENDING:
			hasPending = true
		case gen.DeckJobStatusREADY:
			hasReady = true
		case gen.DeckJobStatusDISPATCHED:
			hasDispatched = true
		case gen.DeckJobStatusRUNNING:
			hasRunning = true
		case gen.DeckJobStatusAMBIGUOUS:
			hasAmbiguous = true
		case gen.DeckJobStatusCOMPLETED:
			hasCompleted = true
		case gen.DeckJobStatusFAILED:
			hasFailed = true
		case gen.DeckJobStatusCANCELLED:
			hasCancelled = true
		}
	}

	allCompleted := hasCompleted &&
		!hasPending && !hasReady && !hasDispatched && !hasRunning &&
		!hasAmbiguous && !hasFailed && !hasCancelled

	switch {
	case allCompleted:
		return gen.COMPLETED
	case hasAmbiguous:
		return gen.AMBIGUOUS
	case hasDispatched || hasRunning:
		return gen.RUNNING
	case hasFailed && !hasReady && !hasDispatched && !hasRunning && !hasAmbiguous:
		return gen.FAILED
	case hasCancelled && !hasReady && !hasDispatched && !hasRunning && !hasAmbiguous && !hasFailed:
		return gen.CANCELLED
	default:
		return gen.PENDING
	}
}

// IsTerminalRunStatus: {COMPLETED, CANCELLED}. See DESIGN.md "State machine
// — run status". FAILED is intentionally non-terminal: a run with FAILED
// jobs sits awaiting operator decision (retry to resume, or cancel to give
// up) rather than auto-terminalizing. AMBIGUOUS is non-terminal for the
// same reason. The orchestrator never decides the experiment is over;
// only the operator does.
func IsTerminalRunStatus(s gen.RunStatus) bool {
	switch s {
	case gen.COMPLETED, gen.CANCELLED:
		return true
	default:
		return false
	}
}
