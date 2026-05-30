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
	"strconv"
	"time"

	"github.com/wang-hantao/parking-free/internal/adapter/stockholm"
	"github.com/wang-hantao/parking-free/internal/config"
	"github.com/wang-hantao/parking-free/internal/domain"
	"github.com/wang-hantao/parking-free/internal/store/postgres"
)

// Sample-capture defaults — central Stockholm (Stureplan), modest radius
// and feature count so each dump file stays small enough to paste.
// Overridable per-run via DUMP_LAT, DUMP_LNG, DUMP_RADIUS_M, DUMP_MAX
// environment variables — handy when diagnosing a specific spot whose
// features don't appear in the default sample.
const (
	dumpLat         = 59.3330
	dumpLng         = 18.0681
	dumpRadiusM     = 2000
	dumpMaxFeatures = 5
)

func dumpParams() (lat, lng float64, radiusM, maxFeatures int) {
	lat, lng = dumpLat, dumpLng
	radiusM, maxFeatures = dumpRadiusM, dumpMaxFeatures
	if v := os.Getenv("DUMP_LAT"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			lat = f
		}
	}
	if v := os.Getenv("DUMP_LNG"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			lng = f
		}
	}
	if v := os.Getenv("DUMP_RADIUS_M"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			radiusM = n
		}
	}
	if v := os.Getenv("DUMP_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxFeatures = n
		}
	}
	return
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "dump":
			requireStockholmKey(logger, cfg)
			runDump(logger, cfg, args[1:])
			return
		case "cleanup":
			// cleanup is a pure DB operation; no LTF-Tolken API key
			// needed. Lets users on offline / read-only Stockholm
			// access still tidy their DB.
			runCleanup(logger, cfg)
			return
		}
	}
	requireStockholmKey(logger, cfg)
	runIngest(logger, cfg, args)
}

func requireStockholmKey(logger *slog.Logger, cfg config.Config) {
	if cfg.Stockholm.APIKey == "" {
		logger.Error("STOCKHOLM_API_KEY is required",
			"hint", "request a free key at https://openparking.stockholm.se/Home/Key")
		os.Exit(1)
	}
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

		// Drop stale segments under this föreskrift's prefix. Without
		// this, every LTF revision that renumbers or removes a
		// feature leaves the old segment behind as an orphan. The
		// prefix matches the segRef format that the transforms use:
		// "<foreskrift>/<FID>/<extentNo>". Foreskrift values are
		// already the prefix string, so we just append "/".
		pruned, err := store.PruneOrphanRoadSegments(ctx, stockholm.SourceSystem, string(f)+"/")
		if err != nil {
			logger.Warn("prune orphans failed (non-fatal)", "foreskrift", f, "err", err)
			pruned = 0
		}

		logger.Info("ingested",
			"foreskrift", f,
			"bytes", len(raw),
			"road_segments", len(batch.RoadSegments),
			"regulations", len(batch.Regulations),
			"rules", len(resolved),
			"rules_dropped", dropped,
			"features_skipped", batch.SkippedFeatures,
			"segments_pruned", pruned,
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

	lat, lng, radiusM, maxFeatures := dumpParams()
	logger.Info("dump scope",
		"lat", lat, "lng", lng, "radius_m", radiusM, "max_features", maxFeatures)

	for _, f := range targets {
		t0 := time.Now()
		raw, err := client.FetchSample(ctx, f, lat, lng, radiusM, maxFeatures)
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

// runCleanup wipes every orphan road_segment under
// stockholm.SourceSystem — i.e. segments left behind by previous
// LTF revisions that no longer have any rule attached. The
// per-föreskrift prune in runIngest handles steady-state
// idempotency; this subcommand exists to clean up the accumulation
// from before that logic landed.
//
// Read-only with respect to LTF — no Stockholm API key needed.
//
// Usage:
//
//	go run ./cmd/ingester cleanup
func runCleanup(logger *slog.Logger, cfg config.Config) {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Ingest.Timeout)
	defer cancel()

	store, err := postgres.Open(ctx, cfg.Postgres.DSN)
	if err != nil {
		logger.Error("postgres open failed", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	t0 := time.Now()
	deleted, err := store.PruneAllOrphanRoadSegments(ctx, stockholm.SourceSystem)
	if err != nil {
		logger.Error("cleanup failed", "err", err)
		os.Exit(1)
	}
	logger.Info("cleanup complete",
		"source_system", stockholm.SourceSystem,
		"segments_pruned", deleted,
		"elapsed_ms", time.Since(t0).Milliseconds())
}
