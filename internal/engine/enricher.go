package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wang-hantao/parking-free/internal/domain"
)

// EngineVersion is reported in Verdict.Metadata. Bump on
// behaviour-affecting changes so clients can detect drift.
const EngineVersion = "0.1.0"

// Optional sub-interfaces that a RuleSource may also implement to
// enrich the Verdict. The engine type-asserts and calls only those
// the source supports — adding a new enrichment to the platform is a
// purely additive change to the implementing store.
//
// Note: tariff/pricing is no longer interface-driven; it's derived
// from Rule.TariffClassCode against the in-process TariffClasses
// registry (see tariffs.go).

// ZoneSource resolves the zone a position is in (paid/residential/etc.).
type ZoneSource interface {
	ZoneAt(ctx context.Context, pos domain.Coordinate) (*domain.ZoneRef, string /*street*/, string /*municipality*/, error)
}

// OperatorSource returns the operators that can take payment for a zone.
type OperatorSource interface {
	OperatorsForZone(ctx context.Context, zoneID, plate string) ([]domain.OperatorOption, error)
}

// CityOperatorSource returns the operators that serve an entire
// municipality, regardless of zone. Used as a fallback when
// OperatorsForZone yields nothing but payment is required. In
// Stockholm all four authorised operators (EasyPark, Parkster,
// Mobill, ePARK) serve the whole city; future cities with operator-
// per-zone contracts can still implement OperatorSource alongside
// this for richer mapping.
type CityOperatorSource interface {
	CityOperators(ctx context.Context, municipality, plate string) ([]domain.OperatorOption, error)
}

// HazardSource returns predictive warnings near a position.
type HazardSource interface {
	HazardsNearby(ctx context.Context, pos domain.Coordinate, at time.Time) ([]domain.Warning, error)
}

// enrich populates the optional fields of v based on whatever sub-
// interfaces the source implements. Errors from individual enrichers
// are non-fatal: the rest of the verdict is still returned.
//
// Metadata is populated here only when the caller hasn't already set
// it (so Evaluate can stamp the effective query mode before enriching
// without it being clobbered).
func (e *Evaluator) enrich(ctx context.Context, q Query, v *domain.Verdict, active []scoredRule) {
	if v.Metadata == nil {
		v.Metadata = &domain.Metadata{}
	}
	v.Metadata.EvaluatedAt = q.At
	v.Metadata.EngineVersion = EngineVersion

	// Constraints come straight from the active rules — no source needed.
	v.Constraints = constraintsFromRules(active)

	// Location.
	if zs, ok := e.src.(ZoneSource); ok {
		if zone, street, muni, err := zs.ZoneAt(ctx, q.Position); err == nil && zone != nil {
			v.Location = &domain.LocationInfo{Zone: zone, Street: street, Municipality: muni}
		}
	}

	// Pricing: derived from each active rule's TariffClassCode against
	// the in-process class registry. No store interface needed.
	v.Pricing = e.pricingFromActive(q, active)

	// Operators. Two-tier lookup:
	//
	//   1. Zone-based via OperatorsForZone, when a zone matches. Each
	//      operator may have a zone-specific deeplink template (with
	//      a real external_zone_id substituted into the URL).
	//
	//   2. City-wide via CityOperators, when zone-based yields
	//      nothing but payment is required. The operator's default
	//      landing URL — user types the area code from the on-street
	//      sign after the app opens.
	//
	// Stockholm uses (2) almost exclusively today: the four
	// authorised operators all serve the whole municipality and we
	// don't ingest operator-specific zone mappings.
	if v.Location != nil && v.Location.Zone != nil {
		if os, ok := e.src.(OperatorSource); ok {
			if ops, err := os.OperatorsForZone(ctx, v.Location.Zone.ID, q.Vehicle.Plate); err == nil {
				if v.Pricing == nil {
					v.Pricing = &domain.PricingInfo{}
				}
				v.Pricing.Operators = ops
			}
		}
	}
	if needsPaymentOperators(v) {
		municipality := ""
		if v.Location != nil {
			municipality = v.Location.Municipality
		}
		if municipality == "" {
			municipality = municipalityFromActiveRules(active)
		}
		if municipality != "" {
			if cos, ok := e.src.(CityOperatorSource); ok {
				if ops, err := cos.CityOperators(ctx, municipality, q.Vehicle.Plate); err == nil && len(ops) > 0 {
					if v.Pricing == nil {
						v.Pricing = &domain.PricingInfo{}
					}
					v.Pricing.Operators = ops
				}
			}
		}
	}

	// Warnings: combine source-provided hazards with engine-derived ones.
	if hs, ok := e.src.(HazardSource); ok {
		if hz, err := hs.HazardsNearby(ctx, q.Position, q.At); err == nil {
			v.Warnings = append(v.Warnings, hz...)
		}
	}
	v.Warnings = append(v.Warnings, e.derivedWarnings(q, active)...)

	// Estimated cost: only if the client supplied a desired duration
	// AND we have pricing.
	if q.Duration > 0 && v.Pricing != nil {
		v.EstimatedCost = e.estimateCost(q, active, q.At.Add(q.Duration))
	}
}

