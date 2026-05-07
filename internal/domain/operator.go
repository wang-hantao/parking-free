package domain

import "time"

// OperatorKind distinguishes the legal regime under which an operator
// issues fines and collects fees.
type OperatorKind string

const (
	OperatorMunicipal OperatorKind = "municipal" // gatumark, Felparkeringsavgift
	OperatorPrivate   OperatorKind = "private"   // tomtmark, Kontrollavgift
	OperatorMVNE      OperatorKind = "mvne"      // wholesale platform
)

// Operator is a parking-payment provider or land-management entity.
// In Stockholm: EasyPark, Parkster, Mobill, ePARK are OperatorMunicipal
// (authorised by the city). Aimo, Apcoa, etc. are OperatorPrivate.
type Operator struct {
	ID   string       `json:"id"`
	Name string       `json:"name"`
	Kind OperatorKind `json:"kind"`
}

// OperatorZone maps an operator's internal zone identifier to a
// city-defined Zone. Many-to-many in the worst case (operator zones
// can split or combine municipal zones).
type OperatorZone struct {
	OperatorID       string `json:"operator_id"`
	ExternalZoneID   string `json:"external_zone_id"`
	MapsToZoneID     string `json:"maps_to_zone_id"`
	DeeplinkTemplate string `json:"deeplink_template"` // e.g. "easypark://start?zone={external}&plate={plate}"
}

// Tariff is a price specification for an operator zone in a time window.
type Tariff struct {
	ID             string        `json:"id"`
	OperatorZoneID string        `json:"operator_zone_id"`
	Currency       string        `json:"currency"` // ISO 4217, "SEK"
	RatePerUnit    float64       `json:"rate_per_unit"`
	TimeUnit       time.Duration `json:"time_unit"`
	MaxSessionCost float64       `json:"max_session_cost,omitempty"`
}

// PermitKind enumerates the permit categories relevant for parking
// permission decisions.
type PermitKind string

const (
	PermitResidential PermitKind = "residential"
	PermitDisabled    PermitKind = "disabled"
	PermitElectric    PermitKind = "electric"
	PermitCarpool     PermitKind = "carpool"
	PermitGuest       PermitKind = "guest"
	PermitNyttoA      PermitKind = "nytto_a"
	PermitNyttoB      PermitKind = "nytto_b"
)

// Permit is a permission tied to a vehicle plate, a zone, and a date
// range. It is the dominant factor in residential-parking eligibility.
type Permit struct {
	ID        string     `json:"id"`
	Kind      PermitKind `json:"kind"`
	ZoneID    string     `json:"zone_id,omitempty"` // empty = city-wide
	Plate     string     `json:"plate"`
	HolderRef string     `json:"holder_ref,omitempty"`
	ValidFrom time.Time  `json:"valid_from"`
	ValidTo   time.Time  `json:"valid_to"`
}

// IsValidAt reports whether the permit covers the given moment.
func (p Permit) IsValidAt(t time.Time) bool {
	return !t.Before(p.ValidFrom) && t.Before(p.ValidTo)
}
