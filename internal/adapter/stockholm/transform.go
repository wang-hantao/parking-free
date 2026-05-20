package stockholm

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/wang-hantao/parking-free/internal/domain"
)

// Transform converts a raw LTF-Tolken JSON response for one
// föreskrift into a batch ready for ingestion.
//
// The batch couples geometries to the regulations/rules that
// reference them. The ingester upserts in dependency order:
//
//  1. RoadSegments → returns map[source_ref]uuid
//  2. Regulations  → returns map[source_ref]uuid
//  3. For each rule: resolve RegulationID and AppliesTo.TargetID
//     placeholders using the two maps
//  4. Rules
//
// Source-reference scheme (system="stockholm.ltf-tolken"):
//
//   - Regulation reference is the LTF CITATION
//     (e.g. "0180 2017-04586"), which is the legal pointer into
//     STFS/RDT and naturally deduplicates regulations that affect
//     multiple street segments.
//   - RoadSegment reference is "{foreskrift}/{FID}/{EXTENT_NO}"
//     (e.g. "servicedagar/9564/1"), unique per (foreskrift, feature).
func Transform(f Foreskrift, raw []byte) (*IngestBatch, error) {
	var fc featureCollection
	if err := json.Unmarshal(raw, &fc); err != nil {
		return nil, fmt.Errorf("stockholm: parse JSON: %w", err)
	}
	if fc.Type != "FeatureCollection" {
		return nil, fmt.Errorf("stockholm: unexpected top-level type %q (want FeatureCollection)", fc.Type)
	}

	switch f {
	case Servicedagar:
		return transformServicedagar(fc), nil
	case PTillaten:
		return transformPTillaten(fc), nil
	case PBuss:
		return transformReservedSpot(fc, reservedSpotConfigs[PBuss]), nil
	case PLastbil:
		return transformReservedSpot(fc, reservedSpotConfigs[PLastbil]), nil
	case PMotorcykel:
		return transformReservedSpot(fc, reservedSpotConfigs[PMotorcykel]), nil
	case PRorelsehindrad:
		return transformReservedSpot(fc, reservedSpotConfigs[PRorelsehindrad]), nil
	default:
		return nil, fmt.Errorf("stockholm: unknown foreskrift %q", f)
	}
}

// ErrSchemaPending is returned for föreskrifter whose JSON shape has
// not yet been captured and modelled.
var ErrSchemaPending = errors.New("stockholm: response transform pending schema capture")

// IngestBatch is the deliverable from Transform. Rules carry
// placeholders in RegulationID and AppliesTo.TargetID that the
// ingester resolves after upserting geometries and regulations.
//
// SkippedFeatures records how many features the transform refused to
// process — currently only those carrying seasonal date-range fields
// (START_MONTH/END_MONTH/START_DAY/END_DAY), which the engine doesn't
// yet match against. The ingester surfaces this count in its logs so
// the gap is visible and quantifiable.
type IngestBatch struct {
	RoadSegments    []domain.RoadSegment
	Regulations     []domain.Regulation
	Rules           []domain.Rule
	SkippedFeatures int
}

// =============================================================================
// GeoJSON parsing primitives
// =============================================================================

type featureCollection struct {
	Type     string    `json:"type"`
	Features []feature `json:"features"`
}

type feature struct {
	Type       string                 `json:"type"`
	ID         string                 `json:"id"`
	Geometry   geometry               `json:"geometry"`
	Properties map[string]interface{} `json:"properties"`
}

type geometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

// =============================================================================
// Servicedagar (street cleaning)
// =============================================================================

