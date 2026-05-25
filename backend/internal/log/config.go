// Package log builds *slog.Logger from Config loaded by the root config package.
package log

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	// Level is one of "debug", "info", "warn", "error". Case-insensitive.
	Level string `yaml:"level" env:"LOG_LEVEL" env-default:"info"`
	// Format is "text" or "json". Text is friendlier in a terminal during
	// dev; json is structured for log shipping in production.
	Format string `yaml:"format" env:"LOG_FORMAT" env-default:"text"`
}

// New builds a *slog.Logger from cfg; invalid level/format fail at boot.
func New(cfg Config) (*slog.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	handlerOpts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "text", "":
		handler = slog.NewTextHandler(os.Stderr, handlerOpts)
	case "json":
		handler = slog.NewJSONHandler(os.Stderr, handlerOpts)
	default:
		return nil, fmt.Errorf("log: unknown format %q (want text or json)", cfg.Format)
	}

	return slog.New(handler), nil
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info", "":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("log: unknown level %q", s)
	}
}
