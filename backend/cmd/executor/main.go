// Command executor is the per-deck executor binary.
//
// Composition lives in internal/app. main parses -config and hands off
// to app.RunExecutor; per-deck runtime values (deck id, addresses,
// timing) come from the YAML file the dev supervisor materializes, or
// from env vars when running without a config file. Chaos /
// failure-injection is not a CLI concern — it is driven exclusively by
// the /executor/chaos HTTP surface (used by the UI and the orchestrator
// proxy) and by direct struct writes in the integration harness.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"deck-fleet/backend/internal/app"
	execconfig "deck-fleet/backend/internal/executor/config"
	applog "deck-fleet/backend/internal/log"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "executor:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to a YAML config file; if empty, env + defaults are used")
	flag.Parse()

	cfg, err := execconfig.Load(*configPath)
	if err != nil {
		return err
	}

	logger, err := applog.New(cfg.Log)
	if err != nil {
		return err
	}
	logger = logger.With("deck_id", cfg.DeckID)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	return app.RunExecutor(ctx, cfg, logger)
}