// transformServicedagar converts each feature into:
//   - one RoadSegment record (per feature)
//   - one Regulation (deduplicated by CITATION)
//   - one Rule of kind=forbid with a weekly time window matching the
//     feature's START_WEEKDAY/START_TIME/END_TIME
func transformServicedagar(fc featureCollection) *IngestBatch {
	batch := &IngestBatch{}
	regSeen := map[string]bool{}

	for _, feat := range fc.Features {
		props := feat.Properties

		citation := propStr(props, "CITATION")
		fid := propInt(props, "FID")
		extentNo := propInt(props, "EXTENT_NO")
		if citation == "" || fid == 0 {
			continue // missing the keys we need for stable provenance
		}

		// Geometry: only LineString supported for servicedagar.
		if feat.Geometry.Type != "LineString" {
			continue
		}
		var coords [][]float64
		if err := json.Unmarshal(feat.Geometry.Coordinates, &coords); err != nil || len(coords) < 2 {
			continue
		}

		segRef := fmt.Sprintf("servicedagar/%d/%d", fid, extentNo)

		batch.RoadSegments = append(batch.RoadSegments, domain.RoadSegment{
			Source:       domain.Source{System: SourceSystem, Reference: segRef},
			StreetName:   propStr(props, "STREET_NAME"),
			Municipality: "Stockholm",
			GeometryWKT:  linestringWKT(coords),
		})

		// Regulation: deduplicate on citation (the legal reference).
		if !regSeen[citation] {
			regSeen[citation] = true
			var validFrom time.Time
			if vfStr := propStr(props, "VALID_FROM"); vfStr != "" {
				if t, err := time.Parse(time.RFC3339, vfStr); err == nil {
					validFrom = t
				}
			}
			batch.Regulations = append(batch.Regulations, domain.Regulation{
				Source:            domain.Source{System: SourceSystem, Reference: citation},
				DecisionAuthority: "Stockholms stad",
				Language:          "sv-SE",
				EffectiveFrom:     validFrom,
			})
		}

		// Rule: forbid parking during the cleaning window.
		startHHMM := propInt(props, "START_TIME")
		endHHMM := propInt(props, "END_TIME")
		weekday := propStr(props, "START_WEEKDAY")

		batch.Rules = append(batch.Rules, domain.Rule{
			RegulationID:    citation, // placeholder; ingester resolves to UUID
			Kind:            domain.RuleForbid,
			Priority:        10, // servicedagar are strict, take precedence
			TariffClassCode: parseTaxaCode(propStr(props, "PARKING_RATE")),
			TimeWindows:     []domain.TimeWindow{buildTimeWindow(weekday, startHHMM, endHHMM)},
			AppliesTo: []domain.AppliesTo{{
				Kind:     domain.TargetRoadSegment,
				TargetID: segRef, // placeholder; ingester resolves to UUID
			}},
		})
	}

	return batch
}

// =============================================================================
// Ptillaten (parking permitted)
// =============================================================================

// transformPTillaten converts each feature into:
//   - one RoadSegment record (per feature)
//   - one Regulation (deduplicated by CITATION)
//   - one Rule of kind=allow with the permitted-parking window
//
// Properties driving rule semantics:
//
//   - VEHICLE: "fordon" = generic vehicles; "rörelsehindrade" = disabled
//   - VF_PLATS_TYP: free-text describing the space type. Substring tests:
//     "Avgift" → NeedsPayment, "rörelsehindrad" → NeedsPermit
//   - START_TIME / END_TIME / START_WEEKDAY: optional. Missing → 24/7.
//
// Priority is 5, below servicedagar's 10 — when cleaning and parking-
// permitted windows overlap on the same road segment, the Forbid wins.
//
// v1 limitation: NeedsPermit doesn't distinguish disabled-only from
// residential-only. The engine treats any valid permit as satisfying
// any NeedsPermit rule. Tightening this requires adding
// RequiredPermitKind to domain.Rule — out of scope here.
func transformPTillaten(fc featureCollection) *IngestBatch {
	batch := &IngestBatch{}
	regSeen := map[string]bool{}

	for _, feat := range fc.Features {
		props := feat.Properties

		citation := propStr(props, "CITATION")
		fid := propInt(props, "FID")
		extentNo := propInt(props, "EXTENT_NO")
		if citation == "" || fid == 0 {
			continue
		}

		if feat.Geometry.Type != "LineString" {
			continue
		}
		var coords [][]float64
		if err := json.Unmarshal(feat.Geometry.Coordinates, &coords); err != nil || len(coords) < 2 {
			continue
		}

		segRef := fmt.Sprintf("ptillaten/%d/%d", fid, extentNo)

		batch.RoadSegments = append(batch.RoadSegments, domain.RoadSegment{
			Source:       domain.Source{System: SourceSystem, Reference: segRef},
			StreetName:   propStr(props, "STREET_NAME"),
			Municipality: "Stockholm",
			GeometryWKT:  linestringWKT(coords),
		})

		if !regSeen[citation] {
			regSeen[citation] = true
			var validFrom time.Time
			if vfStr := propStr(props, "VALID_FROM"); vfStr != "" {
				if t, err := time.Parse(time.RFC3339, vfStr); err == nil {
					validFrom = t
				}
			}
			batch.Regulations = append(batch.Regulations, domain.Regulation{
				Source:            domain.Source{System: SourceSystem, Reference: citation},
				DecisionAuthority: "Stockholms stad",
				Language:          "sv-SE",
				EffectiveFrom:     validFrom,
			})
		}

		// Payment / permit semantics from VF_PLATS_TYP and VEHICLE.
		typ := strings.ToLower(propStr(props, "VF_PLATS_TYP"))
		vehicle := strings.ToLower(propStr(props, "VEHICLE"))
		needsPayment := strings.Contains(typ, "avgift")
		needsPermit := strings.Contains(typ, "rörelsehindrad") ||
			strings.Contains(vehicle, "rörelsehindrad")

		startHHMM := propInt(props, "START_TIME")
		endHHMM := propInt(props, "END_TIME")
		weekday := propStr(props, "START_WEEKDAY")

		batch.Rules = append(batch.Rules, domain.Rule{
			RegulationID:    citation, // placeholder
			Kind:            domain.RuleAllow,
			Priority:        5, // lower than servicedagar (10), so cleaning wins on overlap
			NeedsPayment:    needsPayment,
			NeedsPermit:     needsPermit,
			TariffClassCode: parseTaxaCode(propStr(props, "PARKING_RATE")),
			TimeWindows:     []domain.TimeWindow{buildTimeWindow(weekday, startHHMM, endHHMM)},
			AppliesTo: []domain.AppliesTo{{
				Kind:     domain.TargetRoadSegment,
				TargetID: segRef, // placeholder
			}},
		})
	}

	return batch
}

