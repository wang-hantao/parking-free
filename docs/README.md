# Stockholm Parking — Source of Truth

A curated reference bundle covering Stockholm parking regulations, the data
APIs that expose them, and the abstractions needed to extend to other cities.
Designed to be the canonical knowledge base for a parking-rules platform.

## Compiled

- **Date**: 2026-05-07
- **Compiled by**: Claude (Opus 4.7), working from public sources
- **Intended use**: Project knowledge base / authoritative reference for an
  engineering team building a parking-rules platform

## File index

| File | Scope | Volatility |
|------|-------|------------|
| `01-national-rules.md` | Trafikförordningen rules that apply everywhere in Sweden (10m rule, 24h rule, parking definitions, who decides what) | Low — changes ~yearly |
| `02-sign-grammar.md` | How to read Swedish parking signs: brackets, red days, supplementary plates, parking discs | Very low |
| `03-fines-and-disputes.md` | Stockholm fine tiers (900/1100/1300 SEK), national range (75–1300), escalation, dispute procedure (yellow vs white ticket) | Medium — Stockholm raises tiers ~every few years |
| `04-stockholm-ltf-tolken-api.md` | Full reference for `openparking.stockholm.se/LTF-Tolken/v1/` — the machine-readable parking-regulation API | Low — schema stable |
| `05-stockholm-datasets-inventory.md` | All Stockholm parking + road-network datasets including WMS/WFS-only layers (residential zones, taxa areas, junctions for the 10m rule) | Low — additive changes |
| `06-stockholm-operators.md` | The 4-provider authorization system, EasyPark/Parkster/Mobill/ePARK, residential & utility permits, fees | Medium — pricing changes annually |
| `07-cross-city-and-eu-extension.md` | STFS/RDT national database, DATEX II EU standard, city-onboarding playbook | Low |
| `manifest.json` | Machine-readable index with source URLs and last-verified dates | Updated alongside docs |

## Freshness policy

Every assertion in these files is tied to a primary source linked inline.
The `manifest.json` contains the canonical source URL for each file's
content so a script can re-verify content periodically.

When ingesting into a live system: treat **API specs** (LTF-Tolken,
WFS endpoints) as stable; treat **fee amounts**, **operator lists**, and
**dataset inventories** as quarterly-refresh items.

## What's NOT in here (and why)

- **Live regulation data** (actual servicedagar/ptillaten records). Requires
  a Trafikkontoret API key — see `04-stockholm-ltf-tolken-api.md` for how
  to request one. Once you have a key, the same file gives you the URL
  templates to fetch.
- **Per-zone pricing**. Not in open data — must be sourced from the four
  authorized operators or scraped from `parkering.stockholm`.
- **Real-time spot occupancy**. Not exposed by Trafikkontoret as open data.
  Some private garages publish it via their own APIs.
- **Residential permit holders**. Personally identifiable; never published.
- **Operator zone-ID mappings**. Each operator (EasyPark etc.) maintains
  its own internal zone IDs. Cross-mapping requires partnership or scraping.

## Reading order for a new engineer

1. `01-national-rules.md` — the substrate every Swedish municipality builds on
2. `02-sign-grammar.md` — the failure mode that fines tourists most often
3. `03-fines-and-disputes.md` — the consequence space your product lives in
4. `04-stockholm-ltf-tolken-api.md` — the primary integration target
5. `05-stockholm-datasets-inventory.md` — the rest of the data surface
6. `06-stockholm-operators.md` — the commercial layer
7. `07-cross-city-and-eu-extension.md` — when Stockholm works, this is next

## Out-of-scope

- Driver licensing, vehicle registration, road tolls (trängselskatt),
  congestion-zone rules. These touch parking but are separate domains.
- EV-charging rules and tariffs (laddplats has its own regulation pattern;
  briefly noted in `06-stockholm-operators.md`).
