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
	Env         string     `env:"APP_ENV" envDefault:"development"`
	LogLevel    slog.Level `env:"LOG_LEVEL" envDefault:"info"`
	DatabaseURL string     `env:"DATABASE_URL,required,notEmpty"`
	HTTP        HTTP       `envPrefix:"HTTP_"`
	// Storefront and admin origins allowed to call the API from browsers.
	CORSAllowedOrigins []string  `env:"CORS_ALLOWED_ORIGINS" envSeparator:","`
	S3                 S3        `envPrefix:"S3_"`
	Imgproxy           Imgproxy  `envPrefix:"IMGPROXY_"`
	RateLimit          RateLimit `envPrefix:"RATE_LIMIT_"`
	JWT                JWT       `envPrefix:"JWT_"`
	TelegramBotToken   string    `env:"TELEGRAM_BOT_TOKEN,required,notEmpty"`
	// Business timezone for date filters, the dashboard and reports.
	Timezone string `env:"APP_TIMEZONE" envDefault:"Asia/Bishkek"`
	// Storefront base URL reachable from the API (on-demand revalidation); empty disables it.
	WebInternalURL   string `env:"WEB_INTERNAL_URL"`
	RevalidateSecret string `env:"REVALIDATE_SECRET"`
}

// JWT holds admin token settings.
type JWT struct {
	Secret string        `env:"SECRET,required,notEmpty"`
	TTL    time.Duration `env:"TTL" envDefault:"12h"`
}

// HTTP holds HTTP server settings.
type HTTP struct {
	Addr            string        `env:"ADDR" envDefault:":8080"`
	ReadTimeout     time.Duration `env:"READ_TIMEOUT" envDefault:"10s"`
	WriteTimeout    time.Duration `env:"WRITE_TIMEOUT" envDefault:"30s"`
	IdleTimeout     time.Duration `env:"IDLE_TIMEOUT" envDefault:"120s"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"15s"`
	// CIDRs of reverse proxies whose X-Forwarded-For is trusted for the client IP.
	// Empty: the TCP peer address is the client IP.
	TrustedProxyCIDRs []string `env:"TRUSTED_PROXY_CIDRS" envSeparator:","`
}

// S3 holds object storage settings.
type S3 struct {
	Endpoint  string `env:"ENDPOINT,required,notEmpty"`
	Region    string `env:"REGION" envDefault:"us-east-1"`
	Bucket    string `env:"BUCKET,required,notEmpty"`
	AccessKey string `env:"ACCESS_KEY,required,notEmpty"`
	SecretKey string `env:"SECRET_KEY,required,notEmpty"`
}

// Imgproxy holds image URL signing settings.
type Imgproxy struct {
	URL  string `env:"URL,required,notEmpty"`
	Key  string `env:"KEY,required,notEmpty"`
	Salt string `env:"SALT,required,notEmpty"`
}

// RateLimit holds per-IP request limits (requests per minute; 0 disables).
type RateLimit struct {
	// Order creation and promo code checks.
	CheckoutPerMinute int `env:"CHECKOUT_PER_MINUTE" envDefault:"10"`
	// Order status lookups.
	LookupPerMinute int `env:"LOOKUP_PER_MINUTE" envDefault:"60"`
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
