package domain

import "time"

// LocationInfo describes where the queried position is, for client UI
// confirmation ("you're parking on Odengatan in Zone 14").
type LocationInfo struct {
	Zone         *ZoneRef `json:"zone,omitempty"`
	Street       string   `json:"street,omitempty"`
	Municipality string   `json:"municipality,omitempty"`
}

// ZoneRef is the client-facing summary of a Zone (the full Zone struct
// holds geometry, which we don't ship in API responses).
type ZoneRef struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	City string `json:"city"`
	Kind string `json:"kind"`
}

// PricingInfo enumerates payment-related context for the position.
//
// The Stockholm authorisation model means all four authorised
// operators charge the same base rate. Operators is therefore a
// "which app to use" picker, not a price comparison.
type PricingInfo struct {
	Currency       string           `json:"currency"`
	CurrentRate    *Rate            `json:"current_rate,omitempty"`
	IsFreeNow      bool             `json:"is_free_now"`
	NextRateChange *time.Time       `json:"next_rate_change,omitempty"`
	NextRate       *Rate            `json:"next_rate,omitempty"`
	MaxSessionCost *float64         `json:"max_session_cost,omitempty"`
	Operators      []OperatorOption `json:"operators,omitempty"`
}

// Rate is a price expressed per a time unit.
type Rate struct {
	Amount float64 `json:"amount"`
	Per    string  `json:"per"` // "hour" | "day" | "minute"
}

// OperatorOption is a single payment-app choice presented to the user.
// Deeplink is templated server-side so the client just opens the URL.
type OperatorOption struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	ExternalZoneID string `json:"external_zone_id,omitempty"`
	Deeplink       string `json:"deeplink,omitempty"`
}

// Constraints summarises the practical limits of the current allow
// (or what would-be limits if the verdict were re-asked under different
// conditions). These values come from the rules that fired during
// evaluation; surfacing them avoids the client having to reparse the
// reasons array.
type Constraints struct {
	MaxStayMinutes  int            `json:"max_stay_minutes,omitempty"`
	MaxStayUntil    *time.Time     `json:"max_stay_until,omitempty"`
	PaymentRequired bool           `json:"payment_required"`
	PermitRequired  bool           `json:"permit_required"`
	VehicleClasses  []VehicleClass `json:"vehicle_classes,omitempty"`
}

// Warning is a predictive flag — something that's not blocking right
// now but is likely to become a problem.
type Warning struct {
	Kind          string     `json:"kind"`
	Severity      string     `json:"severity"` // "info" | "warning" | "critical"
	StartsAt      *time.Time `json:"starts_at,omitempty"`
	EndsAt        *time.Time `json:"ends_at,omitempty"`
	DistanceM     *float64   `json:"distance_m,omitempty"`
	HumanReadable string     `json:"human_readable"`
}

// Warning kinds. Centralised so clients can switch on stable strings.
const (
	WarnServicedagUpcoming = "servicedag_upcoming"
	WarnNearJunction       = "near_junction"
	WarnNearCrosswalk      = "near_crosswalk"
	WarnMaxStayExpiring    = "max_stay_expiring"
	WarnPermitExpiringSoon = "permit_expiring_soon"
	WarnEVChargeRequired   = "ev_charge_required"
)

// CostEstimate is computed when the client supplies a desired duration.
// Total is in major currency units (e.g. SEK 75.00, not 7500 öre).
type CostEstimate struct {
	DurationMinutes int           `json:"duration_minutes"`
	Currency        string        `json:"currency"`
	Total           float64       `json:"total"`
	Breakdown       []CostSegment `json:"breakdown,omitempty"`
}

// CostSegment is one constant-rate slice of the total stay.
type CostSegment struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	Rate float64   `json:"rate"`
	Cost float64   `json:"cost"`
}

// Metadata is honest signal about how the verdict was produced.
type Metadata struct {
	EvaluatedAt   time.Time `json:"evaluated_at"`
	EngineVersion string    `json:"engine_version"`
}
