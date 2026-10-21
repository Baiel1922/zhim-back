// Command migrate applies the embedded goose migrations.
//
//	migrate up | down | status | version
//	migrate create <name>   (writes a new SQL file into ./migrations, no database needed)
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
	"github.com/pressly/goose/v3"

	"github.com/Baiel1922/zhim-back/backend/internal/repo"
)

const usage = "usage: migrate up|down|status|version | migrate create <name>"

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("migrate failed", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}

	if args[0] == "create" {
		if len(args) != 2 {
			return errors.New(usage)
		}
		goose.SetSequential(true)
		return goose.Create(nil, "migrations", args[1], "sql")
	}

	cfg, err := env.ParseAs[struct {
		DatabaseURL string `env:"DATABASE_URL,required,notEmpty"`
	}]()
	if err != nil {
		return fmt.Errorf("parse env: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sqlDB, err := repo.OpenSQL(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	migrator, err := repo.NewMigrator(sqlDB)
	if err != nil {
		return err
	}

	switch args[0] {
	case "up":
		results, err := migrator.Up(ctx)
		for _, r := range results {
			fmt.Println(r)
		}
		if err != nil {
			return err
		}
		if len(results) == 0 {
			fmt.Println("no pending migrations")
		}
	case "down":
		r, err := migrator.Down(ctx)
		if r != nil {
			fmt.Println(r)
		}
		return err
	case "status":
		statuses, err := migrator.Status(ctx)
		if err != nil {
			return err
		}
		for _, s := range statuses {
			applied := "pending"
			if s.State == goose.StateApplied {
				applied = "applied " + s.AppliedAt.Format("2006-01-02 15:04:05")
			}
			fmt.Printf("%-40s %s\n", s.Source.Path, applied)
		}
	case "version":
		v, err := migrator.GetDBVersion(ctx)
		if err != nil {
			return err
		}
		fmt.Println(v)
	default:
		return errors.New(usage)
	}
	return nil
}