// constraintsFromRules collapses the active rules' max-stay,
// payment-required, permit-required, and vehicle-class info into a
// single Constraints summary. The strictest values win.
func constraintsFromRules(active []scoredRule) *domain.Constraints {
	if len(active) == 0 {
		return nil
	}
	c := &domain.Constraints{}
	classSet := map[domain.VehicleClass]struct{}{}
	for _, s := range active {
		if s.rule.NeedsPayment {
			c.PaymentRequired = true
		}
		if s.rule.NeedsPermit {
			c.PermitRequired = true
		}
		if d := int(s.rule.MaxDuration / time.Minute); d > 0 {
			if c.MaxStayMinutes == 0 || d < c.MaxStayMinutes {
				c.MaxStayMinutes = d
			}
		}
		for _, vc := range s.rule.VehicleClasses {
			classSet[vc] = struct{}{}
		}
	}
	if len(classSet) > 0 {
		for vc := range classSet {
			c.VehicleClasses = append(c.VehicleClasses, vc)
		}
		// Stable order for testability.
		sort.Slice(c.VehicleClasses, func(i, j int) bool { return c.VehicleClasses[i] < c.VehicleClasses[j] })
	}
	if c.PaymentRequired || c.PermitRequired || c.MaxStayMinutes > 0 || len(c.VehicleClasses) > 0 {
		return c
	}
	return nil
}

// pricingFromActive computes the Pricing block from the active
// rules' tariff class codes. The first class found in the active
// set is the "primary" one. With multiple distinct classes nearby
// (rare in practice), this picks whichever rule lists first after
// priority sort — deterministic for tests, good enough for v1.
func (e *Evaluator) pricingFromActive(q Query, active []scoredRule) *domain.PricingInfo {
	var class *TariffClass
	for _, s := range active {
		code := s.rule.TariffClassCode
		if code == "" {
			continue
		}
		if c, ok := e.tariffClasses[code]; ok {
			class = &c
			break
		}
	}
	if class == nil {
		return nil
	}

	current := pickActiveTariffWindow(class.Windows, q.At, e.cal)

	p := &domain.PricingInfo{Currency: class.Currency}
	if current == nil {
		// No window matches — parking is free at this moment under
		// this class (e.g. taxa 3 outside its priced hours).
		p.IsFreeNow = true
	} else {
		p.IsFreeNow = current.Rate == 0
		if !p.IsFreeNow {
			p.CurrentRate = &domain.Rate{
				Amount: current.Rate,
				Per:    perLabel(time.Duration(current.PerSec) * time.Second),
			}
		}
	}

	// Next rate change: the next moment within the next 48h at which
	// the active window would differ from the current one.
	if next, nextRate := nextTariffChange(class.Windows, q.At, e.cal, current); !next.IsZero() {
		p.NextRateChange = &next
		if nextRate != nil {
			p.NextRate = &domain.Rate{Amount: *nextRate, Per: perLabel(time.Hour)}
		} else {
			// Rate becomes zero (free hours).
			p.NextRate = &domain.Rate{Amount: 0, Per: "hour"}
		}
	}

	return p
}

// pickActiveTariffWindow returns the highest-priority window in ws
// that matches the given moment, or nil if none do.
func pickActiveTariffWindow(ws []TariffWindowSpec, at time.Time, cal *HolidayCalendar) *TariffWindowSpec {
	dt := cal.DayType(at)
	tod := minutesOfDay(at)
	wbit := 1 << int(at.Weekday())

	var best *TariffWindowSpec
	for i := range ws {
		w := &ws[i]
		if !tariffWindowMatches(*w, dt, tod, wbit) {
			continue
		}
		if best == nil || w.Priority > best.Priority {
			best = w
		}
	}
	return best
}

// tariffWindowMatches mirrors the rule-window matcher.
func tariffWindowMatches(w TariffWindowSpec, dt domain.DayType, tod, weekdayBit int) bool {
	if w.WeekdayMask != 0 && w.WeekdayMask&weekdayBit == 0 {
		return false
	}
	if w.DayType != "" && w.DayType != dt {
		return false
	}
	return inTimeRange(tod, w.StartMin, w.EndMin)
}

// nextTariffChange returns the next moment at or after `at` where the
// active rate would change away from `current`, and the rate that
// applies at that moment (nil meaning free / no window matches).
// Searches up to 48h ahead — beyond that there's no useful precision
// for a verdict that's already capped to a 24h ExpiresAt.
func nextTariffChange(ws []TariffWindowSpec, at time.Time, cal *HolidayCalendar, current *TariffWindowSpec) (time.Time, *float64) {
	// Walk every window boundary (start_min and end_min) projected to
	// today and tomorrow, look for the earliest one strictly after
	// `at` where the active window differs from `current`.
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
	candidates := []time.Time{}
	for _, offset := range []int{0, 1} {
		base := day.AddDate(0, 0, offset)
		for _, w := range ws {
			if w.StartMin > 0 {
				candidates = append(candidates, base.Add(time.Duration(w.StartMin)*time.Minute))
			}
			if w.EndMin > 0 && w.EndMin < 1440 {
				candidates = append(candidates, base.Add(time.Duration(w.EndMin)*time.Minute))
			}
			// EndMin == 1440 means end of day; the next day's
			// 00:00 boundary is already covered as offset=1 start.
			if w.EndMin == 1440 {
				candidates = append(candidates, base.AddDate(0, 0, 1))
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Before(candidates[j]) })

	for _, t := range candidates {
		if !t.After(at) {
			continue
		}
		next := pickActiveTariffWindow(ws, t, cal)
		if sameWindow(next, current) {
			continue
		}
		if next == nil {
			return t, nil
		}
		r := next.Rate
		return t, &r
	}
	return time.Time{}, nil
}

func sameWindow(a, b *TariffWindowSpec) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Rate == b.Rate && a.StartMin == b.StartMin && a.EndMin == b.EndMin &&
		a.WeekdayMask == b.WeekdayMask && a.DayType == b.DayType && a.Priority == b.Priority
}

