// Package integration holds the deck-fleet backend integration test
// suite. Everything in this file is the conftest-style harness shared
// by every *_test.go sibling: bootstrapping a real in-process
// orchestrator + N real in-process executors, talking over httptest
// servers, and the typed client and wait helpers each test uses.
//
// Design goals (kept honest by the corpus that imports this file):
//
//   - Configuration is the exact same struct used by the binaries.
//     Orchestrator config is *config.Config (the production aggregator);
//     each executor's spec embeds worker.Config and outbox.Config
//     verbatim. The harness never re-defines fields a binary already
//     reads — it just constructs the same structs with test-friendly
//     defaults that callers can override field-by-field.
//   - One file. Everything lives here so a new test can grep one place
//     to understand the harness.
//   - In-process. We reuse api.NewServer, store.Open, worker.New,
//     outbox.New, server.New — every constructor cmd/orchestrator and
//     cmd/executor use. The test wiring and the production wiring
//     point at the same code.
package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"deck-fleet/backend/internal/api"
	apigen "deck-fleet/backend/internal/api/gen"
	"deck-fleet/backend/internal/app"
	"deck-fleet/backend/internal/config"
	"deck-fleet/backend/internal/executor/chaos"
	"deck-fleet/backend/internal/executor/client"
	execconfig "deck-fleet/backend/internal/executor/config"
	"deck-fleet/backend/internal/executor/localstore"
	"deck-fleet/backend/internal/executor/outbox"
	"deck-fleet/backend/internal/executor/worker"
	"deck-fleet/backend/internal/store"
	storegen "deck-fleet/backend/internal/store/gen"
	"deck-fleet/backend/internal/timeouts"
)

type harnessOptions struct {
	Orchestrator *config.Config
	Executors    []executorSpec
	LogLevel     slog.Level
	// Default true; degraded_mode_test sets false to observe 503 while DEGRADED_MODE is set.
	AwaitDegraded bool
}

type executorSpec struct {
	Worker worker.Config
	Outbox outbox.Config
	Chaos  chaos.InitialState
	DBPath string
	// Replaces os.Exit(1) on crash-after-step; nil panics so tests without OnCrash fail loudly.
	OnCrash func()
}

func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }

// Compressed timings so failure-mode tests converge in under a minute.
func defaultTimeouts() timeouts.Config {
	return timeouts.Config{
		HeartbeatInterval:       100 * time.Millisecond,
		StaleThreshold:          500 * time.Millisecond,
		AmbiguousDeadline:       1 * time.Second,
		AttemptDeadlineBase:     1500 * time.Millisecond,
		AbortRetryInitial:       50 * time.Millisecond,
		AbortRetryMaxDuration:   2 * time.Second,
		ReconcileRequestTimeout: 1 * time.Second,
		ReconcileHTTPTimeout:    2 * time.Second,
		LivenessSweepInterval:   100 * time.Millisecond,
		AbortHTTPTimeout:        2 * time.Second,
		AbortConcurrency:        16,
	}
}

// FleetSize 16 pre-seeds deck-1..deck-16 without bloating /api/state responses.
func defaultOrchestratorConfig(t testing.TB) *config.Config {
	t.Helper()
	dir := t.TempDir()
	return &config.Config{
		HTTP: api.Config{
			Addr:              "", // httptest assigns the listener
			BodyLimitBytes:    1 << 20,
			ShutdownGrace:     200 * time.Millisecond,
			ReadHeaderTimeout: 1 * time.Second,
		},
		CORS: api.CORSConfig{AllowedOrigins: nil},
		Store: store.Config{
			Path:         filepath.Join(dir, "orchestrator.db"),
			BusyTimeout:  5 * time.Second,
			MaxReadConns: 4,
		},
		Timeouts:  defaultTimeouts(),
		FleetSize: 16,
	}
}

func defaultExecutorSpec(deckID string) executorSpec {
	return executorSpec{
		Worker: worker.Config{
			DeckID:            deckID,
			HeartbeatInterval: 100 * time.Millisecond,
			PollInterval:      50 * time.Millisecond,
			StepDuration:      50 * time.Millisecond,
		},
		Outbox: outbox.Config{
			Initial: 50 * time.Millisecond,
			Max:     500 * time.Millisecond,
		},
	}
}

