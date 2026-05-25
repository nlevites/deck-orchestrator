package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"deck-fleet/backend/internal/executor/chaos"
	"deck-fleet/backend/internal/executor/client"
	execconfig "deck-fleet/backend/internal/executor/config"
	"deck-fleet/backend/internal/executor/localstore"
	"deck-fleet/backend/internal/executor/outbox"
	executorsrv "deck-fleet/backend/internal/executor/server"
	"deck-fleet/backend/internal/executor/worker"
)

// Executor is the wired graph cmd/executor and integration tests share.
// Exported fields are the same instances constructors returned so tests
// can inject chaos transports and assert on local-store rows.
type Executor struct {
	Cfg    *execconfig.Config
	Logger *slog.Logger

	Handler http.Handler
	Local   *localstore.Store
	Client  *client.Client
	Worker  *worker.Worker
	Outbox  *outbox.Flusher
	Server  *executorsrv.Server
	Chaos   *chaos.State

	// OnCrash is shared by the worker (crash-after-step) and chaos HTTP
	// (/crash now) so both triggers hit the same exit path.
	OnCrash func()
}

// BuildExecutor wires the executor graph. Does not bind a listener or start goroutines.
// onFatalExit: heartbeat 404/410 → exit 78 (EX_CONFIG) so the supervisor stops respawning.
func BuildExecutor(ctx context.Context, cfg *execconfig.Config, logger *slog.Logger, onCrash func(), onFatalExit func(int)) (*Executor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("app: executor config is nil")
	}
	if logger == nil {
		return nil, fmt.Errorf("app: executor logger is nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.NormalizeURLs()

	deckLogger := logger.With("deck_id", cfg.DeckID)

	local, err := localstore.Open(ctx, cfg.DBPath, deckLogger)
	if err != nil {
		return nil, fmt.Errorf("localstore.Open: %w", err)
	}

	chaosState := chaos.New(cfg.Chaos)

	hc := client.New(cfg.OrchestratorURL)
	// PauseEgress wraps the existing transport so dial/timeout config stays intact.
	hc.HTTPClient.Transport = chaos.NewEgressTransport(hc.HTTPClient.Transport, chaosState)

	w := worker.New(worker.Deps{
		Cfg:       cfg.Worker,
		Store:     local,
		Client:    hc,
		Chaos:     chaosState,
		Logger:    deckLogger,
		CrashFn:   onCrash,
		FatalExit: onFatalExit,
	})
	f := outbox.New(outbox.Deps{
		Store:  local,
		Client: hc,
		Cfg:    cfg.Outbox,
		Chaos:  chaosState,
		Logger: deckLogger,
	})
	srv := executorsrv.New(cfg.DeckID, local, chaosState, deckLogger)
	srv.SetOnCrash(onCrash)

	// PauseIngress drops inbound traffic; /executor/chaos and /health bypass it.
	handler := chaos.NewIngressHandler(srv.Handler(), chaosState)

	return &Executor{
		Cfg:     cfg,
		Logger:  deckLogger,
		Handler: handler,
		Local:   local,
		Client:  hc,
		Worker:  w,
		Outbox:  f,
		Server:  srv,
		Chaos:   chaosState,
		OnCrash: onCrash,
	}, nil
}

// Run runs worker + outbox until ctx is cancelled; wait for both before Close().
func (e *Executor) Run(ctx context.Context) {
	workerDone := make(chan struct{})
	flushDone := make(chan struct{})
	go func() { defer close(workerDone); e.Worker.Run(ctx) }()
	go func() { defer close(flushDone); e.Outbox.Run(ctx) }()
	<-workerDone
	<-flushDone
}

// Close releases the local store after Run returns.
func (e *Executor) Close() error {
	if err := e.Local.Close(); err != nil {
		return fmt.Errorf("close local: %w", err)
	}
	return nil
}
