# 04 — Stockholm LTF-Tolken API Reference

The primary machine-readable interface to Stockholm's parking
regulations. Operated by Trafikkontoret (the city Traffic Office).
Data is sourced from the database of local traffic ordinances
(lokala trafikföreskrifter) and reinterpreted for machine consumption.

## Base URL

```
https://openparking.stockholm.se/LTF-Tolken/v1/{föreskrift}/{operation}
```

All requests require an `apiKey` query parameter.

## Authentication — getting an API key

API keys are issued free of charge by Trafikkontoret. Request via:

> <https://openparking.stockholm.se/Home/Key>

Form fields ask for: name, organization (optional), email, intended
use, and chosen coordinate system. Approval is typically same-day to
a few business days. There is no documented hard rate limit but
abusive clients can be revoked.

## Available föreskrifter (regulation types)

| Endpoint | What it returns |
|----------|-----------------|
| `servicedagar` | Street-cleaning windows (when it is **forbidden** to park because of cleaning). Inner-city: typically one weekday 00:00–06:00; outer areas: one weekday 08:00–16:00. |
| `ptillaten` | Where and when parking **is permitted**. The positive-permission counterpart to most other rules. |
| `pbuss` | Spaces where only **buses** may park. All other vehicles forbidden. |
| `plastbil` | Spaces where only **trucks/lorries** may park. All other vehicles forbidden. |
| `pmotorcykel` | Spaces where only **motorcycles** may park. All other vehicles forbidden. |
| `prorelsehindrad` | Spaces where only **disabled drivers with valid permits** may park. |

Note the asymmetry: only one positive-permission föreskrift
(`ptillaten`), but several class-restricted ones. Forbidden-parking
zones (`Parkering förbjuden`), tariff areas (`Taxaområden`), and
residential zones (`Boendeparkeringsområden`) are exposed only via
WMS/WFS, **not** via LTF-Tolken — see file 05.

## Operations (per föreskrift)

| Operation | Path suffix | Use case |
|-----------|-------------|----------|
| `all` | `/all` | Bulk dump, useful for initial data ingest |
| `weekday` | `/weekday/{weekday}` | All rules applicable on a given weekday (Swedish names: måndag, tisdag, onsdag, torsdag, fredag, lördag, söndag) |
| `area` | `/area/{areaName}` | All rules within a named administrative area |
| `street` | `/street/{streetName}` | All rules on a named street |
| `within` | `/within?radius={m}&lat={lat}&lng={lng}` | All rules within a radius of a coordinate. **The primary geofence-query path.** |
| `untilNextWeekday` | `/untilNextWeekday` | Rules from now until the same time next weekday (servicedagar only) |
| `wfs` | `/wfs` | WFS pass-through proxy |

## Common parameters

| Parameter | Required | Description |
|-----------|----------|-------------|
| `apiKey` | Yes | Issued key |
| `outputFormat` | No | `xml` (default) or `json` |
| `maxFeatures` | No | Cap on result count |
| `callback` | No | JSONP callback name (only with `outputFormat=json`) |

Coordinate systems are configured per API key at registration time.
Common choices: SWEREF99 TM (default for Sweden) or WGS84. See
<https://openstreetgs.stockholm.se/home/Guide/coordinatesystem>.

## Example request templates

```
# All servicedagar (cleaning days)
https://openparking.stockholm.se/LTF-Tolken/v1/servicedagar/all
  ?outputFormat=json&apiKey=YOUR_KEY

# Cleaning windows happening on Mondays
https://openparking.stockholm.se/LTF-Tolken/v1/servicedagar/weekday/måndag
  ?outputFormat=json&apiKey=YOUR_KEY

# Cleaning windows in the next 24h from a vehicle's location (geofence)
https://openparking.stockholm.se/LTF-Tolken/v1/servicedagar/untilNextWeekday
  ?outputFormat=json&apiKey=YOUR_KEY

# All parking-permitted rules within 100m of a point
https://openparking.stockholm.se/LTF-Tolken/v1/ptillaten/within
  ?radius=100&lat=59.32784&lng=18.05306&outputFormat=json&apiKey=YOUR_KEY

# All MC parking on Odengatan
https://openparking.stockholm.se/LTF-Tolken/v1/pmotorcykel/street/Odengatan
  ?outputFormat=json&apiKey=YOUR_KEY

# All bus parking within 100m of a point
https://openparking.stockholm.se/LTF-Tolken/v1/pbuss/within
  ?radius=100&lat=59.32784&lng=18.05306&outputFormat=json&apiKey=YOUR_KEY
```

