package api

import "time"

// HTTP listener settings colocated with consumers of this package.
type Config struct {
	Addr string `yaml:"addr" env:"HTTP_ADDR" env-default:":8080"`

	// BodyLimitBytes caps inbound request bodies. API.md §8.1 specifies 1 MB.
	BodyLimitBytes int64 `yaml:"body_limit_bytes" env:"HTTP_BODY_LIMIT_BYTES" env-default:"1048576"`

	// Grace period before force-close on SIGINT/SIGTERM.
	ShutdownGrace time.Duration `yaml:"shutdown_grace" env:"HTTP_SHUTDOWN_GRACE" env-default:"5s"`

	// ReadHeaderTimeout protects against slow-header attacks.
	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout" env:"HTTP_READ_HEADER_TIMEOUT" env-default:"5s"`

	// PollHoldMax caps how long /executor/poll holds a connection waiting
	// for a dispatch. 0 disables long-polling and falls back to the legacy
	// 200/204-immediate behavior. Pick well under any upstream proxy or
	// LB idle timeout and below ReadHeaderTimeout headroom — the handler
	// returns 204 on deadline. Default 25s keeps an executor's poll cadence
	// matched to its heartbeat_interval default (no head-of-line blocking
	// of heartbeats). Backs S1 in analysis/inefficiencies/inefficiencies.md.
	PollHoldMax time.Duration `yaml:"poll_hold_max" env:"HTTP_POLL_HOLD_MAX" env-default:"25s"`
}

// CORS settings kept separate so origin allowlist changes stand out in review.
type CORSConfig struct {
	// Empty means no CORS headers; default matches Vite dev server origin.
	AllowedOrigins []string `yaml:"allowed_origins" env:"HTTP_CORS_ALLOWED_ORIGINS" env-default:"http://localhost:5173" env-separator:","`
}
