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

	client := stockholm.NewClient(cfg.Stockholm.BaseURL, cfg.Stockholm.APIKey)

	targets := stockholm.AllForeskrifter
	if len(os.Args) > 1 {
		targets = []stockholm.Foreskrift{stockholm.Foreskrift(os.Args[1])}
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Ingest.Timeout)
	defer cancel()

	for _, f := range targets {
		t0 := time.Now()
		raw, err := client.FetchAll(ctx, f)
		if err != nil {
			logger.Error("fetch failed", "foreskrift", f, "err", err)
			continue
		}

		_, _, err = stockholm.Transform(f, raw)
		switch {
		case errors.Is(err, stockholm.ErrSchemaPending):
			logger.Warn("transform not implemented; raw bytes available for schema derivation",
				"foreskrift", f, "bytes", len(raw))
		case err != nil:
			logger.Error("transform failed", "foreskrift", f, "err", err)
		default:
			// TODO: persist via store once postgres.Store is implemented.
			logger.Info("transformed", "foreskrift", f, "bytes", len(raw),
				"elapsed_ms", time.Since(t0).Milliseconds())
		}
	}
}
