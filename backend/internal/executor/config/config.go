// Package config is the executor's root configuration (cleanenv + optional YAML).
// Runtime chaos seeds from Chaos at boot; harness sets fields directly before BuildExecutor.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"

	"deck-fleet/backend/internal/executor/chaos"
	"deck-fleet/backend/internal/executor/outbox"
	"deck-fleet/backend/internal/executor/worker"
	"deck-fleet/backend/internal/log"
)

type Config struct {
	DeckID string `yaml:"deck_id" env:"DECK_ID"`

	OrchestratorURL string `yaml:"orchestrator_url" env:"ORCHESTRATOR_URL" env-default:"http://localhost:8080"`

	ListenAddr string `yaml:"listen_addr" env:"LISTEN_ADDR" env-default:":9001"`

	// Empty -> derived from ListenAddr by NormalizeURLs.
	AdvertiseURL string `yaml:"advertise_url" env:"ADVERTISE_URL"`

	// Empty -> "executor-<DeckID>.db" by NormalizeURLs.
	DBPath string `yaml:"db_path" env:"DB_PATH"`

	// Caps slowloris on the executor HTTP server.
	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout" env:"READ_HEADER_TIMEOUT" env-default:"5s"`

	ShutdownGrace time.Duration `yaml:"shutdown_grace" env:"SHUTDOWN_GRACE" env-default:"5s"`

	Log    log.Config    `yaml:"log"`
	Worker worker.Config `yaml:"worker"`
	Outbox outbox.Config `yaml:"outbox"`

	// Zero in production (chaos via HTTP); harness sets before BuildExecutor.
	Chaos chaos.InitialState `yaml:"chaos"`
}

func Load(path string) (*Config, error) {
	cfg := defaults()
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

// defaults applies nested struct defaults; cleanenv env-default only covers top-level fields.
func defaults() Config {
	return Config{
		Worker: worker.Config{
			HeartbeatInterval: 2 * time.Second,
			PollInterval:      500 * time.Millisecond,
			StepDuration:      2 * time.Second,
			PollTimeout:       30 * time.Second,
		},
		Outbox: outbox.Config{
			Initial: 1 * time.Second,
			Max:     30 * time.Second,
		},
	}
}

func (c *Config) Validate() error {
	if c.DeckID == "" {
		return errors.New("config: deck_id is required")
	}
	if c.OrchestratorURL == "" {
		return errors.New("config: orchestrator_url is required")
	}
	if c.ListenAddr == "" {
		return errors.New("config: listen_addr is required")
	}
	return nil
}

// NormalizeURLs fills AdvertiseURL, DBPath, and Worker.DeckID/EndpointURL. Idempotent.
func (c *Config) NormalizeURLs() {
	if c.AdvertiseURL == "" {
		c.AdvertiseURL = "http://localhost" + normalizeAddr(c.ListenAddr)
	}
	if c.DBPath == "" {
		c.DBPath = "executor-" + c.DeckID + ".db"
	}
	// Worker reads DeckID/EndpointURL from its own config; sync from top-level here.
	if c.Worker.DeckID == "" {
		c.Worker.DeckID = c.DeckID
	}
	if c.Worker.EndpointURL == "" {
		c.Worker.EndpointURL = c.AdvertiseURL
	}
}

// normalizeAddr strips host so AdvertiseURL can prepend "http://localhost".
func normalizeAddr(addr string) string {
	if len(addr) > 0 && addr[0] == ':' {
		return addr
	}
	for i := len(addr) - 1; i > 0; i-- {
		if addr[i] == ':' {
			return addr[i:]
		}
	}
	return ":" + addr
}
