package api

import (
	"log/slog"
	"net/http"
	"time"
)

// Flusher passthrough preserves SSE streaming.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			lrw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(lrw, r)
			logger.LogAttrs(r.Context(), slog.LevelInfo, "http",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", lrw.status),
				slog.Duration("duration", time.Since(start)),
				slog.String("request_id", RequestIDFromContext(r.Context())),
				slog.Int64("bytes_in", r.ContentLength),
				slog.Int64("bytes_out", lrw.bytesWritten),
			)
		})
	}
}

// loggingResponseWriter preserves http.Flusher so SSE handlers can flush.
type loggingResponseWriter struct {
	http.ResponseWriter
	status       int
	bytesWritten int64
	wroteHeader  bool
}

func (l *loggingResponseWriter) WriteHeader(code int) {
	if l.wroteHeader {
		return
	}
	l.status = code
	l.wroteHeader = true
	l.ResponseWriter.WriteHeader(code)
}

func (l *loggingResponseWriter) Write(b []byte) (int, error) {
	if !l.wroteHeader {
		l.WriteHeader(http.StatusOK)
	}
	n, err := l.ResponseWriter.Write(b)
	l.bytesWritten += int64(n)
	return n, err
}

func (l *loggingResponseWriter) Flush() {
	if f, ok := l.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
