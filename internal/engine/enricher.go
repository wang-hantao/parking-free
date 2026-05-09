package engine

import (
	"context"
	"fmt"
	"sort"
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

// ZoneSource resolves the zone a position is in (paid/residential/etc.).
type ZoneSource interface {
	ZoneAt(ctx context.Context, pos domain.Coordinate) (*domain.ZoneRef, string /*street*/, string /*municipality*/, error)
}

// TariffSource returns the active tariff(s) for a position at a moment.
// Multiple results are allowed (e.g. weekday + holiday tariffs that
// would apply at different times); the engine picks the one whose
// time-window contains `at` for CurrentRate, and the chronologically
// next one for NextRate.
type TariffSource interface {
	TariffsAt(ctx context.Context, pos domain.Coordinate, at time.Time) ([]TariffWindow, error)
}

// TariffWindow couples a tariff with the time window during which it
// applies. ZeroAmount indicates a free interval.
type TariffWindow struct {
	From       time.Time
	To         time.Time
	Amount     float64
	Per        string // "hour" | "minute" | "day"
	Currency   string
	MaxSession *float64
}

// OperatorSource returns the operators that can take payment for a zone.
type OperatorSource interface {
	OperatorsForZone(ctx context.Context, zoneID, plate string) ([]domain.OperatorOption, error)
}

// HazardSource returns predictive warnings near a position.
type HazardSource interface {
	HazardsNearby(ctx context.Context, pos domain.Coordinate, at time.Time) ([]domain.Warning, error)
}

// enrich populates the optional fields of v based on whatever sub-
// interfaces the source implements. Errors from individual enrichers
// are non-fatal: the rest of the verdict is still returned.
func (e *Evaluator) enrich(ctx context.Context, q Query, v *domain.Verdict, active []scoredRule) {
	v.Metadata = &domain.Metadata{
		EvaluatedAt:   q.At,
		EngineVersion: EngineVersion,
	}

	// Constraints come straight from the active rules — no source needed.
	v.Constraints = constraintsFromRules(active)

	// Location.
	if zs, ok := e.src.(ZoneSource); ok {
		if zone, street, muni, err := zs.ZoneAt(ctx, q.Position); err == nil && zone != nil {
			v.Location = &domain.LocationInfo{Zone: zone, Street: street, Municipality: muni}
		}
	}

	// Pricing.
	if ts, ok := e.src.(TariffSource); ok {
		if tws, err := ts.TariffsAt(ctx, q.Position, q.At); err == nil && len(tws) > 0 {
			v.Pricing = pricingFromTariffs(tws, q.At)
		}
	}

	// Operators (only populates if we know the zone).
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
		v.EstimatedCost = estimateCost(ctx, e.src, q, q.At.Add(q.Duration))
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

// pricingFromTariffs picks the tariff whose window contains `at` as
// CurrentRate, and the next chronological window as NextRate.
func pricingFromTariffs(tws []TariffWindow, at time.Time) *domain.PricingInfo {
	sort.Slice(tws, func(i, j int) bool { return tws[i].From.Before(tws[j].From) })
	p := &domain.PricingInfo{}
	for i, tw := range tws {
		if !at.Before(tw.From) && at.Before(tw.To) {
			p.Currency = tw.Currency
			p.IsFreeNow = tw.Amount == 0
			if !p.IsFreeNow {
				p.CurrentRate = &domain.Rate{Amount: tw.Amount, Per: tw.Per}
			}
			p.MaxSessionCost = tw.MaxSession
			if i+1 < len(tws) {
				next := tws[i+1]
				nextTime := next.From
				p.NextRateChange = &nextTime
				p.NextRate = &domain.Rate{Amount: next.Amount, Per: next.Per}
			}
			return p
		}
	}
	// No active window — pricing simply isn't surfaced.
	return nil
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
// segment. If TariffSource isn't available the function returns nil.
func estimateCost(ctx context.Context, src RuleSource, q Query, end time.Time) *domain.CostEstimate {
	ts, ok := src.(TariffSource)
	if !ok {
		return nil
	}
	tws, err := ts.TariffsAt(ctx, q.Position, q.At)
	if err != nil || len(tws) == 0 {
		return nil
	}
	sort.Slice(tws, func(i, j int) bool { return tws[i].From.Before(tws[j].From) })

	est := &domain.CostEstimate{
		DurationMinutes: int(end.Sub(q.At) / time.Minute),
	}
	cursor := q.At
	for _, tw := range tws {
		if !cursor.Before(end) {
			break
		}
		segStart := maxTime(cursor, tw.From)
		segEnd := minTime(end, tw.To)
		if !segStart.Before(segEnd) {
			continue
		}
		seg := domain.CostSegment{
			From: segStart,
			To:   segEnd,
			Rate: tw.Amount,
		}
		seg.Cost = costForSegment(tw, segStart, segEnd)
		est.Total += seg.Cost
		est.Currency = tw.Currency
		est.Breakdown = append(est.Breakdown, seg)
		cursor = segEnd
	}
	if est.Currency == "" {
		return nil
	}
	if est.Total < 0 {
		est.Total = 0
	}
	return est
}

func costForSegment(tw TariffWindow, from, to time.Time) float64 {
	dur := to.Sub(from)
	switch tw.Per {
	case "minute":
		return tw.Amount * dur.Minutes()
	case "day":
		return tw.Amount * (dur.Hours() / 24)
	default: // "hour"
		return tw.Amount * dur.Hours()
	}
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
