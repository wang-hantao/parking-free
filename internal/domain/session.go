package domain

import "time"

// ParkingSession represents an in-progress or historical parking
// engagement by a vehicle.
type ParkingSession struct {
	ID                string     `json:"id"`
	Plate             string     `json:"plate"`
	StartedAt         time.Time  `json:"started_at"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`
	Position          Coordinate `json:"position"`
	ZoneID            string     `json:"zone_id,omitempty"`
	OperatorID        string     `json:"operator_id,omitempty"`
	ExternalSessionID string     `json:"external_session_id,omitempty"`
	CostMinor         int        `json:"cost_minor,omitempty"` // currency minor units (öre)
	Status            string     `json:"status"`               // "active" | "ended" | "expired"
}

// Verdict is the answer to "is parking allowed here right now?". It is
// what the engine returns and what the HTTP API exposes.
//
// The first block of fields (Allowed..NeedsAction) is the core verdict.
// The remaining fields are optional enrichment populated by the engine
// when an enriching source is available; they are all omitempty so a
// minimally-configured server still returns a valid, smaller payload.
type Verdict struct {
	Allowed     bool      `json:"allowed"`
	ExpiresAt   time.Time `json:"expires_at"`             // earliest moment the verdict could change
	Reasons     []Reason  `json:"reasons"`                // contributing rules, supportive and contrary
	NeedsAction []string  `json:"needs_action,omitempty"` // e.g. ["pay_via_app", "show_disc"]

	// Enrichment.
	Location      *LocationInfo `json:"location,omitempty"`
	Pricing       *PricingInfo  `json:"pricing,omitempty"`
	Constraints   *Constraints  `json:"constraints,omitempty"`
	Warnings      []Warning     `json:"warnings,omitempty"`
	EstimatedCost *CostEstimate `json:"estimated_cost,omitempty"`
	Metadata      *Metadata     `json:"metadata,omitempty"`
}

// Reason links a Verdict back to the rules that produced it. The
// presence of every contributing rule (with its source reference) is
// what makes a Verdict defensible — both for end-user explanation and
// for generating dispute letters.
type Reason struct {
	RuleID        string   `json:"rule_id"`
	RegulationID  string   `json:"regulation_id"`
	Source        Source   `json:"source"`
	Disposition   RuleKind `json:"disposition"`
	HumanReadable string   `json:"human_readable"`
	Supports      bool     `json:"supports"` // true if this reason supports the verdict
}

// FineEvent is recorded when a parking fine is issued for or against a
// session. Used for analytics and dispute automation.
type FineEvent struct {
	ID         string    `json:"id"`
	SessionID  string    `json:"session_id,omitempty"`
	TicketNo   string    `json:"ticket_no"`
	Issuer     string    `json:"issuer"`      // "stockholm.stad" | private operator name
	IssuerKind string    `json:"issuer_kind"` // "yellow" | "white"
	AmountSEK  int       `json:"amount_sek"`
	ReasonCode string    `json:"reason_code,omitempty"`
	IssuedAt   time.Time `json:"issued_at"`
	PhotoURLs  []string  `json:"photo_urls,omitempty"`
}
