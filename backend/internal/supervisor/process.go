package supervisor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// exitCodeFatalConfig (EX_CONFIG): permanent config error — do not respawn.
// Executor uses this for heartbeat 404/410.
const exitCodeFatalConfig = 78

func (s *Supervisor) buildOrchestratorSpec() *ProcessEntry {
	args := []string{"-config", s.cfg.Orchestrator.ConfigPath}
	var env []string
	if s.cfg.Orchestrator.DBPath != "" {
		env = append(env, "DB_PATH="+s.cfg.Orchestrator.DBPath)
	}
	return &ProcessEntry{
		Label:   orchestratorLabel,
		Kind:    KindOrchestrator,
		Port:    s.cfg.Orchestrator.HTTPPort,
		Args:    args,
		Env:     env,
		LogPath: filepath.Join(s.cfg.StateDir, "logs", "orchestrator.log"),
		Policy:  PolicyAlways,
		State:   StateStarting,
	}
}

func (s *Supervisor) buildExecutorSpec(deckID string, port int, freshState bool) (*ProcessEntry, error) {
	advertise := fmt.Sprintf("%s://%s:%d",
		s.cfg.Executor.AdvertiseScheme, s.cfg.Executor.AdvertiseHost, port)
	listenAddr := fmt.Sprintf(":%d", port)
	dbPath := filepath.Join(s.cfg.StateDir, fmt.Sprintf("executor-%s.db", deckID))
	if freshState {
		_ = os.Remove(dbPath)
	}
	configPath, err := s.writeExecutorConfig(deckID, listenAddr, advertise, dbPath)
	if err != nil {
		return nil, err
	}
	return &ProcessEntry{
		Label:   executorLabel(deckID),
		Kind:    KindExecutor,
		DeckID:  deckID,
		Port:    port,
		DBPath:  dbPath,
		Args:    []string{"-config", configPath},
		LogPath: filepath.Join(s.cfg.StateDir, "logs", "executor-"+deckID+".log"),
		Policy:  PolicyAlways,
		State:   StateStarting,
	}, nil
}

func (s *Supervisor) upsertAndWatch(ctx context.Context, entry *ProcessEntry) {
	s.mu.Lock()
	if existing, ok := s.entries[entry.Label]; ok {
		if existing.cancel != nil {
			existing.cancel()
		}
	}
	s.entries[entry.Label] = entry
	if entry.Port > 0 {
		s.usedPort[entry.Port] = entry.Label
	}
	_ = s.saveStateLocked()
	s.mu.Unlock()
	s.attachWatcher(ctx, entry)
}

func (s *Supervisor) attachWatcher(ctx context.Context, entry *ProcessEntry) {
	entry.mu.Lock()
	entryCtx, cancel := context.WithCancel(ctx)
	entry.cancel = cancel
	entry.stopped = make(chan struct{})
	stopped := entry.stopped
	entry.mu.Unlock()
	s.watchers.Add(1)
	go func() {
		defer s.watchers.Done()
		defer close(stopped)
		s.respawnLoop(entryCtx, entry)
	}()
}

// reapStaleChild SIGTERMs a persisted PID from a prior supervisor incarnation.
// Without this, restart after kill -9 can leave two executors polling the same deck_id.
func (s *Supervisor) reapStaleChild(entry *ProcessEntry) {
	entry.mu.Lock()
	pid := entry.PID
	label := entry.Label
	entry.PID = 0
	entry.mu.Unlock()
	if pid <= 0 {
		return
	}
	// signal 0 probe; ESRCH → already gone
	if err := syscall.Kill(pid, 0); err != nil {
		s.logger.Info("supervisor: persisted PID already gone", "label", label, "pid", pid, "err", err)
		return
	}
	s.logger.Warn("supervisor: reaping stale child from previous incarnation", "label", label, "pid", pid)
	// SIGTERM process group (Setpgid in spawnOnce); fall back to PID.
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		pgid = pid
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			s.logger.Info("supervisor: stale child reaped via SIGTERM", "label", label, "pid", pid)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	s.logger.Warn("supervisor: stale child SIGKILLed after grace", "label", label, "pid", pid)
}

// respawnLoop spawns, waits, and relaunches per Policy. Exit 78 → FatalConfig, no respawn.
func (s *Supervisor) respawnLoop(ctx context.Context, entry *ProcessEntry) {
	// Reap orphaned PIDs from persisted state on first iteration.
	s.reapStaleChild(entry)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		entry.mu.Lock()
		policy := entry.Policy
		state := entry.State
		entry.mu.Unlock()
		if policy == PolicyNever || state == StateStopped || state == StateFatalConfig {
			// Poll for policy/state changes; handlers have no direct signal channel.
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
				continue
			}
		}

		exitCode, exitReason, ranOK := s.spawnOnce(ctx, entry)

		entry.mu.Lock()
		entry.LastExitCode = &exitCode
		entry.LastExitReason = exitReason
		entry.PID = 0
		nextState := StateCrashing
		nextPolicy := entry.Policy
		switch {
		case exitCode == exitCodeFatalConfig:
			nextState = StateFatalConfig
			nextPolicy = PolicyNever
		case entry.Policy == PolicyNever:
			nextState = StateStopped
		}
		entry.State = nextState
		entry.Policy = nextPolicy
		entry.mu.Unlock()

		s.mu.Lock()
		_ = s.saveStateLocked()
		s.mu.Unlock()

		if !ranOK {
			// Spawn failure (missing binary, etc.) — back off to avoid busy-loop.
			select {
			case <-ctx.Done():
				return
			case <-time.After(s.cfg.RespawnDelay):
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.cfg.RespawnDelay):
		}
	}
}

