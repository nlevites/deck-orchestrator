package api

import (
	"net/http"

	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-ID"

// UUIDv7 unless client sent X-Request-ID. Stamped on context for error envelopes (API.md §4).
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		ctx := WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRequestID() string {
	id, err := uuid.NewV7()
	if err != nil {
		// NewV7 fails only on rand.Read errors; fall back rather than fail the request.
		return uuid.NewString()
	}
	return id.String()
}
