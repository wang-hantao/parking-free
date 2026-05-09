// Package postgres implements store.Store against Postgres + PostGIS.
//
// The package provides:
//   - the required store.Store interface (read + write paths)
//   - the engine.RuleSource interface (subset used at query time)
//   - the optional engine sub-interfaces ZoneSource, TariffSource,
//     OperatorSource (enrichment data path)
//
// It does not implement engine.HazardSource — predictive warnings are
// derived computations and live in a future package on top of the
// store rather than inside it.
package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wang-hantao/parking-free/internal/domain"
	"github.com/wang-hantao/parking-free/internal/engine"
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

// =============================================================================
// engine.RuleSource — read paths used at query time
// =============================================================================

// RulesNearby returns rules whose applies-to geometry comes within
// (radius + per-rule offset) of the position. Four spatial queries
// run (one per target kind) and the results are deduplicated by rule
// ID. Each rule's time windows are then hydrated in a second batch
// query.
func (s *Store) RulesNearby(ctx context.Context, pos domain.Coordinate, radiusM float64) ([]domain.Rule, error) {
	queries := []string{
		sqlRulesByZone,
		sqlRulesByParkingArea,
		sqlRulesByRoadSegment,
		sqlRulesByPOI,
	}

	seen := map[string]int{} // rule ID -> index in `rules`
	var rules []domain.Rule

	for _, q := range queries {
		rs, err := s.fetchRules(ctx, q, pos.Lng, pos.Lat, radiusM)
		if err != nil {
			return nil, fmt.Errorf("postgres: rules-nearby: %w", err)
		}
		for _, r := range rs {
			if _, ok := seen[r.ID]; ok {
				continue
			}
			seen[r.ID] = len(rules)
			rules = append(rules, r)
		}
	}

	if len(rules) == 0 {
		return nil, nil
	}

	// Hydrate time windows.
	ids := make([]string, 0, len(rules))
	for _, r := range rules {
		ids = append(ids, r.ID)
	}
	if err := s.hydrateTimeWindows(ctx, rules, seen, ids); err != nil {
		return nil, fmt.Errorf("postgres: hydrate windows: %w", err)
	}

	return rules, nil
}

func (s *Store) fetchRules(ctx context.Context, query string, lng, lat, radius float64) ([]domain.Rule, error) {
	rows, err := s.pool.Query(ctx, query, lng, lat, radius)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Rule
	for rows.Next() {
		var (
			r         domain.Rule
			maxDurSec *int
			classes   []string
		)
		if err := rows.Scan(
			&r.ID, &r.RegulationID, &r.Kind,
			&maxDurSec, &r.NeedsPayment, &r.NeedsPermit,
			&classes, &r.Priority,
		); err != nil {
			return nil, err
		}
		if maxDurSec != nil {
			r.MaxDuration = time.Duration(*maxDurSec) * time.Second
		}
		for _, c := range classes {
			r.VehicleClasses = append(r.VehicleClasses, domain.VehicleClass(c))
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) hydrateTimeWindows(ctx context.Context, rules []domain.Rule, idx map[string]int, ids []string) error {
	rows, err := s.pool.Query(ctx, sqlTimeWindowsForRules, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			ruleID  string
			tw      domain.TimeWindow
			dayType string
		)
		if err := rows.Scan(&ruleID, &tw.WeekdayMask, &dayType, &tw.StartMin, &tw.EndMin); err != nil {
			return err
		}
		tw.DayType = domain.DayType(dayType)
		if i, ok := idx[ruleID]; ok {
			rules[i].TimeWindows = append(rules[i].TimeWindows, tw)
		}
	}
	return rows.Err()
}

// PermitsForPlate returns currently-active or future-active permits
// for a registration. Past-expired permits are filtered out at the
// SQL level.
func (s *Store) PermitsForPlate(ctx context.Context, plate string) ([]domain.Permit, error) {
	rows, err := s.pool.Query(ctx, sqlPermitsByPlate, plate)
	if err != nil {
		return nil, fmt.Errorf("postgres: permits: %w", err)
	}
	defer rows.Close()

	var out []domain.Permit
	for rows.Next() {
		var (
			p    domain.Permit
			kind string
		)
		if err := rows.Scan(&p.ID, &kind, &p.ZoneID, &p.Plate, &p.HolderRef, &p.ValidFrom, &p.ValidTo); err != nil {
			return nil, err
		}
		p.Kind = domain.PermitKind(kind)
		out = append(out, p)
	}
	return out, rows.Err()
}

// =============================================================================
// engine.ZoneSource — enrichment
// =============================================================================

// ZoneAt returns the smallest zone containing the position (smallest
// because residential zones often overlap broader paid zones, and the
// more specific one is the user-relevant one), plus the closest
// road_segment's street name and municipality.
func (s *Store) ZoneAt(ctx context.Context, pos domain.Coordinate) (*domain.ZoneRef, string, string, error) {
	var z domain.ZoneRef
	err := s.pool.QueryRow(ctx, sqlZoneAt, pos.Lng, pos.Lat).
		Scan(&z.ID, &z.Code, &z.City, &z.Kind)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, "", "", fmt.Errorf("postgres: zone-at: %w", err)
	}
	zonePtr := &z
	if errors.Is(err, pgx.ErrNoRows) {
		zonePtr = nil
	}

	var street, muni string
	err = s.pool.QueryRow(ctx, sqlStreetAt, pos.Lng, pos.Lat).Scan(&street, &muni)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return zonePtr, "", "", fmt.Errorf("postgres: street-at: %w", err)
	}
	return zonePtr, street, muni, nil
}

