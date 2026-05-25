// Package aborts dials POST /executor/abort when an operator cancels a run.
package aborts

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"deck-fleet/backend/internal/store"
	"deck-fleet/backend/internal/timeouts"
)

type Dialer struct {
	Store      *store.DB
	HTTPClient *http.Client
	Logger     *slog.Logger
	Timeouts   timeouts.Config

	Concurrency int

	// BaseCtx is the cancellation root for scheduled abort dials. When
	// set, Schedule ignores its ctx argument (handlers pass a
	// long-lived ctx anyway; the request ctx is already cancelled when
	// the goroutine starts). Wired post-construction by the entrypoint
	// because Run owns the lifecycle, not Build.
	BaseCtx context.Context

	semOnce sync.Once
	sem     chan struct{}
	wg      sync.WaitGroup
}

// Deps configures New. BaseCtx is set by the entrypoint after Build.
type Deps struct {
	Store       *store.DB
	Logger      *slog.Logger
	Timeouts    timeouts.Config
	HTTPClient  *http.Client  // nil -> &http.Client{Timeout: HTTPTimeout}
	HTTPTimeout time.Duration // applied only when HTTPClient is nil
	Concurrency int           // 0 -> 16
}

func New(d Deps) *Dialer {
	if d.Store == nil {
		panic("aborts.New: Store is required")
	}
	if d.Logger == nil {
		panic("aborts.New: Logger is required")
	}
	hc := d.HTTPClient
	if hc == nil {
		timeout := d.HTTPTimeout
		if timeout <= 0 {
			timeout = 6 * time.Second
		}
		hc = &http.Client{Timeout: timeout}
	}
	concurrency := d.Concurrency
	if concurrency <= 0 {
		concurrency = 16
	}
	return &Dialer{
		Store:       d.Store,
		HTTPClient:  hc,
		Logger:      d.Logger,
		Timeouts:    d.Timeouts,
		Concurrency: concurrency,
	}
}

func (d *Dialer) Schedule(ctx context.Context, deckID, attemptID string) {
	d.semOnce.Do(func() {
		c := d.Concurrency
		if c <= 0 {
			c = 16
		}
		d.sem = make(chan struct{}, c)
	})
	if d.BaseCtx != nil {
		ctx = d.BaseCtx
	}
	d.wg.Add(1)
	go d.dial(ctx, deckID, attemptID)
}

func (d *Dialer) Wait() {
	d.wg.Wait()
}

func (d *Dialer) dial(ctx context.Context, deckID, attemptID string) {
	defer d.wg.Done()
	select {
	case d.sem <- struct{}{}:
		defer func() { <-d.sem }()
	case <-ctx.Done():
		return
	}

	initial := d.Timeouts.AbortRetryInitial
	if initial <= 0 {
		initial = 1 * time.Second
	}
	budget := d.Timeouts.AbortRetryMaxDuration
	if budget <= 0 {
		budget = 5 * time.Minute
	}
	deadline := time.Now().Add(budget)
	backoff := initial

	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return
		}
		ok, retryable, err := d.dialOnce(ctx, deckID, attemptID)
		if ok {
			d.Logger.Info("abort: delivered", "deck_id", deckID, "attempt_id", attemptID, "attempts", attempt+1)
			return
		}
		if !retryable {
			d.Logger.Warn("abort: terminal non-retry", "deck_id", deckID, "attempt_id", attemptID, "err", err)
			return
		}
		if time.Now().After(deadline) {
			d.Logger.Warn("abort: gave up", "deck_id", deckID, "attempt_id", attemptID, "err", err)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func (d *Dialer) dialOnce(ctx context.Context, deckID, attemptID string) (ok bool, retryable bool, err error) {
	deck, err := d.Store.ReadQueries.GetDeck(ctx, deckID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, fmt.Errorf("deck %q not registered", deckID)
	}
	if err != nil {
		return false, true, err
	}
	if !deck.EndpointUrl.Valid || deck.EndpointUrl.String == "" {
		return false, true, errors.New("empty endpoint_url")
	}
	u, err := url.Parse(deck.EndpointUrl.String)
	if err != nil {
		return false, false, fmt.Errorf("parse endpoint_url: %w", err)
	}
	u.Path = "/executor/abort/" + attemptID

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, u.String(), bytes.NewReader(nil))
	if err != nil {
		return false, false, err
	}
	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return false, true, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return true, false, nil
	case resp.StatusCode == http.StatusNotFound:
		return true, false, nil
	default:
		return false, true, fmt.Errorf("status %d", resp.StatusCode)
	}
}
