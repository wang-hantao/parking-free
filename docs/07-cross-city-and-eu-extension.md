# 07 — Cross-City and EU Extension

How to reuse the platform across Swedish municipalities and into the
EU. The data model in file 05 is designed to be city-agnostic; this
file documents the substrates that make that practical.

## Strategy in one sentence

Build the rule-evaluation kernel against a normalized internal
schema. Write a thin **adapter** per data source. The kernel never
knows which city it's serving; the adapter handles ingestion, schema
mapping, and provenance.

## Sweden: the STFS / RDT national database

**STFS** (Svensk trafikföreskriftssamling) is the national database
where all Swedish authorities are legally required to file local
traffic regulations. **RDT** (Rikstäckande databas för
trafikföreskrifter) is the underlying database; STFS is the public
face.

Authority: Transportstyrelsen (Swedish Transport Agency).

Why this matters: every Swedish municipality publishes to the same
database. If your platform integrates with RDT directly, you can
serve any Swedish city — Stockholm's LTF-Tolken is essentially a
city-specific re-interpretation of data that lives in RDT.

### Filing authorities

Each municipality, county board (Länsstyrelse), Trafikverket,
Polismyndigheten, Försvarsmakten, and Transportstyrelsen itself
files into STFS:

- Municipalities: parking, speed, urban-area definitions
- Länsstyrelser: roads outside urban areas, dangerous goods, race
  events
- Trafikverket: speed 80–120, weight class 4 conditions
- Polisen: emergency LTFs

### What's in STFS

Two registers:
- **Föreskriftsregistret** — historical record of all LTFs ever
  published (post-2010)
- **Gällanderegistret** — currently effective LTFs at a chosen date

STFS is designed to accept **machine-processable** and
**road-network-anchored** data, which is exactly what enables
software like LTF-Tolken to exist.

### How to access STFS programmatically

There is an external web service interface documented in:
> RDT-Externt Webservice Gränssnitt (specification)

Available from Transportstyrelsen's "Styrande och stödjande
dokument" page:
> <https://www.transportstyrelsen.se/sv/vagtrafik/trafikregler-och-vagmarken/trafikregler/stfs-for-myndigheter-som-beslutar-trafikforeskrifter/grunderna-for-stfs/styrande-och-stodjande-dokument/>

The full RDT data catalogue and XML format specification are
available on the same page. Note: STFS access for production
ingestion may require coordination with Transportstyrelsen; the
public web search is not a high-volume API.

### What STFS does NOT solve

- It tells you the rule, not the **interpretation** for end users.
  Stockholm's LTF-Tolken adds value precisely by interpreting the
  raw föreskrifter for machine consumption.
- Other municipalities have not all built their own LTF-Tolken
  equivalents. For non-Stockholm cities, you may need to do the
  interpretation yourself from the raw STFS data.
- Geometry quality varies by municipality. Stockholm has good
  road-network linkage; some smaller municipalities have only
  textual descriptions.

### National adjacency datasets

For the road network and junction geometry (needed for the 10m rule
nationally), use **NVDB** (Nationell Vägdatabas) directly:
- Operated by Trafikverket
- Available via Trafikverket's Lastkajen and Datex II APIs
- Stockholm's NVDB layers (file 05) are a regional view; nationally
  you need Trafikverket's full feed

> <https://www.trafikverket.se/e-tjanster/hamta-data-fran-trafikverket/>

## EU: DATEX II

**DATEX II** is a European standard for traffic information exchange.
XML-schema-based, used by most European road authorities including
Trafikverket, the Norwegian Statens Vegvesen, the Dutch NDW, the
German road authority, etc.

Relevant DATEX II profiles:
- **Parking facilities** — static info about car parks (location,
  capacity, prices, opening hours)
- **Parking availability** — dynamic occupancy
- **Safety-Related Traffic Information (SRTI)**
- **Static road network** — road geometry, restrictions

Designing your internal schema as a superset of the DATEX II parking
profile means a Norwegian or Dutch city becomes a new adapter, not a
re-architecture.

EU regulatory context:
- **Regulation 2022/670** on EU-wide multimodal travel information
  services makes parking-data publication increasingly mandatory
  across member states.
