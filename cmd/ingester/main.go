// Command ingester pulls regulation data from Stockholm's LTF-Tolken
// API and stores it in Postgres.
//
// Usage:
//
//	ingester                       # ingest all six föreskrifter (full)
//	ingester <foreskrift>          # ingest one (full)
//	ingester dump <dir>            # capture small samples of all six to <dir>
//	ingester dump <dir> <name>...  # capture samples of named föreskrifter
//
// Modes:
//
//   - Default (ingest): requires STOCKHOLM_API_KEY and PG_DSN. Calls
//     FetchAll, runs the transform, upserts into Postgres.
//   - Dump: requires only STOCKHOLM_API_KEY. Calls FetchSample (small,
//     bounded by maxFeatures), writes raw JSON to disk. Useful for
//     verifying schema before implementing or revising the transform.
//
// Get an API key for free at https://openparking.stockholm.se/Home/Key
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/wang-hantao/parking-free/internal/adapter/stockholm"
	"github.com/wang-hantao/parking-free/internal/config"
	"github.com/wang-hantao/parking-free/internal/domain"
	"github.com/wang-hantao/parking-free/internal/store/postgres"
)

// Sample-capture defaults — central Stockholm (Stureplan), modest radius
// and feature count so each dump file stays small enough to paste.
const (
	dumpLat         = 59.3330
	dumpLng         = 18.0681
	dumpRadiusM     = 2000
	dumpMaxFeatures = 5
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

	args := os.Args[1:]
	if len(args) > 0 && args[0] == "dump" {
		runDump(logger, cfg, args[1:])
		return
	}
	runIngest(logger, cfg, args)
}

// runIngest is the default ingestion path: fetch full data, transform,
// upsert into Postgres.
//
// Per-foreskrift flow:
//
//  1. Fetch raw JSON via FetchAll.
//  2. Transform → IngestBatch (RoadSegments, Regulations, Rules with
//     placeholder source-refs in their cross-record fields).
//  3. UpsertRoadSegments → map[source_ref]uuid for geometries.
//  4. UpsertRegulations → map[source_ref]uuid for regulations.
//  5. Resolve each Rule's RegulationID and AppliesTo.TargetID
//     placeholders using the two maps. Drop rules whose targets
//     didn't resolve (defensive).
//  6. UpsertRules.
func runIngest(logger *slog.Logger, cfg config.Config, args []string) {
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
	if len(args) > 0 {
		targets = []stockholm.Foreskrift{stockholm.Foreskrift(args[0])}
	}

	for _, f := range targets {
		t0 := time.Now()
		raw, err := client.FetchAll(ctx, f)
		if err != nil {
			logger.Error("fetch failed", "foreskrift", f, "err", err)
			continue
		}

		batch, err := stockholm.Transform(f, raw)
		switch {
		case errors.Is(err, stockholm.ErrSchemaPending):
			logger.Warn("transform pending; skipping",
				"foreskrift", f, "bytes", len(raw),
				"hint", "use `ingester dump <dir> "+string(f)+"` and share the sample")
			continue
		case err != nil:
			logger.Error("transform failed", "foreskrift", f, "err", err)
			continue
		}

		// 1. Upsert geometries first; map source_ref -> UUID.
		segIDs, err := store.UpsertRoadSegments(ctx, batch.RoadSegments)
		if err != nil {
			logger.Error("upsert road segments failed", "foreskrift", f, "err", err)
			continue
		}

		// 2. Upsert regulations; map source_ref -> UUID.
		regIDs, err := store.UpsertRegulations(ctx, batch.Regulations)
		if err != nil {
			logger.Error("upsert regulations failed", "foreskrift", f, "err", err)
			continue
		}

		// 3. Resolve rule placeholders. Drop rules whose targets are
		//    unknown — keeps the batch consistent rather than failing
		//    the whole foreskrift on a single malformed feature.
		resolved := make([]domain.Rule, 0, len(batch.Rules))
		var dropped int
		for _, r := range batch.Rules {
			regUUID, ok := regIDs[r.RegulationID]
			if !ok {
				dropped++
				continue
			}
			r.RegulationID = regUUID

			ok = true
			for i := range r.AppliesTo {
				if r.AppliesTo[i].Kind != domain.TargetRoadSegment {
					continue
				}
				segUUID, found := segIDs[r.AppliesTo[i].TargetID]
				if !found {
					ok = false
					break
				}
				r.AppliesTo[i].TargetID = segUUID
			}
			if !ok {
				dropped++
				continue
			}
			resolved = append(resolved, r)
		}

		if err := store.UpsertRules(ctx, resolved); err != nil {
			logger.Error("upsert rules failed", "foreskrift", f, "err", err)
			continue
		}

		logger.Info("ingested",
			"foreskrift", f,
			"bytes", len(raw),
			"road_segments", len(batch.RoadSegments),
			"regulations", len(batch.Regulations),
			"rules", len(resolved),
			"rules_dropped", dropped,
			"elapsed_ms", time.Since(t0).Milliseconds())
	}
}

// runDump fetches a small sample of each requested föreskrift and
// writes the raw JSON to disk. Use this to verify the LTF-Tolken
// response schema before implementing or revising the transform.
func runDump(logger *slog.Logger, cfg config.Config, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: ingester dump <output_dir> [foreskrift...]")
		os.Exit(2)
	}
	dir := args[0]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.Error("mkdir failed", "dir", dir, "err", err)
		os.Exit(1)
	}

	targets := stockholm.AllForeskrifter
	if len(args) > 1 {
		targets = nil
		for _, name := range args[1:] {
			targets = append(targets, stockholm.Foreskrift(name))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Ingest.Timeout)
	defer cancel()

	client := stockholm.NewClient(cfg.Stockholm.BaseURL, cfg.Stockholm.APIKey)

	for _, f := range targets {
		t0 := time.Now()
		raw, err := client.FetchSample(ctx, f, dumpLat, dumpLng, dumpRadiusM, dumpMaxFeatures)
		if err != nil {
			logger.Error("fetch sample failed", "foreskrift", f, "err", err)
			continue
		}
		out := filepath.Join(dir, fmt.Sprintf("%s.json", f))
		if err := os.WriteFile(out, raw, 0o644); err != nil {
			logger.Error("write file failed", "path", out, "err", err)
			continue
		}
		logger.Info("dumped sample",
			"foreskrift", f, "path", out, "bytes", len(raw),
			"elapsed_ms", time.Since(t0).Milliseconds())
	}
}
