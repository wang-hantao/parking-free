# parking-free

A backend for evaluating "is parking allowed here right now?" against the
real regulation graph of a city. Stockholm-first, designed to extend.

The product hypothesis and architectural rationale are in [`docs/`](docs/).
Read [`docs/README.md`](docs/README.md) first.

## What this is

A Go service that:

1. Ingests parking regulations from city open-data APIs (starting with
   Stockholm's [LTF-Tolken](docs/04-stockholm-ltf-tolken-api.md)) into a
   normalized regulation graph.
2. Evaluates a `(lat, lng, vehicle, time)` query against the graph and
   returns a verdict: `{allowed, expires_at, reasons[]}`.
3. Exposes the evaluator over HTTP for any client (mobile app, web app,
   B2B integration).

The internal model is city-agnostic. Adding Göteborg, Oslo, or any other
city is a question of writing a source adapter, not changing the kernel.
See [`docs/07-cross-city-and-eu-extension.md`](docs/07-cross-city-and-eu-extension.md).

## Status

Scaffold. The HTTP server starts and the evaluator skeleton is wired,
but no regulations are loaded yet — that requires an API key from
Trafikkontoret (free, see
[`docs/04-stockholm-ltf-tolken-api.md`](docs/04-stockholm-ltf-tolken-api.md#authentication-getting-an-api-key)).

## Layout

```
cmd/
  server/       # HTTP service entry point
  ingester/     # CLI for pulling LTF-Tolken data into Postgres

internal/
  config/       # env-based configuration
  domain/       # core types: Regulation, Rule, TimeWindow, Verdict, etc.
  adapter/      # source-specific ingestion
    stockholm/  #   LTF-Tolken HTTP client + transform to domain
  engine/       # rule evaluation kernel + holiday calendar
  store/        # persistence interface + Postgres+PostGIS implementation
  http/         # HTTP handlers, CORS, and routing

migrations/     # Postgres + PostGIS DDL
docs/           # source-of-truth knowledge base

web/            # React + Vite + TypeScript frontend (its own npm project)
```

The dependency direction is `cmd → http → engine → store → domain`,
with `adapter` bridging external sources into `domain`. `domain` has
no dependencies on anything else. The frontend in `web/` is an
independent npm project; the Go toolchain ignores it.

## Quick start

Prerequisites: Go 1.22+, Docker, Make.

```bash
# Bring up Postgres + PostGIS for local development
docker compose up -d

# Apply migrations (uses psql in the container)
make migrate

# Optional: seed a small demo dataset (one zone in central Stockholm,
# the four authorised operators, and a paid-parking rule tagged with
# Stockholm tariff class `stockholm.taxa.2`) so /allowed returns a
# fully enriched response. Pricing comes from the in-process tariff
# class registry (internal/engine/tariffs.go), not the seed.
make seed

# Copy the example env and edit
cp .env.example .env

# Run the HTTP server
make run

# In another terminal:
curl http://localhost:8080/healthz
curl 'http://localhost:8080/allowed?lat=59.3330&lng=18.0681&plate=ABC123'

# With duration to get an estimated_cost block in the response:
curl 'http://localhost:8080/allowed?lat=59.3330&lng=18.0681&plate=ABC123&duration_minutes=180'

# Strict mode — only rules that legally apply to the exact position.
# Cuts noise from nearby segments and resolves false-positives like
# a bus-only spot 30m away firing on a car query:
curl 'http://localhost:8080/allowed?lat=59.3373&lng=18.0802&plate=ABC123&mode=strict'
```

The `/allowed` endpoint will return a default-allow response with no
enrichment fields when the database is empty. Run `make seed` to load
the Stureplan demo dataset and see the full response shape — including
`location`, `pricing` (current rate, operator deeplinks),
`constraints` (max stay, payment required), `warnings` (max stay
expiring), and — when `duration_minutes` is supplied — `estimated_cost`
with a per-window breakdown.

The `mode` query parameter selects how rules are resolved:

- `mode=nearby` (default): rules within `radius` metres (50m default).
  Returns wider context — useful for "what's around here" views.
- `mode=strict`: rules that legally apply to the exact point — every
  road-segment rule within 5m of the query point (naturally captures
  the multiple overlapping föreskrifter on the same curb without
  bleeding across a normal-width street), plus zones and parking
  areas containing the point, plus POI rules within their declared
  offset. Use this for "can I park exactly here right now?". The
  response's `metadata.mode` reports the effective mode.

When multiple Allow rules apply at the same location (common in
strict mode where ptillaten + servicedagar + a reserved-class spot
all overlap on the same curb), the engine resolves by priority:

- Reserved-class spots (disabled bays, bus stops, motorcycle bays):
  **priority 20** — supersede general allows at the same location
- Street cleaning (servicedagar): priority 10, but it's a Forbid, so
  it wins regardless of priority
- General paid parking (ptillaten): priority 5

So at a disabled bay carved out of a paid-parking strip, the
disabled-only requirement binds: a car without a disabled permit
gets `allowed: false` even though the general ptillaten rule would
otherwise allow payment. The lower-priority rule appears in
`reasons` with `superseded: true` for traceability.

### Vehicle-class reservations

Reserved-class spots (pbuss, plastbil, pmotorcykel) carry
`VehicleClasses` on their Allow rules. Semantics are asymmetric by
rule kind:

- **Allow with VehicleClasses**: the spot is **reserved** for those
  classes. Non-matching vehicles are blocked. A car at a bus stop
  gets `allowed: false` with a reason `"Parking reserved for buses"`
  — even though the bus-stop rule technically "doesn't apply" to
  cars in the classical sense.
- **Forbid with VehicleClasses**: a class-specific restriction.
  Doesn't fire for non-matching vehicles. Hypothetical "no trucks
  9-17" leaves cars unaffected.

This asymmetry matches Stockholm enforcement: a bus stop blocks
non-buses, while a class-specific Forbid only blocks the named
classes.

### Ingester idempotency

`UpsertRules` is destructive per regulation: each ingest run deletes
existing rules and re-inserts the current snapshot. `UpsertRoadSegments`
is purely additive, so segments from features that Stockholm removes
between LTF revisions would otherwise linger as orphans.

After each föreskrift's run, the ingester calls
`PruneOrphanRoadSegments` to delete segments under that föreskrift's
prefix that have no `rule_applies_to` entries. The count is logged
as `segments_pruned`. Net effect: re-ingestion is idempotent — DB
state matches whatever Stockholm's LTF currently exposes.

## Frontend

The `web/` directory is a React + Vite + TypeScript single-page app
that asks for your location, shows it on a Google Map, calls
`/allowed`, and renders the verdict with pricing and payment-app
deep-links. See [`web/README.md`](web/README.md) for setup and
deployment instructions.

Quick local run, alongside the backend:

```bash
# Backend with CORS for the dev origin
export CORS_ALLOWED_ORIGINS=http://localhost:5173
make docker-up && make migrate && make seed && make run

# In another terminal
cd web
cp .env.example .env.local   # then set VITE_GOOGLE_MAPS_API_KEY
npm install
npm run dev
# Open http://localhost:5173
```

To run actual ingestion against LTF-Tolken:

```bash
# After setting STOCKHOLM_API_KEY in .env, and after `make migrate`
# has applied migrations/0005 which adds geometry provenance indexes:
make ingest                    # all six föreskrifter
go run ./cmd/ingester servicedagar  # just one
```

The ingester:

1. Fetches the full JSON for each föreskrift via LTF-Tolken's `/all`
   endpoint.
2. Transforms into a batch of `RoadSegment` + `Regulation` + `Rule`
   records, with placeholder source-refs in cross-record fields.
3. Upserts road segments, then regulations, then resolves placeholders
   to UUIDs, then upserts rules.

After ingesting `servicedagar`, the `/allowed` endpoint will return
real cleaning-window enforcement: a verdict of `allowed: false` if you
query a position on a road segment during its weekly servicedag, with
the contributing rule and the citation URL surfaced in `reasons`.

All six föreskrifter — `servicedagar`, `ptillaten`, `pbuss`,
`plastbil`, `pmotorcykel`, `prorelsehindrad` — are transformed into
domain Rules. The transforms preserve the LTF citation as the
regulation source-reference, so every rule is traceable back to its
RDT decision URL.

Pmotorcykel features can carry seasonal date-range fields
(`START_MONTH`/`END_MONTH`/`START_DAY`/`END_DAY`) describing rules
that vary by season (e.g. no street cleaning in summer). The engine
honours these via four nullable month/day columns on
`rule_time_window`, including cross-year wraps (Aug 16 → Jun 14,
i.e. "everything except summer"). About 40% of pmotorcykel features
use this pattern; all are now ingested.

Disabled-only spots (`prorelsehindrad`, and ptillaten features whose
`VF_PLATS_TYP` mentions "rörelsehindrad") carry
`RequiredPermitKind="disabled"`. The engine's satisfiability check
now filters by kind: a user with a residential permit on the plate
will see `allowed=false` at a disabled-only spot, with `needs_action`
listing `obtain_permit` and the reason text mentioning the specific
permit kind.

### Schema inspection

The LTF-Tolken JSON response shape isn't publicly documented. Before
implementing or revising `internal/adapter/stockholm/transform.go`,
capture a small sample with the built-in `dump` subcommand:

```bash
# Captures ~5 features per föreskrift into ./samples/ (Stureplan, 2km radius)
go run ./cmd/ingester dump ./samples

# Or just one föreskrift:
go run ./cmd/ingester dump ./samples servicedagar
```

`dump` requires only `STOCKHOLM_API_KEY`, not `PG_DSN` — it writes raw
JSON to disk and exits without touching the database.

## Pricing model

Each `Rule` carries a `tariff_class_code` (e.g. `stockholm.taxa.3`),
parsed from the `PARKING_RATE` field of LTF-Tolken features at ingest
time. The class definitions — recurring per-day-type windows with
rates and priorities — live in
[`internal/engine/tariffs.go`](internal/engine/tariffs.go) as an
in-process registry. The enricher resolves a rule's code against the
registry to populate the `pricing` block (`current_rate`,
`next_rate_change`, `next_rate`) and any `estimated_cost`.

Five Stockholm classes are encoded: `taxa 1` (flat 55 SEK/h), `taxa 2`
(31 SEK/h Mon-Fri 07-21 + pre-holiday/holiday 09-19, 20 SEK/h other),
`taxa 3` (20 SEK/h Mon-Fri 07-19, 15 SEK/h pre-holiday 11-17), and the
MC-reduced variants `taxa 12` and `taxa 13`. Tests swap the registry
via `Evaluator.WithTariffClasses(...)`.

## Tests

```bash
make test
```

The engine and holiday calendar have unit tests and don't require the
database or any external API. They cover the rule walker (vehicle-class
filtering, time windows, priority ordering, permit lookup, May 1
holiday) and the enrichment pipeline (location, pricing across rate
boundaries, operator deeplink expansion, cost estimation across the
free/paid cutoff).

### Postgres integration tests

The `internal/store/postgres` package has integration tests against a
real Postgres + PostGIS instance. They are skipped automatically when
`POSTGRES_TEST_DSN` is unset, so they don't break `make test` for
people without a local database.

To run them:

```bash
make docker-up && make migrate
export POSTGRES_TEST_DSN='postgres://parking:parking@localhost:5432/parking?sslmode=disable'
go test ./internal/store/postgres/...
```

The tests are destructive — they truncate all relevant tables before
running. Don't point `POSTGRES_TEST_DSN` at a database with real data.

## Extending to a new city

See [`docs/07-cross-city-and-eu-extension.md`](docs/07-cross-city-and-eu-extension.md)
for the full playbook. The structural change is small:

1. Add `internal/adapter/<city>/` with ingestion logic that produces
   `domain.Regulation` records.
2. Add a holiday calendar entry for the country/region.
3. Register the adapter in `cmd/ingester/main.go`.

The HTTP API and engine require no changes.

## License

MIT — see [LICENSE](LICENSE).