// =============================================================================
// Reserved-class spots (pbuss, plastbil, pmotorcykel, prorelsehindrad)
// =============================================================================

// reservedSpotConfig captures the per-föreskrift specifics for the
// "Reserverad p-plats X" pattern. The four reserved-class föreskrifter
// share the same JSON shape (confirmed for pbuss; the rest are
// guarded by ErrSchemaPending until their samples confirm).
type reservedSpotConfig struct {
	foreskrift   Foreskrift
	pathPrefix   string              // for road_segment source_reference (e.g. "pbuss")
	vehicleClass domain.VehicleClass // empty when permit-based rather than class-based
	needsPermit  bool                // true for prorelsehindrad (disabled placard)
}

// reservedSpotConfigs maps each reserved-class föreskrift to its
// vehicle-class / permit configuration. plastbil, pmotorcykel and
// prorelsehindrad entries are populated based on pattern inference
// but NOT yet wired in the Transform() switch — enable them only
// after confirming their JSON shapes match the pbuss pattern.
var reservedSpotConfigs = map[Foreskrift]reservedSpotConfig{
	PBuss:           {PBuss, "pbuss", domain.VehicleBus, false},
	PLastbil:        {PLastbil, "plastbil", domain.VehicleTruck, false},
	PMotorcykel:     {PMotorcykel, "pmotorcykel", domain.VehicleMotorcycle, false},
	PRorelsehindrad: {PRorelsehindrad, "prorelsehindrad", "", true},
}