// perLabel maps a billing unit duration to the JSON label used in
// PricingInfo.Rate.Per.
func perLabel(d time.Duration) string {
	switch d {
	case time.Minute:
		return "minute"
	case 24 * time.Hour:
		return "day"
	default:
		return "hour"
	}
}

// derivedWarnings produces engine-level warnings without needing a
// HazardSource. These are warnings derivable from the rule set alone.
func (e *Evaluator) derivedWarnings(q Query, active []scoredRule) []domain.Warning {
	var out []domain.Warning
	for _, s := range active {
		if d := s.rule.MaxDuration; d > 0 {
			until := q.At.Add(d)
			out = append(out, domain.Warning{
				Kind:          domain.WarnMaxStayExpiring,
				Severity:      "info",
				EndsAt:        &until,
				HumanReadable: fmt.Sprintf("Maximum stay %d minutes from now", int(d/time.Minute)),
			})
		}
	}
	return out
}

// estimateCost walks tariff windows from start to end, summing per
// segment. Uses the same tariff-class machinery as Pricing.
func (e *Evaluator) estimateCost(q Query, active []scoredRule, end time.Time) *domain.CostEstimate {
	var class *TariffClass
	for _, s := range active {
		code := s.rule.TariffClassCode
		if code == "" {
			continue
		}
		if c, ok := e.tariffClasses[code]; ok {
			class = &c
			break
		}
	}
	if class == nil {
		return nil
	}

	est := &domain.CostEstimate{
		DurationMinutes: int(end.Sub(q.At) / time.Minute),
		Currency:        class.Currency,
	}

	cursor := q.At
	// Bound the segment-walking loop: at most one segment per window
	// boundary within the duration, capped for safety.
	for i := 0; i < 96 && cursor.Before(end); i++ {
		curWin := pickActiveTariffWindow(class.Windows, cursor, e.cal)
		nextTime, _ := nextTariffChange(class.Windows, cursor, e.cal, curWin)
		segEnd := end
		if !nextTime.IsZero() && nextTime.Before(end) {
			segEnd = nextTime
		}

		seg := domain.CostSegment{From: cursor, To: segEnd}
		if curWin != nil {
			seg.Rate = curWin.Rate
			seg.Cost = costPerWindow(*curWin, cursor, segEnd)
		}
		est.Total += seg.Cost
		est.Breakdown = append(est.Breakdown, seg)
		cursor = segEnd
	}

	if est.Total < 0 {
		est.Total = 0
	}
	return est
}

func costPerWindow(w TariffWindowSpec, from, to time.Time) float64 {
	if w.PerSec == 0 {
		return 0
	}
	dur := to.Sub(from).Seconds()
	return w.Rate * (dur / float64(w.PerSec))
}

// needsPaymentOperators reports whether the verdict should be
// augmented with payment-app operators. True when payment is
// actually required right now (not just "available later") AND no
// operators have been attached yet.
//
// Conservative on "right now": if pricing is currently free (e.g.
// query at 22:00 when paid hours are 09-17), we suppress operators
// — the user doesn't need to pay until the rate kicks in. That's
// generally what people expect; if it becomes confusing we can
// surface a "you'll need to pay starting at X" hint instead.
func needsPaymentOperators(v *domain.Verdict) bool {
	if v == nil || v.Pricing == nil {
		return false
	}
	if v.Pricing.IsFreeNow {
		return false
	}
	if v.Pricing.CurrentRate == nil || v.Pricing.CurrentRate.Amount <= 0 {
		return false
	}
	return len(v.Pricing.Operators) == 0
}

// municipalityFromActiveRules infers the municipality from the
// source.system field of the active rules. Useful when the
// enricher's ZoneAt path didn't populate v.Location (no zone
// polygon matched and no road_segment within the sqlStreetAt
// radius). Knowing the city is enough to surface city-wide payment
// operators.
//
// For now this is a hardcoded prefix mapping — Stockholm is the
// only city wired up. Future cities would extend the switch.
func municipalityFromActiveRules(active []scoredRule) string {
	for _, s := range active {
		switch {
		case strings.HasPrefix(s.rule.Source.System, "stockholm."):
			return "Stockholm"
		}
	}
	return ""
}
