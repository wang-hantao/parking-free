package stockholm

import (
	"github.com/wang-hantao/parking-free/internal/domain"
)

// Transform converts LTF-Tolken raw JSON for one föreskrift into
// domain regulations and rules.
//
// The exact LTF-Tolken response schema is not published in the public
// help pages and must be derived from a real authenticated response.
// This function intentionally remains a stub until that derivation is
// done; the caller can recognise the unimplemented case by the
// returned ErrSchemaPending sentinel.
//
// Implementation guidance for whoever wires this up:
//
//   - Servicedagar: each feature is a line/polygon with a weekday and
//     time window (e.g. Tue 00:00–06:00). Map to a single Rule of kind
//     RuleForbid with one TimeWindow.
//
//   - Ptillaten: features carry permission times, max-stay, and
//     payment requirements. Map to Rule of kind RuleAllow or
//     RuleRestrict; populate NeedsPayment and MaxDuration.
//
//   - Pbuss / Plastbil / Pmotorcykel: features describe vehicle-class
//     restricted spots. Map to a Rule of kind RuleAllow with
//     VehicleClasses set, plus an implicit RuleForbid for all other
//     classes (priority below the explicit Allow).
//
//   - Prorelsehindrad: like the class-restricted ones but with
//     NeedsPermit=true and a permit kind of Disabled.
//
// Provenance: every produced Regulation must carry Source.System =
// "stockholm.ltf-tolken" and Source.Reference = the föreskriftsnummer
// from the upstream feature.
func Transform(f Foreskrift, raw []byte) ([]domain.Regulation, []domain.Rule, error) {
	return nil, nil, ErrSchemaPending
}

// ErrSchemaPending indicates that Transform has not yet been
// implemented for live LTF-Tolken responses. It is returned until the
// schema is derived from real API output (which requires an API key).
var ErrSchemaPending = errSchemaPending{}

type errSchemaPending struct{}

func (errSchemaPending) Error() string {
	return "stockholm: LTF-Tolken response transform not yet implemented; derive schema from a real authenticated response and fill in transform.go"
}
