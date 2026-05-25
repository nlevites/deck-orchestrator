package supervisor

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// executorConfigFile is the on-disk YAML the supervisor writes per deck.
// It mirrors the subset of internal/executor/config.Config that the
// supervisor populates from ExecutorSpec; everything else (Outbox,
// timeouts, Log, Chaos) keeps executor-side defaults.
type executorConfigFile struct {
	DeckID          string              `yaml:"deck_id"`
	OrchestratorURL string              `yaml:"orchestrator_url"`
	ListenAddr      string              `yaml:"listen_addr"`
	AdvertiseURL    string              `yaml:"advertise_url"`
	DBPath          string              `yaml:"db_path"`
	Worker          executorWorkerBlock `yaml:"worker,omitempty"`
}

type executorWorkerBlock struct {
	StepDuration      time.Duration `yaml:"step_duration,omitempty"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval,omitempty"`
	PollInterval      time.Duration `yaml:"poll_interval,omitempty"`
}

// executorConfigPath is where the supervisor materializes the per-deck
// executor YAML. Stable across respawns so the file just needs to still
// exist on disk when the respawn loop re-execs entry.Args.
func (s *Supervisor) executorConfigPath(deckID string) string {
	return filepath.Join(s.cfg.StateDir, "configs", "executor-"+deckID+".yaml")
}

// writeExecutorConfig materializes the YAML and returns its path.
func (s *Supervisor) writeExecutorConfig(deckID, listenAddr, advertiseURL, dbPath string) (string, error) {
	path := s.executorConfigPath(deckID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("supervisor: mkdir executor config dir: %w", err)
	}
	f := executorConfigFile{
		DeckID:          deckID,
		OrchestratorURL: s.cfg.Executor.OrchestratorURL,
		ListenAddr:      listenAddr,
		AdvertiseURL:    advertiseURL,
		DBPath:          dbPath,
		Worker: executorWorkerBlock{
			StepDuration:      s.cfg.Executor.StepDuration,
			HeartbeatInterval: s.cfg.Executor.HeartbeatInterval,
			PollInterval:      s.cfg.Executor.PollInterval,
		},
	}
	data, err := yaml.Marshal(&f)
	if err != nil {
		return "", fmt.Errorf("supervisor: marshal executor config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("supervisor: write executor config: %w", err)
	}
	return path, nil
}
