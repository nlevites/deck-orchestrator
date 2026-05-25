// Package config loads env/YAML into subsystem Config structs. Imported only
// by cmd/orchestrator; subsystems receive their slice by value at construction.
package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/ilyakaznacheev/cleanenv"

	"deck-fleet/backend/internal/api"
	"deck-fleet/backend/internal/log"
	"deck-fleet/backend/internal/store"
	"deck-fleet/backend/internal/timeouts"
)

// Config aggregates subsystem configs. New subsystems add a field and an
// env-default-tagged struct in their package.
type Config struct {
	HTTP     api.Config      `yaml:"http"`
	CORS     api.CORSConfig  `yaml:"cors"`
	Store    store.Config    `yaml:"store"`
	Log      log.Config      `yaml:"log"`
	Timeouts timeouts.Config `yaml:"timeouts"`

	// FleetSize sets how many deck slots the orchestrator seeds at boot
	// (deck-1..deck-N). Slots are persistent identities owned by the
	// orchestrator; executors attach to them via heartbeat. Shrinking
	// fleet_size below the highest already-seeded non-decommissioned
	// number fails boot loudly rather than silently dropping rows.
	FleetSize int `yaml:"fleet_size" env:"FLEET_SIZE" env-default:"100"`
}

// Load reads the root config from disk + env. If path is empty, only env +
// defaults populate the struct (cleanenv.ReadEnv). If path is non-empty, the
// YAML file is loaded first and env layers on top (cleanenv.ReadConfig).
// In all cases the env-default tag on each field provides the floor.
func Load(path string) (*Config, error) {
	var cfg Config
	if path == "" {
		if err := cleanenv.ReadEnv(&cfg); err != nil {
			return nil, fmt.Errorf("config: read env: %w", err)
		}
		return &cfg, nil
	}

	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("config: file %q does not exist", path)
	} else if statErr != nil {
		return nil, fmt.Errorf("config: stat %q: %w", path, statErr)
	}

	if err := cleanenv.ReadConfig(path, &cfg); err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}
	return &cfg, nil
}
