// Package app wires orchestrator and executor graphs. cmd/* loads config
// and calls Run*; integration tests call the same Build* constructors.
//
// Build* returns a wired Handler without a listener or background loops.
// Run* adds the listener, signal handling, and graceful shutdown.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"deck-fleet/backend/internal/aborts"
	"deck-fleet/backend/internal/api"
	"deck-fleet/backend/internal/config"
	"deck-fleet/backend/internal/handlers"
	"deck-fleet/backend/internal/liveness"
	"deck-fleet/backend/internal/reconciler"
	"deck-fleet/backend/internal/store"
	storegen "deck-fleet/backend/internal/store/gen"
)

// Orchestrator is the wired graph cmd/orchestrator and integration tests share.
type Orchestrator struct {
	Cfg *config.Config

	Degraded *atomic.Bool

	Handler http.Handler

	// RestartCh closes on POST /api/admin/restart. The entrypoint cancels
	// runCtx, drains HTTP, and exits so the supervisor can respawn.
	// A channel (not os.Exit in the handler) keeps tests observable.
	RestartCh chan struct{}

	logger      *slog.Logger
	db          *store.DB
	reconciler  *reconciler.Reconciler
	monitor     *liveness.Monitor
	abortDialer *aborts.Dialer
	operator    *handlers.Operator
	executorAPI *handlers.ExecutorAPI
	chaos       *handlers.Chaos
	live        *handlers.Live
}

// SetMonitorSweepInterval compresses liveness timing for integration tests.
// Must run before Run; production callers should not use it.
func (o *Orchestrator) SetMonitorSweepInterval(d time.Duration) {
	o.monitor.SweepInterval = d
}

// DB exposes the store for integration tests that need raw SQL. Production
// callers should use the HTTP handlers instead.
func (o *Orchestrator) DB() *store.DB {
	return o.db
}

// BuildOrchestrator opens the store, wires collaborators, and returns the
// HTTP handler. Does not start goroutines or bind a port. On error, any
// opened DB is closed.
func BuildOrchestrator(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*Orchestrator, error) {
	if cfg == nil {
		return nil, fmt.Errorf("app: orchestrator config is nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("app: orchestrator logger is nil")
	}

	db, err := store.Open(ctx, cfg.Store, logger)
	if err != nil {
		return nil, fmt.Errorf("store.Open: %w", err)
	}

	if seedErr := seedFleet(ctx, db, cfg.FleetSize, logger); seedErr != nil {
		_ = db.Close()
		return nil, seedErr
	}

	o := &Orchestrator{
		Cfg:      cfg,
		logger:   logger,
		db:       db,
		Degraded: &atomic.Bool{},
	}
	o.Degraded.Store(true)

	o.reconciler = reconciler.New(reconciler.Deps{
		Store:          db,
		Logger:         logger,
		HTTPTimeout:    cfg.Timeouts.ReconcileHTTPTimeout,
		RequestTimeout: cfg.Timeouts.ReconcileRequestTimeout,
	})
	o.monitor = liveness.New(liveness.Deps{
		Store:         db,
		Reconciler:    o.reconciler,
		Logger:        logger,
		Timeouts:      cfg.Timeouts,
		SweepInterval: cfg.Timeouts.LivenessSweepInterval,
		Degraded:      o.Degraded,
	})
	o.abortDialer = aborts.New(aborts.Deps{
		Store:       db,
		Logger:      logger,
		Timeouts:    cfg.Timeouts,
		HTTPTimeout: cfg.Timeouts.AbortHTTPTimeout,
		Concurrency: cfg.Timeouts.AbortConcurrency,
	})

	o.operator = handlers.NewOperator(handlers.Deps{
		Store:          db,
		Logger:         logger,
		AbortScheduler: o.abortDialer,
	})
	o.executorAPI = handlers.NewExecutorAPI(handlers.ExecutorDeps{
		Store:       db,
		Logger:      logger,
		PollHoldMax: cfg.HTTP.PollHoldMax,
	})
	o.chaos = handlers.NewChaos(handlers.ChaosDeps{
		Store:  db,
		Logger: logger,
	})
	o.live = handlers.NewLive(handlers.LiveDeps{
		Store:  db,
		Logger: logger,
	})
	o.RestartCh = make(chan struct{})

	o.Handler = api.NewServer(api.Deps{
		Logger:         logger,
		HTTP:           cfg.HTTP,
		CORS:           cfg.CORS,
		Degraded:       o.Degraded,
		RegisterRoutes: o.registerRoutes,
	})

	return o, nil
}

func (o *Orchestrator) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/runs", o.operator.SubmitRun)
	mux.HandleFunc("GET /api/runs", o.operator.ListRuns)
	mux.HandleFunc("GET /api/runs/{id}", o.operator.GetRun)
	mux.HandleFunc("GET /api/runs/{id}/state", o.live.GetRunState)
	mux.HandleFunc("POST /api/runs/{id}/cancel", o.operator.CancelRun)
	mux.HandleFunc("POST /api/runs/{id}/jobs/{job_id}/retry", o.operator.RetryJob)
	mux.HandleFunc("POST /api/runs/{id}/jobs/{job_id}/resolve", o.operator.ResolveJob)
	mux.HandleFunc("GET /api/decks", o.operator.ListDecks)
	mux.HandleFunc("POST /api/decks/{deck_id}/release", o.operator.ReleaseDeck)

	mux.HandleFunc("GET /api/state", o.live.GetState)

	mux.HandleFunc("GET /api/decks/{deck_id}/chaos", o.chaos.GetState)
	mux.HandleFunc("POST /api/decks/{deck_id}/chaos", o.chaos.PatchState)
	mux.HandleFunc("POST /api/decks/{deck_id}/chaos/reset", o.chaos.Reset)
	mux.HandleFunc("POST /api/decks/{deck_id}/chaos/crash", o.chaos.Crash)

	mux.HandleFunc("POST /api/admin/restart", o.handleRestart)

	mux.HandleFunc("GET /executor/poll", o.executorAPI.Poll)
	mux.HandleFunc("POST /executor/heartbeat", o.executorAPI.Heartbeat)
	mux.HandleFunc("POST /executor/events", o.executorAPI.Event)
}

