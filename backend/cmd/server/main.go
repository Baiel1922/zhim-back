// Command server runs the zhim.kg HTTP API (and, in later steps, the Telegram bot).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Baiel1922/zhim-back/backend/internal/config"
	apihttp "github.com/Baiel1922/zhim-back/backend/internal/http"
	"github.com/Baiel1922/zhim-back/backend/internal/imgproxy"
	"github.com/Baiel1922/zhim-back/backend/internal/repo"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped with error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := repo.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	store := repo.NewStore(pool)

	images, err := imgproxy.NewSigner(cfg.Imgproxy.URL, cfg.S3.Bucket, cfg.Imgproxy.Key, cfg.Imgproxy.Salt)
	if err != nil {
		return err
	}

	router := apihttp.NewRouter(apihttp.Deps{
		Log:               log,
		Catalog:           service.NewCatalog(store),
		Checkout:          service.NewCheckout(store),
		Images:            images,
		CORSOrigins:       cfg.CORSAllowedOrigins,
		TrustedProxyCIDRs: cfg.HTTP.TrustedProxyCIDRs,
		CheckoutRateLimit: cfg.RateLimit.CheckoutPerMinute,
		LookupRateLimit:   cfg.RateLimit.LookupPerMinute,
	})

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http server listening", slog.String("addr", cfg.HTTP.Addr), slog.String("env", cfg.Env))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
