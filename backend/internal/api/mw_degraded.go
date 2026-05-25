package api

import (
	"net/http"
	"strings"
	"sync/atomic"

	"deck-fleet/backend/internal/api/gen"
)

// Returns 503 for /api/* mutations while flag is set; reads and /executor/* stay open.
func Degraded(flag *atomic.Bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if flag.Load() && shouldBlock(r) {
				WriteSimpleError(w, r, gen.ErrorCodeDEGRADEDMODE,
					"Orchestrator reconciling; mutations temporarily refused.")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func shouldBlock(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return false
	}
	return strings.HasPrefix(r.URL.Path, "/api/")
}
