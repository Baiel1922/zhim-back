// Command server runs the zhim.kg HTTP API (and, in later steps, the Telegram bot).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // the distroless/alpine images have no zoneinfo

	"github.com/Baiel1922/zhim-back/backend/internal/auth"
	"github.com/Baiel1922/zhim-back/backend/internal/bot"
	"github.com/Baiel1922/zhim-back/backend/internal/config"
	"github.com/Baiel1922/zhim-back/backend/internal/conversions"
	apihttp "github.com/Baiel1922/zhim-back/backend/internal/http"
	"github.com/Baiel1922/zhim-back/backend/internal/imgproxy"
	"github.com/Baiel1922/zhim-back/backend/internal/repo"
	"github.com/Baiel1922/zhim-back/backend/internal/revalidate"
	"github.com/Baiel1922/zhim-back/backend/internal/service"
	"github.com/Baiel1922/zhim-back/backend/internal/storage"
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
	objects, err := storage.NewS3(cfg.S3.Endpoint, cfg.S3.Region, cfg.S3.AccessKey, cfg.S3.SecretKey, cfg.S3.Bucket)
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return err
	}
	tokens, err := auth.NewTokens(cfg.JWT.Secret, cfg.JWT.TTL)
	if err != nil {
		return err
	}

	notifier := revalidate.New(cfg.WebInternalURL, cfg.RevalidateSecret, log)
	if cfg.WebInternalURL == "" {
		log.Warn("WEB_INTERNAL_URL is empty: storefront revalidation is disabled")
	}
	go notifier.Run(ctx)

	checkout := service.NewCheckout(store, notifier)
	if sender := conversions.New(conversions.Config{
		MetaPixelID:         cfg.Meta.PixelID,
		MetaAccessToken:     cfg.Meta.CAPIAccessToken,
		MetaTestEventCode:   cfg.Meta.TestEventCode,
		TikTokPixelID:       cfg.TikTok.PixelID,
		TikTokAccessToken:   cfg.TikTok.EventsAccessToken,
		TikTokTestEventCode: cfg.TikTok.TestEventCode,
		WebURL:              cfg.WebURL,
	}, log); sender != nil {
		checkout.SetConversions(sender)
		go sender.Run(ctx)
		log.Info("server-side conversions enabled", slog.Any("platforms", sender.Platforms()))
	}
	admin := service.NewAdmin(store, objects, notifier, loc, log)
	if cfg.Telegram.BotEnabled {
		reportAt, err := time.Parse("15:04", cfg.Telegram.DailyReportTime)
		if err != nil {
			return fmt.Errorf("TELEGRAM_DAILY_REPORT_TIME: %w", err)
		}
		tg, err := bot.New(bot.Config{
			Token:        cfg.Telegram.BotToken,
			AdminURL:     cfg.AdminURL,
			WebURL:       cfg.WebURL,
			Location:     loc,
			ReportHour:   reportAt.Hour(),
			ReportMinute: reportAt.Minute(),
		}, store, admin, log)
		if err != nil {
			return err
		}
		checkout.SetOrderEvents(tg)
		admin.SetOrderEvents(tg)
		go tg.Run(ctx)
	} else {
		log.Warn("TELEGRAM_BOT_ENABLED=false: no order notifications from this process")
	}

	router := apihttp.NewRouter(apihttp.Deps{
		Log:               log,
		Catalog:           service.NewCatalog(store),
		Checkout:          checkout,
		Admin:             admin,
		AdminAuth:         service.NewAdminAuth(store, tokens, cfg.Telegram.BotToken),
		Images:            images,
		WebURL:            cfg.WebURL,
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
