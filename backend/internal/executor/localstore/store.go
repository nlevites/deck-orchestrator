package localstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	localstoregen "deck-fleet/backend/internal/executor/localstore/gen"
	migrations "deck-fleet/backend/sql/executor"
)

type State string

const (
	StateReceived   State = "RECEIVED"
	StateInProgress State = "IN_PROGRESS"
	StateCompleted  State = "COMPLETED"
	StateFailed     State = "FAILED"
)

type EventKind string

const (
	EventRunning       EventKind = "RUNNING"
	EventCompleted     EventKind = "COMPLETED"
	EventFailed        EventKind = "FAILED"
	EventStepCompleted EventKind = "STEP_COMPLETED"
)

type Attempt struct {
	AttemptID         string
	RunID             string
	JobID             string
	DeckID            string
	StepsJSON         string
	ReceivedAt        time.Time
	StartedAt         *time.Time
	TerminalAt        *time.Time
	State             State
	Result            *string
	Error             *string
	AbortRequested    bool
	LastCompletedStep int // C2: per-step crash-resume cursor; 0 means no steps completed
}

type OutboxRow struct {
	Seq           int64
	AttemptID     string
	Kind          EventKind
	Payload       string
	OccurredAt    time.Time
	Retries       int64
	LastAttemptAt *time.Time
}

// Store wraps the per-deck SQLite DB. SQL lives in sql/executor/queries/*.sql
// (localstoregen); this file owns open/migrate, row conversions, and atomic helpers.
type Store struct {
	DB     *sql.DB
	q      *localstoregen.Queries
	logger *slog.Logger
}

func Open(ctx context.Context, path string, logger *slog.Logger) (*Store, error) {
	if path == "" {
		return nil, errors.New("localstore: empty path")
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, p := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.ExecContext(ctx, p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(gooseLogger{logger: logger})
	if err := goose.SetDialect("sqlite3"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.Up(db, "."); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("goose up: %w", err)
	}
	return &Store{DB: db, q: localstoregen.New(db), logger: logger}, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// withTx runs fn in a write transaction. Mirrors orchestrator store.WithTx.
func (s *Store) withTx(ctx context.Context, fn func(q *localstoregen.Queries) error) (err error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				err = errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
			}
			return
		}
		if cErr := tx.Commit(); cErr != nil {
			err = fmt.Errorf("commit: %w", cErr)
		}
	}()
	err = fn(s.q.WithTx(tx))
	return err
}

func (s *Store) InsertReceived(ctx context.Context, a Attempt) (inserted bool, err error) {
	rows, err := s.q.InsertReceived(ctx, localstoregen.InsertReceivedParams{
		AttemptID:  a.AttemptID,
		RunID:      a.RunID,
		JobID:      a.JobID,
		DeckID:     a.DeckID,
		Steps:      a.StepsJSON,
		ReceivedAt: a.ReceivedAt.UnixMilli(),
	})
	if err != nil {
		return false, fmt.Errorf("insert received: %w", err)
	}
	return rows == 1, nil
}

func (s *Store) GetAttempt(ctx context.Context, attemptID string) (Attempt, error) {
	row, err := s.q.GetAttempt(ctx, attemptID)
	if err != nil {
		return Attempt{}, err
	}
	return rowToAttempt(row), nil
}

func (s *Store) ListRecentAttempts(ctx context.Context, limit int) ([]Attempt, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.q.ListRecentAttempts(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list attempts: %w", err)
	}
	out := make([]Attempt, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowToAttempt(r))
	}
	return out, nil
}

func (s *Store) CurrentInFlight(ctx context.Context) (Attempt, bool, error) {
	row, err := s.q.GetCurrentInFlight(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return Attempt{}, false, nil
	}
	if err != nil {
		return Attempt{}, false, err
	}
	return rowToAttempt(row), true, nil
}

// BumpStepCursor records step completion (1-indexed). Monotonic guard
// makes out-of-order replay a no-op. Prefer BumpStepCursorWithEvent
// in production; this is for test pre-seeding.
func (s *Store) BumpStepCursor(ctx context.Context, attemptID string, step int) error {
	return s.q.BumpStepCursor(ctx, localstoregen.BumpStepCursorParams{
		Step:      int64(step),
		AttemptID: attemptID,
	})
}

