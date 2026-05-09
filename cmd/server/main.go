// Command server runs the parking-free HTTP API.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/wang-hantao/parking-free/internal/config"
	"github.com/wang-hantao/parking-free/internal/engine"
	httpapi "github.com/wang-hantao/parking-free/internal/http"
	"github.com/wang-hantao/parking-free/internal/store/postgres"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.Logging)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Resolve a RuleSource: postgres when configured, empty otherwise.
	// The empty source lets the server start cleanly for development
	// even before the database is reachable.
	var src engine.RuleSource = emptyRuleSource{}
	if cfg.Postgres.DSN != "" {
		pg, err := postgres.Open(ctx, cfg.Postgres.DSN)
		if err != nil {
			logger.Warn("postgres unavailable, using empty source", "err", err)
		} else {
			defer pg.Close()
			src = pg
			logger.Info("using postgres rule source", "dsn_present", true)
		}
	} else {
		logger.Info("PG_DSN not set, using empty rule source")
	}

	cal := engine.NewHolidayCalendarSE()
	ev := engine.New(src, cal)

	srv := httpapi.New(httpapi.Config{
		Addr:         cfg.HTTP.Addr,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}, logger, ev)

	if err := srv.Run(ctx); err != nil {
		logger.Error("server stopped with error", "err", err)
		os.Exit(1)
	}
	logger.Info("server stopped cleanly")
}

func newLogger(cfg config.LoggingConfig) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if cfg.Format == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	} else {
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h)
}
