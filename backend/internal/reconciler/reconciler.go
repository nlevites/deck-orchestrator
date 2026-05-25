package reconciler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/dispatch"
	"deck-fleet/backend/internal/eventlog"
	"deck-fleet/backend/internal/state"
	"deck-fleet/backend/internal/store"
	storegen "deck-fleet/backend/internal/store/gen"
)

type Outcome string

const (
	OutcomeApplied     Outcome = "applied"
	OutcomeRunning     Outcome = "running"
	OutcomeNoChange    Outcome = "no_change"
	OutcomeAmbiguous   Outcome = "ambiguous"
	OutcomeUnreachable Outcome = "unreachable"
	// OutcomeNoDispatch: executor UNKNOWN with no prior evidence — keep
	// DISPATCHED; next poll re-delivers the same attempt (STATE_MACHINE §8.5).
	OutcomeNoDispatch Outcome = "no_dispatch"
)

type Reconciler struct {
	Store          *store.DB
	HTTPClient     *http.Client
	Logger         *slog.Logger
	RequestTimeout time.Duration
}

type Deps struct {
	Store          *store.DB
	Logger         *slog.Logger
	HTTPClient     *http.Client // nil → default client with HTTPTimeout
	HTTPTimeout    time.Duration
	RequestTimeout time.Duration
}

func New(d Deps) *Reconciler {
	if d.Store == nil {
		panic("reconciler.New: Store is required")
	}
	if d.Logger == nil {
		panic("reconciler.New: Logger is required")
	}
	hc := d.HTTPClient
	if hc == nil {
		timeout := d.HTTPTimeout
		if timeout <= 0 {
			timeout = 6 * time.Second
		}
		hc = &http.Client{Timeout: timeout}
	}
	rt := d.RequestTimeout
	if rt <= 0 {
		rt = 5 * time.Second
	}
	return &Reconciler{
		Store:          d.Store,
		HTTPClient:     hc,
		Logger:         d.Logger,
		RequestTimeout: rt,
	}
}

