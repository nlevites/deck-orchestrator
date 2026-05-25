package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"deck-fleet/backend/internal/executor/chaos"
	"deck-fleet/backend/internal/executor/localstore"
)

type Server struct {
	deckID  string
	store   *localstore.Store
	chaos   *chaos.State
	onCrash func()
	logger  *slog.Logger
}

func New(deckID string, store *localstore.Store, chaosState *chaos.State, logger *slog.Logger) *Server {
	return &Server{deckID: deckID, store: store, chaos: chaosState, logger: logger}
}

// SetOnCrash registers the callback POST /executor/chaos/crash
// invokes. Production wires this to os.Exit(1) via the same callback
// the worker's crash-after-step arm uses; the harness wires it to a
// Restart hook so a chaos crash in a test doesn't terminate the test
// process. Called once during BuildExecutor after the server is built.
func (s *Server) SetOnCrash(fn func()) {
	s.onCrash = fn
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /executor/state", s.getState)
	mux.HandleFunc("POST /executor/abort/{attempt_id}", s.postAbort)
	mux.HandleFunc("GET /executor/chaos", s.getChaos)
	mux.HandleFunc("POST /executor/chaos", s.postChaos)
	mux.HandleFunc("POST /executor/chaos/reset", s.postChaosReset)
	mux.HandleFunc("POST /executor/chaos/crash", s.postChaosCrash)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

type stateAttempt struct {
	AttemptID  string     `json:"attempt_id"`
	State      string     `json:"state"`
	ReceivedAt time.Time  `json:"received_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	TerminalAt *time.Time `json:"terminal_at,omitempty"`
	Result     any        `json:"result,omitempty"`
	Error      *string    `json:"error,omitempty"`
}

type stateOverall struct {
	DeckID           string         `json:"deck_id"`
	CurrentAttemptID *string        `json:"current_attempt_id,omitempty"`
	CurrentState     *string        `json:"current_state,omitempty"`
	RecentAttempts   []stateAttempt `json:"recent_attempts"`
}

func (s *Server) getState(w http.ResponseWriter, r *http.Request) {
	attemptID := strings.TrimSpace(r.URL.Query().Get("attempt_id"))
	if attemptID == "" {
		s.respondOverall(w, r)
		return
	}
	a, err := s.store.GetAttempt(r.Context(), attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"code":       "UNKNOWN_ATTEMPT",
			"attempt_id": attemptID,
			"message":    "executor has no record of this attempt",
		})
		return
	}
	if err != nil {
		s.logger.Error("getState: load", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, toStateAttempt(a))
}

func (s *Server) respondOverall(w http.ResponseWriter, r *http.Request) {
	current, hasCurrent, err := s.store.CurrentInFlight(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": err.Error()})
		return
	}
	recent, err := s.store.ListRecentAttempts(r.Context(), 20)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": err.Error()})
		return
	}
	out := stateOverall{
		DeckID:         s.deckID,
		RecentAttempts: make([]stateAttempt, 0, len(recent)),
	}
	if hasCurrent {
		id := current.AttemptID
		state := string(current.State)
		out.CurrentAttemptID = &id
		out.CurrentState = &state
	}
	for _, a := range recent {
		out.RecentAttempts = append(out.RecentAttempts, toStateAttempt(a))
	}
	writeJSON(w, http.StatusOK, out)
}

func toStateAttempt(a localstore.Attempt) stateAttempt {
	out := stateAttempt{
		AttemptID:  a.AttemptID,
		State:      string(a.State),
		ReceivedAt: a.ReceivedAt,
		StartedAt:  a.StartedAt,
		TerminalAt: a.TerminalAt,
		Error:      a.Error,
	}
	if a.Result != nil {
		out.Result = json.RawMessage(*a.Result)
	}
	return out
}

func (s *Server) postAbort(w http.ResponseWriter, r *http.Request) {
	attemptID := r.PathValue("attempt_id")
	if attemptID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_REQUEST", "message": "missing attempt_id"})
		return
	}
	a, err := s.store.GetAttempt(r.Context(), attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"code": "UNKNOWN_ATTEMPT", "message": "executor has no record of this attempt"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": err.Error()})
		return
	}
	if a.State == localstore.StateCompleted || a.State == localstore.StateFailed {
		state := string(a.State)
		writeJSON(w, http.StatusOK, map[string]any{
			"status":      "already_terminal",
			"attempt_id":  attemptID,
			"final_state": state,
		})
		return
	}
	if setErr := s.store.SetAbortRequested(r.Context(), attemptID); setErr != nil {
		s.logger.Error("postAbort: set", "err", setErr)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"code": "INTERNAL_ERROR", "message": setErr.Error()})
		return
	}
	s.logger.Info("postAbort: flag set", "attempt_id", attemptID, "deck_id", s.deckID)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "abort_requested",
		"attempt_id": attemptID,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// getChaos returns chaos snapshot JSON. Nil chaos returns a zero snapshot.
func (s *Server) getChaos(w http.ResponseWriter, _ *http.Request) {
	if s.chaos == nil {
		writeJSON(w, http.StatusOK, chaos.Snapshot{})
		return
	}
	writeJSON(w, http.StatusOK, s.chaos.SnapshotState())
}

func (s *Server) postChaos(w http.ResponseWriter, r *http.Request) {
	if s.chaos == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"code":    "CHAOS_UNAVAILABLE",
			"message": "executor has no chaos state wired",
		})
		return
	}
	var patch chaos.InitialState
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&patch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"code":    "INVALID_BODY",
			"message": err.Error(),
		})
		return
	}
	s.chaos.Apply(patch)
	s.logger.Info("chaos: patched", "deck_id", s.deckID)
	writeJSON(w, http.StatusOK, s.chaos.SnapshotState())
}

func (s *Server) postChaosReset(w http.ResponseWriter, _ *http.Request) {
	if s.chaos == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"code":    "CHAOS_UNAVAILABLE",
			"message": "executor has no chaos state wired",
		})
		return
	}
	s.chaos.Reset()
	s.logger.Info("chaos: reset", "deck_id", s.deckID)
	writeJSON(w, http.StatusOK, s.chaos.SnapshotState())
}

// postChaosCrash invokes onCrash after 200 so the client sees a clean response
// before process death (production: os.Exit(1); harness: Restart hook).
func (s *Server) postChaosCrash(w http.ResponseWriter, _ *http.Request) {
	if s.onCrash == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"code":    "CRASH_UNAVAILABLE",
			"message": "executor has no crash callback wired",
		})
		return
	}
	s.logger.Warn("chaos: crash requested", "deck_id", s.deckID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "crashing"})
	// Response must flush before onCrash runs in a goroutine.
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go s.onCrash()
}
