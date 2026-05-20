package engine

import (
	"context"
	"sort"
	"time"

	"github.com/wang-hantao/parking-free/internal/domain"
)

// RuleSource is the read interface the engine needs. Implemented by
// the store layer in production and by in-memory fakes in tests.
//
// A RuleSource may also implement any of the optional sub-interfaces
// declared in enricher.go (ZoneSource, OperatorSource, HazardSource)
// to populate the corresponding Verdict fields. The engine type-
// asserts and skips those a source does not support. Pricing is no
// longer interface-driven — it derives from each Rule's
// TariffClassCode against the in-process TariffClasses registry.
type RuleSource interface {
	// RulesNearby returns all rules whose applies-to geometry includes
	// or comes within the given radius of the position. The store is
	// responsible for offset arithmetic (10m-before-junction, etc.).
	RulesNearby(ctx context.Context, pos domain.Coordinate, radiusM float64) ([]domain.Rule, error)

	// PermitsForPlate returns all permits currently registered to a plate.
	PermitsForPlate(ctx context.Context, plate string) ([]domain.Permit, error)
}

// Query is the input to Evaluate.
type Query struct {
	Position domain.Coordinate
	Vehicle  domain.Vehicle
	At       time.Time
	RadiusM  float64       // search radius for nearby rules; 0 means default (50m)
	Duration time.Duration // optional desired stay; if > 0, EstimatedCost is populated
}

// scoredRule pairs a rule with the time windows that matched at the
// query moment. Lifted to package level so enricher.go can use it.
type scoredRule struct {
	rule    domain.Rule
	windows []domain.TimeWindow
}

// Evaluator computes a Verdict for a Query. It is stateless beyond
// its holiday calendar, a RuleSource, and the tariff class registry.
type Evaluator struct {
	src           RuleSource
	cal           *HolidayCalendar
	tariffClasses map[string]TariffClass
}

// New constructs an Evaluator using the default Stockholm tariff
// class registry. Tests can swap the registry with WithTariffClasses.
func New(src RuleSource, cal *HolidayCalendar) *Evaluator {
	return &Evaluator{src: src, cal: cal, tariffClasses: TariffClasses}
}

// WithTariffClasses replaces the registry used for pricing
// enrichment. Returns the same evaluator for chaining; intended for
// tests that need to assert behaviour against a known schedule.
func (e *Evaluator) WithTariffClasses(classes map[string]TariffClass) *Evaluator {
	e.tariffClasses = classes
	return e
}

