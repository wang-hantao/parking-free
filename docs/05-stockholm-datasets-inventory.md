# 05 — Stockholm Datasets Inventory

A complete catalogue of the open datasets exposed by Trafikkontoret
that are relevant to a parking-rules platform. Includes the layers
that are **only** available via WMS/WFS (not LTF-Tolken) — these are
critical for residential parking and the 10m rule.

## Parking datasets — exposed via LTF-Tolken AND WMS/WFS

These are the ones in file 04, repeated here for completeness:

| Dataset (Swedish) | LTF-Tolken endpoint | WFS layer name |
|-------------------|---------------------|----------------|
| Parkering tillåten | `ptillaten` | `LTFR_P_TILLATEN` |
| Servicedagar | `servicedagar` | `LTFR_SERVICEDAG` |
| Specialparkering för Buss | `pbuss` | `LTFR_P_BUSS` |
| Specialparkering för Lastbil | `plastbil` | `LTFR_P_LASTBIL` |
| Specialparkering för MC | `pmotorcykel` | `LTFR_P_MOTORCYKEL` |
| Specialparkering för rörelsehindrade | `prorelsehindrad` | `LTFR_P_RORELSEHINDRADE` |

## Parking datasets — WMS/WFS ONLY (not in LTF-Tolken)

These are critical and frequently missed:

| Dataset (Swedish) | What it is | Why it matters |
|-------------------|-----------|----------------|
| **Boendeparkeringsområden** | Residential parking zone polygons | Required to know whether a vehicle's owner has a valid permit for the spot. Permit zones are NOT the same as administrative districts. |
| **Taxaområden** | Tariff zone polygons | Defines pricing zones. Stockholm has 30 zones. Required to know what rate applies. |
| **Parkering förbjuden** | Parking-forbidden geometries | The negative space — lines/polygons where parking is prohibited even without a sign. |
| **Ändamålsplats** | Purpose-specific spots: load zones, taxi spots, school transport, drop-off, etc. | Most are time-restricted. Misuse during restricted hours is a fine class. |

Access via:
```
https://openparking.stockholm.se/Home/Gs       # WMS/WFS endpoints
https://openparking.stockholm.se/Home/OgcApi   # OGC API (newer)
```

The OGC API Features endpoint is preferred for new development; it's
RESTful and returns GeoJSON natively.

## Local Traffic Regulations (LTF) — broader set

Beyond parking, full LTF data is exposed as three layers:

| Layer | Geometry | Description |
|-------|----------|-------------|
| `LTFR_FORESKRIFT` | Lines | LTFs with linear extent (along streets) |
| `LTFR_FORESKRIFT_YTA` | Polygons | LTFs with areal extent (zones) |
| `LTFR_FORESKRIFT_GEOM` | Both | Union of the above |

Regulation types covered include:
- Other special traffic rules
- Axle/bogie/triple-axle weight or gross weight limits below default
- Bus stops
- Roundabouts and bicycle crossings
- Direction prohibitions/mandates (turn or drive in a specific direction)
- Overtaking prohibitions
- Vehicle traffic prohibitions
- Free-text-format regulations
- Pedestrian streets and shared-space areas
- Priority road, motorway, motortrafikled
- Restrictions on vehicle width or length below default
- Lanes for line-traffic vehicles
- Purpose-spots and EV charging spots

For a parking platform, the most-used non-parking LTF layer is
**Pedestrian streets and shared-space areas** (gågata / gångfartsområde)
— vehicles cannot park here without explicit permission.

## NVDB datasets (national road database, exposed via Stockholm)

The Swedish NVDB (Nationell vägdatabas) is the road-network ground
truth. Stockholm exposes a subset relevant to local enforcement.
Critical layers for a parking platform:

