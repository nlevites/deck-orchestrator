package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"deck-fleet/backend/internal/api"
	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/store"
	storegen "deck-fleet/backend/internal/store/gen"
)

// LiveDeps is intentionally narrow: live state is read-only projection.
type LiveDeps struct {
	Store  *store.DB
	Logger *slog.Logger
}

// Live serves the snapshot-or-delta protocol (StateSnapshot /
// RunStateSnapshot) the React console polls for live updates.
type Live struct {
	deps LiveDeps
}

func NewLive(deps LiveDeps) *Live { return &Live{deps: deps} }

// Defaults mirror the OpenAPI schema so the handler is self-describing.
const (
	stateDefaultLimit = 500
	stateMaxLimit     = 1000
)

func (l *Live) GetState(w http.ResponseWriter, r *http.Request) {
	sinceSeq := parseInt64(r.URL.Query().Get("since_seq"))
	limit := parseLimit(r.URL.Query().Get("limit"), stateDefaultLimit, stateMaxLimit)

	serverSeq, err := l.deps.Store.ReadQueries.MaxEventSeq(r.Context())
	if err != nil {
		l.deps.Logger.Error("getState: max event seq", "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, err.Error())
		return
	}

	eventRows, err := l.deps.Store.ReadQueries.EventsSince(r.Context(), storegen.EventsSinceParams{
		Seq:   sinceSeq,
		Limit: limit,
	})
	if err != nil {
		l.deps.Logger.Error("getState: events since", "since_seq", sinceSeq, "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, err.Error())
		return
	}
	events, err := rowsToEvents(eventRows)
	if err != nil {
		l.deps.Logger.Error("getState: marshal events", "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, err.Error())
		return
	}

	snap := gen.StateSnapshot{
		ServerSeq: serverSeq,
		Events:    events,
	}

	if sinceSeq == 0 {
		// Bootstrap: full authoritative slices for runs + decks.
		decks, dErr := l.bootstrapDecks(r)
		if dErr != nil {
			l.deps.Logger.Error("getState: bootstrap decks", "err", dErr)
			api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, dErr.Error())
			return
		}
		snap.Decks = &decks

		runs, rErr := l.bootstrapRuns(r)
		if rErr != nil {
			l.deps.Logger.Error("getState: bootstrap runs", "err", rErr)
			api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, rErr.Error())
			return
		}
		snap.Runs = &runs
	} else {
		// Delta: ship only decks touched since the client's cursor.
		// Union of (a) decks referenced by any event with seq > since_seq
		// and (b) decks whose last_heartbeat_at advanced inside the
		// freshness window. Bounded by activity, not fleet size.
		// Backs S3 in analysis/inefficiencies/inefficiencies.md.
		delta, dErr := l.deltaDecks(r, eventRows)
		if dErr != nil {
			l.deps.Logger.Error("getState: delta decks", "err", dErr)
			api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, dErr.Error())
			return
		}
		// Always emit decks_delta on delta polls, even when empty — the
		// client uses presence-of-field, not nil-vs-empty, to decide
		// merge-vs-replace.
		snap.DecksDelta = &delta
	}

	api.WriteJSON(w, http.StatusOK, snap)
}