// =============================================================================
// engine.TariffSource — enrichment
// =============================================================================

// TariffsAt returns one open-ended TariffWindow per tariff active for
// the zone containing the position. v1 limitation: tariffs in the
// current schema are time-of-day-agnostic, so the returned window
// covers `at` to `at + 24h`. A future migration adding weekday/time
// columns to `tariff` will let this method return real per-window
// pricing without changing its signature.
func (s *Store) TariffsAt(ctx context.Context, pos domain.Coordinate, at time.Time) ([]engine.TariffWindow, error) {
	rows, err := s.pool.Query(ctx, sqlTariffsAt, pos.Lng, pos.Lat)
	if err != nil {
		return nil, fmt.Errorf("postgres: tariffs-at: %w", err)
	}
	defer rows.Close()

	var out []engine.TariffWindow
	for rows.Next() {
		var (
			currency string
			rate     float64
			unitSec  int
			maxCost  *float64
		)
		if err := rows.Scan(&currency, &rate, &unitSec, &maxCost); err != nil {
			return nil, err
		}
		tw := engine.TariffWindow{
			From:     at,
			To:       at.Add(24 * time.Hour),
			Amount:   rate,
			Per:      perLabel(time.Duration(unitSec) * time.Second),
			Currency: currency,
		}
		if maxCost != nil {
			tw.MaxSession = maxCost
		}
		out = append(out, tw)
	}
	return out, rows.Err()
}

func perLabel(d time.Duration) string {
	switch {
	case d <= time.Minute:
		return "minute"
	case d >= 24*time.Hour:
		return "day"
	default:
		return "hour"
	}
}

// =============================================================================
// engine.OperatorSource — enrichment
// =============================================================================

// OperatorsForZone returns the operators that map to a city zone, with
// their deeplink templates expanded for the given plate. The plate is
// substituted into the {plate} placeholder in deeplink_template.
func (s *Store) OperatorsForZone(ctx context.Context, zoneID, plate string) ([]domain.OperatorOption, error) {
	rows, err := s.pool.Query(ctx, sqlOperatorsForZone, zoneID)
	if err != nil {
		return nil, fmt.Errorf("postgres: operators: %w", err)
	}
	defer rows.Close()

	var out []domain.OperatorOption
	for rows.Next() {
		var op domain.OperatorOption
		var deeplinkTpl string
		if err := rows.Scan(&op.ID, &op.Name, &op.ExternalZoneID, &deeplinkTpl); err != nil {
			return nil, err
		}
		if deeplinkTpl != "" {
			op.Deeplink = expandDeeplink(deeplinkTpl, op.ExternalZoneID, plate)
		}
		out = append(out, op)
	}
	return out, rows.Err()
}

// expandDeeplink does a tiny string substitution. Kept simple — text/template
// is overkill and slow at this volume.
func expandDeeplink(tpl, externalZoneID, plate string) string {
	out := tpl
	out = replaceAll(out, "{external}", externalZoneID)
	out = replaceAll(out, "{plate}", plate)
	return out
}

