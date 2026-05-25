// Command orchestrator is the orchestrator binary.
//
// It loads config exactly once, builds the logger, and hands off to
// app.RunOrchestrator. All composition lives in internal/app so the
// integration harness exercises the same dependency graph without
// duplicating wiring.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"deck-fleet/backend/internal/app"
	"deck-fleet/backend/internal/config"
	applog "deck-fleet/backend/internal/log"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "orchestrator:", err)
		os.Exit(1)
	}
}

func run() error {
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to a YAML config file; if empty, env + defaults are used")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	logger, err := applog.New(cfg.Log)
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	return app.RunOrchestrator(ctx, cfg, logger)
}
