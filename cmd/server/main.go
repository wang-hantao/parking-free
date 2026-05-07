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
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.Logging)

	// For now we wire the engine against a no-op rule source. Once the
	// store is implemented (see internal/store/postgres), swap this to
	// use postgres.Open and pass the Store as the engine's RuleSource.
	src := emptyRuleSource{}
	cal := engine.NewHolidayCalendarSE()
	ev := engine.New(src, cal)

	srv := httpapi.New(httpapi.Config{
		Addr:         cfg.HTTP.Addr,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}, logger, ev)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
