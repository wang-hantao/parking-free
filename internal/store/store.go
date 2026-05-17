// Package store defines the persistence interface used by the engine
// and HTTP layer. Implementations live in subpackages (postgres for
// production, in-memory for tests).
package store

import (
	"context"

	"github.com/wang-hantao/parking-free/internal/domain"
)

// Store is the unified persistence contract.
//
// Upsert methods that need to surface generated UUIDs return a map
// keyed by the input record's Source.Reference. The ingester uses
// these maps to resolve cross-record references — most importantly,
// to turn a Rule's geometry-by-source-ref into geometry-by-UUID
// before persisting the rule.
type Store interface {
	// Read paths.
	RulesNearby(ctx context.Context, pos domain.Coordinate, radiusM float64) ([]domain.Rule, error)
	PermitsForPlate(ctx context.Context, plate string) ([]domain.Permit, error)

	// Write paths. Returned maps key source_reference -> UUID.
	UpsertRoadSegments(ctx context.Context, segs []domain.RoadSegment) (map[string]string, error)
	UpsertRegulations(ctx context.Context, regs []domain.Regulation) (map[string]string, error)
	UpsertRules(ctx context.Context, rules []domain.Rule) error
	UpsertPermits(ctx context.Context, permits []domain.Permit) error

	// Lifecycle.
	Close() error
}
