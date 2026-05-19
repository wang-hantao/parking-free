package stockholm

import (
	"os"
	"testing"
	"time"

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

func TestTransform_PMotorcykel_RealSample(t *testing.T) {
	raw := loadFixture(t, "pmotorcykel_sample.json")
	batch, err := Transform(PMotorcykel, raw)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}

	// Sample has 5 features. FID 371 and 372 carry seasonal date-range
	// fields and should be skipped. Survivors: FID 128, 515, 235.
	if got := len(batch.RoadSegments); got != 3 {
		t.Errorf("road segments: want 3 (after skipping 2 seasonal), got %d", got)
	}
	if got := batch.SkippedFeatures; got != 2 {
		t.Errorf("skipped features: want 2 (FID 371 and 372), got %d", got)
	}
	if got := len(batch.Regulations); got != 3 {
		t.Errorf("regulations: want 3, got %d", got)
	}
	if got := len(batch.Rules); got != 3 {
		t.Errorf("rules: want 3, got %d", got)
	}

	// Every surviving rule: motorcycle class + payment required.
	for i, r := range batch.Rules {
		if len(r.VehicleClasses) != 1 || r.VehicleClasses[0] != domain.VehicleMotorcycle {
			t.Errorf("rule %d: want VehicleClasses=[motorcycle], got %v", i, r.VehicleClasses)
		}
		if !r.NeedsPayment {
			t.Errorf("rule %d: want NeedsPayment=true (boende in VF_PLATS_TYP), got false", i)
		}
		if r.Kind != domain.RuleAllow {
			t.Errorf("rule %d: want kind=allow, got %s", i, r.Kind)
		}
	}
}

func TestTransform_PMotorcykel_SkippedFeaturesNotInBatch(t *testing.T) {
	// The two seasonal FIDs (371, 372) should not produce any rules.
	raw := loadFixture(t, "pmotorcykel_sample.json")
	batch, _ := Transform(PMotorcykel, raw)
	for _, r := range batch.Rules {
		ref := r.AppliesTo[0].TargetID
		if ref == "pmotorcykel/371/1" || ref == "pmotorcykel/372/1" {
			t.Errorf("seasonal feature should have been skipped: got rule for %s", ref)
		}
	}
	for _, seg := range batch.RoadSegments {
		ref := seg.Source.Reference
		if ref == "pmotorcykel/371/1" || ref == "pmotorcykel/372/1" {
			t.Errorf("seasonal feature should have been skipped: got segment for %s", ref)
		}
	}
}

func TestTransform_PMotorcykel_BoendeMapsToNeedsPayment(t *testing.T) {
	// FID 128 has VF_PLATS_TYP="Reserverad p-plats motorcykel boende"
	// (no "avgift" string). The transform should still set
	// NeedsPayment=true because of the "boende" suffix.
	raw := loadFixture(t, "pmotorcykel_sample.json")
	batch, _ := Transform(PMotorcykel, raw)
	for _, r := range batch.Rules {
		if r.AppliesTo[0].TargetID == "pmotorcykel/128/1" {
			if !r.NeedsPayment {
				t.Errorf("FID 128: 'boende' should imply NeedsPayment=true")
			}
			return
		}
	}
	t.Errorf("FID 128 rule not found")
}

func TestTransform_PLastbil_RealSample(t *testing.T) {
	raw := loadFixture(t, "plastbil_sample.json")
	batch, err := Transform(PLastbil, raw)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if got := len(batch.RoadSegments); got != 2 {
		t.Errorf("road segments: want 2, got %d", got)
	}
	if got := len(batch.Regulations); got != 2 {
		t.Errorf("regulations: want 2 (different citations), got %d", got)
	}
	if got := len(batch.Rules); got != 2 {
		t.Errorf("rules: want 2, got %d", got)
	}

	// Every rule should be truck-class only with 30min MaxDuration.
	for i, r := range batch.Rules {
		if len(r.VehicleClasses) != 1 || r.VehicleClasses[0] != domain.VehicleTruck {
			t.Errorf("rule %d: want VehicleClasses=[truck], got %v", i, r.VehicleClasses)
		}
		if r.MaxDuration != 30*time.Minute {
			t.Errorf("rule %d: want MaxDuration 30m, got %v", i, r.MaxDuration)
		}
		// Wed 06:00 → end of day. Mask=8 (onsdag), [360, 1440).
		tw := r.TimeWindows[0]
		if tw.WeekdayMask != 8 || tw.StartMin != 360 || tw.EndMin != 1440 {
			t.Errorf("rule %d: window: want mask=8, [360,1440), got mask=%d, [%d,%d)",
				i, tw.WeekdayMask, tw.StartMin, tw.EndMin)
		}
	}
}