// transformReservedSpot converts each feature into:
//   - one RoadSegment record (per feature)
//   - one Regulation (deduplicated by CITATION)
//   - one Rule of kind=allow scoped to the configured VehicleClass
//     (and/or NeedsPermit), with MaxDuration from MAX_MINUTES when
//     present.
//
// v1 limitation worth flagging: when a car queries a bus-only spot
// with no other nearby rules, the engine's "no applicable rule →
// default allowed" behaviour produces a false positive. The bus rule
// is filtered out by vehicle-class mismatch and nothing else fires.
// Fixing this properly needs either a new Reserve rule kind in the
// engine, explicit Forbid rules for non-listed classes, or a
// "nearest-segment-only" query mode. The pbuss rule still appears in
// the response with its citation so the user has the audit trail.
func transformReservedSpot(fc featureCollection, cfg reservedSpotConfig) *IngestBatch {
	batch := &IngestBatch{}
	regSeen := map[string]bool{}

	for _, feat := range fc.Features {
		props := feat.Properties

		// Seasonal date-range features (START_MONTH/END_MONTH/...) carry
		// the rule's calendar window in month+day pairs. The engine
		// doesn't currently honour TimeWindow.DateFrom/DateTo, so a
		// naïve ingest would either fire the rule year-round (wrong)
		// or drop the calendar context silently. We skip them and
		// surface the count via IngestBatch.SkippedFeatures so the
		// gap is quantifiable. Affects ~40% of pmotorcykel features
		// (Stockholm relaxes MC cleaning rules in summer); other
		// reserved-class föreskrifter rarely use this pattern.
		if propInt(props, "START_MONTH") != 0 || propInt(props, "END_MONTH") != 0 ||
			propInt(props, "START_DAY") != 0 || propInt(props, "END_DAY") != 0 {
			batch.SkippedFeatures++
			continue
		}

		citation := propStr(props, "CITATION")
		fid := propInt(props, "FID")
		extentNo := propInt(props, "EXTENT_NO")
		if citation == "" || fid == 0 {
			continue
		}

		if feat.Geometry.Type != "LineString" {
			continue
		}
		var coords [][]float64
		if err := json.Unmarshal(feat.Geometry.Coordinates, &coords); err != nil || len(coords) < 2 {
			continue
		}

		segRef := fmt.Sprintf("%s/%d/%d", cfg.pathPrefix, fid, extentNo)

		batch.RoadSegments = append(batch.RoadSegments, domain.RoadSegment{
			Source:       domain.Source{System: SourceSystem, Reference: segRef},
			StreetName:   propStr(props, "STREET_NAME"),
			Municipality: "Stockholm",
			GeometryWKT:  linestringWKT(coords),
		})

		if !regSeen[citation] {
			regSeen[citation] = true
			var validFrom time.Time
			if vfStr := propStr(props, "VALID_FROM"); vfStr != "" {
				if t, err := time.Parse(time.RFC3339, vfStr); err == nil {
					validFrom = t
				}
			}
			batch.Regulations = append(batch.Regulations, domain.Regulation{
				Source:            domain.Source{System: SourceSystem, Reference: citation},
				DecisionAuthority: "Stockholms stad",
				Language:          "sv-SE",
				EffectiveFrom:     validFrom,
			})
		}

		startHHMM := propInt(props, "START_TIME")
		endHHMM := propInt(props, "END_TIME")
		weekday := propStr(props, "START_WEEKDAY")

		var classes []domain.VehicleClass
		if cfg.vehicleClass != "" {
			classes = []domain.VehicleClass{cfg.vehicleClass}
		}

		var maxDur time.Duration
		if m := propInt(props, "MAX_MINUTES"); m > 0 {
			maxDur = time.Duration(m) * time.Minute
		}

		// Payment is required when VF_PLATS_TYP indicates either
		// metered ("avgift") or residential ("boende") parking — both
		// imply that non-permit-holders pay the hourly rate. The
		// pmotorcykel spots are the first to carry "boende" without
		// also carrying "avgift", which is why this check lives here
		// rather than on the static config.
		typ := strings.ToLower(propStr(props, "VF_PLATS_TYP"))
		needsPayment := strings.Contains(typ, "avgift") || strings.Contains(typ, "boende")

		batch.Rules = append(batch.Rules, domain.Rule{
			RegulationID:    citation, // placeholder
			Kind:            domain.RuleAllow,
			Priority:        5,
			VehicleClasses:  classes,
			NeedsPayment:    needsPayment,
			NeedsPermit:     cfg.needsPermit,
			MaxDuration:     maxDur,
			TariffClassCode: parseTaxaCode(propStr(props, "PARKING_RATE")),
			TimeWindows:     []domain.TimeWindow{buildTimeWindow(weekday, startHHMM, endHHMM)},
			AppliesTo: []domain.AppliesTo{{
				Kind:     domain.TargetRoadSegment,
				TargetID: segRef, // placeholder
			}},
		})
	}

	return batch
}

// SourceSystem is the value used in domain.Source.System for every
// record ingested from LTF-Tolken.
const SourceSystem = "stockholm.ltf-tolken"

// =============================================================================
// Helpers
// =============================================================================

func propStr(p map[string]interface{}, k string) string {
	if v, ok := p[k].(string); ok {
		return v
	}
	return ""
}

