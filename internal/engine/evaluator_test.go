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

// =============================================================================
// Reason deduplication tests (2.3)
// =============================================================================

func TestEvaluate_DedupesReasonsByCitation(t *testing.T) {
	// Three rules all attributing to the same LTF citation — the real
	// shape when a regulation spans multiple road segments and the
	// spatial query picks up all of them within the 50m radius.
	cite := domain.Source{System: "stockholm.ltf-tolken", Reference: "0180 2017-04586"}
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "rule-segA", Kind: domain.RuleAllow, NeedsPayment: true,
				Source:      cite,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
			{
				ID: "rule-segB", Kind: domain.RuleAllow, NeedsPayment: true,
				Source:      cite,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
			{
				ID: "rule-segC", Kind: domain.RuleAllow, NeedsPayment: true,
				Source:      cite,
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
	if len(v.Reasons) != 1 {
		t.Errorf("expected 1 deduped reason for shared citation, got %d", len(v.Reasons))
	}
	if v.Reasons[0].Source.Reference != cite.Reference {
		t.Errorf("kept reason should carry the shared citation; got %+v", v.Reasons[0].Source)
	}
}

func TestEvaluate_DedupeKeepsBlockingReason(t *testing.T) {
	// Same citation, but one rule will block (disabled-permit user
	// without permit) and the other is satisfiable. The blocking
	// reason is more informative for a not-allowed verdict, so dedup
	// must keep it. Build the scenario so the disabled permit rule is
	// in the active set alongside another paid-allow rule.
	citePermit := domain.Source{System: "stockholm.ltf-tolken", Reference: "0180 2017-02280"}
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "permit-only", Kind: domain.RuleAllow, NeedsPermit: true,
				Source:      citePermit,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
			// Duplicate citation, satisfiable (e.g. cleaner version of
			// the same rule for a different segment that happens to
			// share the citation).
			{
				ID: "permit-only-dup", Kind: domain.RuleAllow, NeedsPermit: true,
				Source:      citePermit,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, _ := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC123", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 12, 0),
	})
	if len(v.Reasons) != 1 {
		t.Fatalf("expected 1 deduped reason, got %d", len(v.Reasons))
	}
	// Verdict is not allowed (only permit rules in scope, user has no permit).
	if v.Allowed {
		t.Errorf("expected allowed=false")
	}
	// The kept reason should be the blocking one.
	if !v.Reasons[0].Blocks {
		t.Errorf("expected the surviving reason to be marked Blocks=true")
	}
}

func TestEvaluate_DedupeLeavesEmptyReferencesAlone(t *testing.T) {
	// Rules without a source reference (e.g. tests or non-LTF sources)
	// should not collapse onto each other just because they share an
	// empty key.
	src := &fakeSource{
		rules: []domain.Rule{
			{ID: "a", Kind: domain.RuleAllow, NeedsPayment: true,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}}},
			{ID: "b", Kind: domain.RuleAllow, NeedsPayment: true,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}}},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, _ := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 12, 0),
	})
	if len(v.Reasons) != 2 {
		t.Errorf("expected 2 reasons (no shared reference to dedup on), got %d", len(v.Reasons))
	}
}

// =============================================================================
// Strict-mode tests (3.1)
// =============================================================================

// strictSource implements both RuleSource and StrictRuleSource so we
// can exercise the engine's branching. The two methods return
// different sets so tests can assert which path was taken.
type strictSource struct {
	nearby []domain.Rule // returned by RulesNearby
	at     []domain.Rule // returned by RulesAt (strict mode)
}

func (s *strictSource) RulesNearby(_ context.Context, _ domain.Coordinate, _ float64) ([]domain.Rule, error) {
	return s.nearby, nil
}
func (s *strictSource) RulesAt(_ context.Context, _ domain.Coordinate) ([]domain.Rule, error) {
	return s.at, nil
}
func (s *strictSource) PermitsForPlate(_ context.Context, _ string) ([]domain.Permit, error) {
	return nil, nil
}