| Layer | Why it matters for parking |
|-------|----------------------------|
| **Korsning** (Junctions) | Required to compute the 10m-before-junction rule. This is the single most common "hidden trap" fine. |
| **Hastighetsgräns** (Speed limits) | Useful context for some derived rules |
| **Tättbebyggt område** (Urban area) | Determines whether municipal authority applies (vs Länsstyrelsen) |
| **Gågata** (Pedestrian street) | No parking allowed |
| **Gångfartsområde** (Walking-pace area) | Parking only in marked spots |
| **Cirkulationsplats** (Roundabout) | No parking on/near; geometry needed for offset rule |
| **Kollektivkörfält** (Bus lane) | No stopping; high fine tier |
| **GCM-passage** (Gång/Cykel/Moped passage) | Triggers the 10m rule |

The Korsning + GCM-passage layers are what you spatially join to a
candidate parking location to determine "are you within 10m of any
of these?" This is the cornerstone of any sign-aware parking app and
is **not** what LTF-Tolken provides.

## Other potentially-useful datasets

| Dataset | Use case |
|---------|----------|
| Parkeringsautomat | Locations of physical pay-and-display machines (legacy; less useful since Betala P discontinued) |
| Laddplats | EV charging-bay geometries; relevant because parking on a laddplats without charging is its own fine class |
| Cykelparkering | Bicycle parking, not relevant to cars but useful if expanding to mobility apps |
| Cykelstråk / Cykelplan | Bike paths, used together with GCM-passage for the 10m rule |
| Gatuarbete / TA-plan | Active street works that temporarily change parking rules |
| Vintervaghallning | Winter road maintenance schedules — temporary parking restrictions |
| Barmarksrenhallning | Bare-ground street cleaning (the wider context for servicedagar) |
| Markupplåtelse | Land-use permits (events, construction) that may close parking |

## Stockholm dataportalen — broader catalogue

The full city data portal:
> <https://dataportalen.stockholm.se/dataportalen/>

Most datasets are referenced by ID like `LvFeature4000022`. Each has
a metadata page reachable via:
```
https://dataportalen.stockholm.se/dataportalen/GetMetaDataById?id={id}
```

The metadata pages give canonical dataset descriptions, update
frequencies, and licensing info.

## Coordinate systems

Stockholm's default for elevations is RH 2000 (Rikets Höjdsystem 2000).
For 2D coordinates, choose at API-key registration:
- **SWEREF99 TM** (national Swedish standard) — typical
- **WGS84** (international, lat/lng) — for mobile apps

For geofence queries from mobile, WGS84 is usually simpler. Be
explicit when calling `within` operations — radius is interpreted in
the configured coordinate system's units (metres for SWEREF99 TM).

## Geometry classes

Different parking datasets store geometry differently. Some are line
features along the curb, some are point features at parking-meter
locations, some are area polygons. When designing your data model:

- Don't assume one geometry per regulation
- Some föreskrifter are exposed as **both lines and polygons** in
  WFS — use `LTFR_FORESKRIFT_GEOM` as the unified view if you don't
  care which
- Polygon containment is faster than line buffering for runtime
  geofence queries; precompute polygon buffers for line-based rules
  during ingest

## Update cadence (empirical)

- LTF base data: changes when the parking commission decides; on
  the order of weekly small additions, with periodic larger
  re-organisations (e.g. new residential zones)
- Servicedagar: very stable, changes ~once per year
- Tariffs and zone definitions: stable in geometry, but pricing
  metadata may change yearly (set by Trafiknämnden each fall, taking
  effect next February)
- NVDB layers: weekly to monthly

A nightly bulk refresh for the lower 95%, with manual triggers for
known-breaking events (city decisions), works well.

## Sources

- Stockholm Trafikkontoret — full dataset list:
  <https://openparking.stockholm.se/Home/Data>
- WMS/WFS endpoint info:
  <https://openparking.stockholm.se/Home/Gs>
- OGC API Features:
  <https://openparking.stockholm.se/Home/OgcApi>
- Stockholm Dataportalen:
  <https://dataportalen.stockholm.se/dataportalen/>
- Coordinate-system selection guide:
  <https://openstreetgs.stockholm.se/home/Guide/coordinatesystem>
