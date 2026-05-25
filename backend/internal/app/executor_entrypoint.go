package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	execconfig "deck-fleet/backend/internal/executor/config"
)

// RunExecutor builds, binds cfg.ListenAddr, and blocks until ctx is cancelled.
func RunExecutor(ctx context.Context, cfg *execconfig.Config, logger *slog.Logger) error {
	onCrash := func() {
		logger.Warn("executor: crash-after-step engaged; os.Exit(1)")
		os.Exit(1)
	}
	// Heartbeat 404/410 → exit 78 (EX_CONFIG); supervisor must not respawn.
	onFatalExit := func(code int) {
		logger.Error("executor: fatal config error; not retrying", "exit_code", code)
		os.Exit(code)
	}
	e, err := BuildExecutor(ctx, cfg, logger, onCrash, onFatalExit)
	if err != nil {
		return err
	}
	defer func() {
		if cErr := e.Close(); cErr != nil {
			logger.Error("executor: close", "err", cErr)
		}
	}()

	logger.Info("executor: starting",
		"orchestrator", cfg.OrchestratorURL,
		"listen", cfg.ListenAddr,
		"advertise", cfg.AdvertiseURL,
		"db", cfg.DBPath,
		"step_duration", cfg.Worker.StepDuration.String(),
	)

	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           e.Handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
	}

	httpErrCh := make(chan error, 1)
	go func() {
		logger.Info("executor: HTTP listening", "addr", cfg.ListenAddr)
		if lErr := httpSrv.ListenAndServe(); !errors.Is(lErr, http.ErrServerClosed) {
			httpErrCh <- lErr
		}
		close(httpErrCh)
	}()

	var runWG sync.WaitGroup
	runWG.Add(1)
	go func() {
		defer runWG.Done()
		e.Run(ctx)
	}()

	select {
	case <-ctx.Done():
		logger.Info("executor: shutdown signal")
	case sErr := <-httpErrCh:
		if sErr != nil {
			runWG.Wait()
			return fmt.Errorf("http server: %w", sErr)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer shutdownCancel()
	if sErr := httpSrv.Shutdown(shutdownCtx); sErr != nil {
		logger.Error("http shutdown", "err", sErr)
	}
	runWG.Wait()
	select {
	case <-httpErrCh:
	case <-time.After(100 * time.Millisecond):
	}
	logger.Info("executor: stopped")
	return nil
}