func TestEvaluate_StrictMode_UsesRulesAt(t *testing.T) {
	src := &strictSource{
		nearby: []domain.Rule{
			{
				ID: "wide-net", Kind: domain.RuleAllow, NeedsPayment: true,
				Source:      domain.Source{System: "test", Reference: "wide"},
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
		at: []domain.Rule{
			{
				ID: "exact", Kind: domain.RuleAllow, NeedsPayment: true,
				Source:      domain.Source{System: "test", Reference: "exact"},
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, err := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 12, 0),
		Mode:    QueryModeStrict,
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(v.Reasons) != 1 || v.Reasons[0].Source.Reference != "exact" {
		t.Errorf("strict mode should use RulesAt; got reasons %+v", v.Reasons)
	}
	if v.Metadata == nil || v.Metadata.Mode != "strict" {
		t.Errorf("Metadata.Mode: want strict, got %+v", v.Metadata)
	}
}

func TestEvaluate_NearbyMode_UsesRulesNearby(t *testing.T) {
	src := &strictSource{
		nearby: []domain.Rule{
			{
				ID: "wide-net", Kind: domain.RuleAllow, NeedsPayment: true,
				Source:      domain.Source{System: "test", Reference: "wide"},
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
		at: []domain.Rule{
			{
				ID: "exact", Kind: domain.RuleAllow, NeedsPayment: true,
				Source:      domain.Source{System: "test", Reference: "exact"},
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, _ := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 12, 0),
		Mode:    QueryModeNearby, // explicit default
	})
	if len(v.Reasons) != 1 || v.Reasons[0].Source.Reference != "wide" {
		t.Errorf("nearby mode should use RulesNearby; got reasons %+v", v.Reasons)
	}
	if v.Metadata == nil || v.Metadata.Mode != "" {
		t.Errorf("Metadata.Mode: want empty for nearby, got %+v", v.Metadata)
	}
}

func TestEvaluate_StrictMode_FallsBackWhenSourceDoesntImplement(t *testing.T) {
	// fakeSource (used by other tests) is plain RuleSource — no
	// StrictRuleSource. Requesting strict mode should not error; it
	// should silently fall back to RulesNearby, and Metadata.Mode
	// reports the effective mode so the client can detect.
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "r1", Kind: domain.RuleAllow, NeedsPayment: true,
				Source:      domain.Source{System: "test", Reference: "0180 2017-99999"},
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, err := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 12, 0),
		Mode:    QueryModeStrict,
	})
	if err != nil {
		t.Fatalf("strict-without-support should not error: %v", err)
	}
	if v.Metadata == nil || v.Metadata.Mode != "" {
		t.Errorf("Metadata.Mode: want empty (fallback), got %+v", v.Metadata)
	}
	if len(v.Reasons) != 1 {
		t.Errorf("fallback should still return the rule via RulesNearby; got %d reasons", len(v.Reasons))
	}
}

// =============================================================================
// Required-permit-kind tests (2.2)
// =============================================================================

func TestEvaluate_RequiredPermitKind_WrongKindIsUnsatisfied(t *testing.T) {
	// User has a residential permit; rule requires a disabled permit.
	// Verdict: not allowed (the residential permit doesn't satisfy
	// the disabled-only requirement).
	now := atUTC(2026, 5, 7, 12, 0)
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "disabled-only", Kind: domain.RuleAllow, NeedsPermit: true,
				RequiredPermitKind: domain.PermitDisabled,
				Source:             domain.Source{System: "test", Reference: "0180 2017-02280"},
				TimeWindows:        []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
		permits: map[string][]domain.Permit{
			"ABC123": {{
				Kind:      domain.PermitResidential, // wrong kind!
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
	if v.Allowed {
		t.Errorf("residential permit must not satisfy a disabled-only rule")
	}
	if !contains(v.NeedsAction, "obtain_permit") {
		t.Errorf("expected obtain_permit in needs_action; got %v", v.NeedsAction)
	}
}

func TestEvaluate_RequiredPermitKind_RightKindIsSatisfied(t *testing.T) {
	now := atUTC(2026, 5, 7, 12, 0)
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "disabled-only", Kind: domain.RuleAllow, NeedsPermit: true,
				RequiredPermitKind: domain.PermitDisabled,
				TimeWindows:        []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
		permits: map[string][]domain.Permit{
			"ABC123": {{
				Kind:      domain.PermitDisabled, // matches!
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
		t.Errorf("disabled permit should satisfy a disabled-only rule")
	}
	if contains(v.NeedsAction, "obtain_permit") {
		t.Errorf("obtain_permit should not appear when user has the right permit")
	}
}

func TestEvaluate_RequiredPermitKind_EmptyKindAcceptsAnyPermit(t *testing.T) {
	// Backward compatibility: rules whose RequiredPermitKind is empty
	// continue accepting any permit on the plate (v1 behaviour).
	now := atUTC(2026, 5, 7, 12, 0)
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "any-permit-ok", Kind: domain.RuleAllow, NeedsPermit: true,
				// RequiredPermitKind unset → any kind satisfies.
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
		permits: map[string][]domain.Permit{
			"ABC123": {{
				Kind:      domain.PermitResidential,
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
		t.Errorf("any permit should satisfy a rule with no RequiredPermitKind")
	}
}

func TestEvaluate_RequiredPermitKind_HumaniseMentionsTheKind(t *testing.T) {
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "d", Kind: domain.RuleAllow, NeedsPermit: true,
				RequiredPermitKind: domain.PermitDisabled,
				Source:             domain.Source{System: "test", Reference: "X"},
				TimeWindows:        []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, _ := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 12, 0),
	})
	if len(v.Reasons) != 1 {
		t.Fatalf("expected 1 reason, got %d", len(v.Reasons))
	}
	got := v.Reasons[0].HumanReadable
	want := "disabled-parking permit"
	if !contains([]string{got}, want) {
		// Loose match — the phrase appears somewhere in the sentence.
		if !containsSubstring(got, want) {
			t.Errorf("humanise should mention %q; got %q", want, got)
		}
	}
}

func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// =============================================================================
// Seasonal date-range tests
// =============================================================================

func TestInSeasonalRange_Unset_AlwaysMatches(t *testing.T) {
	w := domain.TimeWindow{} // all month/day fields zero
	cases := []time.Time{
		atUTC(2026, 1, 1, 12, 0),
		atUTC(2026, 6, 15, 12, 0),
		atUTC(2026, 12, 31, 12, 0),
	}
	for _, at := range cases {
		if !inSeasonalRange(at, w) {
			t.Errorf("unset seasonal range should match %s", at)
		}
	}
}

func TestInSeasonalRange_MidYearRange(t *testing.T) {
	// June 15 – August 15.
	w := domain.TimeWindow{StartMonth: 6, StartDay: 15, EndMonth: 8, EndDay: 15}
	cases := []struct {
		at    time.Time
		match bool
	}{
		{atUTC(2026, 5, 30, 12, 0), false}, // before
		{atUTC(2026, 6, 15, 12, 0), true},  // inclusive start
		{atUTC(2026, 7, 4, 12, 0), true},   // inside
		{atUTC(2026, 8, 15, 12, 0), true},  // inclusive end
		{atUTC(2026, 8, 16, 12, 0), false}, // after
		{atUTC(2027, 7, 1, 12, 0), true},   // next year, still in season
	}
	for _, c := range cases {
		if got := inSeasonalRange(c.at, w); got != c.match {
			t.Errorf("inSeasonalRange(%s, Jun15-Aug15): want %v, got %v", c.at.Format("2006-01-02"), c.match, got)
		}
	}
}

func TestInSeasonalRange_CrossYearWrap(t *testing.T) {
	// August 16 – June 14 (everything except summer).
	w := domain.TimeWindow{StartMonth: 8, StartDay: 16, EndMonth: 6, EndDay: 14}
	cases := []struct {
		at    time.Time
		match bool
	}{
		{atUTC(2026, 6, 14, 12, 0), true},  // inclusive end
		{atUTC(2026, 6, 15, 12, 0), false}, // start of summer gap
		{atUTC(2026, 7, 4, 12, 0), false},  // middle of summer gap
		{atUTC(2026, 8, 15, 12, 0), false}, // last day of gap
		{atUTC(2026, 8, 16, 12, 0), true},  // inclusive start
		{atUTC(2026, 12, 31, 12, 0), true}, // late winter
		{atUTC(2027, 1, 15, 12, 0), true},  // next year, still in season
	}
	for _, c := range cases {
		if got := inSeasonalRange(c.at, w); got != c.match {
			t.Errorf("inSeasonalRange(%s, Aug16-Jun14): want %v, got %v", c.at.Format("2006-01-02"), c.match, got)
		}
	}
}

func TestEvaluate_SeasonalWindow_OutsideRangeIsNotActive(t *testing.T) {
	// A rule that only fires June 15 – August 15. Query at May 7 →
	// rule not active, so allowed=true by default.
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "summer-only", Kind: domain.RuleForbid,
				Source: domain.Source{System: "test", Reference: "X"},
				TimeWindows: []domain.TimeWindow{{
					WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440,
					StartMonth: 6, StartDay: 15, EndMonth: 8, EndDay: 15,
				}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, _ := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      atUTC(2026, 5, 7, 12, 0), // May — out of season
	})
	if !v.Allowed {
		t.Errorf("out-of-season Forbid should not fire; got allowed=false")
	}
	if len(v.Reasons) != 0 {
		t.Errorf("expected no reasons (rule inactive), got %d", len(v.Reasons))
	}
}

func TestEvaluate_SeasonalWindow_InsideRangeIsActive(t *testing.T) {
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "summer-only", Kind: domain.RuleForbid,
				Source: domain.Source{System: "test", Reference: "X"},
				TimeWindows: []domain.TimeWindow{{
					WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440,
					StartMonth: 6, StartDay: 15, EndMonth: 8, EndDay: 15,
				}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, _ := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      atUTC(2026, 7, 4, 12, 0), // July — in season
	})
	if v.Allowed {
		t.Errorf("in-season Forbid should fire; got allowed=true")
	}
}

// =============================================================================
// Priority-bucket / supersession tests — reserved-class spots overriding
// general allow rules at the same physical location.
// =============================================================================

func TestEvaluate_HigherPriorityAllow_SupersedesLowerPriority(t *testing.T) {
	// Real-world case: user is at a disabled bay carved into a paid
	// parking strip. The disabled rule (priority 20) and the general
	// paid-parking rule (priority 5) both apply. The user has no
	// permits — they can pay for the general spot but not satisfy the
	// disabled requirement. Verdict: NOT allowed, because the higher
	// priority rule binds.
	now := atUTC(2026, 5, 7, 12, 0)
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "general-paid", Kind: domain.RuleAllow, NeedsPayment: true,
				Priority:    5,
				Source:      domain.Source{System: "test", Reference: "ptillaten/X"},
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
			{
				ID: "disabled-only", Kind: domain.RuleAllow, NeedsPermit: true,
				Priority:           20,
				RequiredPermitKind: domain.PermitDisabled,
				Source:             domain.Source{System: "test", Reference: "prorelsehindrad/Y"},
				TimeWindows:        []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, _ := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "JAT52Y", Class: domain.VehicleCar},
		At:      now,
	})
	if v.Allowed {
		t.Errorf("higher-priority disabled-only rule should bind; got allowed=true")
	}
	if !contains(v.NeedsAction, "obtain_permit") {
		t.Errorf("expected obtain_permit in needs_action; got %v", v.NeedsAction)
	}
	if contains(v.NeedsAction, "pay_via_app") {
		t.Errorf("pay_via_app should NOT appear (general-paid is superseded); got %v", v.NeedsAction)
	}
}

func TestEvaluate_HigherPriorityAllow_BothReasonsListed(t *testing.T) {
	// Same setup as above — verify Reasons array contains both rules
	// for traceability, with the superseded one flagged.
	now := atUTC(2026, 5, 7, 12, 0)
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "general", Kind: domain.RuleAllow, NeedsPayment: true,
				Priority: 5, Source: domain.Source{System: "test", Reference: "A"},
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
			{
				ID: "disabled", Kind: domain.RuleAllow, NeedsPermit: true,
				RequiredPermitKind: domain.PermitDisabled,
				Priority:           20,
				Source:             domain.Source{System: "test", Reference: "B"},
				TimeWindows:        []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, _ := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC123", Class: domain.VehicleCar},
		At:      now,
	})

	if len(v.Reasons) != 2 {
		t.Fatalf("expected 2 reasons (both rules for transparency), got %d", len(v.Reasons))
	}

	var supersededCount, blockingCount int
	for _, r := range v.Reasons {
		if r.Superseded {
			supersededCount++
			if r.Source.Reference != "A" {
				t.Errorf("wrong rule marked superseded: %s (want A)", r.Source.Reference)
			}
		}
		if r.Blocks {
			blockingCount++
			if r.Source.Reference != "B" {
				t.Errorf("wrong rule marked blocking: %s (want B)", r.Source.Reference)
			}
		}
	}
	if supersededCount != 1 {
		t.Errorf("want exactly 1 superseded reason, got %d", supersededCount)
	}
	if blockingCount != 1 {
		t.Errorf("want exactly 1 blocking reason, got %d", blockingCount)
	}
}

func TestEvaluate_HigherPriorityAllow_UserSatisfiesIt(t *testing.T) {
	// Same disabled bay, but user HAS a disabled permit. The higher-
	// priority rule is satisfied. Verdict: allowed.
	now := atUTC(2026, 5, 7, 12, 0)
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "general", Kind: domain.RuleAllow, NeedsPayment: true,
				Priority:    5,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
			{
				ID: "disabled", Kind: domain.RuleAllow, NeedsPermit: true,
				RequiredPermitKind: domain.PermitDisabled,
				Priority:           20,
				TimeWindows:        []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
		permits: map[string][]domain.Permit{
			"ABC123": {{
				Kind: domain.PermitDisabled, Plate: "ABC123",
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
		t.Errorf("disabled permit holder should be allowed; got %s", v.Summary)
	}
	if contains(v.NeedsAction, "pay_via_app") {
		t.Errorf("pay_via_app should NOT appear when disabled rule supersedes general paid; got %v", v.NeedsAction)
	}
}

func TestEvaluate_SamePriorityAllows_StillAlternatives(t *testing.T) {
	// Two Allow rules at the same priority — peers, not superseded.
	// If either is satisfied, allowed. This is the original engine
	// semantics, preserved when priorities tie.
	now := atUTC(2026, 5, 7, 12, 0)
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "pay", Kind: domain.RuleAllow, NeedsPayment: true,
				Priority:    5,
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
			{
				ID: "permit-residential", Kind: domain.RuleAllow, NeedsPermit: true,
				RequiredPermitKind: domain.PermitResidential,
				Priority:           5,
				TimeWindows:        []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, _ := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      now,
	})
	if !v.Allowed {
		t.Errorf("equal-priority peers: either-satisfied → allowed; got %s", v.Summary)
	}
}

func TestEvaluate_ForbidWinsOverEverything(t *testing.T) {
	// Even if a high-priority reserved-class Allow is satisfied, an
	// applicable Forbid (e.g. servicedagar street cleaning) blocks.
	now := atUTC(2026, 5, 7, 12, 0)
	src := &fakeSource{
		rules: []domain.Rule{
			{
				ID: "cleaning", Kind: domain.RuleForbid,
				Priority:    10, // servicedagar
				Source:      domain.Source{System: "test", Reference: "servicedagar/X"},
				TimeWindows: []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
			{
				ID: "disabled", Kind: domain.RuleAllow, NeedsPermit: true,
				RequiredPermitKind: domain.PermitDisabled,
				Priority:           20,
				TimeWindows:        []domain.TimeWindow{{WeekdayMask: allWeekdays, StartMin: 0, EndMin: 1440}},
			},
		},
		permits: map[string][]domain.Permit{
			"ABC": {{
				Kind: domain.PermitDisabled, Plate: "ABC",
				ValidFrom: now.Add(-24 * time.Hour),
				ValidTo:   now.Add(24 * time.Hour),
			}},
		},
	}
	ev := New(src, NewHolidayCalendarSE())
	v, _ := ev.Evaluate(context.Background(), Query{
		Vehicle: domain.Vehicle{Plate: "ABC", Class: domain.VehicleCar},
		At:      now,
	})
	if v.Allowed {
		t.Errorf("Forbid should bind even when a higher-priority Allow is satisfied")
	}
}
