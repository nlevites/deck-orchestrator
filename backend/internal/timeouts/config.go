// Package timeouts centralizes orchestrator timing constants in one Config
// struct so relationships stay legible and each value is env-overridable.
package timeouts

import "time"

type Config struct {
	// HeartbeatInterval is the cadence the executor uses; the orchestrator's
	// staleness threshold is a multiple of this.
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval" env:"HEARTBEAT_INTERVAL" env-default:"5s"`

	// StaleThreshold is when the orchestrator demotes a deck from HEALTHY to
	// STALE (no heartbeat received within this window).
	StaleThreshold time.Duration `yaml:"stale_threshold" env:"STALE_THRESHOLD" env-default:"30s"`

	// AmbiguousDeadline is how long an unreachable in-flight attempt stays
	// in flight before being declared AMBIGUOUS.
	AmbiguousDeadline time.Duration `yaml:"ambiguous_deadline" env:"AMBIGUOUS_DEADLINE" env-default:"2m"`

	// AttemptDeadlineBase is the constant headroom of the per-attempt
	// hang-detection ceiling. The effective deadline for an attempt is
	// `Base + total_steps * PerStep`, so a 50-step DAG and a 1-step DAG
	// don't share a clock. Attempts past their effective deadline trigger
	// a reconciliation pull; if the executor still reports IN_PROGRESS
	// the attempt is declared AMBIGUOUS (reason DEADLINE_EXCEEDED).
	AttemptDeadlineBase time.Duration `yaml:"attempt_deadline_base" env:"ATTEMPT_DEADLINE_BASE" env-default:"30m"`

	// AttemptDeadlinePerStep is added per step in the attempt's job. Set
	// to 0 to disable scaling and use AttemptDeadlineBase as a flat
	// ceiling (the legacy behaviour).
	AttemptDeadlinePerStep time.Duration `yaml:"attempt_deadline_per_step" env:"ATTEMPT_DEADLINE_PER_STEP" env-default:"0s"`

	// AbortRetryInitial is the first backoff step for failed abort delivery.
	AbortRetryInitial time.Duration `yaml:"abort_retry_initial" env:"ABORT_RETRY_INITIAL" env-default:"1s"`

	// AbortRetryMaxDuration is how long the orchestrator keeps retrying
	// abort delivery before giving up.
	AbortRetryMaxDuration time.Duration `yaml:"abort_retry_max_duration" env:"ABORT_RETRY_MAX_DURATION" env-default:"5m"`

	// ReconcileRequestTimeout caps each reconciler dial to /executor/state.
	// Distinct from the underlying *http.Client.Timeout so the reconciler
	// can use a shorter ctx-deadline than the connection-pool default.
	ReconcileRequestTimeout time.Duration `yaml:"reconcile_request_timeout" env:"RECONCILE_REQUEST_TIMEOUT" env-default:"5s"`

	// ReconcileHTTPTimeout is the *http.Client.Timeout for the
	// reconciler's outbound client. Should be > ReconcileRequestTimeout
	// so a slow connection-establish surfaces as a ctx-deadline rather
	// than a transport timeout.
	ReconcileHTTPTimeout time.Duration `yaml:"reconcile_http_timeout" env:"RECONCILE_HTTP_TIMEOUT" env-default:"6s"`

	// LivenessSweepInterval is the cadence the liveness Monitor sweeps
	// stale heartbeats and overdue attempts. Compressed at test time
	// via the harness; production runs at this default.
	LivenessSweepInterval time.Duration `yaml:"liveness_sweep_interval" env:"LIVENESS_SWEEP_INTERVAL" env-default:"2s"`

	// AbortHTTPTimeout is the *http.Client.Timeout for the abort
	// dialer's outbound client.
	AbortHTTPTimeout time.Duration `yaml:"abort_http_timeout" env:"ABORT_HTTP_TIMEOUT" env-default:"6s"`

	// AbortConcurrency caps concurrent abort dials (default 16).
	AbortConcurrency int `yaml:"abort_concurrency" env:"ABORT_CONCURRENCY" env-default:"16"`
}
