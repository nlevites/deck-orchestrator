package store

import "time"

// Config is SQLite connection settings for store.Open. Composed by internal/config.
type Config struct {
	// SQLite file path (created on first open; parent dir must exist).
	Path string `yaml:"path" env:"DB_PATH" env-default:"./orchestrator.db"`
	// Max wait for a write lock before SQLITE_BUSY.
	BusyTimeout time.Duration `yaml:"busy_timeout" env:"DB_BUSY_TIMEOUT" env-default:"5s"`
	// Read pool size. Write pool is always 1 (WAL allows concurrent readers).
	MaxReadConns int `yaml:"max_read_conns" env:"DB_MAX_READ_CONNS" env-default:"8"`
}
