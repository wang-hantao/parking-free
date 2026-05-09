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
// declared in enricher.go (ZoneSource, TariffSource, OperatorSource,
// HazardSource) to populate the corresponding Verdict fields. The
// engine type-asserts and skips those a source does not support.
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
// its holiday calendar and a RuleSource.
type Evaluator struct {
	src RuleSource
	cal *HolidayCalendar
}

// New constructs an Evaluator.
func New(src RuleSource, cal *HolidayCalendar) *Evaluator {
	return &Evaluator{src: src, cal: cal}
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
		Allowed:   true, // default; a Forbid will overturn this
		ExpiresAt: q.At.Add(24 * time.Hour),
	}

	for _, s := range active {
		reason := domain.Reason{
			RuleID:        s.rule.ID,
			RegulationID:  s.rule.RegulationID,
			Disposition:   s.rule.Kind,
			HumanReadable: humanise(s.rule),
		}

		switch s.rule.Kind {
		case domain.RuleForbid:
			verdict.Allowed = false
			reason.Supports = true
			verdict.Reasons = append(verdict.Reasons, reason)
		case domain.RuleAllow, domain.RuleRestrict:
			if s.rule.NeedsPermit && !hasMatchingPermit(permits, q.At) {
				verdict.NeedsAction = appendUnique(verdict.NeedsAction, "obtain_permit")
			}
			if s.rule.NeedsPayment {
				verdict.NeedsAction = appendUnique(verdict.NeedsAction, "pay_via_app")
			}
			reason.Supports = verdict.Allowed
			verdict.Reasons = append(verdict.Reasons, reason)
		}

		// Tighten ExpiresAt to the earliest window boundary that
		// could change the verdict.
		if next := nextWindowBoundary(q.At, s.windows); !next.IsZero() && next.Before(verdict.ExpiresAt) {
			verdict.ExpiresAt = next
		}
	}

	// Enrichment: optional fields populated when the source supports them.
	e.enrich(ctx, q, &verdict, active)

	return verdict, nil
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

// humanise produces a brief human-readable description of a rule.
// In production, this would be a templated, localised renderer; the
// stub here is deliberately minimal.
func humanise(r domain.Rule) string {
	switch r.Kind {
	case domain.RuleForbid:
		return "Parking forbidden"
	case domain.RuleAllow:
		if r.NeedsPayment {
			return "Parking allowed with payment"
		}
		return "Parking allowed"
	case domain.RuleRestrict:
		return "Parking allowed with restrictions"
	}
	return string(r.Kind)
}
