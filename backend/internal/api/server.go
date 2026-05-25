package api

import (
	"log/slog"
	"net/http"
	"sync/atomic"
)

type Deps struct {
	Logger *slog.Logger
	HTTP   Config
	CORS   CORSConfig

	// Cycle-breaker: handlers register routes via closure from cmd.
	RegisterRoutes func(mux *http.ServeMux)

	Degraded *atomic.Bool // gates /api/* mutations during startup reconciliation
}

func NewServer(deps Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", handleHealth)

	if deps.RegisterRoutes != nil {
		deps.RegisterRoutes(mux)
	}

	// Outermost to innermost: requestid -> recover -> logging -> compress -> cors -> bodylimit -> mux.
	// requestid first so recover/logging see the id; cors before bodylimit so OPTIONS skips body read.
	// compress sits inside logging on purpose — bytes_out then reflects the
	// pre-compression body size, which is what the analysis pillar uses to
	// compare ingress/egress costs across runs (S8). The HTTP client sees
	// the compressed wire size; the log line sees the logical payload.
	var handler http.Handler = mux
	handler = BodyLimit(deps.HTTP.BodyLimitBytes)(handler)
	handler = CORS(deps.CORS.AllowedOrigins)(handler)
	if deps.Degraded != nil {
		handler = Degraded(deps.Degraded)(handler)
	}
	handler = Compress()(handler)
	handler = Logging(deps.Logger)(handler)
	handler = Recover(deps.Logger)(handler)
	handler = RequestID(handler)
	return handler
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
