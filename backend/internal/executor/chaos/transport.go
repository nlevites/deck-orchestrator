package chaos

import (
	"errors"
	"net/http"
)

// ErrEgressPaused is returned when PauseEgress is set. Callers log and retry;
// sentinel allows selective ignore.
var ErrEgressPaused = errors.New("chaos: egress paused")

// EgressTransport short-circuits when PauseEgress is set ("executor can't reach
// orchestrator"). Failure is observed without DNS/TCP/TLS so integration tests stay fast.
type EgressTransport struct {
	Inner http.RoundTripper
	State *State
}

// NewEgressTransport wraps inner (DefaultTransport if nil). Nil state disables chaos.
func NewEgressTransport(inner http.RoundTripper, state *State) *EgressTransport {
	if inner == nil {
		inner = http.DefaultTransport
	}
	return &EgressTransport{Inner: inner, State: state}
}

func (t *EgressTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.State != nil && t.State.PauseEgress() {
		return nil, ErrEgressPaused
	}
	return t.Inner.RoundTrip(req)
}