func specsFor(deckIDs ...string) []executorSpec {
	out := make([]executorSpec, len(deckIDs))
	for i, id := range deckIDs {
		out[i] = defaultExecutorSpec(id)
	}
	return out
}

type Harness struct {
	T             testing.TB
	Logger        *slog.Logger
	Orch          *Orchestrator
	Executors     map[string]*Executor
	Client        *Client
	awaitDegraded bool
}

// Goose uses package-global state; serialize migrations during parallel test setup.
var migrationMu sync.Mutex

func newHarness(t testing.TB, opts harnessOptions) *Harness {
	t.Helper()

	level := opts.LogLevel
	if level == 0 {
		level = slog.LevelWarn
	}
	logger := newTestLogger(t, level)

	if opts.Orchestrator == nil {
		opts.Orchestrator = defaultOrchestratorConfig(t)
	}

	h := &Harness{
		T:             t,
		Logger:        logger,
		Executors:     make(map[string]*Executor),
		awaitDegraded: opts.AwaitDegraded,
	}

	orch, err := startOrchestrator(t, logger, opts.Orchestrator)
	if err != nil {
		t.Fatalf("harness: start orchestrator: %v", err)
	}
	h.Orch = orch
	h.Client = newClient(orch.URL())

	for i := range opts.Executors {
		spec := opts.Executors[i]
		if spec.Worker.DeckID == "" {
			t.Fatalf("harness: executor[%d] has empty DeckID", i)
		}
		if spec.DBPath == "" {
			spec.DBPath = filepath.Join(t.TempDir(), "executor-"+spec.Worker.DeckID+".db")
		}
		ex, exErr := startExecutor(t, logger, orch.URL(), spec)
		if exErr != nil {
			t.Fatalf("harness: start executor %q: %v", spec.Worker.DeckID, exErr)
		}
		h.Executors[spec.Worker.DeckID] = ex
	}

	if opts.AwaitDegraded {
		waitForFalse(t, orch.Degraded, 3*time.Second, "orchestrator stayed in DEGRADED_MODE")
	}

	// Wait for first heartbeat so dispatch is deterministic on immediate POST /api/runs.
	for deckID := range h.Executors {
		h.WaitForDeckHealth(t, deckID, apigen.HEALTHY, 2*time.Second)
	}

	return h
}

type Orchestrator struct {
	*app.Orchestrator

	server *httptest.Server
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex // guards server during Restart
}

func (o *Orchestrator) URL() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.server.URL
}

func startOrchestrator(t testing.TB, logger *slog.Logger, cfg *config.Config) (*Orchestrator, error) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	migrationMu.Lock()
	built, err := app.BuildOrchestrator(ctx, cfg, logger)
	migrationMu.Unlock()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("app.BuildOrchestrator: %w", err)
	}

	// Production default (2s) is too slow for failure-mode tests.
	built.SetMonitorSweepInterval(100 * time.Millisecond)

	o := &Orchestrator{Orchestrator: built}
	o.cancel = cancel
	o.server = httptest.NewServer(built.Handler)

	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		built.Run(ctx)
	}()

	t.Cleanup(func() {
		o.cancel()
		o.server.Close()
		o.wg.Wait()
		if sErr := built.Shutdown(); sErr != nil {
			logger.Warn("harness: orchestrator shutdown", "err", sErr)
		}
	})

	return o, nil
}

// Restart preserves on-disk SQLite; repoints executor egress and Client to the new listener.
func (h *Harness) Restart(t testing.TB) {
	t.Helper()

	cfg := h.Orch.Cfg
	logger := h.Logger

	h.Orch.cancel()
	h.Orch.server.Close()
	h.Orch.wg.Wait()
	if err := h.Orch.Shutdown(); err != nil {
		t.Fatalf("harness Restart: shutdown: %v", err)
	}

	newOrch, err := startOrchestrator(t, logger, cfg)
	if err != nil {
		t.Fatalf("harness Restart: %v", err)
	}
	h.Orch = newOrch
	h.Client.Rebind(newOrch.URL())
	newTarget, err := url.Parse(newOrch.URL())
	if err != nil {
		t.Fatalf("harness Restart: parse new orchestrator URL: %v", err)
	}
	for _, ex := range h.Executors {
		ex.egress.SetTarget(newTarget)
		ex.orchURL = newOrch.URL()
	}

	if h.awaitDegraded {
		waitForFalse(t, newOrch.Degraded, 3*time.Second, "orchestrator stayed in DEGRADED_MODE after Restart")
	}
}

