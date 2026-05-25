package supervisor

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

func (s *Supervisor) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /supervisor/processes", s.handleListProcesses)
	mux.HandleFunc("POST /supervisor/orchestrator/restart", s.handleOrchestratorRestart)
	mux.HandleFunc("POST /supervisor/orchestrator/stop", s.handleOrchestratorStop)
	mux.HandleFunc("POST /supervisor/orchestrator/start", s.handleOrchestratorStart)
	mux.HandleFunc("POST /supervisor/orchestrator/kill", s.handleOrchestratorKill)
	mux.HandleFunc("POST /supervisor/executors", s.handleAttachExecutor)
	mux.HandleFunc("POST /supervisor/executors/{deck_id}/stop", s.handleExecutorStop)
	mux.HandleFunc("POST /supervisor/executors/{deck_id}/start", s.handleExecutorStart)
	mux.HandleFunc("POST /supervisor/executors/{deck_id}/restart", s.handleExecutorRestart)
	mux.HandleFunc("DELETE /supervisor/executors/{deck_id}", s.handleDetachExecutor)
}

// Orchestrator on its own field so the UI can render it above the fleet grid.
type processesPayload struct {
	Orchestrator *ProcessEntrySnapshot  `json:"orchestrator,omitempty"`
	Executors    []ProcessEntrySnapshot `json:"executors"`
}

func (s *Supervisor) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Supervisor) handleListProcesses(w http.ResponseWriter, _ *http.Request) {
	entries := s.listEntries()
	out := processesPayload{Executors: []ProcessEntrySnapshot{}}
	for i := range entries {
		e := entries[i]
		if e.Kind == KindOrchestrator {
			snap := e
			out.Orchestrator = &snap
			continue
		}
		out.Executors = append(out.Executors, e)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Supervisor) handleOrchestratorRestart(w http.ResponseWriter, r *http.Request) {
	if err := s.orchestratorGracefulRestart(r.Context()); err != nil {
		if errors.Is(err, ErrNotFound) {
			writeError(w, http.StatusNotFound, "orchestrator not under this supervisor")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "restarting"})
}

func (s *Supervisor) handleOrchestratorStop(w http.ResponseWriter, _ *http.Request) {
	if err := s.stopLabel(orchestratorLabel); err != nil {
		mapErrToStatus(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Supervisor) handleOrchestratorStart(w http.ResponseWriter, _ *http.Request) {
	if err := s.startLabel(orchestratorLabel); err != nil {
		mapErrToStatus(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleOrchestratorKill SIGKILLs the orchestrator process group. Used by
// the e2e chaos suite to simulate a crash that bypasses graceful
// shutdown. Policy stays Always so the supervisor's respawn loop brings
// it back; the caller can rely on the next /health to flip from down to
// up. /restart is the operator-friendly path; /kill is the chaos hook.
func (s *Supervisor) handleOrchestratorKill(w http.ResponseWriter, _ *http.Request) {
	if err := s.killLabel(orchestratorLabel); err != nil {
		mapErrToStatus(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "killed"})
}

type attachRequest struct {
	DeckID     string `json:"deck_id"`
	FreshState bool   `json:"fresh_state,omitempty"`
}

func (s *Supervisor) handleAttachExecutor(w http.ResponseWriter, r *http.Request) {
	var body attachRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if strings.TrimSpace(body.DeckID) == "" {
		writeError(w, http.StatusBadRequest, "deck_id is required")
		return
	}
	// Use supervisor ctx, not r.Context(), so the watcher outlives the request.
	entry, err := s.attachExecutor(s.backgroundCtx(), body.DeckID, body.FreshState)
	switch {
	case errors.Is(err, ErrAlreadyAttached):
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "executor already attached for this deck_id",
			"entry": entry,
		})
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, entry)
}

func (s *Supervisor) handleExecutorStop(w http.ResponseWriter, r *http.Request) {
	deckID := r.PathValue("deck_id")
	if err := s.stopExecutor(deckID); err != nil {
		mapErrToStatus(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Supervisor) handleExecutorStart(w http.ResponseWriter, r *http.Request) {
	deckID := r.PathValue("deck_id")
	if err := s.startExecutor(deckID); err != nil {
		mapErrToStatus(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Supervisor) handleExecutorRestart(w http.ResponseWriter, r *http.Request) {
	deckID := r.PathValue("deck_id")
	if err := s.restartExecutor(deckID); err != nil {
		mapErrToStatus(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Supervisor) handleDetachExecutor(w http.ResponseWriter, r *http.Request) {
	deckID := r.PathValue("deck_id")
	if err := s.detachExecutor(deckID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func mapErrToStatus(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrAlreadyAttached):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func decodeJSON(r *http.Request, v any) error {
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(io.LimitReader(r.Body, 64*1024))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
