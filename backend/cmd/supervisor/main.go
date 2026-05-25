// Command supervisor is the dev-only process supervisor. It launches
// the orchestrator and per-deck executor binaries, watches them for
// exit, respawns on failure (except exit code 78, which is "do not
// retry" per the EX_CONFIG convention used by the executor on
// heartbeat 404 / 410), and exposes /supervisor/* HTTP routes that the
// React UI's Settings > Fleet Management page consumes.
//
// Invoked by `make demo` and the Playwright e2e webServer; the moral
// equivalent of systemd / k8s for the demo. The orchestrator does NOT
// spawn processes; that's an explicit architectural boundary.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ilyakaznacheev/cleanenv"

	applog "deck-fleet/backend/internal/log"
	"deck-fleet/backend/internal/supervisor"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "supervisor:", err)
		os.Exit(1)
	}
}

func run() error {
	var configPath string
	var clean bool
	flag.StringVar(&configPath, "config", "", "path to a YAML config file; if empty, env + defaults are used")
	flag.BoolVar(&clean, "clean", false, "wipe state_dir before starting (used by e2e to ensure a hermetic boot)")
	flag.Parse()

	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}
	// Validate now (mutates *cfg) so the prelaunch log below reflects the
	// expanded executor_count list, not the pre-expansion empty slice.
	if vErr := cfg.Validate(); vErr != nil {
		return fmt.Errorf("supervisor: invalid config: %w", vErr)
	}

	if clean {
		if rErr := os.RemoveAll(cfg.StateDir); rErr != nil {
			return fmt.Errorf("clean state_dir %q: %w", cfg.StateDir, rErr)
		}
	}

	logger, err := applog.New(cfg.Log)
	if err != nil {
		return err
	}

	sup, err := supervisor.New(*cfg, logger)
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("supervisor: starting",
		"listen", cfg.ListenAddr,
		"state_dir", cfg.StateDir,
		"prelaunch", cfg.PrelaunchExecutors,
	)
	if rErr := sup.Run(ctx); rErr != nil {
		return rErr
	}
	return nil
}

func loadConfig(path string) (*supervisor.Config, error) {
	var cfg supervisor.Config
	if path == "" {
		if err := cleanenv.ReadEnv(&cfg); err != nil {
			return nil, fmt.Errorf("read env: %w", err)
		}
		return &cfg, nil
	}
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("config: file %q does not exist", path)
	} else if statErr != nil {
		return nil, fmt.Errorf("config: stat %q: %w", path, statErr)
	}
	if err := cleanenv.ReadConfig(path, &cfg); err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	return &cfg, nil
}
