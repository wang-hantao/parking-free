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

func TestEvaluate_SourcePropagatesToReason(t *testing.T) {
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "r1", Kind: domain.RuleAllow,
				Source: domain.Source{
					System:    "stockholm.ltf-tolken",
					Reference: "0180 2017-01931",
				},
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, err := ev.Evaluate(context.Background(), Query{Vehicle: domain.Vehicle{Class: domain.VehicleCar}, At: atUTC(2026, 5, 7, 12, 0)})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(v.Reasons) != 1 {
		t.Fatalf("expected 1 reason, got %d", len(v.Reasons))
	}
	if v.Reasons[0].Source.System != "stockholm.ltf-tolken" {
		t.Errorf("source system: want stockholm.ltf-tolken, got %q", v.Reasons[0].Source.System)
	}
	if v.Reasons[0].Source.Reference != "0180 2017-01931" {
		t.Errorf("source ref: want '0180 2017-01931', got %q", v.Reasons[0].Source.Reference)
	}
}

func TestEvaluate_HumaniseDisambiguatesPermitFromPayment(t *testing.T) {
	cases := []struct {
		name         string
		needsPayment bool
		needsPermit  bool
		want         string
	}{
		{"plain", false, false, "Parking allowed"},
		{"payment", true, false, "Parking allowed with payment"},
		{"permit", false, true, "Parking allowed only for permit holders"},
		{"both", true, true, "Parking allowed with payment or valid permit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := &fakeSource{
				rules: []domain.Rule{
					{
						ID: "r1", Kind: domain.RuleAllow,
						NeedsPayment: tc.needsPayment,
						NeedsPermit:  tc.needsPermit,
						TimeWindows:  []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
					},
				},
			}
			ev := New(src, NewHolidayCalendarSE())
			v, _ := ev.Evaluate(context.Background(), Query{Vehicle: domain.Vehicle{Class: domain.VehicleCar}, At: atUTC(2026, 5, 7, 12, 0)})
			if len(v.Reasons) != 1 {
				t.Fatalf("want 1 reason, got %d", len(v.Reasons))
			}
			if v.Reasons[0].HumanReadable != tc.want {
				t.Errorf("human_readable: want %q, got %q", tc.want, v.Reasons[0].HumanReadable)
			}
		})
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

// =============================================================================
// Satisfiability-based verdict tests
// =============================================================================

func TestEvaluate_PermitRequiredNoPermit_DisallowedWhenNoAlternative(t *testing.T) {
	// Disabled-only spot, user has no permit. Only Allow rule in scope
	// is unsatisfiable. Verdict must be false.
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "disabled-only", Kind: domain.RuleAllow,
				NeedsPermit: true,
				Source:      domain.Source{System: "stockholm.ltf-tolken", Reference: "0180 2017-02280"},
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
		t.Fatalf("err: %v", err)
	}
	if v.Allowed {
		t.Errorf("expected allowed=false when only rule requires permit user lacks")
	}
	if len(v.Reasons) != 1 {
		t.Fatalf("expected 1 reason, got %d", len(v.Reasons))
	}
	if !v.Reasons[0].Blocks {
		t.Errorf("expected the disabled rule to be marked Blocks=true")
	}
	if v.Summary == "" {
		t.Errorf("expected a Summary string")
	}
	if !contains(v.NeedsAction, "obtain_permit") {
		t.Errorf("needs_action should contain obtain_permit; got %v", v.NeedsAction)
	}
}

func TestEvaluate_PermitRequiredWithPermit_Allowed(t *testing.T) {
	// Same disabled-only spot, but user has a valid permit.
	now := atUTC(2026, 5, 7, 12, 0)
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "disabled-only", Kind: domain.RuleAllow,
				NeedsPermit: true,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
		permits: map[string][]domain.Permit{
			"ABC123": {{
				Kind:      domain.PermitDisabled,
				Plate:     "ABC123",
				ValidFrom: now.Add(-24 * time.Hour),
				ValidTo:   now.Add(24 * time.Hour),
			}},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, _ := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC123", Class: domain.VehicleCar},
		At:      now,
	})
	if !v.Allowed {
		t.Errorf("expected allowed=true when user has a matching permit")
	}
	if v.Reasons[0].Blocks {
		t.Errorf("Blocks should be false when verdict is allowed")
	}
	if contains(v.NeedsAction, "obtain_permit") {
		t.Errorf("needs_action should NOT contain obtain_permit when user has one")
	}
}

func TestEvaluate_PermitPlusPaidNearby_AllowedViaPaidPath(t *testing.T) {
	// Disabled spot + paid spot both within radius. User has no permit
	// but can pay. Should be allowed via the paid path; unsatisfied
	// permit rule is informational, not blocking.
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "disabled-spot", Kind: domain.RuleAllow,
				NeedsPermit: true,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
			{
				ID: "paid-spot", Kind: domain.RuleAllow,
				NeedsPayment: true,
				TimeWindows:  []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, _ := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC123", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 12, 0),
	})
	if !v.Allowed {
		t.Errorf("expected allowed=true when a satisfiable path exists")
	}
	// Both reasons appear; neither blocks since allowed=true.
	for _, r := range v.Reasons {
		if r.Blocks {
			t.Errorf("no reason should Block when allowed=true; got %+v", r)
		}
	}
	// Summary mentions the permit-only context.
	if !contains([]string{v.Summary}, "Parking allowed with payment (some nearby spots are permit-only)") {
		t.Errorf("unexpected summary: %q", v.Summary)
	}
}

func TestEvaluate_ForbidStillWins(t *testing.T) {
	// Forbid is highest precedence even if there's a satisfiable Allow.
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "cleaning", Kind: domain.RuleForbid, Priority: 10,
				Source:      domain.Source{System: "stockholm.ltf-tolken", Reference: "0180 2017-04586"},
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
			{
				ID: "paid", Kind: domain.RuleAllow, Priority: 5,
				NeedsPayment: true,
				TimeWindows:  []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, _ := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC123", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 12, 0),
	})
	if v.Allowed {
		t.Errorf("Forbid should override satisfiable Allow")
	}
	// The Forbid should Block, the Allow should not.
	var forbidBlocks, allowBlocks bool
	for _, r := range v.Reasons {
		if r.RuleID == "cleaning" {
			forbidBlocks = r.Blocks
		}
		if r.RuleID == "paid" {
			allowBlocks = r.Blocks
		}
	}
	if !forbidBlocks {
		t.Errorf("Forbid rule should have Blocks=true")
	}
	if allowBlocks {
		t.Errorf("Allow rule should NOT have Blocks=true when Forbid is the cause")
	}
	if v.Summary != "Parking forbidden at this location" {
		t.Errorf("unexpected summary: %q", v.Summary)
	}
}

func TestEvaluate_NoRulesAtAll_DefaultAllowed(t *testing.T) {
	// With no rules in scope, default to allowed (nothing forbids).
	ev := New(&fakeSource{}, NewHolidayCalendarSE())
	v, _ := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC123", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 12, 0),
	})
	if !v.Allowed {
		t.Errorf("expected allowed=true with no rules; got false")
	}
	if v.Summary != "Parking allowed" {
		t.Errorf("unexpected summary: %q", v.Summary)
	}
}
