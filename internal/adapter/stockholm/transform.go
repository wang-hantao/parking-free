package stockholm

import (
	"encoding/json"
	"errors"
	"fmt"
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
	case PTillaten, PBuss, PLastbil, PMotorcykel, PRorelsehindrad:
		return nil, fmt.Errorf("%w: %s schema not yet captured; run `ingester dump <dir> %s` and share the sample so transform can be written", ErrSchemaPending, f, f)
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
type IngestBatch struct {
	RoadSegments []domain.RoadSegment
	Regulations  []domain.Regulation
	Rules        []domain.Rule
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
		startMin := hhmmToMin(propInt(props, "START_TIME"))
		endMin := hhmmToMin(propInt(props, "END_TIME"))
		weekday := propStr(props, "START_WEEKDAY")
		mask := weekdayMask(weekday)

		batch.Rules = append(batch.Rules, domain.Rule{
			RegulationID: citation, // placeholder; ingester resolves to UUID
			Kind:         domain.RuleForbid,
			Priority:     10, // servicedagar are strict, take precedence
			TimeWindows: []domain.TimeWindow{{
				WeekdayMask: mask,
				StartMin:    startMin,
				EndMin:      endMin,
			}},
			AppliesTo: []domain.AppliesTo{{
				Kind:     domain.TargetRoadSegment,
				TargetID: segRef, // placeholder; ingester resolves to UUID
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