func (s *Supervisor) spawnOnce(ctx context.Context, entry *ProcessEntry) (exitCode int, exitReason string, ranOK bool) {
	entry.mu.Lock()
	args := append([]string(nil), entry.Args...)
	env := append([]string(nil), entry.Env...)
	logPath := entry.LogPath
	binary := s.binaryFor(entry)
	entry.mu.Unlock()

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		s.logger.Error("supervisor: open log", "label", entry.Label, "err", err)
		return -1, "open log: " + err.Error(), false
	}
	defer func() { _ = logFile.Close() }()
	_, _ = fmt.Fprintf(logFile, "\n[%s] [%s] starting %s %v\n", time.Now().Format(time.RFC3339), entry.Label, binary, args)

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	// Setpgid so shutdown/reap can SIGTERM the whole subtree.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if startErr := cmd.Start(); startErr != nil {
		s.logger.Error("supervisor: start", "label", entry.Label, "err", startErr)
		return -1, "start: " + startErr.Error(), false
	}
	now := time.Now().UTC()
	entry.mu.Lock()
	entry.State = StateRunning
	entry.PID = cmd.Process.Pid
	entry.StartedAt = &now
	entry.LastExitCode = nil
	entry.LastExitReason = ""
	entry.mu.Unlock()
	s.mu.Lock()
	_ = s.saveStateLocked()
	s.mu.Unlock()
	s.logger.Info("supervisor: spawned", "label", entry.Label, "pid", cmd.Process.Pid)

	waitErr := cmd.Wait()
	code := 0
	reason := "exited cleanly"
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			code = exitErr.ExitCode()
			reason = "exit code " + strconv.Itoa(code)
		} else {
			code = -1
			reason = waitErr.Error()
		}
	}
	_, _ = fmt.Fprintf(logFile, "[%s] [%s] exited: %s\n", time.Now().Format(time.RFC3339), entry.Label, reason)
	s.logger.Info("supervisor: child exited", "label", entry.Label, "code", code, "reason", reason)
	return code, reason, true
}

func (s *Supervisor) binaryFor(e *ProcessEntry) string {
	if e.Kind == KindOrchestrator {
		return s.cfg.Orchestrator.BinaryPath
	}
	return s.cfg.Executor.BinaryPath
}

// attachExecutor adds an executor entry and starts its watcher.
// Running/Starting → ErrAlreadyAttached; Stopped/FatalConfig → resume in place.
func (s *Supervisor) attachExecutor(ctx context.Context, deckID string, freshState bool) (*ProcessEntrySnapshot, error) {
	if deckID == "" {
		return nil, errors.New("deck_id is required")
	}
	label := executorLabel(deckID)
	s.mu.Lock()
	if existing, ok := s.entries[label]; ok {
		existing.mu.Lock()
		state := existing.State
		switch state {
		case StateRunning, StateStarting:
			existing.mu.Unlock()
			snap := existing.snapshot()
			s.mu.Unlock()
			return &snap, ErrAlreadyAttached
		case StateStopped, StateFatalConfig, StateCrashing:
			existing.Policy = PolicyAlways
			existing.State = StateStarting
			existing.LastExitCode = nil
			existing.LastExitReason = ""
			existing.mu.Unlock()
			snap := existing.snapshot()
			_ = s.saveStateLocked()
			s.mu.Unlock()
			return &snap, nil
		default:
			existing.mu.Unlock()
		}
	}
	port, err := s.allocatePortLocked(label)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	entry, err := s.buildExecutorSpec(deckID, port, freshState)
	if err != nil {
		s.releasePortLocked(port)
		s.mu.Unlock()
		return nil, err
	}
	s.entries[label] = entry
	_ = s.saveStateLocked()
	s.mu.Unlock()
	s.attachWatcher(ctx, entry)
	snap := entry.snapshot()
	return &snap, nil
}

var ErrAlreadyAttached = errors.New("supervisor: executor already attached")

var ErrNotFound = errors.New("supervisor: executor not found in supervisor table")

func (s *Supervisor) stopExecutor(deckID string) error {
	return s.stopLabel(executorLabel(deckID))
}

func (s *Supervisor) startExecutor(deckID string) error {
	return s.startLabel(executorLabel(deckID))
}

func (s *Supervisor) restartExecutor(deckID string) error {
	return s.restartLabel(executorLabel(deckID))
}