func replaceAll(s, old, new string) string {
	if old == "" {
		return s
	}
	out := ""
	for {
		i := indexOf(s, old)
		if i < 0 {
			return out + s
		}
		out += s[:i] + new
		s = s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// =============================================================================
// store.Store — write paths used by the ingester
// =============================================================================

// UpsertRegulations idempotently writes regulations, keyed on
// (source_system, source_reference). Returns nil on no-op input.
func (s *Store) UpsertRegulations(ctx context.Context, regs []domain.Regulation) error {
	if len(regs) == 0 {
		return nil
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		for _, r := range regs {
			id := r.ID
			if id == "" {
				id = newUUID()
			}
			var effectiveTo any
			if !r.EffectiveTo.IsZero() {
				effectiveTo = r.EffectiveTo
			}
			lang := r.Language
			if lang == "" {
				lang = "sv-SE"
			}
			var returnedID string
			if err := tx.QueryRow(ctx, sqlUpsertRegulation,
				id, r.Source.System, r.Source.Reference, r.DecisionAuthority,
				lang, r.EffectiveFrom, effectiveTo,
			).Scan(&returnedID); err != nil {
				return fmt.Errorf("upsert regulation %s/%s: %w", r.Source.System, r.Source.Reference, err)
			}
		}
		return nil
	})
}

// UpsertRules destructively replaces all rules for the regulation IDs
// represented in `rules`: existing rules (and their time-windows /
// applies-to children, via ON DELETE CASCADE) are removed, then the
// supplied rules are inserted.
//
// IMPORTANT: callers must pass the COMPLETE current set of rules for
// each affected regulation, not a partial update. LTF-Tolken returns
// full snapshots; partial deltas are not supported.
func (s *Store) UpsertRules(ctx context.Context, rules []domain.Rule) error {
	if len(rules) == 0 {
		return nil
	}

	byReg := map[string][]domain.Rule{}
	for _, r := range rules {
		if r.RegulationID == "" {
			return errors.New("rule missing regulation_id")
		}
		byReg[r.RegulationID] = append(byReg[r.RegulationID], r)
	}

	return s.inTx(ctx, func(tx pgx.Tx) error {
		for regID, rs := range byReg {
			if _, err := tx.Exec(ctx, sqlDeleteRulesForRegulation, regID); err != nil {
				return fmt.Errorf("delete rules for reg %s: %w", regID, err)
			}
			for _, r := range rs {
				if r.ID == "" {
					r.ID = newUUID()
				}
				classes := make([]string, 0, len(r.VehicleClasses))
				for _, c := range r.VehicleClasses {
					classes = append(classes, string(c))
				}
				var maxDurSec any
				if r.MaxDuration > 0 {
					maxDurSec = int(r.MaxDuration / time.Second)
				}
				if _, err := tx.Exec(ctx, sqlInsertRule,
					r.ID, r.RegulationID, string(r.Kind), maxDurSec,
					r.NeedsPayment, r.NeedsPermit, classes, r.Priority,
				); err != nil {
					return fmt.Errorf("insert rule %s: %w", r.ID, err)
				}
				for _, w := range r.TimeWindows {
					var dayType any
					if w.DayType != "" {
						dayType = string(w.DayType)
					}
					var dateFrom, dateTo any
					if w.DateFrom != "" {
						dateFrom = w.DateFrom
					}
					if w.DateTo != "" {
						dateTo = w.DateTo
					}
					if _, err := tx.Exec(ctx, sqlInsertTimeWindow,
						r.ID, w.WeekdayMask, dayType, w.StartMin, w.EndMin, dateFrom, dateTo,
					); err != nil {
						return fmt.Errorf("insert time window for rule %s: %w", r.ID, err)
					}
				}
				for _, a := range r.AppliesTo {
					if _, err := tx.Exec(ctx, sqlInsertAppliesTo,
						r.ID, string(a.Kind), a.TargetID, a.OffsetFromMeters, a.OffsetToMeters,
					); err != nil {
						return fmt.Errorf("insert applies_to for rule %s: %w", r.ID, err)
					}
				}
			}
		}
		return nil
	})
}

// UpsertPermits writes permits, idempotent on permit ID.
func (s *Store) UpsertPermits(ctx context.Context, permits []domain.Permit) error {
	if len(permits) == 0 {
		return nil
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		for _, p := range permits {
			id := p.ID
			if id == "" {
				id = newUUID()
			}
			var zoneID, holderRef any
			if p.ZoneID != "" {
				zoneID = p.ZoneID
			}
			if p.HolderRef != "" {
				holderRef = p.HolderRef
			}
			if _, err := tx.Exec(ctx, sqlUpsertPermit,
				id, string(p.Kind), zoneID, p.Plate, holderRef, p.ValidFrom, p.ValidTo,
			); err != nil {
				return fmt.Errorf("upsert permit %s: %w", p.Plate, err)
			}
		}
		return nil
	})
}

// =============================================================================
// helpers
// =============================================================================

// inTx runs fn inside a transaction, committing on success and rolling
// back on any error.
func (s *Store) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // safe after Commit (no-op)
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// newUUID returns a freshly generated v4 UUID. Avoids pulling in
// google/uuid for a single use; the layout is per RFC 4122 §4.4.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
