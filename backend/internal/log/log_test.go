package log_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"deck-fleet/backend/internal/log"
)

func TestNew_defaultConfig_returnsLogger(t *testing.T) {
	l, err := log.New(log.Config{Level: "info", Format: "text"})
	require.NoError(t, err)
	require.NotNil(t, l)
}

func TestNew_jsonFormat_returnsLogger(t *testing.T) {
	l, err := log.New(log.Config{Level: "debug", Format: "json"})
	require.NoError(t, err)
	require.NotNil(t, l)
}

func TestNew_allLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "warning", "error", ""} {
		_, err := log.New(log.Config{Level: level, Format: "text"})
		require.NoError(t, err, "level=%q", level)
	}
}

func TestNew_unknownLevel_returnsError(t *testing.T) {
	_, err := log.New(log.Config{Level: "verbose", Format: "text"})
	require.Error(t, err)
}

func TestNew_unknownFormat_returnsError(t *testing.T) {
	_, err := log.New(log.Config{Level: "info", Format: "yaml"})
	require.Error(t, err)
}
