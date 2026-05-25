package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"deck-fleet/backend/internal/executor/chaos"
	"deck-fleet/backend/internal/executor/client"
	"deck-fleet/backend/internal/executor/localstore"
)

// Config holds runtime tuning. Chaos flags live on *chaos.State, read each iteration.
type Config struct {
	DeckID            string        `yaml:"deck_id"`
	EndpointURL       string        `yaml:"endpoint_url"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	PollInterval      time.Duration `yaml:"poll_interval"`
	StepDuration      time.Duration `yaml:"step_duration"`
	// PollTimeout caps each /executor/poll request. Must exceed the
	// orchestrator's poll_hold_max or the executor disconnects mid-hold
	// and cycles the connection unnecessarily (defeats long-poll). The
	// orchestrator publishes the hold via its own config; the executor
	// sets this generously since over-budget here just costs a longer
	// idle wait on a genuinely silent orchestrator. Defaults to 30s in
	// production, which is poll_hold_max (25s demo) + 5s slack.
	PollTimeout time.Duration `yaml:"poll_timeout"`
}

type Worker struct {
	cfg       Config
	store     *localstore.Store
	client    *client.Client
	chaos     *chaos.State
	logger    *slog.Logger
	crashFn   func()
	fatalExit func(int)
}

type Deps struct {
	Cfg    Config
	Store  *localstore.Store
	Client *client.Client
	Chaos  *chaos.State
	Logger *slog.Logger

	// CrashFn is invoked when the worker's chaos-driven
	// crash-after-step arm fires. Production passes os.Exit(1); the
	// integration harness passes a Restart hook so a chaos crash in a
	// test doesn't terminate the test process.
	CrashFn func()

	// FatalExit is invoked on permanent fleet-membership errors from
	// the orchestrator (heartbeat 404 / 410). Production passes
	// os.Exit(78) (EX_CONFIG); tests pass a closure that records the
	// call. nil falls back to a panic so misconfigured tests fail
	// loudly rather than silently retrying.
	FatalExit func(int)
}

func New(d Deps) *Worker {
	if d.Store == nil {
		panic("worker.New: Store is required")
	}
	if d.Client == nil {
		panic("worker.New: Client is required")
	}
	if d.Logger == nil {
		panic("worker.New: Logger is required")
	}
	return &Worker{
		cfg:       d.Cfg,
		store:     d.Store,
		client:    d.Client,
		chaos:     d.Chaos,
		logger:    d.Logger,
		crashFn:   d.CrashFn,
		fatalExit: d.FatalExit,
	}
}

func (w *Worker) chaosHang() bool {
	if w.chaos == nil {
		return false
	}
	return w.chaos.Hang()
}

// waitForHangChange blocks until either ctx is cancelled or chaos
// state changes in a way that might release the hang (Reset,
// SetHang, or an Apply that touches Hang). Loops if hang is still
// asserted after a wakeup (e.g. operator immediately re-armed it).
// Returns true if the worker should continue the step loop, false
// if ctx is done. Nil-safe on w.chaos.
func (w *Worker) waitForHangChange(ctx context.Context) bool {
	if w.chaos == nil {
		return true
	}
	for {
		// Grab the current broadcast channel before re-checking the flag.
		// If we read the flag first and the operator's Reset closes the
		// channel between that read and the select, we'd block on a
		// channel that's already been replaced and miss the wakeup.
		ch := w.chaos.HangChangedCh()
		if !w.chaos.Hang() {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ch:
		}
	}
}

func (w *Worker) chaosSilent() bool {
	if w.chaos == nil {
		return false
	}
	return w.chaos.Silent()
}

func (w *Worker) chaosHangAfterStep() int {
	if w.chaos == nil {
		return 0
	}
	return w.chaos.HangAfterStep()
}

func (w *Worker) chaosCrashAfterStep() int {
	if w.chaos == nil {
		return 0
	}
	return w.chaos.CrashAfterStep()
}

func (w *Worker) Run(ctx context.Context) {
	heartbeatCh := w.heartbeatLoop(ctx)
	for {
		if err := ctx.Err(); err != nil {
			<-heartbeatCh
			return
		}
		w.iterate(ctx)
	}
}

func (w *Worker) heartbeatLoop(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		if w.cfg.HeartbeatInterval <= 0 {
			return
		}
		t := time.NewTicker(w.cfg.HeartbeatInterval)
		defer t.Stop()
		w.sendHeartbeat(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				w.sendHeartbeat(ctx)
			}
		}
	}()
	return done
}

func (w *Worker) sendHeartbeat(ctx context.Context) {
	if w.chaosSilent() {
		return
	}
	cur, ok, err := w.store.CurrentInFlight(ctx)
	if err != nil {
		w.logger.Warn("worker: read current attempt", "err", err)
	}
	var attemptID string
	if ok {
		attemptID = cur.AttemptID
	}
	hbCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := w.client.Heartbeat(hbCtx, w.cfg.DeckID, w.cfg.EndpointURL, attemptID); err != nil {
		if errors.Is(err, client.ErrFleetRejected) {
			// Permanent fleet-membership error; exit 78 (EX_CONFIG)
			// so the supervisor stops respawning.
			w.logger.Error("worker: orchestrator rejected heartbeat; exiting to avoid respawn loop",
				"deck_id", w.cfg.DeckID, "err", err)
			if w.fatalExit != nil {
				w.fatalExit(78)
			} else {
				panic(fmt.Sprintf("worker: fleet rejection for %s but no fatalExit wired: %v",
					w.cfg.DeckID, err))
			}
			return
		}
		w.logger.Debug("worker: heartbeat failed", "err", err)
	}
}

func (w *Worker) iterate(ctx context.Context) {
	cur, hasInFlight, err := w.store.CurrentInFlight(ctx)
	if err != nil {
		w.logger.Error("worker: load in-flight", "err", err)
		w.sleep(ctx, w.cfg.PollInterval)
		return
	}
	if hasInFlight {
		w.runAttempt(ctx, cur)
		return
	}

	pollTimeout := w.cfg.PollTimeout
	if pollTimeout <= 0 {
		pollTimeout = 30 * time.Second
	}
	pollCtx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()
	dispatch, ok, err := w.client.Poll(pollCtx, w.cfg.DeckID)
	if err != nil {
		w.logger.Debug("worker: poll failed", "err", err)
		w.sleep(ctx, w.cfg.PollInterval)
		return
	}
	if !ok {
		w.sleep(ctx, w.cfg.PollInterval)
		return
	}

	// Commit RECEIVED to local SQLite before doing anything else. INSERT
	// OR IGNORE makes re-delivery of the same attempt_id a safe no-op,
	// which is the load-bearing dedup guarantee from STATE_MACHINE.md §6.
	stepsJSON, err := json.Marshal(dispatch.Steps)
	if err != nil {
		w.logger.Error("worker: marshal steps", "err", err)
		return
	}
	now := time.Now().UTC()
	a := localstore.Attempt{
		AttemptID:  dispatch.AttemptID,
		RunID:      dispatch.RunID,
		JobID:      dispatch.JobID,
		DeckID:     w.cfg.DeckID,
		StepsJSON:  string(stepsJSON),
		ReceivedAt: now,
	}
	inserted, err := w.store.InsertReceived(ctx, a)
	if err != nil {
		w.logger.Error("worker: insert received", "err", err)
		w.sleep(ctx, w.cfg.PollInterval)
		return
	}
	if !inserted {
		w.logger.Info("worker: re-delivered attempt; using local copy", "attempt_id", dispatch.AttemptID)
	} else {
		w.logger.Info("worker: received dispatch",
			"attempt_id", dispatch.AttemptID, "run_id", dispatch.RunID, "job_id", dispatch.JobID,
			"steps", len(dispatch.Steps))
	}
	loaded, err := w.store.GetAttempt(ctx, dispatch.AttemptID)
	if err != nil {
		w.logger.Error("worker: load attempt after insert", "err", err)
		return
	}
	w.runAttempt(ctx, loaded)
}

func (w *Worker) runAttempt(ctx context.Context, a localstore.Attempt) {
	if a.State == localstore.StateCompleted || a.State == localstore.StateFailed {
		w.logger.Debug("worker: skip terminal attempt", "attempt_id", a.AttemptID, "state", a.State)
		return
	}

	var steps []client.Step
	if err := json.Unmarshal([]byte(a.StepsJSON), &steps); err != nil {
		w.failAttempt(ctx, a.AttemptID, fmt.Sprintf("malformed steps json: %v", err))
		return
	}

	if a.State == localstore.StateReceived {
		now := time.Now().UTC()
		if err := w.store.MarkInProgressWithEvent(ctx, a.AttemptID, now); err != nil {
			w.logger.Error("worker: mark in_progress", "err", err)
			return
		}
	}

	totalSteps := len(steps)
	// C2: per-step crash-resume cursor. last_completed_step is
	// 1-indexed and monotonically non-decreasing within an attempt.
	// On first run it's 0; on resume it's whatever survived to commit
	// before the crash. We skip every step strictly less than the
	// cursor and pick up at cursor+1 (i.e. loop index == cursor).
	resumeFrom := a.LastCompletedStep
	if resumeFrom > 0 {
		w.logger.Info("worker: resuming attempt with completed steps",
			"attempt_id", a.AttemptID, "skip_steps", resumeFrom, "total", totalSteps)
	}
	for i, step := range steps {
		if i < resumeFrom {
			continue
		}
		if err := ctx.Err(); err != nil {
			return
		}

		if w.chaosHang() {
			w.logger.Warn("worker: chaos.hang engaged; waiting for unstick",
				"completed_steps", i, "total", totalSteps)
			if !w.waitForHangChange(ctx) {
				return
			}
			w.logger.Info("worker: chaos.hang cleared; resuming",
				"completed_steps", i, "total", totalSteps)
		}

		cur, err := w.store.GetAttempt(ctx, a.AttemptID)
		if err != nil {
			w.logger.Error("worker: reload attempt for abort check", "err", err)
			return
		}
		if cur.AbortRequested {
			w.logger.Info("worker: abort observed between steps; failing attempt",
				"attempt_id", a.AttemptID, "completed_steps", i, "total", totalSteps)
			w.failAttempt(ctx, a.AttemptID, fmt.Sprintf("aborted by operator after %d/%d steps", i, totalSteps))
			return
		}

		w.logger.Info("worker: step start",
			"attempt_id", a.AttemptID, "step", i+1, "type", step.Type, "desc", step.Description)
		w.sleep(ctx, w.cfg.StepDuration)
		w.logger.Info("worker: step done", "attempt_id", a.AttemptID, "step", i+1)

		stepNum := i + 1
		now := time.Now().UTC()
		// CrashAfterStep targets this commit point: cursor + outbox
		// event land atomically; resume skips completed steps.
		if cErr := w.store.BumpStepCursorWithEvent(ctx, a.AttemptID, stepNum, totalSteps, now); cErr != nil {
			w.logger.Error("worker: bump step cursor", "attempt_id", a.AttemptID,
				"step", stepNum, "err", cErr)
			// Fall through: re-running the step on resume is safer than
			// failing the whole attempt over a transient SQLite write.
		}
		if armed := w.chaosHangAfterStep(); armed > 0 && stepNum == armed {
			w.logger.Warn("worker: chaos.hang_after_step engaged; parking on broadcast",
				"after_step", stepNum)
			// Promote step-aware arm to persistent hang so GET
			// /executor/chaos reflects the parked state.
			if w.chaos != nil {
				w.chaos.SetHang(true)
			}
			// Pre-fix: blocked on <-ctx.Done() forever; hang_after_step
			// required a process restart to release.
			if !w.waitForHangChange(ctx) {
				return
			}
			w.logger.Info("worker: chaos.hang cleared; resuming after promoted step",
				"after_step", stepNum)
		}
		if armed := w.chaosCrashAfterStep(); armed > 0 && stepNum == armed {
			w.logger.Warn("worker: chaos.crash_after_step engaged; exiting",
				"after_step", stepNum)
			w.crashFn()
			return
		}
	}

	now := time.Now().UTC()
	resultPayload := map[string]any{
		"steps_completed": totalSteps,
		"deck_id":         a.DeckID,
	}
	resultJSON, err := json.Marshal(resultPayload)
	if err != nil {
		w.logger.Error("worker: marshal result", "err", err)
		return
	}
	eventPayload, err := json.Marshal(map[string]any{"result": resultPayload})
	if err != nil {
		w.logger.Error("worker: marshal event payload", "err", err)
		return
	}
	if err := w.store.MarkTerminalWithEvent(ctx, a.AttemptID, localstore.StateCompleted,
		string(resultJSON), "", eventPayload, now); err != nil {
		w.logger.Error("worker: mark completed", "err", err)
		return
	}
}

func (w *Worker) failAttempt(ctx context.Context, attemptID, msg string) {
	now := time.Now().UTC()
	eventPayload, err := json.Marshal(map[string]any{"error": msg})
	if err != nil {
		w.logger.Error("worker: marshal fail payload", "err", err)
		return
	}
	if err := w.store.MarkTerminalWithEvent(ctx, attemptID, localstore.StateFailed,
		"", msg, eventPayload, now); err != nil {
		w.logger.Error("worker: mark failed", "err", err)
	}
}

func (w *Worker) sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
