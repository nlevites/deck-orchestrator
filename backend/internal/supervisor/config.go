// Package supervisor is the dev-only process supervisor for orchestrator
// and executor children (successor to the bash respawn loops once in
// scripts/demo.sh — now `make demo`).
// Production uses systemd/k8s; the orchestrator never spawns processes.
package supervisor

import (
	"errors"
	"fmt"
	"time"

	applog "deck-fleet/backend/internal/log"
)

type Config struct {
	ListenAddr string `yaml:"listen_addr" env:"LISTEN_ADDR" env-default:":8090"`

	// Wiped by demo.sh --clean alongside the orchestrator DB.
	StateDir string `yaml:"state_dir" env:"STATE_DIR" env-default:"./.supervisor-state"`

	// Delay before respawning an unexpectedly exited child (demo.sh used sleep 1).
	RespawnDelay time.Duration `yaml:"respawn_delay" env:"RESPAWN_DELAY" env-default:"1s"`

	Log applog.Config `yaml:"log"`

	Orchestrator OrchestratorSpec `yaml:"orchestrator"`
	Executor     ExecutorSpec     `yaml:"executor"`

	// Attached automatically at boot (demo/e2e).
	PrelaunchExecutors []string `yaml:"prelaunch_executors" env:"PRELAUNCH_EXECUTORS" env-separator:","`

	// Shorthand for PrelaunchExecutors=["deck-1",...,"deck-N"]. Mutually
	// exclusive with PrelaunchExecutors. Used by the Playwright e2e
	// webServer so the suite can dial deck count via EXECUTOR_COUNT
	// without templating YAML.
	ExecutorCount int `yaml:"executor_count" env:"EXECUTOR_COUNT" env-default:"0"`
}

type OrchestratorSpec struct {
	BinaryPath     string `yaml:"binary_path" env:"ORCH_BINARY_PATH"`
	ConfigPath     string `yaml:"config_path" env:"ORCH_CONFIG_PATH"`
	HTTPPort       int    `yaml:"http_port" env:"ORCH_HTTP_PORT" env-default:"8080"`
	HTTPSchemeHost string `yaml:"http_scheme_host" env:"ORCH_HTTP_SCHEME_HOST" env-default:"http://localhost"`
	// Injected as DB_PATH on every spawn so one config works across demo/e2e.
	DBPath string `yaml:"db_path" env:"ORCH_DB_PATH"`
}

type ExecutorSpec struct {
	BinaryPath        string        `yaml:"binary_path" env:"EXEC_BINARY_PATH"`
	OrchestratorURL   string        `yaml:"orchestrator_url" env:"EXEC_ORCH_URL" env-default:"http://localhost:8080"`
	PortBase          int           `yaml:"port_base" env:"EXEC_PORT_BASE" env-default:"9000"`
	StepDuration      time.Duration `yaml:"step_duration" env:"EXEC_STEP_DURATION"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval" env:"EXEC_HEARTBEAT_INTERVAL"`
	PollInterval      time.Duration `yaml:"poll_interval" env:"EXEC_POLL_INTERVAL"`
	AdvertiseScheme   string        `yaml:"advertise_scheme" env:"EXEC_ADVERTISE_SCHEME" env-default:"http"`
	// Hostname the orchestrator dials for /executor/state.
	AdvertiseHost string `yaml:"advertise_host" env:"EXEC_ADVERTISE_HOST" env-default:"localhost"`
}

func (c *Config) Validate() error {
	var errs []error
	if c.Orchestrator.BinaryPath == "" {
		errs = append(errs, errors.New("orchestrator.binary_path is required"))
	}
	if c.Executor.BinaryPath == "" {
		errs = append(errs, errors.New("executor.binary_path is required"))
	}
	if c.StateDir == "" {
		errs = append(errs, errors.New("state_dir is required"))
	}
	if c.Executor.PortBase <= 0 {
		errs = append(errs, fmt.Errorf("executor.port_base must be > 0 (got %d)", c.Executor.PortBase))
	}
	if c.RespawnDelay < 0 {
		errs = append(errs, fmt.Errorf("respawn_delay must be >= 0 (got %s)", c.RespawnDelay))
	}
	if c.ExecutorCount < 0 {
		errs = append(errs, fmt.Errorf("executor_count must be >= 0 (got %d)", c.ExecutorCount))
	}
	if c.ExecutorCount > 0 && len(c.PrelaunchExecutors) > 0 {
		errs = append(errs, errors.New("executor_count and prelaunch_executors are mutually exclusive"))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if c.ExecutorCount > 0 {
		c.PrelaunchExecutors = make([]string, c.ExecutorCount)
		for i := 0; i < c.ExecutorCount; i++ {
			c.PrelaunchExecutors[i] = fmt.Sprintf("deck-%d", i+1)
		}
		c.ExecutorCount = 0 // idempotent: second Validate() is a no-op
	}
	return nil
}