- The European NAP (National Access Point) per country is the
  starting place for a country's data — typically a national portal
  that aggregates DATEX II feeds.

### National Access Points (selected)

| Country | NAP |
|---------|-----|
| Sweden | <https://www.trafiklab.se/> + Trafikverket's Datautbytesportal |
| Norway | <https://transportportal.no/> |
| Denmark | <https://www.nap.fdfm.dk/> |
| Germany | <https://www.mobilithek.info/> |
| Netherlands | <https://www.ndw.nu/> |
| Finland | <https://www.fintraffic.fi/> |

For the Nordics specifically, the data is high-quality and
DATEX-compliant. Adding Oslo, Copenhagen, Helsinki to a Stockholm-
first product is mostly an adapter exercise.

## City-onboarding playbook

When adding a new city to the platform, work through this
checklist. The model from file 05 is what you target; this is how
you populate it.

### 1. Identify the legal regime

- Country-level traffic ordinance (TrF for Sweden, etc.)
- Local-rule mechanism (LTF for Sweden; equivalents elsewhere)
- Public vs private parking distinction (very different in some
  countries, e.g. Germany has no equivalent of Kontrollavgift)
- Permit categories (residential, disabled, commercial)
- Fine structure and authority

### 2. Locate the data sources

- National database (STFS for Sweden; equivalents per country)
- City-specific overlay APIs (LTF-Tolken for Stockholm; check what
  the new city has)
- Road-network source (NVDB, OpenStreetMap as a fallback)
- Holiday calendar for the country/region

### 3. Identify the operators

- Which payment apps are usable in this city
- Whether there's an authorization scheme (like Stockholm's) or a
  free market
- Public vs private split

### 4. Map to the internal schema

For each source, write an adapter that produces:
- `Regulation` records with provenance (source ID)
- `Rule` records with normalized vehicle-class filters
- `TimeWindow` records with normalized day-types
- `AppliesTo` links to `RoadSegment`, `Zone`, `ParkingArea`,
  or `PointOfInterest`
- `HolidayCalendar` entries for the country/region

### 5. Verify with sample queries

For 20–50 known coordinates, evaluate the rule kernel and compare
against ground truth (visit, photograph signs, or use the city's
official map view). Track the verdict-accuracy rate; aim for >95%
before launch.

### 6. Set up monitoring

- Daily re-fetch from upstream
- Diff detection on the regulation set
- Alert on >X% rule changes in a 24h window (suggests a city policy
  change you need to communicate to users)

## Cross-city differences to expect

| Dimension | Stockholm | Other Swedish | Nordic | EU |
|-----------|-----------|---------------|--------|-----|
| LTF in machine-readable form | Yes (LTF-Tolken) | Partial via STFS | Varies, generally good | Varies; DATEX II is the lingua franca |
| Authorization-system for operators | Yes, 4 providers | Free market in most cities | Mixed; Oslo has similar | Highly varied |
| Sign grammar | Swedish bracket/red-day | Same | Different per country | Different per country |
| "10m rule" equivalent | 10m before crossings/junctions | Same | Differs (some countries 5m) | Differs widely |
| Strict liability for owner | Yes | Yes | Yes (most) | Mostly yes |
| Cross-border enforcement | Improving via EU framework | Same | Improving | EU directive in force |

## Sources

- Transportstyrelsen — STFS overview:
  <https://www.transportstyrelsen.se/sv/vagtrafik/trafikregler-och-vagmarken/trafikregler/stfs-for-myndigheter-som-beslutar-trafikforeskrifter/om-stfs/>
- Transportstyrelsen — STFS public search:
  <https://rdt.transportstyrelsen.se/rdt/defaultstfs.aspx>
- Transportstyrelsen — STFS technical documentation:
  <https://www.transportstyrelsen.se/sv/vagtrafik/trafikregler-och-vagmarken/trafikregler/stfs-for-myndigheter-som-beslutar-trafikforeskrifter/grunderna-for-stfs/styrande-och-stodjande-dokument/>
- Trafikverket — Open data (incl. Datex II):
  <https://www.trafikverket.se/e-tjanster/hamta-data-fran-trafikverket/>
- Trafiklab (Sweden's data clearinghouse):
  <https://www.trafiklab.se/>
- DATEX II official:
  <https://www.datex2.eu/>
