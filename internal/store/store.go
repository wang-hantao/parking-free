// Package store defines the persistence interface used by the engine
// and HTTP layer. Implementations live in subpackages (postgres for
// production, in-memory for tests).
package store

import (
	"context"

	"github.com/wang-hantao/parking-free/internal/domain"
)

// Store is the unified persistence contract.
type Store interface {
	// Read paths used by the engine.
	RulesNearby(ctx context.Context, pos domain.Coordinate, radiusM float64) ([]domain.Rule, error)
	PermitsForPlate(ctx context.Context, plate string) ([]domain.Permit, error)

	// Write paths used by the ingester.
	UpsertRegulations(ctx context.Context, regs []domain.Regulation) error
	UpsertRules(ctx context.Context, rules []domain.Rule) error
	UpsertPermits(ctx context.Context, permits []domain.Permit) error

	// Lifecycle.
	Close() error
}