type Executor struct {
	Spec executorSpec

	Local      *localstore.Store
	HTTPClient *client.Client
	BaseURL    string
	Chaos      *chaos.State

	server   *httptest.Server
	egress   *switchableRoundTripper
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	orchURL  string
	logger   *slog.Logger
	specOrig executorSpec
	t        testing.TB
}

// PauseEgress drops executor -> orchestrator traffic until ResumeEgress.
func (e *Executor) PauseEgress() { e.Chaos.Apply(chaos.InitialState{PauseEgress: boolPtr(true)}) }

func (e *Executor) ResumeEgress() { e.Chaos.Apply(chaos.InitialState{PauseEgress: boolPtr(false)}) }

// PauseHTTPServer drops orchestrator -> executor traffic; ingress closes connections immediately.
func (e *Executor) PauseHTTPServer() { e.Chaos.Apply(chaos.InitialState{PauseIngress: boolPtr(true)}) }

func (e *Executor) ResumeHTTPServer() {
	e.Chaos.Apply(chaos.InitialState{PauseIngress: boolPtr(false)})
}

// Restart reopens the same on-disk SQLite file (preserves attempts + outbox).
func (e *Executor) Restart(t testing.TB) {
	t.Helper()

	e.cancel()
	e.server.Close()
	e.wg.Wait()
	if err := e.Local.Close(); err != nil {
		t.Fatalf("executor %q Restart: close local: %v", e.Spec.Worker.DeckID, err)
	}

	if err := bringUpExecutor(t, e, e.specOrig); err != nil {
		t.Fatalf("executor %q Restart: %v", e.Spec.Worker.DeckID, err)
	}
}

func startExecutor(t testing.TB, logger *slog.Logger, orchURL string, spec executorSpec) (*Executor, error) {
	t.Helper()
	deckLogger := logger.With("deck_id", spec.Worker.DeckID)
	e := &Executor{
		orchURL:  orchURL,
		logger:   deckLogger,
		specOrig: spec,
		t:        t,
	}
	if err := bringUpExecutor(t, e, spec); err != nil {
		return nil, err
	}
	t.Cleanup(func() {
		e.cancel()
		e.server.Close()
		e.wg.Wait()
		if cErr := e.Local.Close(); cErr != nil {
			deckLogger.Warn("executor close local", "err", cErr)
		}
	})
	return e, nil
}

// bringUpExecutor translates executorSpec into execconfig.Config and calls app.BuildExecutor.
// switchableRoundTripper sits between the worker client and chaos.EgressTransport so Restart
// can swap orchestrator URLs without racing the poll loop on client.BaseURL.
func bringUpExecutor(t testing.TB, e *Executor, spec executorSpec) error {
	t.Helper()
	deckLogger := e.logger

	ctx, cancel := context.WithCancel(context.Background())

	onCrash := spec.OnCrash
	if onCrash == nil {
		onCrash = func() {
			t.Errorf("executor %q: CrashAfterStep fired with no OnCrash wired", spec.Worker.DeckID)
		}
	}
	onFatalExit := func(code int) {
		t.Errorf("executor %q: fleet-rejected heartbeat (exit code %d) in a test that did not opt in", spec.Worker.DeckID, code)
	}

	httpSrv := httptest.NewUnstartedServer(http.NotFoundHandler())
	advertiseURL := "http://" + httpSrv.Listener.Addr().String()

	cfg := &execconfig.Config{
		DeckID: spec.Worker.DeckID,
		// Stable sentinel; switchableRoundTripper rewrites the host on every outbound request.
		OrchestratorURL: harnessOrchestratorURL,
		ListenAddr:      ":0", // required by BuildExecutor.Validate; harness serves via httptest instead
		AdvertiseURL:    advertiseURL,
		DBPath:          spec.DBPath,
		Worker:          spec.Worker,
		Outbox:          spec.Outbox,
		Chaos:           spec.Chaos,
	}

	migrationMu.Lock()
	built, err := app.BuildExecutor(ctx, cfg, deckLogger, onCrash, onFatalExit)
	migrationMu.Unlock()
	if err != nil {
		cancel()
		httpSrv.Close()
		return fmt.Errorf("app.BuildExecutor: %w", err)
	}

	httpSrv.Config.Handler = built.Handler
	httpSrv.Start()

	target, err := url.Parse(e.orchURL)
	if err != nil {
		cancel()
		httpSrv.Close()
		return fmt.Errorf("parse orchestrator URL: %w", err)
	}
	egress := &switchableRoundTripper{
		inner:  built.Client.HTTPClient.Transport,
		target: target,
	}
	if egress.inner == nil {
		egress.inner = http.DefaultTransport
	}
	built.Client.HTTPClient.Transport = egress

	e.Spec = spec
	e.Local = built.Local
	e.HTTPClient = built.Client
	e.BaseURL = httpSrv.URL
	e.Chaos = built.Chaos
	e.server = httpSrv
	e.egress = egress
	e.cancel = cancel
	e.specOrig = spec

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		built.Run(ctx)
	}()
	return nil
}

