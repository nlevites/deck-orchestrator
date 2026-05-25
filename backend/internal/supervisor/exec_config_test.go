package supervisor

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	execconfig "deck-fleet/backend/internal/executor/config"
)

func newTestSupervisor(t *testing.T, exec ExecutorSpec) *Supervisor {
	t.Helper()
	return &Supervisor{
		cfg: Config{
			StateDir: t.TempDir(),
			Executor: exec,
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestWriteExecutorConfig_RoundTrips(t *testing.T) {
	s := newTestSupervisor(t, ExecutorSpec{
		OrchestratorURL:   "http://localhost:8080",
		StepDuration:      3 * time.Second,
		HeartbeatInterval: 2 * time.Second,
		PollInterval:      750 * time.Millisecond,
	})

	path, err := s.writeExecutorConfig("deck-7", ":9007", "http://localhost:9007",
		filepath.Join(s.cfg.StateDir, "executor-deck-7.db"))
	if err != nil {
		t.Fatalf("writeExecutorConfig: %v", err)
	}
	if got, want := path, s.executorConfigPath("deck-7"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config not on disk: %v", err)
	}

	cfg, err := execconfig.Load(path)
	if err != nil {
		t.Fatalf("execconfig.Load: %v", err)
	}
	cfg.NormalizeURLs()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("loaded config invalid: %v", err)
	}

	if cfg.DeckID != "deck-7" {
		t.Errorf("DeckID = %q, want deck-7", cfg.DeckID)
	}
	if cfg.OrchestratorURL != "http://localhost:8080" {
		t.Errorf("OrchestratorURL = %q", cfg.OrchestratorURL)
	}
	if cfg.ListenAddr != ":9007" {
		t.Errorf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.AdvertiseURL != "http://localhost:9007" {
		t.Errorf("AdvertiseURL = %q", cfg.AdvertiseURL)
	}
	if !strings.HasSuffix(cfg.DBPath, "executor-deck-7.db") {
		t.Errorf("DBPath = %q", cfg.DBPath)
	}
	if cfg.Worker.StepDuration != 3*time.Second {
		t.Errorf("StepDuration = %s", cfg.Worker.StepDuration)
	}
	if cfg.Worker.HeartbeatInterval != 2*time.Second {
		t.Errorf("HeartbeatInterval = %s", cfg.Worker.HeartbeatInterval)
	}
	if cfg.Worker.PollInterval != 750*time.Millisecond {
		t.Errorf("PollInterval = %s", cfg.Worker.PollInterval)
	}
}

func TestWriteExecutorConfig_ZeroWorkerOmitted(t *testing.T) {
	s := newTestSupervisor(t, ExecutorSpec{
		OrchestratorURL: "http://localhost:8080",
	})
	path, err := s.writeExecutorConfig("deck-1", ":9001", "http://localhost:9001",
		filepath.Join(s.cfg.StateDir, "executor-deck-1.db"))
	if err != nil {
		t.Fatalf("writeExecutorConfig: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, key := range []string{"step_duration", "heartbeat_interval", "poll_interval"} {
		if strings.Contains(string(raw), key) {
			t.Errorf("expected %q to be omitted, got:\n%s", key, raw)
		}
	}

	cfg, err := execconfig.Load(path)
	if err != nil {
		t.Fatalf("execconfig.Load: %v", err)
	}
	if cfg.Worker.StepDuration == 0 || cfg.Worker.HeartbeatInterval == 0 || cfg.Worker.PollInterval == 0 {
		t.Errorf("executor defaults not applied: %+v", cfg.Worker)
	}
}
