package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"deck-fleet/backend/internal/api"
	"deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/store"
)

// ChaosDeps are collaborators for the chaos proxy, kept separate from
// Operator.Deps so chaos doesn't widen every handler's dependency surface.
type ChaosDeps struct {
	Store      *store.DB
	Logger     *slog.Logger
	HTTPClient *http.Client // dialed at the executor
}

// Chaos proxies orchestrator chaos endpoints to each deck's executor.
// endpoint_url stays server-side; the browser talks to one base URL.
type Chaos struct {
	deps ChaosDeps
}

// NewChaos uses a 5s-timeout client when HTTPClient is nil, matching
// other executor-bound clients. Chaos POSTs should be near-instant.
func NewChaos(deps ChaosDeps) *Chaos {
	if deps.HTTPClient == nil {
		deps.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &Chaos{deps: deps}
}

func (c *Chaos) GetState(w http.ResponseWriter, r *http.Request) {
	c.proxy(w, r, http.MethodGet, "/executor/chaos", nil)
}

// PatchState forwards the body verbatim; the executor owns the chaos schema.
func (c *Chaos) PatchState(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		api.WriteSimpleError(w, r, gen.ErrorCodeINVALIDJSON, "read body: "+err.Error())
		return
	}
	c.proxy(w, r, http.MethodPost, "/executor/chaos", body)
}

func (c *Chaos) Reset(w http.ResponseWriter, r *http.Request) {
	c.proxy(w, r, http.MethodPost, "/executor/chaos/reset", nil)
}

// Crash: the executor writes 200 then exits; the orchestrator returns whatever the
// executor returned.
func (c *Chaos) Crash(w http.ResponseWriter, r *http.Request) {
	c.proxy(w, r, http.MethodPost, "/executor/chaos/crash", nil)
}

// proxy forwards to the executor; transport errors map to
// 502 EXECUTOR_UNREACHABLE so the UI can render a sensible message.
func (c *Chaos) proxy(w http.ResponseWriter, r *http.Request, method, path string, body []byte) {
	deckID := r.PathValue("deck_id")
	if deckID == "" {
		api.WriteSimpleError(w, r, gen.ErrorCodeSCHEMAVIOLATION, "missing deck_id")
		return
	}

	endpointURL, err := c.lookupEndpoint(r.Context(), deckID)
	if errors.Is(err, sql.ErrNoRows) {
		api.WriteSimpleError(w, r, gen.ErrorCodeDECKNOTFOUND, "unknown deck_id")
		return
	}
	if err != nil {
		c.deps.Logger.Error("chaos: lookup deck", "deck_id", deckID, "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, err.Error())
		return
	}

	target, err := joinURL(endpointURL, path)
	if err != nil {
		c.deps.Logger.Error("chaos: parse endpoint_url", "deck_id", deckID, "endpoint_url", endpointURL, "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, "invalid endpoint_url")
		return
	}

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(r.Context(), method, target, reqBody)
	if err != nil {
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, "build request: "+err.Error())
		return
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.deps.HTTPClient.Do(req)
	if err != nil {
		c.deps.Logger.Warn("chaos: executor dial failed", "deck_id", deckID, "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeEXECUTORUNREACHABLE, fmt.Sprintf("executor unreachable: %v", err))
		return
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		c.deps.Logger.Warn("chaos: read executor body", "deck_id", deckID, "err", err)
		api.WriteSimpleError(w, r, gen.ErrorCodeINTERNALERROR, "read executor body: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(raw)
}

func (c *Chaos) lookupEndpoint(ctx context.Context, deckID string) (string, error) {
	row, err := c.deps.Store.ReadQueries.GetDeck(ctx, deckID)
	if err != nil {
		return "", err
	}
	if !row.EndpointUrl.Valid || row.EndpointUrl.String == "" {
		return "", fmt.Errorf("deck %q has no endpoint_url (slot is empty)", deckID)
	}
	return row.EndpointUrl.String, nil
}

// joinURL appends path to base and clears query strings (chaos paths take none).
func joinURL(base, path string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = path
	u.RawQuery = ""
	return u.String(), nil
}
