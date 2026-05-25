package testutil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// AbortScheduler records Schedule calls for handler tests.
type AbortScheduler struct {
	mu    sync.Mutex
	Calls []AbortCall
}

type AbortCall struct {
	DeckID    string
	AttemptID string
}

func (s *AbortScheduler) Schedule(_ context.Context, deckID, attemptID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Calls = append(s.Calls, AbortCall{DeckID: deckID, AttemptID: attemptID})
}

func (s *AbortScheduler) Snapshot() []AbortCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AbortCall, len(s.Calls))
	copy(out, s.Calls)
	return out
}

// ExecutorServer is an httptest stub for /executor/state and /executor/abort/.
// Set StateFn/AbortFn before requests; nil handlers return 404.
type ExecutorServer struct {
	URL  string
	srv  *httptest.Server
	mu   sync.Mutex
	hits []ExecutorHit

	StateFn func(deckID string) (status int, body []byte)
	AbortFn func(attemptID string) (status int, body []byte)
}

type ExecutorHit struct {
	Method string
	Path   string
}

func NewExecutorServer(t *testing.T) *ExecutorServer {
	t.Helper()
	es := &ExecutorServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/executor/state", func(w http.ResponseWriter, r *http.Request) {
		es.recordHit(r)
		es.mu.Lock()
		fn := es.StateFn
		es.mu.Unlock()
		deckID := r.URL.Query().Get("deck_id")
		writeFakeResp(w, fn, deckID)
	})
	mux.HandleFunc("/executor/abort/", func(w http.ResponseWriter, r *http.Request) {
		es.recordHit(r)
		es.mu.Lock()
		fn := es.AbortFn
		es.mu.Unlock()
		attemptID := strings.TrimPrefix(r.URL.Path, "/executor/abort/")
		writeFakeResp(w, fn, attemptID)
	})
	es.srv = httptest.NewServer(mux)
	es.URL = es.srv.URL
	t.Cleanup(es.srv.Close)
	return es
}

func (es *ExecutorServer) Hits() []ExecutorHit {
	es.mu.Lock()
	defer es.mu.Unlock()
	out := make([]ExecutorHit, len(es.hits))
	copy(out, es.hits)
	return out
}

func (es *ExecutorServer) recordHit(r *http.Request) {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.hits = append(es.hits, ExecutorHit{Method: r.Method, Path: r.URL.Path})
}

func writeFakeResp(w http.ResponseWriter, fn func(string) (int, []byte), arg string) {
	if fn == nil {
		http.NotFound(w, &http.Request{URL: &url.URL{}})
		return
	}
	status, body := fn(arg)
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// RecordingTransport records outbound requests; RoundTripFn supplies responses (default 200).
type RecordingTransport struct {
	mu       sync.Mutex
	requests []*http.Request

	// RoundTripFn produces the response for each request. nil -> 200 empty body.
	RoundTripFn func(*http.Request) (*http.Response, error)
}

func (rt *RecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.requests = append(rt.requests, req.Clone(req.Context()))
	fn := rt.RoundTripFn
	rt.mu.Unlock()
	if fn == nil {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       http.NoBody,
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}
	return fn(req)
}

func (rt *RecordingTransport) Requests() []*http.Request {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := make([]*http.Request, len(rt.requests))
	copy(out, rt.requests)
	return out
}

func (rt *RecordingTransport) Client() *http.Client {
	return &http.Client{Transport: rt}
}