// BumpStepCursorWithEvent atomically advances the step cursor and enqueues
// STEP_COMPLETED. Split writes leave local state ahead of the outbox (same
// bug class as MarkInProgressWithEvent / MarkTerminalWithEvent). Duplicate
// delivery is safe: orchestrator applies monotonic MAX on its side.
func (s *Store) BumpStepCursorWithEvent(ctx context.Context, attemptID string, step, total int, now time.Time) error {
	payload := fmt.Sprintf(`{"step":%d,"total":%d}`, step, total)
	return s.withTx(ctx, func(q *localstoregen.Queries) error {
		if err := q.BumpStepCursor(ctx, localstoregen.BumpStepCursorParams{
			Step:      int64(step),
			AttemptID: attemptID,
		}); err != nil {
			return fmt.Errorf("bump cursor: %w", err)
		}
		if err := q.EnqueueEvent(ctx, localstoregen.EnqueueEventParams{
			AttemptID:  attemptID,
			Kind:       string(EventStepCompleted),
			Payload:    payload,
			OccurredAt: now.UnixMilli(),
			CreatedAt:  now.UnixMilli(),
		}); err != nil {
			return fmt.Errorf("enqueue step event: %w", err)
		}
		return nil
	})
}

// MarkInProgress transitions RECEIVED → IN_PROGRESS. Production uses
// MarkInProgressWithEvent; this is for tests that pre-seed local state.
func (s *Store) MarkInProgress(ctx context.Context, attemptID string, now time.Time) error {
	return s.q.MarkInProgress(ctx, localstoregen.MarkInProgressParams{
		StartedAt: sql.NullInt64{Int64: now.UnixMilli(), Valid: true},
		AttemptID: attemptID,
	})
}

// MarkTerminal writes terminal local state without the outbox row.
// Production uses MarkTerminalWithEvent; for test pre-seeding only.
func (s *Store) MarkTerminal(ctx context.Context, attemptID string, finalState State, resultJSON, errStr string, now time.Time) error {
	if finalState != StateCompleted && finalState != StateFailed {
		return fmt.Errorf("MarkTerminal: bad state %q", finalState)
	}
	return s.q.MarkTerminal(ctx, markTerminalParams(attemptID, finalState, resultJSON, errStr, now))
}

// MarkInProgressWithEvent: RECEIVED → IN_PROGRESS + RUNNING outbox row atomically.
// Split writes left local state ahead of the outbox; reconciler was the only catch-up.
func (s *Store) MarkInProgressWithEvent(ctx context.Context, attemptID string, now time.Time) error {
	return s.withTx(ctx, func(q *localstoregen.Queries) error {
		if err := q.MarkInProgress(ctx, localstoregen.MarkInProgressParams{
			StartedAt: sql.NullInt64{Int64: now.UnixMilli(), Valid: true},
			AttemptID: attemptID,
		}); err != nil {
			return fmt.Errorf("mark in_progress: %w", err)
		}
		if err := q.EnqueueEvent(ctx, localstoregen.EnqueueEventParams{
			AttemptID:  attemptID,
			Kind:       string(EventRunning),
			Payload:    "{}",
			OccurredAt: now.UnixMilli(),
			CreatedAt:  now.UnixMilli(),
		}); err != nil {
			return fmt.Errorf("enqueue running: %w", err)
		}
		return nil
	})
}

// MarkTerminalWithEvent: terminal state + outbox row atomically. Without this,
// worker resume skips terminal attempts and orchestrator only catches up via reconcile.
func (s *Store) MarkTerminalWithEvent(ctx context.Context, attemptID string, finalState State, resultJSON, errStr string, eventPayload []byte, now time.Time) error {
	if finalState != StateCompleted && finalState != StateFailed {
		return fmt.Errorf("MarkTerminalWithEvent: bad state %q", finalState)
	}
	if len(eventPayload) == 0 {
		eventPayload = []byte("{}")
	}
	var kind EventKind
	switch finalState {
	case StateCompleted:
		kind = EventCompleted
	case StateFailed:
		kind = EventFailed
	case StateReceived, StateInProgress:
		panic("MarkTerminalWithEvent called with non-terminal state: " + string(finalState))
	}
	return s.withTx(ctx, func(q *localstoregen.Queries) error {
		if err := q.MarkTerminal(ctx, markTerminalParams(attemptID, finalState, resultJSON, errStr, now)); err != nil {
			return fmt.Errorf("mark terminal: %w", err)
		}
		if err := q.EnqueueEvent(ctx, localstoregen.EnqueueEventParams{
			AttemptID:  attemptID,
			Kind:       string(kind),
			Payload:    string(eventPayload),
			OccurredAt: now.UnixMilli(),
			CreatedAt:  now.UnixMilli(),
		}); err != nil {
			return fmt.Errorf("enqueue terminal: %w", err)
		}
		return nil
	})
}

