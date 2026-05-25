package liveness

import (
	"context"
	"database/sql"
	"log/slog"
	"sync/atomic"
	"time"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/eventlog"
	"deck-fleet/backend/internal/reconciler"
	"deck-fleet/backend/internal/store"
	storegen "deck-fleet/backend/internal/store/gen"
	"deck-fleet/backend/internal/timeouts"
)

type Monitor struct {
	Store         *store.DB
	Reconciler    *reconciler.Reconciler
	Logger        *slog.Logger
	Timeouts      timeouts.Config
	SweepInterval time.Duration // defaults to 2s when unset

	// Degraded latch: cleared once in-flight work (DISPATCHED/RUNNING/AMBIGUOUS) is empty.
	// Startup reconcile may leave it set after probe failure; without this hook it stays 503 forever.
	Degraded *atomic.Bool
}

type Deps struct {
	Store         *store.DB
	Reconciler    *reconciler.Reconciler
	Logger        *slog.Logger
	Timeouts      timeouts.Config
	SweepInterval time.Duration // 0 -> 2s
	Degraded      *atomic.Bool  // optional; nil disables the latch hook
}

func New(d Deps) *Monitor {
	if d.Store == nil {
		panic("liveness.New: Store is required")
	}
	if d.Reconciler == nil {
		panic("liveness.New: Reconciler is required")
	}
	if d.Logger == nil {
		panic("liveness.New: Logger is required")
	}
	sweep := d.SweepInterval
	if sweep <= 0 {
		sweep = 2 * time.Second
	}
	return &Monitor{
		Store:         d.Store,
		Reconciler:    d.Reconciler,
		Logger:        d.Logger,
		Timeouts:      d.Timeouts,
		SweepInterval: sweep,
		Degraded:      d.Degraded,
	}
}

func (m *Monitor) Run(ctx context.Context) {
	interval := m.SweepInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	m.sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.sweep(ctx)
		}
	}
}

func (m *Monitor) sweep(ctx context.Context) {
	now := time.Now().UTC()
	if err := m.staleHeartbeatScan(ctx, now); err != nil {
		m.Logger.Error("liveness: stale heartbeat scan", "err", err)
	}
	if err := m.attemptDeadlineScan(ctx, now); err != nil {
		m.Logger.Error("liveness: attempt deadline scan", "err", err)
	}
	if err := m.ambiguousDeadlineScan(ctx, now); err != nil {
		m.Logger.Error("liveness: ambiguous deadline scan", "err", err)
	}
	m.maybeClearDegraded(ctx)
}

// maybeClearDegraded drops the DEGRADED latch when no in-flight deck_jobs remain.
func (m *Monitor) maybeClearDegraded(ctx context.Context) {
	if m.Degraded == nil || !m.Degraded.Load() {
		return
	}
	rows, err := m.Store.ReadQueries.ListInFlightDeckJobs(ctx)
	if err != nil {
		m.Logger.Warn("liveness: degraded check: list in-flight", "err", err)
		return
	}
	if len(rows) == 0 {
		if m.Degraded.CompareAndSwap(true, false) {
			m.Logger.Info("liveness: degraded cleared by sweep (no in-flight work)")
		}
	}
}

func (m *Monitor) staleHeartbeatScan(ctx context.Context, now time.Time) error {
	cutoff := sql.NullInt64{Int64: now.Add(-m.Timeouts.StaleThreshold).UnixMilli(), Valid: true}
	rows, err := m.Store.ReadQueries.ListDecksWithStaleHeartbeat(ctx, cutoff)
	if err != nil {
		return err
	}
	for _, deck := range rows {
		curHealth := gen.DeckHealth(deck.LastKnownHealth)
		if curHealth == gen.HEALTHY {
			if err := m.transitionDeckHealth(ctx, deck.ID, curHealth, gen.STALE, now); err != nil {
				m.Logger.Error("liveness: mark stale", "deck_id", deck.ID, "err", err)
				continue
			}
			curHealth = gen.STALE
		}
		inflight, err := m.Store.ReadQueries.ListInFlightJobsForDeck(ctx, deck.ID)
		if err != nil {
			m.Logger.Error("liveness: in-flight for stale deck", "deck_id", deck.ID, "err", err)
			continue
		}
		anyUnreachable := false
		for _, j := range inflight {
			if !j.CurrentAttemptID.Valid {
				continue
			}
			outcome, rcErr := m.Reconciler.ReconcileAttempt(ctx, deck.ID, j.CurrentAttemptID.String)
			if rcErr != nil {
				m.Logger.Error("liveness: reconcile failed", "deck_id", deck.ID, "err", rcErr)
			}
			if outcome == reconciler.OutcomeUnreachable {
				anyUnreachable = true
			}
		}
		if anyUnreachable && curHealth != gen.UNREACHABLE {
			if err := m.transitionDeckHealth(ctx, deck.ID, curHealth, gen.UNREACHABLE, now); err != nil {
				m.Logger.Error("liveness: mark unreachable", "deck_id", deck.ID, "err", err)
			}
			continue
		}

		// STALE decks with no in-flight work escalate to UNREACHABLE after AmbiguousDeadline.
		if curHealth == gen.STALE && deck.LastHeartbeatAt.Valid {
			age := now.Sub(time.UnixMilli(deck.LastHeartbeatAt.Int64))
			if age > m.Timeouts.AmbiguousDeadline {
				if err := m.transitionDeckHealth(ctx, deck.ID, curHealth, gen.UNREACHABLE, now); err != nil {
					m.Logger.Error("liveness: escalate stale->unreachable (no in-flight)",
						"deck_id", deck.ID, "age", age, "err", err)
				}
			}
		}
	}
	return nil
}

