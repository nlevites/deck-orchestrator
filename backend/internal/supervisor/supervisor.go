package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"
)

type ProcessKind string

const (
	KindOrchestrator ProcessKind = "orchestrator"
	KindExecutor     ProcessKind = "executor"
)

// ProcessState is operator-visible state for /supervisor/processes.
// FatalConfig = exit 78 (EX_CONFIG); do not respawn.
type ProcessState string

const (
	StateStarting    ProcessState = "Starting"
	StateRunning     ProcessState = "Running"
	StateStopped     ProcessState = "Stopped"
	StateCrashing    ProcessState = "Crashing"
	StateFatalConfig ProcessState = "FatalConfig"
)

type Policy string

const (
	PolicyAlways Policy = "always"
	PolicyNever  Policy = "never"
)

// ProcessEntry is one supervised child. snapshot() returns ProcessEntrySnapshot
// (no mutex — copylocks).
type ProcessEntry struct {
	Label   string
	Kind    ProcessKind
	DeckID  string
	Port    int
	DBPath  string
	Args    []string
	Env     []string
	LogPath string

	mu             sync.Mutex
	Policy         Policy
	State          ProcessState
	PID            int
	StartedAt      *time.Time
	LastExitCode   *int
	LastExitReason string
	cancel         context.CancelFunc
	stopped        chan struct{}
}

// ProcessEntrySnapshot is the JSON-safe projection for HTTP and disk persistence.
type ProcessEntrySnapshot struct {
	Label          string       `json:"label"`
	Kind           ProcessKind  `json:"kind"`
	DeckID         string       `json:"deck_id,omitempty"`
	Port           int          `json:"port,omitempty"`
	DBPath         string       `json:"db_path,omitempty"`
	Args           []string     `json:"args,omitempty"`
	Env            []string     `json:"env,omitempty"`
	LogPath        string       `json:"log_path,omitempty"`
	Policy         Policy       `json:"policy"`
	State          ProcessState `json:"state"`
	PID            int          `json:"pid,omitempty"`
	StartedAt      *time.Time   `json:"started_at,omitempty"`
	LastExitCode   *int         `json:"last_exit_code,omitempty"`
	LastExitReason string       `json:"last_exit_reason,omitempty"`
}

func (p *ProcessEntry) snapshot() ProcessEntrySnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	snap := ProcessEntrySnapshot{
		Label:          p.Label,
		Kind:           p.Kind,
		DeckID:         p.DeckID,
		Port:           p.Port,
		DBPath:         p.DBPath,
		Args:           append([]string(nil), p.Args...),
		Env:            append([]string(nil), p.Env...),
		LogPath:        p.LogPath,
		Policy:         p.Policy,
		State:          p.State,
		PID:            p.PID,
		LastExitReason: p.LastExitReason,
	}
	if p.StartedAt != nil {
		t := *p.StartedAt
		snap.StartedAt = &t
	}
	if p.LastExitCode != nil {
		c := *p.LastExitCode
		snap.LastExitCode = &c
	}
	return snap
}

func entryFromSnapshot(s ProcessEntrySnapshot) *ProcessEntry {
	return &ProcessEntry{
		Label:          s.Label,
		Kind:           s.Kind,
		DeckID:         s.DeckID,
		Port:           s.Port,
		DBPath:         s.DBPath,
		Args:           append([]string(nil), s.Args...),
		Env:            append([]string(nil), s.Env...),
		LogPath:        s.LogPath,
		Policy:         s.Policy,
		State:          s.State,
		PID:            s.PID,
		StartedAt:      s.StartedAt,
		LastExitCode:   s.LastExitCode,
		LastExitReason: s.LastExitReason,
	}
}

type Supervisor struct {
	cfg    Config
	logger *slog.Logger

	mu       sync.Mutex
	entries  map[string]*ProcessEntry
	usedPort map[int]string

	// HTTP attach uses runCtx so watcher goroutines outlive the request.
	runCtxMu sync.Mutex
	runCtx   context.Context

	httpClient *http.Client

	// Wait for respawn goroutines on shutdown so children are not orphaned.
	watchers sync.WaitGroup
}

func New(cfg Config, logger *slog.Logger) (*Supervisor, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("supervisor: invalid config: %w", err)
	}
	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		return nil, fmt.Errorf("supervisor: mkdir state_dir: %w", err)
	}
	logDir := filepath.Join(cfg.StateDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("supervisor: mkdir logs: %w", err)
	}
	s := &Supervisor{
		cfg:        cfg,
		logger:     logger,
		entries:    make(map[string]*ProcessEntry),
		usedPort:   make(map[int]string),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
	if err := s.loadState(); err != nil {
		return nil, fmt.Errorf("supervisor: load state: %w", err)
	}
	return s, nil
}

func (s *Supervisor) statePath() string {
	return filepath.Join(s.cfg.StateDir, "supervisor.json")
}