// switchableRoundTripper retargets outbound requests to the orchestrator's current httptest port.
const harnessOrchestratorURL = "http://orchestrator.harness.local"

type switchableRoundTripper struct {
	inner  http.RoundTripper
	mu     sync.RWMutex
	target *url.URL // current orchestrator URL; never nil after init
}

func (s *switchableRoundTripper) SetTarget(u *url.URL) {
	s.mu.Lock()
	s.target = u
	s.mu.Unlock()
}

func (s *switchableRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.RLock()
	target := s.target
	s.mu.RUnlock()
	if target != nil {
		nreq := req.Clone(req.Context())
		nreq.URL.Scheme = target.Scheme
		nreq.URL.Host = target.Host
		nreq.Host = target.Host
		return s.inner.RoundTrip(nreq)
	}
	return s.inner.RoundTrip(req)
}

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// Response avoids returning *http.Response after Client.do drains and closes the body.
type Response struct {
	StatusCode int
	Header     http.Header
}

func newClient(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *Client) Rebind(baseURL string) { c.BaseURL = baseURL }

func (c *Client) SubmitRunFromFile(t testing.TB, sampleName string) apigen.Run {
	t.Helper()
	body := loadSample(t, sampleName)
	resp, rawBody := c.SubmitRunJSON(t, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("SubmitRunFromFile %q: status %d body=%s", sampleName, resp.StatusCode, rawBody)
	}
	var run apigen.Run
	if err := json.Unmarshal(rawBody, &run); err != nil {
		t.Fatalf("SubmitRunFromFile %q: decode: %v body=%s", sampleName, err, rawBody)
	}
	return run
}

func (c *Client) SubmitRunJSON(t testing.TB, body []byte) (*Response, []byte) {
	t.Helper()
	return c.do(t, http.MethodPost, "/api/runs", body)
}

func (c *Client) GetRun(t testing.TB, runID string) apigen.Run {
	t.Helper()
	resp, body := c.do(t, http.MethodGet, "/api/runs/"+runID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GetRun %q: status %d body=%s", runID, resp.StatusCode, body)
	}
	var run apigen.Run
	if err := json.Unmarshal(body, &run); err != nil {
		t.Fatalf("GetRun %q: decode: %v", runID, err)
	}
	return run
}

func (c *Client) ListDecks(t testing.TB) []apigen.Deck {
	t.Helper()
	resp, body := c.do(t, http.MethodGet, "/api/decks", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ListDecks: status %d body=%s", resp.StatusCode, body)
	}
	var env struct {
		Decks []apigen.Deck `json:"decks"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("ListDecks: decode: %v", err)
	}
	return env.Decks
}

func (c *Client) ListRuns(t testing.TB) []apigen.RunSummary {
	t.Helper()
	resp, body := c.do(t, http.MethodGet, "/api/runs", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ListRuns: status %d body=%s", resp.StatusCode, body)
	}
	var env struct {
		Runs []apigen.RunSummary `json:"runs"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("ListRuns: decode: %v", err)
	}
	return env.Runs
}

func (c *Client) CancelRun(t testing.TB, runID string, version int64) (*Response, []byte) {
	t.Helper()
	body := mustJSON(t, apigen.CancelRunRequest{ExpectedVersion: version})
	return c.do(t, http.MethodPost, "/api/runs/"+runID+"/cancel", body)
}

