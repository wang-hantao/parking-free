package engine

import (
	"context"
	"testing"
	"time"

	"github.com/wang-hantao/parking-free/internal/domain"
)

// richSource implements RuleSource plus optional Zone/Operator/Hazard
// sub-interfaces for testing the enrichment paths together. Pricing
// is no longer interface-driven — it comes from Rule.TariffClassCode
// against the registry the Evaluator was constructed with.
type richSource struct {
	rules     []domain.Rule
	permits   map[string][]domain.Permit
	zone      *domain.ZoneRef
	street    string
	muni      string
	operators []domain.OperatorOption
	hazards   []domain.Warning
}

func (r *richSource) RulesNearby(_ context.Context, _ domain.Coordinate, _ float64) ([]domain.Rule, error) {
	return r.rules, nil
}
func (r *richSource) PermitsForPlate(_ context.Context, plate string) ([]domain.Permit, error) {
	return r.permits[plate], nil
}
func (r *richSource) ZoneAt(_ context.Context, _ domain.Coordinate) (*domain.ZoneRef, string, string, error) {
	return r.zone, r.street, r.muni, nil
}
func (r *richSource) OperatorsForZone(_ context.Context, _, _ string) ([]domain.OperatorOption, error) {
	return r.operators, nil
}
func (r *richSource) HazardsNearby(_ context.Context, _ domain.Coordinate, _ time.Time) ([]domain.Warning, error) {
	return r.hazards, nil
}

// testTariffClasses gives the pricing tests a stable, simple
// schedule independent of Stockholm's real registry, so changes in
// taxa rates don't break unrelated tests.
var testTariffClasses = map[string]TariffClass{
	"test.simple": {
		Code:        "test.simple",
		Description: "Test tariff: 25 SEK/h normal weekdays 09-18, otherwise free",
		Currency:    "SEK",
		Windows: []TariffWindowSpec{
			{DayType: domain.DayTypeNormal, StartMin: 540, EndMin: 1080, Rate: 25, PerSec: 3600, Priority: 10},
		},
	},
}

func TestEnrich_EmptySourceProducesMinimalEnrichment(t *testing.T) {
	// fakeSource (from evaluator_test.go) implements only RuleSource.
	ev := New(&fakeSource{}, NewHolidayCalendarSE())
	v, err := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 12, 0),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// No location, pricing, constraints, or warnings should be set.
	if v.Location != nil {
		t.Errorf("expected no Location; got %+v", v.Location)
	}
	if v.Pricing != nil {
		t.Errorf("expected no Pricing; got %+v", v.Pricing)
	}
	if v.EstimatedCost != nil {
		t.Errorf("expected no EstimatedCost; got %+v", v.EstimatedCost)
	}
	if v.Metadata == nil {
		t.Errorf("expected Metadata to be populated even without enrichers")
	}
	if v.Metadata != nil && v.Metadata.EngineVersion != EngineVersion {
		t.Errorf("Metadata.EngineVersion: want %s, got %s", EngineVersion, v.Metadata.EngineVersion)
	}
}

func TestEnrich_LocationFromZoneSource(t *testing.T) {
	src := &richSource{
		zone:   &domain.ZoneRef{ID: "z14", Code: "Zone 14", City: "Stockholm", Kind: "paid"},
		street: "Odengatan",
		muni:   "Stockholm",
	}
	ev := New(src, NewHolidayCalendarSE())
	v, err := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 12, 0),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v.Location == nil || v.Location.Zone == nil {
		t.Fatalf("expected Location.Zone to be populated; got %+v", v.Location)
	}
	if v.Location.Zone.Code != "Zone 14" {
		t.Errorf("zone code: want Zone 14, got %s", v.Location.Zone.Code)
	}
	if v.Location.Street != "Odengatan" {
		t.Errorf("street: want Odengatan, got %s", v.Location.Street)
	}
}

