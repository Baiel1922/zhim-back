// Command seed loads development test data (see internal/seed).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/caarlos0/env/v11"

	"github.com/Baiel1922/zhim-back/backend/internal/repo"
	"github.com/Baiel1922/zhim-back/backend/internal/seed"
)

type config struct {
	Env             string `env:"APP_ENV" envDefault:"development"`
	DatabaseURL     string `env:"DATABASE_URL,required"`
	OwnerTelegramID int64  `env:"SEED_OWNER_TELEGRAM_ID"`
	OwnerName       string `env:"SEED_OWNER_NAME" envDefault:"Owner"`
}

func main() {
	if err := run(); err != nil {
		slog.Error("seed failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := env.ParseAs[config]()
	if err != nil {
		return fmt.Errorf("parse env: %w", err)
	}
	if cfg.Env == "production" {
		return errors.New("seed overwrites catalog data and must not run with APP_ENV=production")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := repo.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	res, err := seed.Run(ctx, repo.NewStore(pool), seed.Options{
		OwnerTelegramID: cfg.OwnerTelegramID,
		OwnerName:       cfg.OwnerName,
	})
	if err != nil {
		return err
	}

	slog.Info("seed done",
		slog.Int("categories", res.Categories),
		slog.Int("colors", res.Colors),
		slog.Int("products", res.Products),
		slog.Int("variants", res.Variants),
		slog.Int("delivery_zones", res.DeliveryZones),
		slog.Int("settings", res.Settings),
		slog.Bool("owner_admin", res.OwnerSeeded),
	)
	if !res.OwnerSeeded {
		slog.Warn("SEED_OWNER_TELEGRAM_ID is empty: owner admin was not created; set it in .env and run `make seed` again")
	}
	return nil
}