func (c *Client) RetryJob(t testing.TB, runID, jobID string, version int64) (*Response, []byte) {
	t.Helper()
	body := mustJSON(t, apigen.RetryJobRequest{ExpectedVersion: version})
	return c.do(t, http.MethodPost, "/api/runs/"+runID+"/jobs/"+jobID+"/retry", body)
}

func (c *Client) ResolveJob(t testing.TB, runID, jobID string, version int64, resolution apigen.AttemptOutcome, note string) (*Response, []byte) {
	t.Helper()
	req := apigen.ResolveJobRequest{ExpectedVersion: version, Resolution: resolution}
	if note != "" {
		req.OperatorNote = &note
	}
	body := mustJSON(t, req)
	return c.do(t, http.MethodPost, "/api/runs/"+runID+"/jobs/"+jobID+"/resolve", body)
}

// sinceSeq=0 returns bootstrap snapshot; sinceSeq>0 returns events with seq > sinceSeq.
func (c *Client) GetState(t testing.TB, sinceSeq int64) apigen.StateSnapshot {
	t.Helper()
	path := fmt.Sprintf("/api/state?since_seq=%d", sinceSeq)
	resp, body := c.do(t, http.MethodGet, path, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GetState since=%d: status %d body=%s", sinceSeq, resp.StatusCode, body)
	}
	var snap apigen.StateSnapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatalf("GetState since=%d: decode: %v body=%s", sinceSeq, err, body)
	}
	return snap
}

func (c *Client) GetRunState(t testing.TB, runID string, sinceSeq int64) (*Response, apigen.RunStateSnapshot, []byte) {
	t.Helper()
	path := fmt.Sprintf("/api/runs/%s/state?since_seq=%d", runID, sinceSeq)
	resp, body := c.do(t, http.MethodGet, path, nil)
	var snap apigen.RunStateSnapshot
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(body, &snap); err != nil {
			t.Fatalf("GetRunState %s since=%d: decode: %v body=%s", runID, sinceSeq, err, body)
		}
	}
	return resp, snap, body
}

func (c *Client) do(t testing.TB, method, path string, body []byte) (*Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, reader)
	if err != nil {
		t.Fatalf("client.do: build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		t.Fatalf("client.do %s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("client.do %s %s: read body: %v", method, path, err)
	}
	return &Response{StatusCode: resp.StatusCode, Header: resp.Header.Clone()}, raw
}

type apiErrorEnvelope struct {
	Error struct {
		Code      apigen.ErrorCode       `json:"code"`
		Message   string                 `json:"message"`
		RequestID string                 `json:"request_id"`
		Details   map[string]any         `json:"details,omitempty"`
		Raw       map[string]any         `json:"-"`
		_         struct{}               // forbid struct-key copies
		Extras    map[string]interface{} `json:"-"`
	} `json:"error"`
}

func decodeError(t testing.TB, body []byte) apiErrorEnvelope {
	t.Helper()
	var env apiErrorEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decodeError: %v body=%s", err, body)
	}
	return env
}

const pollInterval = 20 * time.Millisecond

func eventually(t testing.TB, total time.Duration, fn func() bool, msgf string, args ...any) {
	t.Helper()
	deadline := time.Now().Add(total)
	for {
		if fn() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("eventually: timed out after %s: "+msgf, append([]any{total}, args...)...)
		}
		time.Sleep(pollInterval)
	}
}

func waitForFalse(t testing.TB, b *atomic.Bool, total time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(total)
	for b.Load() {
		if time.Now().After(deadline) {
			t.Fatalf("waitForFalse: %s after %s", msg, total)
		}
		time.Sleep(pollInterval)
	}
}

func (h *Harness) WaitForRunStatus(t testing.TB, runID string, want apigen.RunStatus, timeout time.Duration) apigen.Run {
	t.Helper()
	var last apigen.Run
	eventually(t, timeout, func() bool {
		last = h.Client.GetRun(t, runID)
		return last.Status == want
	}, "run %q never reached %s (last=%s)", runID, want, last.Status)
	return last
}

func (h *Harness) WaitForJobStatus(t testing.TB, runID, jobID string, want apigen.DeckJobStatus, timeout time.Duration) apigen.DeckJob {
	t.Helper()
	var lastJob apigen.DeckJob
	eventually(t, timeout, func() bool {
		run := h.Client.GetRun(t, runID)
		for _, j := range run.DeckJobs {
			if j.Id == jobID {
				lastJob = j
				return j.Status == want
			}
		}
		return false
	}, "job %s/%s never reached %s (last=%s)", runID, jobID, want, lastJob.Status)
	return lastJob
}

