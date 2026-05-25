package api

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"deck-fleet/backend/internal/api/gen"
)

// Panics occur before headers are sent, so writing a 500 here is safe.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.ErrorContext(r.Context(), "panic recovered",
						slog.Any("panic", rec),
						slog.String("request_id", RequestIDFromContext(r.Context())),
						slog.String("stack", string(debug.Stack())),
					)
					WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
