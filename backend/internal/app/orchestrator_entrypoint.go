package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"deck-fleet/backend/internal/config"
)

// RunOrchestrator builds, binds the listener, and blocks until shutdown or restart.
func RunOrchestrator(ctx context.Context, cfg *config.Config, logger *slog.Logger) error {
	o, err := BuildOrchestrator(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer func() {
		if sErr := o.Shutdown(); sErr != nil {
			logger.Error("orchestrator: shutdown", "err", sErr)
		}
	}()

	// runCtx lets /api/admin/restart stop background loops without waiting
	// for the parent ctx (SIGINT). Without cancelRunCtx, bgWG.Wait blocks forever.
	runCtx, cancelRunCtx := context.WithCancel(ctx)
	defer cancelRunCtx()

	dbPath := cfg.Store.Path
	if abs, absErr := filepath.Abs(cfg.Store.Path); absErr == nil {
		dbPath = abs
	}
	logger.Info("orchestrator: starting",
		"db_path", dbPath,
		"http_addr", cfg.HTTP.Addr,
	)

	srv := &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           o.Handler,
		ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
	}

	serveErrCh := make(chan error, 1)
	go func() {
		logger.Info("orchestrator: listening", "addr", cfg.HTTP.Addr)
		if lErr := srv.ListenAndServe(); !errors.Is(lErr, http.ErrServerClosed) {
			serveErrCh <- lErr
		}
		close(serveErrCh)
	}()

	// Abort retries honour runCtx so Shutdown does not wait the full retry window.
	o.abortDialer.BaseCtx = runCtx

	var bgWG sync.WaitGroup
	bgWG.Add(1)
	go func() {
		defer bgWG.Done()
		o.Run(runCtx)
	}()

	select {
	case <-ctx.Done():
		logger.Info("orchestrator: shutdown signal received; draining",
			"grace", cfg.HTTP.ShutdownGrace,
		)
	case <-o.RestartCh:
		logger.Warn("orchestrator: /api/admin/restart accepted; draining and exiting",
			"grace", cfg.HTTP.ShutdownGrace,
		)
		cancelRunCtx()
	case sErr := <-serveErrCh:
		if sErr != nil {
			return fmt.Errorf("http server: %w", sErr)
		}
		return nil
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownGrace)
	defer shutdownCancel()
	if sErr := srv.Shutdown(shutdownCtx); sErr != nil {
		logger.Error("orchestrator: shutdown", "err", sErr)
		return fmt.Errorf("http shutdown: %w", sErr)
	}
	bgWG.Wait()

	select {
	case <-serveErrCh:
	case <-time.After(100 * time.Millisecond):
	}
	logger.Info("orchestrator: stopped")
	return nil
}