func (o *Orchestrator) handleRestart(w http.ResponseWriter, _ *http.Request) {
	o.logger.Warn("admin: restart requested")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"restarting"}`))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	select {
	case <-o.RestartCh:
	default:
		close(o.RestartCh)
	}
}

// Run runs startup reconciliation, clears DEGRADED when every in-flight
// attempt resolves (or there were none), then runs the liveness monitor.
//
// DEGRADED stays set if any probe errors before producing an outcome.
// OutcomeUnreachable counts as a real outcome; the monitor's deadline
// scans finish the rest. Failed probes leave operator mutations at 503.
func (o *Orchestrator) Run(ctx context.Context) {
	resolved := o.startupReconcile(ctx)
	if resolved {
		o.Degraded.Store(false)
		o.logger.Info("orchestrator: startup reconciliation complete; DEGRADED_MODE cleared")
	} else {
		o.logger.Warn("orchestrator: startup reconciliation incomplete; DEGRADED_MODE retained " +
			"until liveness monitor resolves remaining in-flight attempts")
	}
	o.monitor.Run(ctx)
}

// Shutdown waits for abort dials and closes the DB. Cancel Run's ctx and
// wait for Run to return first.
func (o *Orchestrator) Shutdown() error {
	o.abortDialer.Wait()
	if err := o.db.Close(); err != nil {
		return fmt.Errorf("close db: %w", err)
	}
	return nil
}

// seedFleet creates deck-1..deck-N as EMPTY when absent. Shrinking
// fleet_size below the highest active deck fails boot; decommission first.
// fleet_size <= 0 skips seeding (tests pre-populate decks).
func seedFleet(ctx context.Context, db *store.DB, fleetSize int, logger *slog.Logger) error {
	if fleetSize <= 0 {
		logger.Info("orchestrator: fleet_size disabled; skipping seed")
		return nil
	}
	maxN, err := db.ReadQueries.MaxActiveDeckNumber(ctx)
	if err != nil {
		return fmt.Errorf("orchestrator: read max active deck number: %w", err)
	}
	if int(maxN) > fleetSize {
		return fmt.Errorf(
			"orchestrator: fleet_size %d is smaller than the highest non-decommissioned deck (deck-%d); "+
				"decommission those slots explicitly before shrinking the fleet",
			fleetSize, maxN)
	}
	now := time.Now().UTC().UnixMilli()
	err = db.WithTx(ctx, func(q *storegen.Queries) error {
		for i := 1; i <= fleetSize; i++ {
			id := fmt.Sprintf("deck-%d", i)
			if _, qErr := q.UpsertEmptyDeckIfAbsent(ctx, storegen.UpsertEmptyDeckIfAbsentParams{
				ID:          id,
				FirstSeenAt: now,
			}); qErr != nil {
				return fmt.Errorf("seed %s: %w", id, qErr)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	logger.Info("orchestrator: fleet seeded", "fleet_size", fleetSize)
	return nil
}

// startupReconcile probes in-flight attempts (cap 8 concurrent). Returns
// true when every probe yields an outcome or there was nothing to probe.
func (o *Orchestrator) startupReconcile(ctx context.Context) bool {
	rows, err := o.db.ReadQueries.ListInFlightDeckJobs(ctx)
	if err != nil {
		o.logger.Error("startup reconcile: list in-flight", "err", err)
		return false
	}
	if len(rows) == 0 {
		o.logger.Info("startup reconcile: no in-flight work")
		return true
	}
	o.logger.Info("startup reconcile: probing executors", "count", len(rows))

	var failures atomic.Int64
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, j := range rows {
		j := j
		if !j.CurrentAttemptID.Valid {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			outcome, rcErr := o.reconciler.ReconcileAttempt(ctx, j.DeckID, j.CurrentAttemptID.String)
			if rcErr != nil {
				failures.Add(1)
				o.logger.Warn("startup reconcile: error",
					"deck_id", j.DeckID, "attempt_id", j.CurrentAttemptID.String, "err", rcErr)
				return
			}
			o.logger.Info("startup reconcile: result",
				"deck_id", j.DeckID, "attempt_id", j.CurrentAttemptID.String, "outcome", outcome)
		}()
	}
	wg.Wait()
	if n := failures.Load(); n > 0 {
		o.logger.Warn("startup reconcile: probe failures recorded; staying DEGRADED",
			"failed", n, "total", len(rows))
		return false
	}
	return true
}
