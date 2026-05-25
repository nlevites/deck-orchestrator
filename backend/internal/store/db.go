// Package store owns SQLite access (modernc.org/sqlite, pressly/goose).
// Callers use *storegen.Queries or WithTx; domain code does not import the driver.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	_ "modernc.org/sqlite"

	storegen "deck-fleet/backend/internal/store/gen"
)

// DB holds separate write (1 conn) and read pools on one file. Single write conn
// serializes through SQLite's write lock; WAL gives readers a consistent snapshot
// during writes. ReadQueries fans out across the read pool.
type DB struct {
	Write       *sql.DB
	Read        *sql.DB
	Queries     *storegen.Queries
	ReadQueries *storegen.Queries
	logger      *slog.Logger
}

func Open(ctx context.Context, cfg Config, logger *slog.Logger) (*DB, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("store: empty database path")
	}

	dsn := buildDSN(cfg)

	writeDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open write pool: %w", err)
	}
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)

	readDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("store: open read pool: %w", err)
	}
	readDB.SetMaxOpenConns(cfg.MaxReadConns)
	readDB.SetMaxIdleConns(cfg.MaxReadConns)

	if err := applyPragmas(ctx, writeDB); err != nil {
		_ = writeDB.Close()
		_ = readDB.Close()
		return nil, fmt.Errorf("store: apply write pragmas: %w", err)
	}
	if err := applyPragmas(ctx, readDB); err != nil {
		_ = writeDB.Close()
		_ = readDB.Close()
		return nil, fmt.Errorf("store: apply read pragmas: %w", err)
	}

	if err := migrate(writeDB, logger); err != nil {
		_ = writeDB.Close()
		_ = readDB.Close()
		return nil, fmt.Errorf("store: migrate: %w", err)
	}

	return &DB{
		Write:       writeDB,
		Read:        readDB,
		Queries:     storegen.New(writeDB),
		ReadQueries: storegen.New(readDB),
		logger:      logger,
	}, nil
}

func (d *DB) Close() error {
	var firstErr error
	if d.Write != nil {
		if err := d.Write.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if d.Read != nil {
		if err := d.Read.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// buildDSN puts busy_timeout in the DSN (per-conn, no round-trip). Other pragmas
// use Exec because modernc's query-string pragma support is limited.
func buildDSN(cfg Config) string {
	return fmt.Sprintf("file:%s?_pragma=busy_timeout(%d)", cfg.Path, cfg.BusyTimeout.Milliseconds())
}

// applyPragmas runs the DATA_MODEL.md §3 set on every pool connection via db.Conn().
// modernc has no public connect-hook; pool MaxOpen/MaxIdle is fixed at open time.
func applyPragmas(ctx context.Context, db *sql.DB) error {
	// WAL is global but set on each conn anyway.
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA mmap_size = 268435456",
	}

	stats := db.Stats()
	maxOpen := stats.MaxOpenConnections
	if maxOpen <= 0 {
		maxOpen = 1
	}

	conns := make([]*sql.Conn, 0, maxOpen)
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	for i := 0; i < maxOpen; i++ {
		c, err := db.Conn(ctx)
		if err != nil {
			return fmt.Errorf("acquire conn %d: %w", i, err)
		}
		conns = append(conns, c)
		for _, p := range pragmas {
			if _, err := c.ExecContext(ctx, p); err != nil {
				return fmt.Errorf("conn %d %q: %w", i, p, err)
			}
		}
	}
	return nil
}