## Response shape (high-level)

LTF-Tolken returns WFS-style feature collections. Each feature
includes:
- A geometry (point, line, or polygon depending on the föreskrift)
- Time-window descriptors (which days, which hours)
- A reference to the underlying föreskrift (numbered identifier in the
  Trafikkontoret database, traceable to STFS)
- Optional metadata (max stay duration, special conditions, vehicle
  class restrictions)

Exact JSON/XML schemas are not published in the help pages and must
be derived empirically from a few sample responses once a key is
issued. Reserve a small data-modeling spike for this once you have
authenticated access.

## Caching strategy

The underlying föreskrift database changes infrequently (decisions
are taken by the city's parking commission and published periodically).
A reasonable refresh policy:
- Bulk `/all` dumps for each föreskrift: nightly
- Geofence `/within` queries: cache 24h per (lat-rounded, lng-rounded)
  bucket; invalidate on the nightly bulk-dump refresh
- `/untilNextWeekday`: cache only for the duration of a single user
  session (windows shift hourly)

## Equivalent WMS / WFS access

All LTF-Tolken datasets are also available via standard
WMS/WFS at the same domain. The mapping table:

| LTF-Tolken endpoint | WMS/WFS layer |
|---------------------|---------------|
| `servicedagar` | `LTFR_SERVICEDAG` |
| `ptillaten` | `LTFR_P_TILLATEN` |
| `pbuss` | `LTFR_P_BUSS` |
| `plastbil` | `LTFR_P_LASTBIL` |
| `pmotorcykel` | `LTFR_P_MOTORCYKEL` |
| `prorelsehindrad` | `LTFR_P_RORELSEHINDRADE` |

WMS/WFS is more powerful for spatial joins and bounding-box queries
but more verbose to call. Use LTF-Tolken for runtime queries; use
WMS/WFS for offline/analytical jobs and for accessing the layers that
are WFS-only (residential zones, tariff areas, etc — see file 05).

WMS/WFS endpoint info:
> <https://openparking.stockholm.se/Home/Gs>

OGC API Features (newer, RESTful):
> <https://openparking.stockholm.se/Home/OgcApi>

## Visualization (useful for debugging)

Trafikkontoret publishes a viewer that renders all of these layers:
> <https://openstreetgs.stockholm.se/tkkarta/v3/preview_opendata/>

Recommended for verifying that an interpreted rule matches the
reality on the ground.

## Reliability and known gaps

- The interpretation pipeline relies on humans entering föreskrifter
  in a structured way; about 95% are clean. Edge cases (newly
  decided rules, complex multi-time-window signs) sometimes lag in
  the LTF-Tolken view.
- Servicedagar are the highest-quality dataset (well-structured by
  nature: fixed weekday + time window).
- `ptillaten` has the most variability and edge cases.
- Stockholm boundary: the dataset only covers Stockholm Stad. Solna,
  Sundbyberg, Lidingö, Nacka, etc., are different municipalities
  with their own (often more limited) open-data programmes. See
  file 07 for the cross-municipality strategy.

## Sources

- Trafikkontoret — Parking API home:
  <https://openparking.stockholm.se/Home/Parking>
- Datasets index:
  <https://openparking.stockholm.se/Home/Data>
- API key request:
  <https://openparking.stockholm.se/Home/Key>
- Getting-started guide:
  <https://openstreetgs.stockholm.se/home/Guide/TutGetStarted>
- Trafiklab (Sweden's transit-data clearinghouse, lists Stockholm
  open data among others):
  <https://www.trafiklab.se/api/other-apis/stockholm-stad/>