type stateReport struct {
	AttemptID  string          `json:"attempt_id"`
	State      string          `json:"state"`
	ReceivedAt time.Time       `json:"received_at"`
	StartedAt  *time.Time      `json:"started_at,omitempty"`
	TerminalAt *time.Time      `json:"terminal_at,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      *string         `json:"error,omitempty"`
}

func (r *Reconciler) ReconcileAttempt(ctx context.Context, deckID, attemptID string) (Outcome, error) {
	deck, err := r.Store.ReadQueries.GetDeck(ctx, deckID)
	if errors.Is(err, sql.ErrNoRows) {
		return OutcomeUnreachable, fmt.Errorf("deck %q not registered", deckID)
	}
	if err != nil {
		return OutcomeUnreachable, fmt.Errorf("load deck %q: %w", deckID, err)
	}
	if !deck.EndpointUrl.Valid || deck.EndpointUrl.String == "" {
		// No executor has heartbeated yet — nothing to dial.
		return OutcomeUnreachable, nil
	}

	report, unreachable, isUnknown, err := r.dial(ctx, deck.EndpointUrl.String, attemptID)
	if unreachable {
		r.Logger.Info("reconciler: unreachable", "deck_id", deckID, "attempt_id", attemptID, "err", err)
		return OutcomeUnreachable, nil
	}
	if err != nil {
		return OutcomeUnreachable, err
	}

	if isUnknown {
		return r.applyUnknown(ctx, deckID, attemptID)
	}

	switch report.State {
	case string(gen.ExecutorAttemptStateRECEIVED):
		return OutcomeNoChange, nil
	case string(gen.ExecutorAttemptStateINPROGRESS):
		return r.applyInProgress(ctx, deckID, attemptID)
	case string(gen.ExecutorAttemptStateCOMPLETED):
		return r.applyTerminal(ctx, deckID, attemptID, gen.DeckJobStatusCOMPLETED, gen.ExecutorEventKindCOMPLETED, report)
	case string(gen.ExecutorAttemptStateFAILED):
		return r.applyTerminal(ctx, deckID, attemptID, gen.DeckJobStatusFAILED, gen.ExecutorEventKindFAILED, report)
	default:
		return OutcomeUnreachable, fmt.Errorf("unexpected executor state %q", report.State)
	}
}

func (r *Reconciler) dial(ctx context.Context, endpointURL, attemptID string) (stateReport, bool, bool, error) {
	if endpointURL == "" {
		return stateReport{}, true, false, errors.New("empty endpoint_url")
	}
	u, err := url.Parse(endpointURL)
	if err != nil {
		return stateReport{}, true, false, fmt.Errorf("parse endpoint_url: %w", err)
	}
	u.Path = "/executor/state"
	q := u.Query()
	q.Set("attempt_id", attemptID)
	u.RawQuery = q.Encode()

	reqCtx, cancel := context.WithTimeout(ctx, r.RequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return stateReport{}, true, false, err
	}
	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return stateReport{}, true, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return stateReport{}, false, true, nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		var rep stateReport
		if dErr := json.NewDecoder(resp.Body).Decode(&rep); dErr != nil {
			return stateReport{}, false, false, fmt.Errorf("decode state: %w", dErr)
		}
		return rep, false, false, nil
	default:
		return stateReport{}, true, false, fmt.Errorf("executor /state status %d", resp.StatusCode)
	}
}

func (r *Reconciler) applyUnknown(ctx context.Context, deckID, attemptID string) (Outcome, error) {
	hasRunning, err := r.Store.ReadQueries.HasJobRunningEventForAttempt(ctx, sql.NullString{String: attemptID, Valid: true})
	if err != nil {
		return OutcomeUnreachable, fmt.Errorf("evidence (running): %w", err)
	}
	hasClaim, err := r.Store.ReadQueries.HasClaimedHeartbeatForAttempt(ctx, sql.NullString{String: attemptID, Valid: true})
	if err != nil {
		return OutcomeUnreachable, fmt.Errorf("evidence (claim): %w", err)
	}
	if !hasRunning && !hasClaim {
		// §8.5: no evidence → keep DISPATCHED for poll re-delivery.
		r.Logger.Info(
			"reconciler: UNKNOWN with no prior evidence; existing dispatch is valid, executor will re-receive on next poll",
			"deck_id", deckID, "attempt_id", attemptID)
		return OutcomeNoDispatch, nil
	}

	now := time.Now().UTC()
	err = r.Store.WithTx(ctx, func(q *storegen.Queries) error {
		return r.markAmbiguous(ctx, q, attemptID, eventlog.AmbiguousReasonExecutorReportedUnknown, now)
	})
	if err != nil {
		return OutcomeUnreachable, err
	}
	r.Logger.Warn("reconciler: AMBIGUOUS (executor reported unknown with prior evidence)",
		"deck_id", deckID, "attempt_id", attemptID)
	return OutcomeAmbiguous, nil
}

func (r *Reconciler) applyInProgress(ctx context.Context, deckID, attemptID string) (Outcome, error) {
	now := time.Now().UTC()
	err := r.Store.WithTx(ctx, func(q *storegen.Queries) error {
		attempt, aErr := q.GetJobAttempt(ctx, attemptID)
		if aErr != nil {
			return aErr
		}
		jobRow, jErr := q.GetDeckJob(ctx, storegen.GetDeckJobParams{RunID: attempt.RunID, ID: attempt.JobID})
		if jErr != nil {
			return jErr
		}
		// ApplyByAttempt no-ops when current_attempt_id rotated (retry) or status moved.
		rows, uErr := state.ApplyByAttempt(ctx, state.ApplyByAttemptParams{
			Q:                 q,
			AllowedFrom:       []gen.DeckJobStatus{gen.DeckJobStatusDISPATCHED},
			To:                gen.DeckJobStatusRUNNING,
			Trigger:           state.TriggerReconciler,
			RunID:             jobRow.RunID,
			JobID:             jobRow.ID,
			AttemptID:         attemptID,
			NewCurrentAttempt: jobRow.CurrentAttemptID,
			NewError:          jobRow.Error,
		})
		if uErr != nil {
			return uErr
		}
		if rows == 0 {
			return nil
		}
		if _, eErr := eventlog.Append(ctx, q, eventlog.KindJobRunning,
			eventlog.Scope{RunID: jobRow.RunID, JobID: jobRow.ID, DeckID: jobRow.DeckID, AttemptID: attemptID},
			now, eventlog.JobRunningPayload{From: gen.DeckJobStatusDISPATCHED},
		); eErr != nil {
			return eErr
		}
		_, _, mErr := state.MaterializeRunStatus(ctx, q, jobRow.RunID, now)
		return mErr
	})
	if err != nil {
		return OutcomeUnreachable, err
	}
	// OutcomeRunning: executor confirms IN_PROGRESS whether or not we just transitioned.
	r.Logger.Debug("reconciler: executor confirms IN_PROGRESS", "deck_id", deckID, "attempt_id", attemptID)
	return OutcomeRunning, nil
}

func (r *Reconciler) applyTerminal(ctx context.Context, deckID, attemptID string, finalStatus gen.DeckJobStatus, kind gen.ExecutorEventKind, report stateReport) (Outcome, error) {
	now := time.Now().UTC()
	var notifyDecks []string
	err := r.Store.WithTx(ctx, func(q *storegen.Queries) error {
		attempt, aErr := q.GetJobAttempt(ctx, attemptID)
		if aErr != nil {
			return aErr
		}
		jobRow, jErr := q.GetDeckJob(ctx, storegen.GetDeckJobParams{RunID: attempt.RunID, ID: attempt.JobID})
		if jErr != nil {
			return jErr
		}
		if attempt.Outcome.Valid {
			return nil
		}
		jobStatus := gen.DeckJobStatus(jobRow.Status)
		if jobStatus != gen.DeckJobStatusDISPATCHED && jobStatus != gen.DeckJobStatusRUNNING {
			return logReconcileConflict(ctx, q, attempt, jobRow, kind, now)
		}

		var (
			resultJSON sql.NullString
			errN       sql.NullString
		)
		if len(report.Result) > 0 {
			resultJSON = sql.NullString{String: string(report.Result), Valid: true}
		}
		if report.Error != nil && *report.Error != "" {
			errN = sql.NullString{String: *report.Error, Valid: true}
		}
		// rows=0: stale attempt or job already terminal — log conflict, no apply.
		rows, uErr := state.ApplyByAttempt(ctx, state.ApplyByAttemptParams{
			Q: q,
			AllowedFrom: []gen.DeckJobStatus{
				gen.DeckJobStatusDISPATCHED,
				gen.DeckJobStatusRUNNING,
			},
			To:                finalStatus,
			Trigger:           state.TriggerReconciler,
			RunID:             jobRow.RunID,
			JobID:             jobRow.ID,
			AttemptID:         attemptID,
			NewCurrentAttempt: jobRow.CurrentAttemptID,
			NewError:          errN,
		})
		if uErr != nil {
			return uErr
		}
		if rows == 0 {
			return logReconcileConflict(ctx, q, attempt, jobRow, kind, now)
		}

		if _, sErr := q.SetAttemptOutcomeIfUnset(ctx, storegen.SetAttemptOutcomeIfUnsetParams{
			Outcome:       sql.NullString{String: string(kind), Valid: true},
			OutcomeAt:     sql.NullInt64{Int64: now.UnixMilli(), Valid: true},
			OutcomeSource: sql.NullString{String: string(gen.RECONCILE), Valid: true},
			Result:        resultJSON,
			Error:         errN,
			OperatorNote:  sql.NullString{},
			AttemptID:     attemptID,
		}); sErr != nil {
			return sErr
		}

		scope := eventlog.Scope{
			RunID: jobRow.RunID, JobID: jobRow.ID, DeckID: jobRow.DeckID, AttemptID: attemptID,
		}
		switch kind {
		case gen.ExecutorEventKindRUNNING, gen.ExecutorEventKindSTEPCOMPLETED:
			return nil
		case gen.ExecutorEventKindCOMPLETED:
			payload := eventlog.JobCompletedPayload{
				From:          jobStatus,
				OutcomeSource: gen.RECONCILE,
				Result:        report.Result,
			}
			if _, eErr := eventlog.Append(ctx, q, eventlog.KindJobCompleted, scope, now, payload); eErr != nil {
				return eErr
			}
			if pErr := dispatch.PromoteDownstreamReady(ctx, q, jobRow.RunID, now); pErr != nil {
				return pErr
			}
			dispatched, dErr := dispatch.ReadyForRun(ctx, q, jobRow.RunID, now)
			if dErr != nil {
				return dErr
			}
			notifyDecks = append(notifyDecks, dispatched...)
		case gen.ExecutorEventKindFAILED:
			payload := eventlog.JobFailedPayload{From: jobStatus, OutcomeSource: gen.RECONCILE}
			if report.Error != nil {
				payload.Error = *report.Error
			}
			if _, eErr := eventlog.Append(ctx, q, eventlog.KindJobFailed, scope, now, payload); eErr != nil {
				return eErr
			}
		}
		freed, dErr := dispatch.ReadyForDeck(ctx, q, jobRow.DeckID, now)
		if dErr != nil {
			return dErr
		}
		notifyDecks = append(notifyDecks, freed...)
		_, _, mErr := state.MaterializeRunStatus(ctx, q, jobRow.RunID, now)
		return mErr
	})
	if err != nil {
		return OutcomeUnreachable, err
	}
	dispatch.NotifyDecks(notifyDecks)
	r.Logger.Info("reconciler: applied terminal via reconcile",
		"deck_id", deckID, "attempt_id", attemptID, "status", finalStatus)
	return OutcomeApplied, nil
}

func (r *Reconciler) MarkAmbiguousDeadline(ctx context.Context, attemptID string, reason eventlog.AmbiguousReason) error {
	now := time.Now().UTC()
	return r.Store.WithTx(ctx, func(q *storegen.Queries) error {
		return r.markAmbiguous(ctx, q, attemptID, reason, now)
	})
}

func (r *Reconciler) markAmbiguous(ctx context.Context, q *storegen.Queries, attemptID string, reason eventlog.AmbiguousReason, now time.Time) error {
	attempt, err := q.GetJobAttempt(ctx, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	jobRow, err := q.GetDeckJob(ctx, storegen.GetDeckJobParams{RunID: attempt.RunID, ID: attempt.JobID})
	if err != nil {
		return err
	}
	// DEADLINE_EXCEEDED → liveness monitor trigger; other ambiguous reasons → reconciler.
	trigger := state.TriggerReconciler
	if reason == eventlog.AmbiguousReasonDeadlineExceeded {
		trigger = state.TriggerLivenessMonitor
	}
	rows, err := state.ApplyByAttempt(ctx, state.ApplyByAttemptParams{
		Q: q,
		AllowedFrom: []gen.DeckJobStatus{
			gen.DeckJobStatusDISPATCHED,
			gen.DeckJobStatusRUNNING,
		},
		To:                 gen.DeckJobStatusAMBIGUOUS,
		Trigger:            trigger,
		RunID:              jobRow.RunID,
		JobID:              jobRow.ID,
		AttemptID:          attemptID,
		NewCurrentAttempt:  jobRow.CurrentAttemptID,
		NewError:           jobRow.Error,
		NewAmbiguousReason: sql.NullString{String: string(reason), Valid: true},
	})
	if err != nil || rows == 0 {
		return err
	}
	scope := eventlog.Scope{
		RunID: jobRow.RunID, JobID: jobRow.ID, DeckID: jobRow.DeckID, AttemptID: attemptID,
	}
	if _, eErr := eventlog.Append(ctx, q, eventlog.KindJobAmbiguous, scope, now,
		eventlog.JobAmbiguousPayload{From: gen.DeckJobStatus(jobRow.Status), Reason: reason}); eErr != nil {
		return eErr
	}
	_, _, mErr := state.MaterializeRunStatus(ctx, q, jobRow.RunID, now)
	return mErr
}

func logReconcileConflict(ctx context.Context, q *storegen.Queries, attempt storegen.JobAttempts, jobRow storegen.DeckJobs, kind gen.ExecutorEventKind, now time.Time) error {
	payload := eventlog.ExecutorConflictLoggedPayload{
		ExecutorReported:        kind,
		ExecutorEventReceivedAt: now,
	}
	if attempt.Outcome.Valid {
		o := gen.AttemptOutcome(attempt.Outcome.String)
		payload.RecordedOutcome = &o
	}
	if attempt.OutcomeSource.Valid {
		s := gen.OutcomeSource(attempt.OutcomeSource.String)
		payload.RecordedSource = &s
	}
	scope := eventlog.Scope{
		RunID: jobRow.RunID, JobID: jobRow.ID, DeckID: jobRow.DeckID, AttemptID: attempt.AttemptID,
	}
	_, err := eventlog.Append(ctx, q, eventlog.KindExecutorConflictLogged, scope, now, payload)
	return err
}