func (s *Store) SetAbortRequested(ctx context.Context, attemptID string) error {
	return s.q.SetAbortRequested(ctx, attemptID)
}

func (s *Store) EnqueueEvent(ctx context.Context, attemptID string, kind EventKind, payload any, now time.Time) error {
	var pb []byte
	if payload == nil {
		pb = []byte("{}")
	} else {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
		pb = b
	}
	if err := s.q.EnqueueEvent(ctx, localstoregen.EnqueueEventParams{
		AttemptID:  attemptID,
		Kind:       string(kind),
		Payload:    string(pb),
		OccurredAt: now.UnixMilli(),
		CreatedAt:  now.UnixMilli(),
	}); err != nil {
		return fmt.Errorf("insert outbox: %w", err)
	}
	return nil
}

func (s *Store) NextOutbox(ctx context.Context) (OutboxRow, bool, error) {
	row, err := s.q.NextOutbox(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return OutboxRow{}, false, nil
	}
	if err != nil {
		return OutboxRow{}, false, err
	}
	r := OutboxRow{
		Seq:        row.Seq,
		AttemptID:  row.AttemptID,
		Kind:       EventKind(row.Kind),
		Payload:    row.Payload,
		OccurredAt: time.UnixMilli(row.OccurredAt).UTC(),
		Retries:    row.Retries,
	}
	if row.LastAttemptAt.Valid {
		t := time.UnixMilli(row.LastAttemptAt.Int64).UTC()
		r.LastAttemptAt = &t
	}
	return r, true, nil
}

func (s *Store) DeleteOutbox(ctx context.Context, seq int64) error {
	return s.q.DeleteOutbox(ctx, seq)
}

func (s *Store) BumpOutboxRetry(ctx context.Context, seq int64, now time.Time) error {
	return s.q.BumpOutboxRetry(ctx, localstoregen.BumpOutboxRetryParams{
		LastAttemptAt: sql.NullInt64{Int64: now.UnixMilli(), Valid: true},
		Seq:           seq,
	})
}

func markTerminalParams(attemptID string, finalState State, resultJSON, errStr string, now time.Time) localstoregen.MarkTerminalParams {
	var (
		result sql.NullString
		errN   sql.NullString
	)
	if resultJSON != "" {
		result = sql.NullString{String: resultJSON, Valid: true}
	}
	if errStr != "" {
		errN = sql.NullString{String: errStr, Valid: true}
	}
	return localstoregen.MarkTerminalParams{
		State:      string(finalState),
		TerminalAt: sql.NullInt64{Int64: now.UnixMilli(), Valid: true},
		Result:     result,
		Error:      errN,
		AttemptID:  attemptID,
	}
}

func rowToAttempt(r localstoregen.Attempts) Attempt {
	a := Attempt{
		AttemptID:         r.AttemptID,
		RunID:             r.RunID,
		JobID:             r.JobID,
		DeckID:            r.DeckID,
		StepsJSON:         r.Steps,
		ReceivedAt:        time.UnixMilli(r.ReceivedAt).UTC(),
		State:             State(r.State),
		AbortRequested:    r.AbortRequested != 0,
		LastCompletedStep: int(r.LastCompletedStep),
	}
	if r.StartedAt.Valid {
		t := time.UnixMilli(r.StartedAt.Int64).UTC()
		a.StartedAt = &t
	}
	if r.TerminalAt.Valid {
		t := time.UnixMilli(r.TerminalAt.Int64).UTC()
		a.TerminalAt = &t
	}
	if r.Result.Valid {
		v := r.Result.String
		a.Result = &v
	}
	if r.Error.Valid {
		v := r.Error.String
		a.Error = &v
	}
	return a
}

type gooseLogger struct{ logger *slog.Logger }

func (g gooseLogger) Fatalf(format string, v ...any) {
	g.logger.Error(fmt.Sprintf(format, v...))
}

func (g gooseLogger) Printf(format string, v ...any) {
	g.logger.Info(fmt.Sprintf(format, v...))
}