func (m *Monitor) attemptDeadlineScan(ctx context.Context, now time.Time) error {
	rows, err := m.Store.ReadQueries.ListOverdueAttempts(ctx, storegen.ListOverdueAttemptsParams{
		BaseMs:    m.Timeouts.AttemptDeadlineBase.Milliseconds(),
		PerStepMs: m.Timeouts.AttemptDeadlinePerStep.Milliseconds(),
		NowMs:     now.UnixMilli(),
	})
	if err != nil {
		return err
	}
	for _, a := range rows {
		outcome, rcErr := m.Reconciler.ReconcileAttempt(ctx, a.DeckID, a.AttemptID)
		if rcErr != nil {
			m.Logger.Warn("liveness: reconcile overdue attempt", "attempt_id", a.AttemptID, "err", rcErr)
			continue
		}
		// DEADLINE_EXCEEDED only when executor confirms still running past AttemptDeadline.
		// Other outcomes are handled elsewhere; ambiguousDeadlineScan covers UNREACHABLE decks.
		if outcome == reconciler.OutcomeRunning {
			if mErr := m.Reconciler.MarkAmbiguousDeadline(ctx, a.AttemptID, eventlog.AmbiguousReasonDeadlineExceeded); mErr != nil {
				m.Logger.Error("liveness: mark ambiguous (DEADLINE_EXCEEDED)", "attempt_id", a.AttemptID, "err", mErr)
			}
		}
	}
	return nil
}

func (m *Monitor) ambiguousDeadlineScan(ctx context.Context, now time.Time) error {
	cutoff := sql.NullInt64{Int64: now.Add(-m.Timeouts.AmbiguousDeadline).UnixMilli(), Valid: true}
	decks, err := m.Store.ReadQueries.ListDecksWithStaleHeartbeat(ctx, cutoff)
	if err != nil {
		return err
	}
	for _, deck := range decks {
		if gen.DeckHealth(deck.LastKnownHealth) != gen.UNREACHABLE {
			continue
		}
		inflight, err := m.Store.ReadQueries.ListInFlightJobsForDeck(ctx, deck.ID)
		if err != nil {
			m.Logger.Error("liveness: list in-flight for unreachable deck", "deck_id", deck.ID, "err", err)
			continue
		}
		for _, j := range inflight {
			if !j.CurrentAttemptID.Valid {
				continue
			}
			if mErr := m.Reconciler.MarkAmbiguousDeadline(ctx, j.CurrentAttemptID.String, eventlog.AmbiguousReasonDeadlineElapsed); mErr != nil {
				m.Logger.Error("liveness: mark ambiguous (DEADLINE_ELAPSED)", "attempt_id", j.CurrentAttemptID.String, "err", mErr)
			}
		}
	}
	return nil
}

func (m *Monitor) transitionDeckHealth(ctx context.Context, deckID string, from, to gen.DeckHealth, now time.Time) error {
	if from == to {
		return nil
	}
	return m.Store.WithTx(ctx, func(q *storegen.Queries) error {
		rows, err := q.UpdateDeckHealth(ctx, storegen.UpdateDeckHealthParams{
			LastKnownHealth: string(to),
			ID:              deckID,
			ExpectedHealth:  string(from),
		})
		if err != nil {
			return err
		}
		if rows == 0 {
			// CAS lost; skip fictitious from->to in the audit log.
			m.Logger.Debug("liveness: deck health CAS skipped",
				"deck_id", deckID, "expected_from", from, "to", to)
			return nil
		}
		m.Logger.Info("liveness: deck health change", "deck_id", deckID, "from", from, "to", to)
		_, err = eventlog.Append(ctx, q, eventlog.KindDeckHealthChanged,
			eventlog.Scope{DeckID: deckID}, now,
			eventlog.DeckHealthChangedPayload{From: from, To: to, LastHeartbeatAt: now})
		return err
	})
}
