package api

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// gzipMinSize: below this, the wire savings don't outweigh the CPU cost
// or the per-response gzip dictionary overhead. Picked to be just over a
// typical bootstrap-with-no-runs `/api/state` body (~600 B); the routes
// the analysis flagged as worth compressing (`/api/state` at scale,
// `/api/runs/{id}`) are well above this floor.
const gzipMinSize = 1024

// Compress wraps responses with gzip when the client advertises
// `Accept-Encoding: gzip` AND the buffered body crosses gzipMinSize.
// Small responses (heartbeats, 204s, tiny error envelopes) pass through
// untouched so we don't pay encode cost where the win is zero.
//
// Implementation: buffer the response in memory; on Flush or completion
// decide whether to gzip. This is fine because the orchestrator's
// hot-path payloads are bounded (body_limit_bytes caps inbound at 1 MiB
// and responses follow the same order of magnitude). For streaming
// endpoints we'd want a different approach.
//
// Backs inefficiency S8 in analysis/inefficiencies/inefficiencies.md.
func Compress() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !acceptsGzip(r) {
				next.ServeHTTP(w, r)
				return
			}
			cw := newCompressWriter(w)
			next.ServeHTTP(cw, r)
			cw.finalize()
		})
	}
}

func acceptsGzip(r *http.Request) bool {
	for _, enc := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if strings.TrimSpace(strings.Split(enc, ";")[0]) == "gzip" {
			return true
		}
	}
	return false
}

// gzipPool reuses encoders across requests to skip the dictionary
// allocation. Reset(w) before every use; Close after.
var gzipPool = sync.Pool{
	New: func() any { return gzip.NewWriter(io.Discard) },
}

type compressWriter struct {
	http.ResponseWriter
	buf         bytes.Buffer
	status      int
	wroteHeader bool
}

func newCompressWriter(w http.ResponseWriter) *compressWriter {
	return &compressWriter{ResponseWriter: w, status: http.StatusOK}
}

func (c *compressWriter) WriteHeader(code int) {
	// Defer the real WriteHeader until finalize() — we may need to
	// inject Content-Encoding and erase Content-Length.
	c.status = code
	c.wroteHeader = true
}

func (c *compressWriter) Write(p []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	return c.buf.Write(p)
}

func (c *compressWriter) finalize() {
	body := c.buf.Bytes()
	h := c.Header()

	// Don't double-encode if a handler set its own Content-Encoding.
	if h.Get("Content-Encoding") != "" || len(body) < gzipMinSize {
		// Pass through verbatim. If Content-Length was set before
		// Write, the runtime already honored it; otherwise net/http
		// will set chunked / size from the Write call below.
		h.Del("Content-Length") // recompute in case the handler set a stale length
		c.ResponseWriter.WriteHeader(c.status)
		if len(body) > 0 {
			_, _ = c.ResponseWriter.Write(body)
		}
		return
	}

	enc := gzipPool.Get().(*gzip.Writer)
	defer gzipPool.Put(enc)

	var encoded bytes.Buffer
	enc.Reset(&encoded)
	_, _ = enc.Write(body)
	_ = enc.Close()

	h.Set("Content-Encoding", "gzip")
	h.Set("Vary", "Accept-Encoding")
	h.Del("Content-Length") // compressed length differs; let net/http set it
	c.ResponseWriter.WriteHeader(c.status)
	_, _ = c.ResponseWriter.Write(encoded.Bytes())
}

// Flush is a no-op while we're buffering. The hot-path handlers in this
// service don't stream — see the Logging middleware comment for the SSE
// caveat. If a streaming handler is added later, gate it out of this
// middleware via a header check or wrap conditionally in NewServer.
func (c *compressWriter) Flush() {}
