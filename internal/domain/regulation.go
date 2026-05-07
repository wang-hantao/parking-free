package domain

import "time"

// Source identifies the upstream system that a regulation came from.
// It is the foundation of the provenance trail that lets us generate
// defensible dispute letters and audit rule changes.
type Source struct {
	System    string `json:"system"`    // e.g. "stockholm.ltf-tolken", "stfs"
	Reference string `json:"reference"` // upstream ID (föreskriftsnummer, etc.)
}

// Regulation is a single legal instrument: a municipal parking
// ordinance, a national road law, or any other source of rules.
//
// One Regulation typically contains many Rules. Effective dates are
// inclusive of From, exclusive of To.
type Regulation struct {
	ID                string    `json:"id"`
	Source            Source    `json:"source"`
	DecisionAuthority string    `json:"decision_authority"`
	Language          string    `json:"language"` // BCP-47, e.g. "sv-SE"
	EffectiveFrom     time.Time `json:"effective_from"`
	EffectiveTo       time.Time `json:"effective_to,omitempty"` // zero = open-ended
}

// RuleKind describes the disposition of a rule.
type RuleKind string

const (
	RuleAllow    RuleKind = "allow"    // parking permitted under conditions
	RuleForbid   RuleKind = "forbid"   // parking forbidden
	RuleRestrict RuleKind = "restrict" // permitted but with constraints (max stay, etc.)
)

// Rule is one applicable policy fragment. A Regulation may contain
// many Rules with different time windows, vehicle-class filters, or
// geometric scopes.
type Rule struct {
	ID             string         `json:"id"`
	RegulationID   string         `json:"regulation_id"`
	Kind           RuleKind       `json:"kind"`
	MaxDuration    time.Duration  `json:"max_duration,omitempty"`
	NeedsPayment   bool           `json:"needs_payment"`
	NeedsPermit    bool           `json:"needs_permit"`
	VehicleClasses []VehicleClass `json:"vehicle_classes,omitempty"` // empty = all
	Priority       int            `json:"priority"`                  // higher wins on conflict
	TimeWindows    []TimeWindow   `json:"time_windows"`
	AppliesTo      []AppliesTo    `json:"applies_to"`
}

// MatchesVehicle returns true if this rule applies to the given vehicle.
// An empty VehicleClasses list means "applies to all classes".
func (r Rule) MatchesVehicle(v Vehicle) bool {
	if len(r.VehicleClasses) == 0 {
		return true
	}
	for _, c := range r.VehicleClasses {
		if c == v.Class {
			return true
		}
	}
	return false
}

// DayType encodes the bracket/red-day grammar used on Swedish parking
// signs. A given calendar date maps to exactly one DayType under a
// HolidayCalendar.
type DayType string

const (
	DayTypeNormal     DayType = "normal"      // Mon–Fri, not before a holiday
	DayTypePreHoliday DayType = "pre_holiday" // The day before a Sunday/holiday (often Saturday)
	DayTypeHoliday    DayType = "holiday"     // Sundays and public holidays
)

// TimeWindow restricts a rule to particular weekdays, day-types, and
// hours. A rule with no time windows applies at all times.
//
// StartTime and EndTime are minutes from midnight (0..1440). EndTime
// may be less than StartTime to denote a window crossing midnight
// (e.g. servicedag 22:00–06:00).
type TimeWindow struct {
	WeekdayMask int     `json:"weekday_mask"` // bitmask Sun=1, Mon=2, ..., Sat=64
	DayType     DayType `json:"day_type"`
	StartMin    int     `json:"start_min"` // minutes since 00:00
	EndMin      int     `json:"end_min"`
	DateFrom    string  `json:"date_from,omitempty"` // optional YYYY-MM-DD seasonal limit
	DateTo      string  `json:"date_to,omitempty"`
}

// AppliesToKind indicates what geometric target a rule binds to.
type AppliesToKind string

const (
	TargetRoadSegment     AppliesToKind = "road_segment"
	TargetZone            AppliesToKind = "zone"
	TargetParkingArea     AppliesToKind = "parking_area"
	TargetPointOfInterest AppliesToKind = "poi"
)

// AppliesTo links a Rule to a geometric target with optional offsets.
// The 10m-before-junction rule is modelled as
// AppliesTo{Kind: TargetPointOfInterest, OffsetFromMeters: -10, OffsetToMeters: 0}.
type AppliesTo struct {
	RuleID           string        `json:"rule_id"`
	Kind             AppliesToKind `json:"kind"`
	TargetID         string        `json:"target_id"`
	OffsetFromMeters float64       `json:"offset_from_meters,omitempty"`
	OffsetToMeters   float64       `json:"offset_to_meters,omitempty"`
}

// Zone is a city-defined area with semantic meaning (paid zone,
// residential zone, mixed). It is distinct from operator-side zones,
// which live in the operator catalog.
type Zone struct {
	ID     string `json:"id"`
	City   string `json:"city"`
	Code   string `json:"code"`
	Kind   string `json:"kind"` // "paid" | "residential" | "mixed" | "loading"
	Source Source `json:"source"`
	// Polygon stored in DB column; not carried in API responses.
}

// PointOfInterest holds discrete geometry that triggers offset rules:
// junctions, crosswalks, hydrants, bus stops.
type PointOfInterest struct {
	ID       string     `json:"id"`
	Kind     string     `json:"kind"` // "junction" | "crosswalk" | "bus_stop" | etc.
	Position Coordinate `json:"position"`
	Source   Source     `json:"source"`
}
