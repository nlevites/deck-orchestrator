package store

import (
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/pressly/goose/v3"

	migrations "deck-fleet/backend/sql/orchestrator"
)

func migrate(db *sql.DB, logger *slog.Logger) error {
	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(gooseLogger{logger: logger})
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}
	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

type gooseLogger struct {
	logger *slog.Logger
}

func (g gooseLogger) Fatalf(format string, v ...any) {
	g.logger.Error(fmt.Sprintf(format, v...))
}

func (g gooseLogger) Printf(format string, v ...any) {
	g.logger.Info(fmt.Sprintf(format, v...))
}
