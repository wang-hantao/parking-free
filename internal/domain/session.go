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
	Summary     string    `json:"summary,omitempty"`      // plain-English explanation of the verdict
	ExpiresAt   time.Time `json:"expires_at"`             // earliest moment the verdict could change
	Reasons     []Reason  `json:"reasons"`                // contributing rules, supportive and contrary
	NeedsAction []string  `json:"needs_action,omitempty"` // e.g. ["pay_via_app", "show_disc"]

	// DataConfidence reports how grounded the verdict is in our
	// ingested data. Empty when omitted. Values:
	//
	//   "high" — one or more rules apply at this location and the
	//   verdict is computed from them. The summary reflects the rule
	//   outcome.
	//
	//   "low"  — we have no rules at this location. The Allowed flag
	//   defaults to true (Sweden's legal default is "allowed unless
	//   prohibited"), but the user should verify against on-street
	//   signage. Set when the rule set is empty AND no nearby
	//   road_segment exists in our data — i.e. genuine coverage gap,
	//   not just a quiet stretch of curb.
	DataConfidence string `json:"data_confidence,omitempty"`

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
//
// Supports vs Blocks:
//   - Supports = true when this rule is consistent with the verdict
//     outcome (a Forbid in a "not allowed" verdict, or a satisfiable
//     Allow in an "allowed" verdict).
//   - Blocks   = true when this specific rule is a reason the verdict
//     is "not allowed". For a Forbid: always true if the verdict is
//     false. For an unsatisfied Allow (e.g. NeedsPermit and the user
//     has no permit): true only when no other rule satisfies the user
//     (otherwise the unsatisfied rule is merely informational, not
//     blocking — the user could comply with the other rule).
type Reason struct {
	RuleID        string   `json:"rule_id"`
	RegulationID  string   `json:"regulation_id"`
	Source        Source   `json:"source"`
	Disposition   RuleKind `json:"disposition"`
	HumanReadable string   `json:"human_readable"`
	Supports      bool     `json:"supports"`
	Blocks        bool     `json:"blocks,omitempty"`

	// Superseded is set when this Allow rule is overridden by a
	// more-specific Allow rule at the same location (e.g. a disabled
	// bay carving into a general paid-parking strip). Superseded
	// rules are included in the Reasons array for traceability but
	// don't contribute to the Allowed decision or NeedsAction.
	Superseded bool `json:"superseded,omitempty"`
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