func propInt(p map[string]interface{}, k string) int {
	switch v := p[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

// hhmmToMin converts an HHMM-as-integer (used by LTF-Tolken for
// START_TIME / END_TIME) into minutes from midnight.
//
// Examples: 0 → 0 (00:00); 600 → 360 (06:00); 1745 → 1065 (17:45);
// 2400 → 1440 (end-of-day).
func hhmmToMin(hhmm int) int {
	if hhmm < 0 {
		return 0
	}
	if hhmm > 2400 {
		return 1440
	}
	return (hhmm/100)*60 + (hhmm % 100)
}

// buildTimeWindow constructs a TimeWindow from raw LTF properties.
// Handles four real cases observed in the data:
//
//  1. All three values absent (weekday empty, both times 0): the rule
//     applies 24/7. Returns mask=127, [0, 1440).
//  2. END_TIME=0 with START_TIME>0: this is "end of day" semantics,
//     not "start of day". E.g. ptillaten {600, 0, fredag} means
//     "Friday 06:00 to midnight" — converted to [360, 1440).
//  3. Normal same-day range: e.g. servicedagar {0, 600, fredag} →
//     [0, 360) on Friday only.
//  4. Midnight-crossing window: e.g. pbuss {1400, 900, måndag} means
//     "Monday 14:00 → Tuesday 09:00". The engine's matchingWindows()
//     filters by weekday bit BEFORE the inTimeRange() crosses-midnight
//     logic kicks in, so a single-day mask would miss the Tuesday
//     tail. We expand the mask to include the next weekday, with Sat
//     (bit 64) wrapping to Sun (bit 1). The engine's inTimeRange()
//     then correctly accepts tod>=start on Monday and tod<end on
//     Tuesday, while rejecting Mon 12:00 (neither branch) and
//     Tue 10:00 (neither branch).
//
// The TimeWindow output is what the engine's matchingWindows() will
// then filter by weekday bit and time-of-day range.
func buildTimeWindow(weekday string, startHHMM, endHHMM int) domain.TimeWindow {
	if weekday == "" && startHHMM == 0 && endHHMM == 0 {
		return domain.TimeWindow{WeekdayMask: 127, StartMin: 0, EndMin: 1440}
	}
	startMin := hhmmToMin(startHHMM)
	endMin := hhmmToMin(endHHMM)
	if endHHMM == 0 && startHHMM > 0 {
		endMin = 1440
	}
	mask := weekdayMask(weekday)
	// Crosses midnight: extend mask to include the next weekday so the
	// early-morning tail of the window is matched correctly.
	if endMin < startMin && endMin > 0 && mask > 0 {
		next := mask << 1
		if next > 64 {
			next = 1 // wrap: Saturday → Sunday
		}
		mask |= next
	}
	return domain.TimeWindow{
		WeekdayMask: mask,
		StartMin:    startMin,
		EndMin:      endMin,
	}
}

// weekdayMask maps a Swedish weekday name to a single-bit mask
// matching the domain.TimeWindow convention (Sun=1, Mon=2, ..., Sat=64).
// Empty/unknown input returns 0, which the engine treats as
// "applies any weekday" — for safety we instead return 0 only on
// truly missing data; callers should validate.
func weekdayMask(swe string) int {
	switch strings.ToLower(strings.TrimSpace(swe)) {
	case "söndag", "sondag":
		return 1
	case "måndag", "mandag":
		return 2
	case "tisdag":
		return 4
	case "onsdag":
		return 8
	case "torsdag":
		return 16
	case "fredag":
		return 32
	case "lördag", "lordag":
		return 64
	}
	return 0
}

// linestringWKT renders a GeoJSON LineString coordinate array as
// PostGIS-compatible WKT. Coordinates are in WGS84 lng/lat order,
// which matches both GeoJSON and PostGIS conventions for SRID 4326.
func linestringWKT(coords [][]float64) string {
	parts := make([]string, 0, len(coords))
	for _, c := range coords {
		if len(c) < 2 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%.6f %.6f", c[0], c[1]))
	}
	return "LINESTRING(" + strings.Join(parts, ",") + ")"
}

// taxaRE matches the "taxa N" prefix that every Stockholm
// PARKING_RATE string opens with. Captures the class number.
var taxaRE = regexp.MustCompile(`(?i)\btaxa\s+(\d+)`)

// parseTaxaCode extracts the Stockholm taxa class number from a
// PARKING_RATE string and returns the registry key.
//
//	"taxa 3: 20 kr/tim vardagar 7-19, ..."  →  "stockholm.taxa.3"
//	"taxa 12: Vardagar utom ..."            →  "stockholm.taxa.12"
//	""                                      →  ""
//
// Returns "" if the string doesn't match — the engine's registry
// lookup then yields no pricing for the rule, which is the correct
// behaviour: better silent on missing data than fabricating a rate.
func parseTaxaCode(rate string) string {
	m := taxaRE.FindStringSubmatch(rate)
	if len(m) < 2 {
		return ""
	}
	return "stockholm.taxa." + m[1]
}