func TestTransform_PRorelsehindrad_RealSample(t *testing.T) {
	raw := loadFixture(t, "prorelsehindrad_sample.json")
	batch, err := Transform(PRorelsehindrad, raw)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if got := len(batch.RoadSegments); got != 5 {
		t.Errorf("road segments: want 5, got %d", got)
	}
	// All 5 features have distinct citations.
	if got := len(batch.Regulations); got != 5 {
		t.Errorf("regulations: want 5, got %d", got)
	}
	if got := len(batch.Rules); got != 5 {
		t.Errorf("rules: want 5, got %d", got)
	}

	// All rules: permit-based, no vehicle class restriction, 24/7.
	for i, r := range batch.Rules {
		if !r.NeedsPermit {
			t.Errorf("rule %d: want NeedsPermit=true (disabled placard), got false", i)
		}
		if len(r.VehicleClasses) != 0 {
			t.Errorf("rule %d: want no vehicle class restriction, got %v", i, r.VehicleClasses)
		}
		if r.MaxDuration != 0 {
			t.Errorf("rule %d: want no MaxDuration, got %v", i, r.MaxDuration)
		}
		tw := r.TimeWindows[0]
		if tw.WeekdayMask != 127 || tw.StartMin != 0 || tw.EndMin != 1440 {
			t.Errorf("rule %d: want 24/7 window, got mask=%d, [%d,%d)",
				i, tw.WeekdayMask, tw.StartMin, tw.EndMin)
		}
	}
}

