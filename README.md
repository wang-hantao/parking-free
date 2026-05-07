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
  http/         # HTTP handlers and routing

migrations/     # Postgres + PostGIS DDL
docs/           # source-of-truth knowledge base
```

The dependency direction is `cmd → http → engine → store → domain`,
with `adapter` bridging external sources into `domain`. `domain` has
no dependencies on anything else.

## Quick start

Prerequisites: Go 1.22+, Docker, Make.

```bash
# Bring up Postgres + PostGIS for local development
docker compose up -d

# Apply migrations (uses psql in the container)
make migrate

# Copy the example env and edit
cp .env.example .env

# Run the HTTP server
make run

# In another terminal:
curl http://localhost:8080/healthz
curl 'http://localhost:8080/allowed?lat=59.32784&lng=18.05306&plate=ABC123'
```

The `/allowed` endpoint will return a stub response until ingestion
is wired up. To run actual ingestion against LTF-Tolken:

```bash
# After setting STOCKHOLM_API_KEY in .env
make ingest
```

## Tests

```bash
make test
```

The engine and holiday calendar have unit tests and don't require the
database or any external API.

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
