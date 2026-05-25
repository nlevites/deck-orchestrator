package chaos

import (
	"net/http"
	"strings"
)

// chaosPathPrefix bypasses PauseIngress so operators can clear pause without restart.
const chaosPathPrefix = "/executor/chaos"

// IngressHandler closes connections when PauseIngress is set so the reconciler
// sees a transport error (partition semantics), not a 503. /executor/chaos* and
// /health always pass through.
type IngressHandler struct {
	Inner http.Handler
	State *State
}

// NewIngressHandler wraps inner. Nil state disables chaos.
func NewIngressHandler(inner http.Handler, state *State) *IngressHandler {
	return &IngressHandler{Inner: inner, State: state}
}

func (h *IngressHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.State != nil && h.State.PauseIngress() && !bypass(r.URL.Path) {
		// Hijack+close for real transport error; 503 if unsupported (e.g. test wrappers).
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, err := hj.Hijack()
			if err == nil {
				_ = conn.Close()
				return
			}
		}
		http.Error(w, "chaos: ingress paused", http.StatusServiceUnavailable)
		return
	}
	h.Inner.ServeHTTP(w, r)
}

func bypass(path string) bool {
	if path == "/health" {
		return true
	}
	return strings.HasPrefix(path, chaosPathPrefix)
}