// Evaluate runs the rule walk and returns a Verdict.
//
// Algorithm:
//  1. Fetch nearby applicable rules from the source.
//  2. Filter by vehicle class and active time window.
//  3. Sort by priority (higher first); the first matching rule of kind
//     Forbid produces a deny verdict, the first Allow with matching
//     permit/payment requirements produces an allow verdict.
//  4. Compute ExpiresAt as the earliest moment any contributing rule
//     could change disposition (next time-window boundary).
//  5. Enrich the verdict with optional fields (Location, Pricing,
//     Constraints, Warnings, EstimatedCost, Metadata) using whatever
//     sub-interfaces the RuleSource implements.
//
// This is a deliberately simple kernel — the complexity is in the
// regulation graph, not the walker. Future enhancements (conflict
// resolution between national and municipal rules, signage overrides)
// extend this method.
func (e *Evaluator) Evaluate(ctx context.Context, q Query) (domain.Verdict, error) {
	radius := q.RadiusM
	if radius <= 0 {
		radius = 50
	}

	nearby, err := e.src.RulesNearby(ctx, q.Position, radius)
	if err != nil {
		return domain.Verdict{}, err
	}

	permits, err := e.src.PermitsForPlate(ctx, q.Vehicle.Plate)
	if err != nil {
		return domain.Verdict{}, err
	}

	dayType := e.cal.DayType(q.At)
	tod := minutesOfDay(q.At)

	// Filter to active, vehicle-relevant rules.
	var active []scoredRule
	for _, r := range nearby {
		if !r.MatchesVehicle(q.Vehicle) {
			continue
		}
		matched := matchingWindows(r.TimeWindows, q.At, dayType, tod)
		if len(r.TimeWindows) > 0 && len(matched) == 0 {
			continue
		}
		active = append(active, scoredRule{rule: r, windows: matched})
	}

	sort.SliceStable(active, func(i, j int) bool {
		return active[i].rule.Priority > active[j].rule.Priority
	})

	verdict := domain.Verdict{
		ExpiresAt: q.At.Add(24 * time.Hour),
	}

	hasPermit := hasMatchingPermit(permits, q.At)

	// Two-pass build: first construct all reasons and track which Allow
	// rules are satisfiable by this user; then determine Allowed and
	// the Blocks flag on each reason from the aggregate state.
	type stagedReason struct {
		reason      domain.Reason
		isForbid    bool
		isAllow     bool
		satisfiable bool // for Allow only
		needsPermit bool // for Allow only — whether this rule contributes the "obtain_permit" need
	}

	var staged []*stagedReason
	var forbidFired, allowExists, satisfiableAllow bool

	for _, s := range active {
		r := domain.Reason{
			RuleID:        s.rule.ID,
			RegulationID:  s.rule.RegulationID,
			Source:        s.rule.Source,
			Disposition:   s.rule.Kind,
			HumanReadable: humanise(s.rule),
		}
		sr := &stagedReason{reason: r}

		switch s.rule.Kind {
		case domain.RuleForbid:
			forbidFired = true
			sr.isForbid = true
		case domain.RuleAllow, domain.RuleRestrict:
			allowExists = true
			sr.isAllow = true
			sr.satisfiable = true
			if s.rule.NeedsPermit && !hasPermit {
				verdict.NeedsAction = appendUnique(verdict.NeedsAction, "obtain_permit")
				sr.satisfiable = false
				sr.needsPermit = true
			}
			if s.rule.NeedsPayment {
				verdict.NeedsAction = appendUnique(verdict.NeedsAction, "pay_via_app")
				// Payment is always satisfiable: user can choose to pay.
			}
			if sr.satisfiable {
				satisfiableAllow = true
			}
		}

		staged = append(staged, sr)

		// Tighten ExpiresAt to the earliest window boundary that could
		// change the verdict.
		if next := nextWindowBoundary(q.At, s.windows); !next.IsZero() && next.Before(verdict.ExpiresAt) {
			verdict.ExpiresAt = next
		}
	}

	// Decide Allowed.
	//
	//   - Forbid fires → not allowed (highest precedence).
	//   - Else: if any Allow exists, allowed only when at least one is
	//     satisfiable by this user. With no Allows at all, default to
	//     allowed (nothing forbids).
	switch {
	case forbidFired:
		verdict.Allowed = false
	case allowExists:
		verdict.Allowed = satisfiableAllow
	default:
		verdict.Allowed = true
	}

	// Mark Supports and Blocks on each reason.
	for _, sr := range staged {
		switch {
		case sr.isForbid:
			sr.reason.Supports = !verdict.Allowed // supports the not-allowed verdict
			sr.reason.Blocks = !verdict.Allowed
		case sr.isAllow:
			sr.reason.Supports = sr.satisfiable && verdict.Allowed
			// An unsatisfied Allow blocks only when no satisfiable
			// alternative exists — otherwise it's just informational.
			sr.reason.Blocks = !sr.satisfiable && !verdict.Allowed && !forbidFired
		}
		verdict.Reasons = append(verdict.Reasons, sr.reason)
	}

	verdict.Summary = computeSummary(verdict.Allowed, forbidFired, verdict.NeedsAction)
	verdict.Reasons = dedupeReasonsByCitation(verdict.Reasons)

	// Enrichment: optional fields populated when the source supports them.
	e.enrich(ctx, q, &verdict, active)

	return verdict, nil
}

