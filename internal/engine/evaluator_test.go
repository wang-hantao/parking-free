package engine

import (
	"context"
	"testing"
	"time"

	"github.com/wang-hantao/parking-free/internal/domain"
)

// fakeSource is an in-memory RuleSource for tests.
type fakeSource struct {
	rules   []domain.Rule
	permits map[string][]domain.Permit
}

func (f *fakeSource) RulesNearby(_ context.Context, _ domain.Coordinate, _ float64) ([]domain.Rule, error) {
	return f.rules, nil
}
func (f *fakeSource) PermitsForPlate(_ context.Context, plate string) ([]domain.Permit, error) {
	return f.permits[plate], nil
}

func atUTC(y, m, d, hh, mm int) time.Time {
	return time.Date(y, time.Month(m), d, hh, mm, 0, 0, time.UTC)
}

const allWeekdays = (1 << 7) - 1 // bits Sun..Sat

func TestEvaluate_DefaultAllowedWhenNoRules(t *testing.T) {
	ev := New(&fakeSource{}, NewHolidayCalendarSE())
	v, err := ev.Evaluate(context.Background(), Query{
		Position: domain.Coordinate{Lat: 59.32, Lng: 18.05},
		Vehicle:  domain.Vehicle{Plate: "ABC123", Class: domain.VehicleCar},
		At:       atUTC(2026, 5, 7, 12, 0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !v.Allowed {
		t.Errorf("expected allowed=true with no rules")
	}
	if len(v.Reasons) != 0 {
		t.Errorf("expected no reasons, got %d", len(v.Reasons))
	}
}

func TestEvaluate_ForbidWinsOverDefault(t *testing.T) {
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "no-park", RegulationID: "reg-1", Kind: domain.RuleForbid,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, err := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC123", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 12, 0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Allowed {
		t.Errorf("expected allowed=false")
	}
	if len(v.Reasons) != 1 || v.Reasons[0].Disposition != domain.RuleForbid {
		t.Errorf("expected one Forbid reason, got %+v", v.Reasons)
	}
}

func TestEvaluate_VehicleClassFilter(t *testing.T) {
	// A bus-only forbidance must not affect cars.
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "bus-only", RegulationID: "reg-1", Kind: domain.RuleForbid,
				VehicleClasses: []domain.VehicleClass{domain.VehicleBus},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, err := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC123", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 12, 0),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !v.Allowed {
		t.Errorf("car should not be affected by bus-only forbid")
	}
}

func TestEvaluate_TimeWindow_OutsideHours(t *testing.T) {
	// "No parking 09:00–18:00 weekdays" — at 20:00 it should not apply.
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "no-day-park", Kind: domain.RuleForbid,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: 1<<int(time.Thursday) | 1<<int(time.Friday), StartMin: 540, EndMin: 1080}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	thu := atUTC(2026, 5, 7, 20, 0) // 2026-05-07 is a Thursday
	v, err := ev.Evaluate(context.Background(), Query{Vehicle: domain.Vehicle{Class: domain.VehicleCar}, At: thu})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !v.Allowed {
		t.Errorf("expected allowed at 20:00 outside the window")
	}
}

func TestEvaluate_TimeWindow_InsideHours(t *testing.T) {
	// Same rule at 12:00 Thursday should forbid.
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "no-day-park", Kind: domain.RuleForbid,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: 1 << int(time.Thursday), StartMin: 540, EndMin: 1080}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	thu := atUTC(2026, 5, 7, 12, 0)
	v, err := ev.Evaluate(context.Background(), Query{Vehicle: domain.Vehicle{Class: domain.VehicleCar}, At: thu})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v.Allowed {
		t.Errorf("expected forbid inside 09:00–18:00 Thursday")
	}
}

func TestEvaluate_NeedsAction_Payment(t *testing.T) {
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "paid", Kind: domain.RuleAllow, NeedsPayment: true,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, err := ev.Evaluate(context.Background(), Query{Vehicle: domain.Vehicle{Class: domain.VehicleCar}, At: atUTC(2026, 5, 7, 12, 0)})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !v.Allowed {
		t.Errorf("expected allowed for paid rule")
	}
	if !contains(v.NeedsAction, "pay_via_app") {
		t.Errorf("expected pay_via_app in NeedsAction; got %v", v.NeedsAction)
	}
}

func TestEvaluate_NeedsAction_PermitMissing(t *testing.T) {
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "resident-only", Kind: domain.RuleAllow, NeedsPermit: true,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
		permits: map[string][]domain.Permit{},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, err := ev.Evaluate(context.Background(), Query{Vehicle: domain.Vehicle{Plate: "ABC123", Class: domain.VehicleCar}, At: atUTC(2026, 5, 7, 12, 0)})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !contains(v.NeedsAction, "obtain_permit") {
		t.Errorf("expected obtain_permit in NeedsAction; got %v", v.NeedsAction)
	}
}

func TestEvaluate_NeedsAction_PermitPresent(t *testing.T) {
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "resident-only", Kind: domain.RuleAllow, NeedsPermit: true,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
		permits: map[string][]domain.Permit{
			"ABC123": {{
				ID: "p1", Kind: domain.PermitResidential, Plate: "ABC123",
				ValidFrom: atUTC(2026, 1, 1, 0, 0), ValidTo: atUTC(2027, 1, 1, 0, 0),
			}},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, err := ev.Evaluate(context.Background(), Query{Vehicle: domain.Vehicle{Plate: "ABC123", Class: domain.VehicleCar}, At: atUTC(2026, 5, 7, 12, 0)})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if contains(v.NeedsAction, "obtain_permit") {
		t.Errorf("permit is valid; should not be in NeedsAction; got %v", v.NeedsAction)
	}
}

func TestEvaluate_DayType_HolidayWindow(t *testing.T) {
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "holiday-forbid", Kind: domain.RuleForbid,
				TimeWindows: []domain.TimeWindow{{DayType: domain.DayTypeHoliday, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())

	// May 1 2026 is a Friday and a holiday in Sweden.
	v, err := ev.Evaluate(context.Background(), Query{Vehicle: domain.Vehicle{Class: domain.VehicleCar}, At: atUTC(2026, 5, 1, 12, 0)})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v.Allowed {
		t.Errorf("expected forbidden on May 1 holiday")
	}

	// Adjacent Monday May 4 2026 — normal weekday, no holiday.
	v, err = ev.Evaluate(context.Background(), Query{Vehicle: domain.Vehicle{Class: domain.VehicleCar}, At: atUTC(2026, 5, 4, 12, 0)})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !v.Allowed {
		t.Errorf("expected allowed on normal Monday May 4")
	}
}

func TestEvaluate_PriorityOrdering(t *testing.T) {
	// A high-priority Forbid wins over a low-priority Allow at the same location.
	src := &fakeSource{
		rules: []domain.Rule{
			{ID: "low-allow", Kind: domain.RuleAllow, Priority: 1},
			{ID: "high-forbid", Kind: domain.RuleForbid, Priority: 10},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, err := ev.Evaluate(context.Background(), Query{Vehicle: domain.Vehicle{Class: domain.VehicleCar}, At: atUTC(2026, 5, 7, 12, 0)})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v.Allowed {
		t.Errorf("expected high-priority Forbid to win")
	}
	if len(v.Reasons) == 0 || v.Reasons[0].RuleID != "high-forbid" {
		t.Errorf("expected high-forbid first; got %+v", v.Reasons)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