func TestTransform_PRorelsehindrad_PreservesPlaceholderStreetName(t *testing.T) {
	// FID 695 has STREET_NAME="<Gatunamn saknas>" — preserved as-is for
	// fidelity to source. Display layer can handle it.
	raw := loadFixture(t, "prorelsehindrad_sample.json")
	batch, _ := Transform(PRorelsehindrad, raw)

	var found bool
	for _, seg := range batch.RoadSegments {
		if seg.Source.Reference == "prorelsehindrad/695/1" {
			if seg.StreetName != "<Gatunamn saknas>" {
				t.Errorf("FID 695: want street name preserved as %q, got %q",
					"<Gatunamn saknas>", seg.StreetName)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("FID 695 segment not found")
	}
}

func TestTransform_PBuss_RealSample(t *testing.T) {
	raw := loadFixture(t, "pbuss_sample.json")
	batch, err := Transform(PBuss, raw)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if got := len(batch.RoadSegments); got != 5 {
		t.Errorf("road segments: want 5, got %d", got)
	}
	// 4 distinct CITATIONs (FID 34 and 35 share one).
	if got := len(batch.Regulations); got != 4 {
		t.Errorf("regulations: want 4 (FID 34 and 35 share citation), got %d", got)
	}
	if got := len(batch.Rules); got != 5 {
		t.Errorf("rules: want 5, got %d", got)
	}

	// Every rule should be bus-class only.
	for i, r := range batch.Rules {
		if len(r.VehicleClasses) != 1 || r.VehicleClasses[0] != domain.VehicleBus {
			t.Errorf("rule %d: want VehicleClasses=[bus], got %v", i, r.VehicleClasses)
		}
		if r.Kind != domain.RuleAllow {
			t.Errorf("rule %d: want kind=allow, got %s", i, r.Kind)
		}
	}
}

func TestTransform_PBuss_MaxMinutesMapsToMaxDuration(t *testing.T) {
	// FID 34 and 35 have MAX_MINUTES=30; the rest have none.
	raw := loadFixture(t, "pbuss_sample.json")
	batch, _ := Transform(PBuss, raw)

	got := map[string]time.Duration{}
	for _, r := range batch.Rules {
		got[r.AppliesTo[0].TargetID] = r.MaxDuration
	}

	if got["pbuss/34/1"] != 30*time.Minute {
		t.Errorf("FID 34: want MaxDuration 30m, got %v", got["pbuss/34/1"])
	}
	if got["pbuss/35/2"] != 30*time.Minute {
		t.Errorf("FID 35: want MaxDuration 30m, got %v", got["pbuss/35/2"])
	}
	if got["pbuss/41/1"] != 0 {
		t.Errorf("FID 41: want no MaxDuration, got %v", got["pbuss/41/1"])
	}
}

func TestTransform_PBuss_MidnightCrossingWindow(t *testing.T) {
	// FID 20: START_TIME=1400, END_TIME=900, START_WEEKDAY=måndag.
	// Means Mon 14:00 → Tue 09:00. WeekdayMask must include BOTH
	// Monday (bit 2) and Tuesday (bit 4) for the engine to match the
	// Tuesday tail of the window.
	raw := loadFixture(t, "pbuss_sample.json")
	batch, _ := Transform(PBuss, raw)

	var found *domain.Rule
	for i := range batch.Rules {
		if batch.Rules[i].AppliesTo[0].TargetID == "pbuss/20/1" {
			found = &batch.Rules[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("FID 20 rule not found")
	}
	tw := found.TimeWindows[0]
	if tw.WeekdayMask != 2|4 { // Mon | Tue = 6
		t.Errorf("weekday mask: want 6 (Mon+Tue), got %d", tw.WeekdayMask)
	}
	if tw.StartMin != 840 {
		t.Errorf("start min: want 840 (14:00), got %d", tw.StartMin)
	}
	if tw.EndMin != 540 {
		t.Errorf("end min: want 540 (09:00 next day), got %d", tw.EndMin)
	}
}

func TestTransform_PBuss_NoTimeFields_AppliesAllDay(t *testing.T) {
	// FID 34/35 have no time fields at all; just MAX_MINUTES.
	// → 24/7 time window.
	raw := loadFixture(t, "pbuss_sample.json")
	batch, _ := Transform(PBuss, raw)

	var found *domain.Rule
	for i := range batch.Rules {
		if batch.Rules[i].AppliesTo[0].TargetID == "pbuss/34/1" {
			found = &batch.Rules[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("FID 34 rule not found")
	}
	tw := found.TimeWindows[0]
	if tw.WeekdayMask != 127 {
		t.Errorf("weekday mask: want 127 (24/7), got %d", tw.WeekdayMask)
	}
	if tw.StartMin != 0 || tw.EndMin != 1440 {
		t.Errorf("time range: want [0, 1440), got [%d, %d)", tw.StartMin, tw.EndMin)
	}
}

func TestBuildTimeWindow_CrossesMidnight_ExtendsMaskToNextDay(t *testing.T) {
	// Mon 14:00 → 09:00 next day. Mask must include both Mon (2) and Tue (4).
	tw := buildTimeWindow("måndag", 1400, 900)
	if tw.WeekdayMask != 6 {
		t.Errorf("mask: want 6 (Mon+Tue), got %d", tw.WeekdayMask)
	}
	if tw.StartMin != 840 || tw.EndMin != 540 {
		t.Errorf("range: want [840, 540), got [%d, %d)", tw.StartMin, tw.EndMin)
	}
}

func TestBuildTimeWindow_CrossesMidnight_SaturdayWrapsToSunday(t *testing.T) {
	// Sat 23:00 → 02:00 next day. Mask must include Sat (64) and Sun (1) = 65.
	tw := buildTimeWindow("lördag", 2300, 200)
	if tw.WeekdayMask != 65 {
		t.Errorf("mask: want 65 (Sat+Sun), got %d", tw.WeekdayMask)
	}
}

func TestBuildTimeWindow_EndOfDayNotMidnightCrossing(t *testing.T) {
	// END_TIME=0 with START_TIME>0 is end-of-day, not a wrap. Should
	// NOT extend the mask (single-day window 06:00 → 24:00).
	tw := buildTimeWindow("fredag", 600, 0)
	if tw.WeekdayMask != 32 { // fredag only
		t.Errorf("mask: want 32 (Fri only — no wrap), got %d", tw.WeekdayMask)
	}
	if tw.StartMin != 360 || tw.EndMin != 1440 {
		t.Errorf("range: want [360, 1440), got [%d, %d)", tw.StartMin, tw.EndMin)
	}
}

func TestTransform_Ptillaten_RealSample(t *testing.T) {
	raw := loadFixture(t, "ptillaten_sample.json")
	batch, err := Transform(PTillaten, raw)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}

	// 5 features → 5 road segments.
	if got := len(batch.RoadSegments); got != 5 {
		t.Errorf("road segments: want 5, got %d", got)
	}
	// 4 distinct CITATIONs in the sample (FID 25 and 26 share one).
	if got := len(batch.Regulations); got != 4 {
		t.Errorf("regulations: want 4 (FID 25 and 26 share citation), got %d", got)
	}
	// 5 rules, one per feature.
	if got := len(batch.Rules); got != 5 {
		t.Errorf("rules: want 5, got %d", got)
	}
}

func TestTransform_Ptillaten_MidnightCrossingWindow(t *testing.T) {
	// Feature FID 14: START_TIME=600, END_TIME=0, START_WEEKDAY=fredag.
	// Should become "Friday 06:00 to midnight" (360 to 1440), not 360 to 0.
	raw := loadFixture(t, "ptillaten_sample.json")
	batch, err := Transform(PTillaten, raw)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}

	var found *domain.Rule
	for i := range batch.Rules {
		if batch.Rules[i].AppliesTo[0].TargetID == "ptillaten/14/3" {
			found = &batch.Rules[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("FID 14 rule not found")
	}
	if len(found.TimeWindows) != 1 {
		t.Fatalf("expected 1 time window, got %d", len(found.TimeWindows))
	}
	tw := found.TimeWindows[0]
	if tw.WeekdayMask != 32 {
		t.Errorf("weekday mask: want 32 (fredag), got %d", tw.WeekdayMask)
	}
	if tw.StartMin != 360 {
		t.Errorf("start min: want 360 (06:00), got %d", tw.StartMin)
	}
	if tw.EndMin != 1440 {
		t.Errorf("end min: want 1440 (end of day), got %d", tw.EndMin)
	}
}

func TestTransform_Ptillaten_PaidWithVfPlatstyp(t *testing.T) {
	// "P Avgift, boende" → NeedsPayment=true, NeedsPermit=false.
	raw := loadFixture(t, "ptillaten_sample.json")
	batch, err := Transform(PTillaten, raw)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	var found *domain.Rule
	for i := range batch.Rules {
		if batch.Rules[i].AppliesTo[0].TargetID == "ptillaten/14/3" {
			found = &batch.Rules[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("FID 14 rule not found")
	}
	if !found.NeedsPayment {
		t.Errorf("NeedsPayment: want true for 'P Avgift, boende'")
	}
	if found.NeedsPermit {
		t.Errorf("NeedsPermit: want false for 'P Avgift, boende' (paid, anyone can park)")
	}
	if found.Kind != domain.RuleAllow {
		t.Errorf("Kind: want allow, got %s", found.Kind)
	}
}

func TestTransform_Ptillaten_DisabledSpot_NeedsPermit_AppliesAllDay(t *testing.T) {
	// FID 27: VEHICLE=rörelsehindrade, no time fields.
	// → NeedsPermit=true, 24/7 time window.
	raw := loadFixture(t, "ptillaten_sample.json")
	batch, err := Transform(PTillaten, raw)
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	var found *domain.Rule
	for i := range batch.Rules {
		if batch.Rules[i].AppliesTo[0].TargetID == "ptillaten/27/1" {
			found = &batch.Rules[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("FID 27 rule not found")
	}
	if !found.NeedsPermit {
		t.Errorf("NeedsPermit: want true for disabled-reserved spot")
	}
	if len(found.TimeWindows) != 1 {
		t.Fatalf("expected 1 time window")
	}
	tw := found.TimeWindows[0]
	if tw.WeekdayMask != 127 {
		t.Errorf("weekday mask: want 127 (all days, 24/7), got %d", tw.WeekdayMask)
	}
	if tw.StartMin != 0 || tw.EndMin != 1440 {
		t.Errorf("time range: want [0, 1440), got [%d, %d)", tw.StartMin, tw.EndMin)
	}
}

func TestTransform_Ptillaten_PriorityBelowServicedagar(t *testing.T) {
	// Ptillaten rules should have lower priority than servicedagar
	// so that on overlap, the cleaning Forbid wins.
	pRaw := loadFixture(t, "ptillaten_sample.json")
	pBatch, _ := Transform(PTillaten, pRaw)
	sRaw := loadFixture(t, "servicedagar_sample.json")
	sBatch, _ := Transform(Servicedagar, sRaw)

	if len(pBatch.Rules) == 0 || len(sBatch.Rules) == 0 {
		t.Fatalf("missing rules")
	}
	if pBatch.Rules[0].Priority >= sBatch.Rules[0].Priority {
		t.Errorf("ptillaten priority (%d) must be < servicedagar priority (%d)",
			pBatch.Rules[0].Priority, sBatch.Rules[0].Priority)
	}
}

func TestBuildTimeWindow_AllAbsent_Is247(t *testing.T) {
	tw := buildTimeWindow("", 0, 0)
	if tw.WeekdayMask != 127 {
		t.Errorf("weekday mask: want 127, got %d", tw.WeekdayMask)
	}
	if tw.StartMin != 0 || tw.EndMin != 1440 {
		t.Errorf("range: want [0, 1440), got [%d, %d)", tw.StartMin, tw.EndMin)
	}
}

func TestBuildTimeWindow_EndZeroMeansEndOfDay(t *testing.T) {
	// END_TIME=0 with START_TIME>0 is "to midnight" not "to 00:00 today".
	tw := buildTimeWindow("fredag", 600, 0)
	if tw.StartMin != 360 || tw.EndMin != 1440 {
		t.Errorf("want [360, 1440), got [%d, %d)", tw.StartMin, tw.EndMin)
	}
}

func TestBuildTimeWindow_NormalRange(t *testing.T) {
	// Servicedagar-style: 00:00 to 06:00 on Friday.
	tw := buildTimeWindow("fredag", 0, 600)
	if tw.WeekdayMask != 32 {
		t.Errorf("mask: want 32, got %d", tw.WeekdayMask)
	}
	if tw.StartMin != 0 || tw.EndMin != 360 {
		t.Errorf("range: want [0, 360), got [%d, %d)", tw.StartMin, tw.EndMin)
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