// dedupeReasonsByCitation collapses Reasons that share the same
// source.reference. A query within the default 50m radius typically
// touches several road_segments belonging to one regulation (LTF
// regulations span multiple street fragments), and our spatial join
// emits one Rule per segment. Without dedup, the response shows the
// same citation 4-8 times with identical text, which is noise.
//
// When two reasons share a citation we keep the one that's most
// informative for the verdict outcome — a blocking reason beats a
// merely supporting one, which beats a neutral one. Reasons with no
// source reference (e.g. tests without LTF-style attribution) are
// left untouched.
func dedupeReasonsByCitation(reasons []domain.Reason) []domain.Reason {
	seen := map[string]int{} // citation → index in out
	var out []domain.Reason
	for _, r := range reasons {
		key := r.Source.Reference
		if key == "" {
			out = append(out, r)
			continue
		}
		if idx, exists := seen[key]; exists {
			if reasonPreference(r) > reasonPreference(out[idx]) {
				out[idx] = r
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, r)
	}
	return out
}

// reasonPreference scores a Reason for dedup tie-breaking. Higher =
// more informative.
func reasonPreference(r domain.Reason) int {
	switch {
	case r.Blocks:
		return 2
	case r.Supports:
		return 1
	default:
		return 0
	}
}

func minutesOfDay(t time.Time) int { return t.Hour()*60 + t.Minute() }

func matchingWindows(ws []domain.TimeWindow, at time.Time, dt domain.DayType, tod int) []domain.TimeWindow {
	var out []domain.TimeWindow
	weekdayBit := 1 << int(at.Weekday())
	for _, w := range ws {
		if w.WeekdayMask != 0 && w.WeekdayMask&weekdayBit == 0 {
			continue
		}
		if w.DayType != "" && w.DayType != dt {
			continue
		}
		if !inTimeRange(tod, w.StartMin, w.EndMin) {
			continue
		}
		out = append(out, w)
	}
	return out
}

func inTimeRange(tod, start, end int) bool {
	if end >= start {
		return tod >= start && tod < end
	}
	// crosses midnight
	return tod >= start || tod < end
}

// nextWindowBoundary returns the next moment after `at` at which any
// of the given windows begins or ends. Used to set Verdict.ExpiresAt
// so the caller knows when to re-evaluate.
func nextWindowBoundary(at time.Time, ws []domain.TimeWindow) time.Time {
	var best time.Time
	for _, w := range ws {
		for _, m := range []int{w.StartMin, w.EndMin} {
			candidate := time.Date(at.Year(), at.Month(), at.Day(), 0, m, 0, 0, at.Location())
			if !candidate.After(at) {
				candidate = candidate.AddDate(0, 0, 1)
			}
			if best.IsZero() || candidate.Before(best) {
				best = candidate
			}
		}
	}
	return best
}

func hasMatchingPermit(ps []domain.Permit, at time.Time) bool {
	for _, p := range ps {
		if p.IsValidAt(at) {
			return true
		}
	}
	return false
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// computeSummary produces a one-line plain-English explanation of the
// verdict, intended for direct display. The detailed reasons array
// remains the source of truth; this just gives clients a ready-made
// label.
func computeSummary(allowed, forbidFired bool, needsAction []string) string {
	needsPay := containsString(needsAction, "pay_via_app")
	needsPermit := containsString(needsAction, "obtain_permit")

	if !allowed {
		if forbidFired {
			return "Parking forbidden at this location"
		}
		return "Parking not permitted: nearby spots require a permit you don't have"
	}

	switch {
	case needsPay && needsPermit:
		// Mixed paid + permit-only spots within scope; user can park
		// via the paid path. Flag the permit-only context so they know.
		return "Parking allowed with payment (some nearby spots are permit-only)"
	case needsPay:
		return "Parking allowed with payment"
	case needsPermit:
		// Reached only if the user has a permit (else allowed=false).
		return "Parking allowed for permit holders"
	default:
		return "Parking allowed"
	}
}

func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// humanise produces a brief human-readable description of a rule.
// In production this would be a templated, localised renderer; the
// stub here at least distinguishes the four combinations of
// NeedsPayment × NeedsPermit so the reasons array isn't ambiguous.
func humanise(r domain.Rule) string {
	switch r.Kind {
	case domain.RuleForbid:
		return "Parking forbidden"
	case domain.RuleAllow:
		switch {
		case r.NeedsPayment && r.NeedsPermit:
			return "Parking allowed with payment or valid permit"
		case r.NeedsPermit:
			return "Parking allowed only for permit holders"
		case r.NeedsPayment:
			return "Parking allowed with payment"
		default:
			return "Parking allowed"
		}
	case domain.RuleRestrict:
		return "Parking allowed with restrictions"
	}
	return string(r.Kind)
}