// loadState reads supervisor.json; missing file is first boot, corrupt file is fatal.
func (s *Supervisor) loadState() error {
	p := s.statePath()
	raw, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		s.logger.Info("supervisor: no persisted state; starting empty", "path", p)
		return nil
	}
	if err != nil {
		return err
	}
	var doc struct {
		Entries []ProcessEntrySnapshot `json:"entries"`
	}
	if uErr := json.Unmarshal(raw, &doc); uErr != nil {
		return fmt.Errorf("decode supervisor.json: %w", uErr)
	}
	for _, snap := range doc.Entries {
		if snap.Label == "" {
			continue
		}
		entry := entryFromSnapshot(snap)
		s.entries[entry.Label] = entry
		if entry.Port > 0 {
			s.usedPort[entry.Port] = entry.Label
		}
	}
	s.logger.Info("supervisor: loaded persisted state", "entries", len(doc.Entries))
	return nil
}

// saveStateLocked writes via temp+rename. Caller must hold s.mu.
func (s *Supervisor) saveStateLocked() error {
	entries := make([]ProcessEntrySnapshot, 0, len(s.entries))
	for _, e := range s.entries {
		entries = append(entries, e.snapshot())
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Label < entries[j].Label })
	body, err := json.MarshalIndent(struct {
		Entries []ProcessEntrySnapshot `json:"entries"`
	}{Entries: entries}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.statePath() + ".tmp"
	if wErr := os.WriteFile(tmp, body, 0o644); wErr != nil {
		return wErr
	}
	return os.Rename(tmp, s.statePath())
}

func (s *Supervisor) allocatePortLocked(label string) (int, error) {
	for offset := 1; offset < 10_000; offset++ {
		candidate := s.cfg.Executor.PortBase + offset
		if _, taken := s.usedPort[candidate]; taken {
			continue
		}
		s.usedPort[candidate] = label
		return candidate, nil
	}
	return 0, errors.New("supervisor: no free port in executor pool")
}

func (s *Supervisor) releasePortLocked(port int) {
	delete(s.usedPort, port)
}

func (s *Supervisor) listEntries() []ProcessEntrySnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ProcessEntrySnapshot, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e.snapshot())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == KindOrchestrator
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// Run starts HTTP and respawn loops for persisted/prelaunched entries; blocks until ctx done.
func (s *Supervisor) Run(ctx context.Context) error {
	s.runCtxMu.Lock()
	s.runCtx = ctx
	s.runCtxMu.Unlock()

	for _, e := range s.snapshotEntries() {
		s.attachWatcher(ctx, e)
	}

	if _, ok := s.getEntry(orchestratorLabel); !ok {
		spec := s.buildOrchestratorSpec()
		s.upsertAndWatch(ctx, spec)
	}

	for _, deckID := range s.cfg.PrelaunchExecutors {
		label := executorLabel(deckID)
		if _, exists := s.getEntry(label); exists {
			continue
		}
		if _, err := s.attachExecutor(ctx, deckID, false); err != nil {
			s.logger.Warn("supervisor: prelaunch failed", "deck_id", deckID, "err", err)
		}
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)
	srv := &http.Server{
		Addr:              s.cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("supervisor: HTTP listening", "addr", s.cfg.ListenAddr)
		if lErr := srv.ListenAndServe(); !errors.Is(lErr, http.ErrServerClosed) {
			errCh <- lErr
		}
		close(errCh)
	}()

	<-ctx.Done()
	s.logger.Info("supervisor: shutting down")

	// SIGTERM each process group before srv.Shutdown; children use Setpgid.
	// Without this, CommandContext cancel can race Run's return and orphan children.
	for _, e := range s.snapshotEntries() {
		e.mu.Lock()
		pid := e.PID
		e.mu.Unlock()
		if pid > 0 {
			_ = syscall.Kill(-pid, syscall.SIGTERM)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		s.logger.Error("supervisor: http shutdown", "err", err)
	}

	// Bounded wait; CommandContext SIGKILL should unblock Wait in practice.
	done := make(chan struct{})
	go func() {
		s.watchers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		s.logger.Warn("supervisor: watchers did not exit within grace; some children may be orphaned")
	}

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func (s *Supervisor) snapshotEntries() []*ProcessEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*ProcessEntry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e)
	}
	return out
}

func (s *Supervisor) getEntry(label string) (*ProcessEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[label]
	return e, ok
}

const orchestratorLabel = "orchestrator"

func executorLabel(deckID string) string {
	return "executor:" + deckID
}

// backgroundCtx is runCtx for HTTP-driven spawns; Background() before Run (tests).
func (s *Supervisor) backgroundCtx() context.Context {
	s.runCtxMu.Lock()
	defer s.runCtxMu.Unlock()
	if s.runCtx == nil {
		return context.Background()
	}
	return s.runCtx
}
