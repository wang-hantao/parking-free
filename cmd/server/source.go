package main

import (
	"context"

	"github.com/wang-hantao/parking-free/internal/domain"
)

// emptyRuleSource is a placeholder RuleSource that returns no rules
// and no permits. Used until the Postgres store is wired in. Lets the
// HTTP server start cleanly so /healthz and /allowed are exercisable
// against a known-empty regulation graph.
type emptyRuleSource struct{}

func (emptyRuleSource) RulesNearby(_ context.Context, _ domain.Coordinate, _ float64) ([]domain.Rule, error) {
	return nil, nil
}

func (emptyRuleSource) PermitsForPlate(_ context.Context, _ string) ([]domain.Permit, error) {
	return nil, nil
}