// detachExecutor stops, removes the entry, and best-effort POSTs
// /api/decks/{deck_id}/release so the slot does not age UNREACHABLE.
func (s *Supervisor) detachExecutor(deckID string) error {
	label := executorLabel(deckID)
	if err := s.stopLabel(label); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	s.mu.Lock()
	entry, ok := s.entries[label]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	if entry.cancel != nil {
		entry.cancel()
	}
	if entry.Port > 0 {
		s.releasePortLocked(entry.Port)
	}
	delete(s.entries, label)
	_ = s.saveStateLocked()
	s.mu.Unlock()

	_ = os.Remove(s.executorConfigPath(deckID))

	s.releaseOrchestratorSlot(deckID)
	return nil
}

func (s *Supervisor) releaseOrchestratorSlot(deckID string) {
	orchURL := s.cfg.Executor.OrchestratorURL
	if orchURL == "" {
		return
	}
	url := fmt.Sprintf("%s/api/decks/%s/release", orchURL, deckID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		s.logger.Warn("supervisor: build release request", "deck_id", deckID, "err", err)
		return
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		s.logger.Warn("supervisor: release call failed", "deck_id", deckID, "err", err)
		return
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		s.logger.Warn("supervisor: release non-2xx", "deck_id", deckID, "status", resp.StatusCode)
		return
	}
	s.logger.Info("supervisor: orchestrator slot released", "deck_id", deckID)
}

func (s *Supervisor) stopLabel(label string) error {
	entry, ok := s.getEntry(label)
	if !ok {
		return ErrNotFound
	}
	entry.mu.Lock()
	entry.Policy = PolicyNever
	pid := entry.PID
	entry.mu.Unlock()
	if pid > 0 {
		s.logger.Info("supervisor: signalling SIGTERM", "label", label, "pid", pid)
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			s.logger.Warn("supervisor: SIGTERM failed", "label", label, "pid", pid, "err", err)
		}
	}
	s.mu.Lock()
	_ = s.saveStateLocked()
	s.mu.Unlock()
	return nil
}

func (s *Supervisor) startLabel(label string) error {
	entry, ok := s.getEntry(label)
	if !ok {
		return ErrNotFound
	}
	entry.mu.Lock()
	entry.Policy = PolicyAlways
	if entry.State == StateStopped || entry.State == StateFatalConfig {
		entry.State = StateStarting
	}
	entry.LastExitCode = nil
	entry.LastExitReason = ""
	entry.mu.Unlock()
	s.mu.Lock()
	_ = s.saveStateLocked()
	s.mu.Unlock()
	return nil
}

// killLabel SIGKILLs the process group of the named child. Unlike
// stopLabel, it leaves Policy as PolicyAlways so the respawn loop brings
// the child back; mirrors `kill -9` followed by a systemd restart.
func (s *Supervisor) killLabel(label string) error {
	entry, ok := s.getEntry(label)
	if !ok {
		return ErrNotFound
	}
	entry.mu.Lock()
	pid := entry.PID
	entry.mu.Unlock()
	if pid <= 0 {
		return nil
	}
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		pgid = pid
	}
	s.logger.Info("supervisor: SIGKILL", "label", label, "pid", pid, "pgid", pgid)
	if kErr := syscall.Kill(-pgid, syscall.SIGKILL); kErr != nil && !errors.Is(kErr, syscall.ESRCH) {
		return fmt.Errorf("SIGKILL: %w", kErr)
	}
	return nil
}

func (s *Supervisor) restartLabel(label string) error {
	entry, ok := s.getEntry(label)
	if !ok {
		return ErrNotFound
	}
	entry.mu.Lock()
	entry.Policy = PolicyAlways
	pid := entry.PID
	entry.mu.Unlock()
	if pid > 0 {
		s.logger.Info("supervisor: signalling SIGTERM for restart", "label", label, "pid", pid)
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			s.logger.Warn("supervisor: SIGTERM failed", "label", label, "pid", pid, "err", err)
		}
	}
	return nil
}

// orchestratorGracefulRestart POSTs /api/admin/restart; SIGTERM fallback on failure.
func (s *Supervisor) orchestratorGracefulRestart(ctx context.Context) error {
	entry, ok := s.getEntry(orchestratorLabel)
	if !ok {
		return ErrNotFound
	}
	entry.mu.Lock()
	entry.Policy = PolicyAlways
	pid := entry.PID
	entry.mu.Unlock()

	url := fmt.Sprintf("%s:%d/api/admin/restart",
		s.cfg.Orchestrator.HTTPSchemeHost, s.cfg.Orchestrator.HTTPPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(nil))
	if err == nil {
		resp, dErr := s.httpClient.Do(req)
		if dErr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				s.logger.Info("supervisor: orchestrator graceful restart accepted", "status", resp.StatusCode)
				return nil
			}
			s.logger.Warn("supervisor: orchestrator graceful restart returned non-2xx; falling back to SIGTERM",
				"status", resp.StatusCode)
		} else {
			s.logger.Warn("supervisor: orchestrator graceful restart HTTP failed; falling back to SIGTERM",
				"err", dErr)
		}
	}
	if pid > 0 {
		s.logger.Info("supervisor: SIGTERM fallback for orchestrator restart", "pid", pid)
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("SIGTERM: %w", err)
		}
	}
	return nil
}
