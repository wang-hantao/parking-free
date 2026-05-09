// Command ingester pulls regulation data from Stockholm's LTF-Tolken
// API and stores it in Postgres.
//
// Usage:
//
//	ingester                 # ingest all six föreskrifter
//	ingester servicedagar    # ingest only one
//
// Requires STOCKHOLM_API_KEY (request at
// https://openparking.stockholm.se/Home/Key) and PG_DSN to be set.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/wang-hantao/parking-free/internal/adapter/stockholm"
	"github.com/wang-hantao/parking-free/internal/config"
	"github.com/wang-hantao/parking-free/internal/store/postgres"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if cfg.Stockholm.APIKey == "" {
		logger.Error("STOCKHOLM_API_KEY is required",
			"hint", "request a free key at https://openparking.stockholm.se/Home/Key")
		os.Exit(1)
	}
	if cfg.Postgres.DSN == "" {
		logger.Error("PG_DSN is required for ingestion")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Ingest.Timeout)
	defer cancel()

	store, err := postgres.Open(ctx, cfg.Postgres.DSN)
	if err != nil {
		logger.Error("postgres open failed", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	client := stockholm.NewClient(cfg.Stockholm.BaseURL, cfg.Stockholm.APIKey)

	targets := stockholm.AllForeskrifter
	if len(os.Args) > 1 {
		targets = []stockholm.Foreskrift{stockholm.Foreskrift(os.Args[1])}
	}

	for _, f := range targets {
		t0 := time.Now()
		raw, err := client.FetchAll(ctx, f)
		if err != nil {
			logger.Error("fetch failed", "foreskrift", f, "err", err)
			continue
		}

		regs, rules, err := stockholm.Transform(f, raw)
		switch {
		case errors.Is(err, stockholm.ErrSchemaPending):
			logger.Warn("transform not implemented; raw bytes captured but not persisted",
				"foreskrift", f, "bytes", len(raw),
				"hint", "implement internal/adapter/stockholm/transform.go using a real LTF-Tolken response")
			continue
		case err != nil:
			logger.Error("transform failed", "foreskrift", f, "err", err)
			continue
		}

		if err := store.UpsertRegulations(ctx, regs); err != nil {
			logger.Error("upsert regulations failed", "foreskrift", f, "err", err)
			continue
		}
		if err := store.UpsertRules(ctx, rules); err != nil {
			logger.Error("upsert rules failed", "foreskrift", f, "err", err)
			continue
		}

		logger.Info("ingested",
			"foreskrift", f, "bytes", len(raw),
			"regulations", len(regs), "rules", len(rules),
			"elapsed_ms", time.Since(t0).Milliseconds())
	}
}
