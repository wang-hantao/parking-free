// Package postgres implements store.Store against Postgres + PostGIS.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wang-hantao/parking-free/internal/domain"
)

// Store is a PostgreSQL-backed store.Store.
type Store struct {
	pool *pgxpool.Pool
}

// Open creates a Store from a Postgres DSN. The caller must Close.
func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases all pool connections.
func (s *Store) Close() error {
	if s == nil || s.pool == nil {
		return nil
	}
	s.pool.Close()
	return nil
}

// RulesNearby returns rules whose applies-to geometry intersects a
// buffered point. The buffer captures both the search radius and the
// per-rule offset (e.g. 10m before junctions).
//
// Implementation note: the production query uses a PostGIS
// ST_DWithin against the union of LineString/Polygon geometries plus
// per-rule offsets. The skeleton below returns ErrNotImplemented; fill
// in once migrations are loaded with real data.
func (s *Store) RulesNearby(ctx context.Context, pos domain.Coordinate, radiusM float64) ([]domain.Rule, error) {
	_ = ctx
	_ = pos
	_ = radiusM
	return nil, ErrNotImplemented
}

// PermitsForPlate returns active and recent permits for a plate.
func (s *Store) PermitsForPlate(ctx context.Context, plate string) ([]domain.Permit, error) {
	_ = ctx
	_ = plate
	return nil, ErrNotImplemented
}

// UpsertRegulations writes regulations, idempotent on (source, ref).
func (s *Store) UpsertRegulations(ctx context.Context, regs []domain.Regulation) error {
	_ = ctx
	_ = regs
	return ErrNotImplemented
}

// UpsertRules writes rules and their applies-to/time-window children.
func (s *Store) UpsertRules(ctx context.Context, rules []domain.Rule) error {
	_ = ctx
	_ = rules
	return ErrNotImplemented
}

// UpsertPermits writes permits, idempotent on plate + valid_from + kind + zone.
func (s *Store) UpsertPermits(ctx context.Context, permits []domain.Permit) error {
	_ = ctx
	_ = permits
	return ErrNotImplemented
}

// ErrNotImplemented is returned by methods whose query bodies are
// pending. See migrations/ for the table shapes the real
// implementations target.
var ErrNotImplemented = errors.New("postgres: not implemented yet")
