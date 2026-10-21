// Package config loads application configuration from environment variables.
package config

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config is the full runtime configuration of the backend binary.
type Config struct {
	Env      string     `env:"APP_ENV" envDefault:"development"`
	LogLevel slog.Level `env:"LOG_LEVEL" envDefault:"info"`
	HTTP     HTTP       `envPrefix:"HTTP_"`
}

// HTTP holds HTTP server settings.
type HTTP struct {
	Addr            string        `env:"ADDR" envDefault:":8080"`
	ReadTimeout     time.Duration `env:"READ_TIMEOUT" envDefault:"10s"`
	WriteTimeout    time.Duration `env:"WRITE_TIMEOUT" envDefault:"30s"`
	IdleTimeout     time.Duration `env:"IDLE_TIMEOUT" envDefault:"120s"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"15s"`
}

// Load parses the environment into a Config.
func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("parse env: %w", err)
	}
	return cfg, nil
}

// IsProduction reports whether the app runs in production mode.
func (c Config) IsProduction() bool {
	return c.Env == "production"
}
