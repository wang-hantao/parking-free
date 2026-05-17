package stockholm

import (
	"errors"
	"os"
	"testing"

	"github.com/wang-hantao/parking-free/internal/domain"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	return b
}

func TestTransform_Servicedagar_RealSample(t *testing.T) {
	raw := loadFixture(t, "servicedagar_sample.json")
	batch, err := Transform(Servicedagar, raw)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}

	// 5 features → 5 road segments.
	if got := len(batch.RoadSegments); got != 5 {
		t.Errorf("road segments: want 5, got %d", got)
	}

	// 5 distinct CITATION values in the sample → 5 regulations.
	if got := len(batch.Regulations); got != 5 {
		t.Errorf("regulations: want 5, got %d", got)
	}

	// 5 rules, one per feature.
	if got := len(batch.Rules); got != 5 {
		t.Errorf("rules: want 5, got %d", got)
	}
}

func TestTransform_Servicedagar_FirstFeatureMapping(t *testing.T) {
	raw := loadFixture(t, "servicedagar_sample.json")
	batch, err := Transform(Servicedagar, raw)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}

	// First feature: FID=9564, EXTENT_NO=1, fredag 0-600 on Gyllenstiernsgatan.
	seg := batch.RoadSegments[0]
	if seg.Source.System != "stockholm.ltf-tolken" {
		t.Errorf("source system: want stockholm.ltf-tolken, got %s", seg.Source.System)
	}
	if seg.Source.Reference != "servicedagar/9564/1" {
		t.Errorf("source ref: want servicedagar/9564/1, got %s", seg.Source.Reference)
	}
	if seg.StreetName != "Gyllenstiernsgatan" {
		t.Errorf("street: want Gyllenstiernsgatan, got %s", seg.StreetName)
	}
	if seg.Municipality != "Stockholm" {
		t.Errorf("municipality: want Stockholm, got %s", seg.Municipality)
	}
	if seg.GeometryWKT == "" {
		t.Errorf("expected non-empty geometry WKT")
	}
	// The captured first feature has two coordinates; WKT should reflect that.
	if want := "LINESTRING(18.100957 59.337504,18.101038 59.337603)"; seg.GeometryWKT != want {
		t.Errorf("WKT mismatch:\n  want %s\n  got  %s", want, seg.GeometryWKT)
	}

	reg := batch.Regulations[0]
	if reg.Source.Reference != "0180 2017-04586" {
		t.Errorf("regulation source ref: want '0180 2017-04586', got %q", reg.Source.Reference)
	}
	if reg.DecisionAuthority != "Stockholms stad" {
		t.Errorf("decision authority: want 'Stockholms stad', got %q", reg.DecisionAuthority)
	}
	if reg.Language != "sv-SE" {
		t.Errorf("language: want sv-SE, got %s", reg.Language)
	}
	if reg.EffectiveFrom.IsZero() {
		t.Errorf("EffectiveFrom should be set from VALID_FROM")
	}

	rule := batch.Rules[0]
	if rule.Kind != domain.RuleForbid {
		t.Errorf("rule kind: want forbid, got %s", rule.Kind)
	}
	if rule.RegulationID != "0180 2017-04586" {
		t.Errorf("rule regulation_id placeholder: want '0180 2017-04586', got %q", rule.RegulationID)
	}
	if len(rule.TimeWindows) != 1 {
		t.Fatalf("expected 1 time window")
	}
	tw := rule.TimeWindows[0]
	// fredag → bit 32
	if tw.WeekdayMask != 32 {
		t.Errorf("weekday mask: want 32 (fredag), got %d", tw.WeekdayMask)
	}
	// 0 → 0, 600 (HHMM) → 360 minutes
	if tw.StartMin != 0 {
		t.Errorf("start min: want 0, got %d", tw.StartMin)
	}
	if tw.EndMin != 360 {
		t.Errorf("end min: want 360 (06:00), got %d", tw.EndMin)
	}
	if len(rule.AppliesTo) != 1 {
		t.Fatalf("expected 1 applies_to")
	}
	if rule.AppliesTo[0].Kind != domain.TargetRoadSegment {
		t.Errorf("applies_to kind: want road_segment, got %s", rule.AppliesTo[0].Kind)
	}
	if rule.AppliesTo[0].TargetID != "servicedagar/9564/1" {
		t.Errorf("applies_to placeholder: want servicedagar/9564/1, got %s", rule.AppliesTo[0].TargetID)
	}
}

func TestTransform_Servicedagar_WeekdayVariation(t *testing.T) {
	raw := loadFixture(t, "servicedagar_sample.json")
	batch, err := Transform(Servicedagar, raw)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}

	// Sample contains both fredag (most features) and måndag (FID 958).
	// Verify that måndag was mapped to bit 2.
	foundMonday := false
	for _, r := range batch.Rules {
		if r.AppliesTo[0].TargetID == "servicedagar/958/1" {
			if r.TimeWindows[0].WeekdayMask != 2 {
				t.Errorf("måndag mask: want 2, got %d", r.TimeWindows[0].WeekdayMask)
			}
			foundMonday = true
		}
	}
	if !foundMonday {
		t.Errorf("did not find the måndag feature (FID 958)")
	}
}

func TestTransform_OtherForeskrifter_ReturnSchemaPending(t *testing.T) {
	for _, f := range []Foreskrift{PTillaten, PBuss, PLastbil, PMotorcykel, PRorelsehindrad} {
		_, err := Transform(f, []byte(`{"type":"FeatureCollection","features":[]}`))
		if !errors.Is(err, ErrSchemaPending) {
			t.Errorf("%s: expected ErrSchemaPending, got %v", f, err)
		}
	}
}

func TestTransform_RejectNonFeatureCollection(t *testing.T) {
	_, err := Transform(Servicedagar, []byte(`{"type":"Foo"}`))
	if err == nil {
		t.Errorf("expected error for non-FeatureCollection top level")
	}
}

func TestTransform_EmptyFeatureCollection(t *testing.T) {
	batch, err := Transform(Servicedagar, []byte(`{"type":"FeatureCollection","features":[]}`))
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if len(batch.RoadSegments) != 0 || len(batch.Regulations) != 0 || len(batch.Rules) != 0 {
		t.Errorf("expected empty batch, got %d segs, %d regs, %d rules",
			len(batch.RoadSegments), len(batch.Regulations), len(batch.Rules))
	}
}

func TestHHMMToMin(t *testing.T) {
	cases := map[int]int{
		0:    0,    // 00:00
		600:  360,  // 06:00
		800:  480,  // 08:00
		1200: 720,  // 12:00
		1745: 1065, // 17:45
		2400: 1440, // end-of-day
	}
	for hhmm, want := range cases {
		got := hhmmToMin(hhmm)
		if got != want {
			t.Errorf("hhmmToMin(%d): want %d, got %d", hhmm, want, got)
		}
	}
}

func TestWeekdayMask(t *testing.T) {
	cases := map[string]int{
		"söndag":   1,
		"måndag":   2,
		"tisdag":   4,
		"onsdag":   8,
		"torsdag":  16,
		"fredag":   32,
		"lördag":   64,
		"FREDAG":   32,
		" tisdag ": 4,
		"unknown":  0,
		"":         0,
	}
	for in, want := range cases {
		got := weekdayMask(in)
		if got != want {
			t.Errorf("weekdayMask(%q): want %d, got %d", in, want, got)
		}
	}
}