func TestEnrich_PricingFromTariffClass_InsidePaidWindow(t *testing.T) {
	// 14:00 Thursday — inside the priced 09-18 window. Expect 25 SEK/h
	// current, free starting at 18:00.
	at := atUTC(2026, 5, 7, 14, 0)
	src := &richSource{
		rules: []domain.Rule{
			{
				ID: "paid", Kind: domain.RuleAllow, NeedsPayment: true,
				TariffClassCode: "test.simple",
				TimeWindows:     []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE()).WithTariffClasses(testTariffClasses)
	v, err := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      at,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v.Pricing == nil {
		t.Fatalf("expected Pricing")
	}
	if v.Pricing.Currency != "SEK" {
		t.Errorf("currency: want SEK, got %s", v.Pricing.Currency)
	}
	if v.Pricing.IsFreeNow {
		t.Errorf("should not be free at 14:00 inside the paid window")
	}
	if v.Pricing.CurrentRate == nil || v.Pricing.CurrentRate.Amount != 25 {
		t.Errorf("current_rate.amount: want 25, got %+v", v.Pricing.CurrentRate)
	}
	if v.Pricing.NextRateChange == nil || !v.Pricing.NextRateChange.Equal(atUTC(2026, 5, 7, 18, 0)) {
		t.Errorf("next_rate_change: want 18:00, got %+v", v.Pricing.NextRateChange)
	}
	if v.Pricing.NextRate == nil || v.Pricing.NextRate.Amount != 0 {
		t.Errorf("next_rate.amount: want 0 (free), got %+v", v.Pricing.NextRate)
	}
}

func TestEnrich_PricingFromTariffClass_FreeOutsideWindow(t *testing.T) {
	// 20:00 Thursday — past the priced window. Expect free, next
	// rate kicking in at 09:00 Friday.
	at := atUTC(2026, 5, 7, 20, 0)
	src := &richSource{
		rules: []domain.Rule{
			{
				ID: "paid", Kind: domain.RuleAllow, NeedsPayment: true,
				TariffClassCode: "test.simple",
				TimeWindows:     []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE()).WithTariffClasses(testTariffClasses)
	v, _ := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      at,
	})
	if v.Pricing == nil {
		t.Fatalf("expected Pricing")
	}
	if !v.Pricing.IsFreeNow {
		t.Errorf("expected IsFreeNow=true at 20:00")
	}
	if v.Pricing.CurrentRate != nil {
		t.Errorf("CurrentRate should be nil when free; got %+v", v.Pricing.CurrentRate)
	}
	if v.Pricing.NextRateChange == nil || !v.Pricing.NextRateChange.Equal(atUTC(2026, 5, 8, 9, 0)) {
		t.Errorf("next_rate_change: want Fri 09:00, got %+v", v.Pricing.NextRateChange)
	}
	if v.Pricing.NextRate == nil || v.Pricing.NextRate.Amount != 25 {
		t.Errorf("next_rate.amount: want 25, got %+v", v.Pricing.NextRate)
	}
}

func TestEnrich_PricingFromUnknownTariffClass_Skipped(t *testing.T) {
	src := &richSource{
		rules: []domain.Rule{
			{
				ID: "paid", Kind: domain.RuleAllow, NeedsPayment: true,
				TariffClassCode: "nonexistent.code",
				TimeWindows:     []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE()).WithTariffClasses(testTariffClasses)
	v, _ := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 14, 0),
	})
	if v.Pricing != nil {
		t.Errorf("expected no Pricing when class is unknown; got %+v", v.Pricing)
	}
}

func TestEnrich_OperatorsAttachedWhenZoneKnown(t *testing.T) {
	src := &richSource{
		zone: &domain.ZoneRef{ID: "z14", Code: "Zone 14", City: "Stockholm", Kind: "paid"},
		operators: []domain.OperatorOption{
			{ID: "easypark", Name: "EasyPark", ExternalZoneID: "5012"},
			{ID: "parkster", Name: "Parkster"},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, err := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 12, 0),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v.Pricing == nil || len(v.Pricing.Operators) != 2 {
		t.Fatalf("expected 2 operators; got %+v", v.Pricing)
	}
	if v.Pricing.Operators[0].ID != "easypark" {
		t.Errorf("first operator: want easypark, got %s", v.Pricing.Operators[0].ID)
	}
}

func TestEnrich_ConstraintsFromActiveRules(t *testing.T) {
	src := &richSource{
		rules: []domain.Rule{
			{
				ID: "paid-2h", Kind: domain.RuleAllow, NeedsPayment: true,
				MaxDuration: 2 * time.Hour,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, err := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 12, 0),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v.Constraints == nil {
		t.Fatalf("expected Constraints")
	}
	if !v.Constraints.PaymentRequired {
		t.Errorf("payment_required should be true")
	}
	if v.Constraints.MaxStayMinutes != 120 {
		t.Errorf("max_stay_minutes: want 120, got %d", v.Constraints.MaxStayMinutes)
	}
}

func TestEnrich_EstimatedCostAcrossTariffBoundary(t *testing.T) {
	// Park at 16:00 Thursday for 4h: 16:00–18:00 paid at 25/h (=50),
	// 18:00–20:00 free. Total = 50 SEK.
	at := atUTC(2026, 5, 7, 16, 0)
	src := &richSource{
		rules: []domain.Rule{
			{
				ID: "paid", Kind: domain.RuleAllow, NeedsPayment: true,
				TariffClassCode: "test.simple",
				TimeWindows:     []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE()).WithTariffClasses(testTariffClasses)
	v, err := ev.Evaluate(context.Background(), Query{
		Vehicle:  domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:       at,
		Duration: 4 * time.Hour,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v.EstimatedCost == nil {
		t.Fatalf("expected EstimatedCost")
	}
	if v.EstimatedCost.Total != 50 {
		t.Errorf("total: want 50, got %v", v.EstimatedCost.Total)
	}
	if v.EstimatedCost.DurationMinutes != 240 {
		t.Errorf("duration_minutes: want 240, got %d", v.EstimatedCost.DurationMinutes)
	}
	if len(v.EstimatedCost.Breakdown) != 2 {
		t.Errorf("breakdown segments: want 2, got %d", len(v.EstimatedCost.Breakdown))
	}
}

func TestEnrich_HazardsFromHazardSource(t *testing.T) {
	servicedagStart := atUTC(2026, 5, 8, 0, 0)
	src := &richSource{
		hazards: []domain.Warning{
			{
				Kind:          domain.WarnServicedagUpcoming,
				Severity:      "warning",
				StartsAt:      &servicedagStart,
				HumanReadable: "Street cleaning starts at 00:00 — move vehicle before then",
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, err := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 16, 0),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(v.Warnings) == 0 {
		t.Fatalf("expected at least one warning")
	}
	found := false
	for _, w := range v.Warnings {
		if w.Kind == domain.WarnServicedagUpcoming {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected servicedag_upcoming warning; got %+v", v.Warnings)
	}
}