func (h *Harness) WaitForDeckHealth(t testing.TB, deckID string, want apigen.DeckHealth, timeout time.Duration) apigen.Deck {
	t.Helper()
	var last apigen.Deck
	eventually(t, timeout, func() bool {
		decks := h.Client.ListDecks(t)
		for _, d := range decks {
			if d.Id == deckID {
				last = d
				return d.LastKnownHealth == want
			}
		}
		return false
	}, "deck %q never reached %s (last=%s)", deckID, want, last.LastKnownHealth)
	return last
}

// EventRow mirrors the events table; no public list API exists.
type EventRow struct {
	Seq        int64
	OccurredAt int64
	Kind       string
	RunID      string
	JobID      string
	DeckID     string
	AttemptID  string
	Payload    string
}

func (h *Harness) ListEvents(t testing.TB) []EventRow {
	t.Helper()
	rows, err := h.Orch.DB().Read.QueryContext(context.Background(),
		`SELECT seq, occurred_at, kind,
		        COALESCE(run_id,''), COALESCE(job_id,''), COALESCE(deck_id,''), COALESCE(attempt_id,''),
		        payload
		 FROM events ORDER BY seq ASC`)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []EventRow
	for rows.Next() {
		var e EventRow
		if err := rows.Scan(&e.Seq, &e.OccurredAt, &e.Kind, &e.RunID, &e.JobID, &e.DeckID, &e.AttemptID, &e.Payload); err != nil {
			t.Fatalf("ListEvents scan: %v", err)
		}
		out = append(out, e)
	}
	return out
}

func (h *Harness) WaitForEvent(t testing.TB, predicate func(EventRow) bool, timeout time.Duration) EventRow {
	t.Helper()
	var matched EventRow
	found := false
	eventually(t, timeout, func() bool {
		for _, e := range h.ListEvents(t) {
			if predicate(e) {
				matched = e
				found = true
				return true
			}
		}
		return false
	}, "no event matched predicate within %s", timeout)
	_ = found
	return matched
}

func loadSample(t testing.TB, name string) []byte {
	t.Helper()
	p := filepath.Join(repoRoot(t), "backend", "integration", "testdata", name+".json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("loadSample %q: %v", p, err)
	}
	return b
}

func repoRoot(t testing.TB) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("repoRoot: runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "Makefile")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "backend", "integration")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repoRoot: walked to filesystem root from %s", filepath.Dir(thisFile))
		}
		dir = parent
	}
}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func (h *Harness) DBQueries() *storegen.Queries { return h.Orch.DB().ReadQueries }

type testLogHandler struct {
	t     testing.TB
	level slog.Level
	attrs []slog.Attr
	group string
}

func newTestLogger(t testing.TB, level slog.Level) *slog.Logger {
	return slog.New(&testLogHandler{t: t, level: level})
}

func (h *testLogHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level
}

func (h *testLogHandler) Handle(_ context.Context, r slog.Record) error {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "[%s] %s", r.Level, r.Message)
	for _, a := range h.attrs {
		fmt.Fprintf(&buf, " %s=%v", a.Key, a.Value.Any())
	}
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&buf, " %s=%v", a.Key, a.Value.Any())
		return true
	})
	// Recover so a late log from a background goroutine after t.Cleanup does not panic the runtime.
	defer func() { _ = recover() }()
	h.t.Log(buf.String())
	return nil
}

func (h *testLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := *h
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &next
}

func (h *testLogHandler) WithGroup(name string) slog.Handler {
	next := *h
	next.group = name
	return &next
}

func jsonContains(t testing.TB, payload []byte, key, value string) bool {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return false
	}
	v, ok := m[key]
	if !ok {
		return false
	}
	s, ok := v.(string)
	if !ok {
		return false
	}
	return s == value
}

func nullString(ns sql.NullString) string {
	if !ns.Valid {
		return ""
	}
	return ns.String
}

func urlMust(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

var _ = []any{(*sql.DB)(nil), (*url.URL)(nil), (*storegen.Queries)(nil), nullString, urlMust, jsonContains}