// GetRunState bootstraps the full Run at since_seq=0; otherwise returns
// only that run's events with seq > since_seq.
func (l *Live) GetRunState(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	sinceSeq := parseInt64(r.URL.Query().Get("since_seq"))
	limit := parseLimit(r.URL.Query().Get("limit"), stateDefaultLimit, stateMaxLimit)

	// Reject unknown run_id up front — empty 200 would hide typos.
	runRow, err := l.deps.Store.ReadQueries.GetRun(r.Context(), runID)
	if errors.Is(err, sql.ErrNoRows) {
		api.WriteSimpleError(w, r, gen.ErrorCodeRUNNOTFOUND, fmt.Sprintf("run %q does not exist", runID))
		return
	}
	if err != nil {
		l.deps.Logger.Error("getRunState: get run", "run_id", runID, "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, err.Error())
		return
	}

	serverSeq, err := l.deps.Store.ReadQueries.MaxEventSeq(r.Context())
	if err != nil {
		l.deps.Logger.Error("getRunState: max event seq", "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, err.Error())
		return
	}

	eventRows, err := l.deps.Store.ReadQueries.EventsSinceForRun(r.Context(), storegen.EventsSinceForRunParams{
		RunID: sql.NullString{String: runID, Valid: true},
		Seq:   sinceSeq,
		Limit: limit,
	})
	if err != nil {
		l.deps.Logger.Error("getRunState: events since for run", "run_id", runID, "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, err.Error())
		return
	}
	events, err := rowsToEvents(eventRows)
	if err != nil {
		l.deps.Logger.Error("getRunState: marshal events", "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, err.Error())
		return
	}

	snap := gen.RunStateSnapshot{
		ServerSeq: serverSeq,
		Events:    events,
	}

	if sinceSeq == 0 {
		run, rErr := rowToRun(r.Context(), l.deps.Store.ReadQueries, runRow, true)
		if rErr != nil {
			l.deps.Logger.Error("getRunState: rowToRun", "run_id", runID, "err", rErr)
			api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, rErr.Error())
			return
		}
		snap.Run = &run
	}

	api.WriteJSON(w, http.StatusOK, snap)
}

// bootstrapRuns uses ListRuns' default limit; no status/limit filters here.
func (l *Live) bootstrapRuns(r *http.Request) ([]gen.RunSummary, error) {
	rows, err := l.deps.Store.ReadQueries.ListRuns(r.Context(), 50)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	out := make([]gen.RunSummary, 0, len(rows))
	for _, row := range rows {
		s, sErr := rowToRunSummary(r.Context(), l.deps.Store.ReadQueries, row)
		if sErr != nil {
			return nil, fmt.Errorf("rowToRunSummary %s: %w", row.ID, sErr)
		}
		out = append(out, s)
	}
	return out, nil
}

// deltaDeckHeartbeatWindow caps how far back the heartbeat-cutoff query
// looks. Picked to comfortably cover the client's 1Hz poll plus a slow-
// response safety margin. Smaller = less data; larger = more tolerance
// for late polls. The 5min rebootstrap safety net (FE side) catches any
// row missed because its heartbeat slipped outside this window during a
// long pause.
const deltaDeckHeartbeatWindow = 5 * time.Second

// deltaDecks computes the touched-since-since_seq slice for delta polls.
// Two contributing sources, merged by id:
//   - Decks whose id appears in any event row returned this tick (covers
//     DECK_REGISTERED, DECK_HEALTH_CHANGED, JOB_DISPATCHED, terminal
//     job events that clear current_job).
//   - Decks whose last_heartbeat_at advanced inside the freshness window
//     (covers liveness ticks the event log deliberately doesn't capture).
//
// O(events + heartbeats_in_window + |unique_deck_ids| sub-queries) —
// bounded by activity, not fleet size.
func (l *Live) deltaDecks(r *http.Request, eventRows []storegen.Events) ([]gen.Deck, error) {
	deckIDSet := map[string]struct{}{}
	for _, row := range eventRows {
		if row.DeckID.Valid && row.DeckID.String != "" {
			deckIDSet[row.DeckID.String] = struct{}{}
		}
	}

	cutoffMs := time.Now().Add(-deltaDeckHeartbeatWindow).UnixMilli()
	heartbeatRows, err := l.deps.Store.ReadQueries.ListDecksHeartbeatSince(
		r.Context(), sql.NullInt64{Int64: cutoffMs, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list decks heartbeat since: %w", err)
	}

	out := make([]gen.Deck, 0, len(heartbeatRows)+len(deckIDSet))
	seen := map[string]struct{}{}
	for _, row := range heartbeatRows {
		d, dErr := rowToDeck(r.Context(), l.deps.Store.ReadQueries, row)
		if dErr != nil {
			return nil, fmt.Errorf("rowToDeck %s: %w", row.ID, dErr)
		}
		out = append(out, d)
		seen[row.ID] = struct{}{}
	}
	for id := range deckIDSet {
		if _, dup := seen[id]; dup {
			continue
		}
		row, gErr := l.deps.Store.ReadQueries.GetDeck(r.Context(), id)
		if errors.Is(gErr, sql.ErrNoRows) {
			continue
		}
		if gErr != nil {
			return nil, fmt.Errorf("get deck %s: %w", id, gErr)
		}
		d, dErr := rowToDeck(r.Context(), l.deps.Store.ReadQueries, row)
		if dErr != nil {
			return nil, fmt.Errorf("rowToDeck %s: %w", id, dErr)
		}
		out = append(out, d)
	}
	return out, nil
}

// bootstrapDecks mirrors ListDecks query; inlined because ListDecks writes the response.
func (l *Live) bootstrapDecks(r *http.Request) ([]gen.Deck, error) {
	rows, err := l.deps.Store.ReadQueries.ListDecks(r.Context())
	if err != nil {
		return nil, fmt.Errorf("list decks: %w", err)
	}
	out := make([]gen.Deck, 0, len(rows))
	for _, row := range rows {
		d, dErr := rowToDeck(r.Context(), l.deps.Store.ReadQueries, row)
		if dErr != nil {
			return nil, fmt.Errorf("rowToDeck %s: %w", row.ID, dErr)
		}
		out = append(out, d)
	}
	return out, nil
}

// rowsToEvents unmarshals payloads as map[string]any on purpose: the write path
// uses typed eventlog payloads; the live read path keys on kind + scope only.
func rowsToEvents(rows []storegen.Events) ([]gen.Event, error) {
	out := make([]gen.Event, 0, len(rows))
	for _, row := range rows {
		var payload map[string]any
		if row.Payload == "" {
			payload = map[string]any{}
		} else if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			return nil, fmt.Errorf("unmarshal event %d payload: %w", row.Seq, err)
		}
		evt := gen.Event{
			Seq:        row.Seq,
			OccurredAt: time.UnixMilli(row.OccurredAt).UTC(),
			Kind:       gen.EventKind(row.Kind),
			Payload:    payload,
		}
		if row.RunID.Valid {
			s := row.RunID.String
			evt.RunId = &s
		}
		if row.JobID.Valid {
			s := row.JobID.String
			evt.JobId = &s
		}
		if row.DeckID.Valid {
			s := row.DeckID.String
			evt.DeckId = &s
		}
		if row.AttemptID.Valid {
			if u, err := uuid.Parse(row.AttemptID.String); err == nil {
				uid := openapi_types.UUID(u)
				evt.AttemptId = &uid
			}
		}
		out = append(out, evt)
	}
	return out, nil
}

// parseInt64 maps empty/malformed/negative to 0 (bootstrap sentinel).
func parseInt64(s string) int64 {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
