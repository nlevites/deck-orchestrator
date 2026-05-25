package handlers

import (
	"log/slog"
	"time"

	"deck-fleet/backend/internal/store"
)

// ExecutorDeps are collaborators for executor-facing endpoints (API.md §9.1–§9.3).
type ExecutorDeps struct {
	Store  *store.DB
	Logger *slog.Logger

	// PollHoldMax caps how long /executor/poll blocks waiting for work.
	// Zero falls back to the legacy short-poll behavior (200/204 immediate).
	// Backs S1 in analysis/inefficiencies/inefficiencies.md.
	PollHoldMax time.Duration
}

// ExecutorAPI wraps the orchestrator endpoints the executor calls.
type ExecutorAPI struct {
	deps ExecutorDeps
}

func NewExecutorAPI(deps ExecutorDeps) *ExecutorAPI { return &ExecutorAPI{deps: deps} }
