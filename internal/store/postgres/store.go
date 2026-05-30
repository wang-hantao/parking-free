// Package postgres implements store.Store against Postgres + PostGIS.
//
// The package provides:
//   - the required store.Store interface (read + write paths)
//   - the engine.RuleSource interface (subset used at query time)
//   - the optional engine sub-interfaces ZoneSource and OperatorSource
//     (enrichment data path)
//
// Pricing is no longer interface-driven on the store: each rule
// carries a TariffClassCode the engine resolves against an in-process
// registry. The `tariff` table remains in the schema but is unused by
// the read path.
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

// Strict-mode road-segment resolution uses a two-step anchor approach
// rather than a flat "all rules within X meters" radius.
//
// Step 1: find the nearest rule-bearing road_segment to the query
// point, within strictAnchorSearchM meters. Acts as a sanity bound —
// if the user is more than ~30m from any rule-bearing segment, they
// aren't at a parking spot (they're inside a building, in a park, or
// at unmapped infrastructure) so we return nothing.
//
// Step 2: return all rules from segments within strictCoLocatedM
// meters of that anchor segment. Captures multiple overlapping
// föreskrifter that sit on the same physical curb in Stockholm
// (ptillaten + servicedagar + sometimes prorelsehindrad or pbuss
// when a reserved-class bay is carved into a general paid strip).
//
// Why this beats a flat radius:
//
//   - Phone GPS in dense urban environments has ±5-10m horizontal
//     error (urban canyon multipath). Stockholm road_segment
//     geometries trace the road center-line, not the curb, so a
//     user standing at the curb of a 12-15m wide street is already
//     6-7m from the line. A flat 5m radius silently drops these
//     valid hits (observed at Olof Palmes gata / Kungsbron).
//
//   - A flat radius wide enough to catch the offset case (~12-15m)
//     starts bleeding across normal-width streets. The anchor
//     approach naturally hugs the one road the user is at,
//     regardless of GPS offset.
//
//   - Co-located rules still surface: a 2m radius around the anchor
//     captures the reserved-class bay that overlaps the general
//     paid strip without admitting unrelated rules from across the
//     street.
const (
	strictAnchorSearchM = 30.0
	strictCoLocatedM    = 2.0
)

// RulesAt is the strict-mode counterpart to RulesNearby. It returns
// only rules that legally apply to the exact position:
//   - road_segment: two-step anchor — nearest rule-bearing segment
//     within strictAnchorSearchM, then all co-located rules within
//     strictCoLocatedM of the anchor. See comment on
//     sqlRulesByRoadSegmentStrict for the rationale.
//   - zone: ST_Contains (point inside the polygon)
//   - parking_area: ST_Contains
//   - POI: rules within their declared offset extent (no extra
//     search radius)
//
// Implements engine.StrictRuleSource. The engine calls this only when
// the client requested Mode=strict.
func (s *Store) RulesAt(ctx context.Context, pos domain.Coordinate) ([]domain.Rule, error) {
	// Zone, parking-area, and POI queries share a (lng, lat, radius)
	// shape — passing radius=0 leaves only the per-rule offset extent
	// contributing to distance (true containment for polygons,
	// offset-only for POIs). The strict road-segment query is
	// structurally different: it takes (lng, lat, anchor_bound,
	// co_located_radius) for the two-step anchor approach.
	type fetch struct {
		sql  string
		args []any
	}
	fetches := []fetch{
		{sqlRulesByZone, []any{pos.Lng, pos.Lat, 0.0}},
		{sqlRulesByParkingArea, []any{pos.Lng, pos.Lat, 0.0}},
		{sqlRulesByRoadSegmentStrict, []any{pos.Lng, pos.Lat, strictAnchorSearchM, strictCoLocatedM}},
		{sqlRulesByPOI, []any{pos.Lng, pos.Lat, 0.0}},
	}

	seen := map[string]int{}
	var rules []domain.Rule
	for _, f := range fetches {
		rs, err := s.fetchRules(ctx, f.sql, f.args...)
		if err != nil {
			return nil, fmt.Errorf("postgres: rules-at: %w", err)
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

	ids := make([]string, 0, len(rules))
	for _, r := range rules {
		ids = append(ids, r.ID)
	}
	if err := s.hydrateTimeWindows(ctx, rules, seen, ids); err != nil {
		return nil, fmt.Errorf("postgres: hydrate windows: %w", err)
	}
	return rules, nil
}

func (s *Store) fetchRules(ctx context.Context, query string, args ...any) ([]domain.Rule, error) {
	rows, err := s.pool.Query(ctx, query, args...)
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
		var permitKindStr string
		if err := rows.Scan(
			&r.ID, &r.RegulationID, &r.Kind,
			&maxDurSec, &r.NeedsPayment, &r.NeedsPermit,
			&classes, &r.Priority,
			&r.Source.System, &r.Source.Reference,
			&r.TariffClassCode,
			&permitKindStr,
		); err != nil {
			return nil, err
		}
		r.RequiredPermitKind = domain.PermitKind(permitKindStr)
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
		if err := rows.Scan(&ruleID, &tw.WeekdayMask, &dayType, &tw.StartMin, &tw.EndMin,
			&tw.StartMonth, &tw.StartDay, &tw.EndMonth, &tw.EndDay); err != nil {
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

// CityOperators returns operators that serve an entire municipality
// (no zone refinement), with their default landing URL set as the
// Deeplink. Used as a fallback when OperatorsForZone returns empty
// — payment is required at this location but we don't know which
// zone it's in.
//
// `plate` is passed through expandDeeplink for templates that
// happen to include a {plate} placeholder; Stockholm's seeded
// landing URLs don't, but the substitution stays cheap and makes
// future templates trivial to add.
func (s *Store) CityOperators(ctx context.Context, municipality, plate string) ([]domain.OperatorOption, error) {
	if municipality == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, sqlCityOperators, municipality)
	if err != nil {
		return nil, fmt.Errorf("postgres: city operators: %w", err)
	}
	defer rows.Close()

	var out []domain.OperatorOption
	for rows.Next() {
		var op domain.OperatorOption
		var deeplinkTpl string
		if err := rows.Scan(&op.ID, &op.Name, &deeplinkTpl); err != nil {
			return nil, err
		}
		if deeplinkTpl != "" {
			op.Deeplink = expandDeeplink(deeplinkTpl, "", plate)
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
// (source_system, source_reference). Returns a map source_reference ->
// generated UUID for cross-record resolution by callers.
func (s *Store) UpsertRegulations(ctx context.Context, regs []domain.Regulation) (map[string]string, error) {
	ids := make(map[string]string, len(regs))
	if len(regs) == 0 {
		return ids, nil
	}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
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
			var returnedID, returnedRef string
			if err := tx.QueryRow(ctx, sqlUpsertRegulation,
				id, r.Source.System, r.Source.Reference, r.DecisionAuthority,
				lang, r.EffectiveFrom, effectiveTo,
			).Scan(&returnedID, &returnedRef); err != nil {
				return fmt.Errorf("upsert regulation %s/%s: %w", r.Source.System, r.Source.Reference, err)
			}
			ids[returnedRef] = returnedID
		}
		return nil
	})
	return ids, err
}

// UpsertRoadSegments writes road geometries from an external source,
// idempotent on (source_system, source_reference). Returns a map
// source_reference -> generated UUID so callers can resolve Rule
// AppliesTo targets.
func (s *Store) UpsertRoadSegments(ctx context.Context, segs []domain.RoadSegment) (map[string]string, error) {
	ids := make(map[string]string, len(segs))
	if len(segs) == 0 {
		return ids, nil
	}
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		for _, seg := range segs {
			if seg.GeometryWKT == "" {
				return fmt.Errorf("road segment %s: missing GeometryWKT", seg.Source.Reference)
			}
			if seg.Source.System == "" || seg.Source.Reference == "" {
				return fmt.Errorf("road segment: Source.System and Source.Reference are required")
			}
			var street, direction any
			if seg.StreetName != "" {
				street = seg.StreetName
			}
			if seg.Direction != "" {
				direction = seg.Direction
			}
			muni := seg.Municipality
			if muni == "" {
				muni = "Unknown"
			}
			var returnedID, returnedRef string
			if err := tx.QueryRow(ctx, sqlUpsertRoadSegment,
				street, muni, direction, seg.Source.System, seg.Source.Reference, seg.GeometryWKT,
			).Scan(&returnedID, &returnedRef); err != nil {
				return fmt.Errorf("upsert road segment %s/%s: %w", seg.Source.System, seg.Source.Reference, err)
			}
			ids[returnedRef] = returnedID
		}
		return nil
	})
	return ids, err
}

// PruneOrphanRoadSegments deletes road_segment rows under a given
// source_system + reference prefix that have no rule_applies_to
// entries pointing at them. Returns the count of deleted rows.
//
// Intended for the ingester to call after each föreskrift's full
// upsert cycle, scoping by prefix (e.g. system="stockholm.ltf-tolken",
// prefix="ptillaten/"). Keeps the table consistent with Stockholm's
// LTF data evolution — features that disappear between snapshots
// have their rules deleted by UpsertRules' destructive replace, and
// this method then drops the now-orphan segment rows.
//
// Safe because the destructive UpsertRules has already run by the
// time this is called: any segment with no rule_applies_to entry
// truly belongs to a removed/renumbered feature.
func (s *Store) PruneOrphanRoadSegments(ctx context.Context, sourceSystem, prefix string) (int64, error) {
	if sourceSystem == "" || prefix == "" {
		return 0, fmt.Errorf("PruneOrphanRoadSegments: sourceSystem and prefix are required")
	}
	// $2 is a LIKE pattern; the caller passes "ptillaten/" and we
	// append %. Keeps the call sites readable without ambiguity over
	// who appends the wildcard.
	tag, err := s.pool.Exec(ctx, sqlPruneOrphanRoadSegments, sourceSystem, prefix+"%")
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PruneAllOrphanRoadSegments is the unscoped variant: deletes every
// orphan road_segment under sourceSystem regardless of reference
// prefix. Used by the `ingester cleanup` command to wipe orphans
// accumulated before per-ingest prune logic was introduced. For
// routine maintenance the per-prefix PruneOrphanRoadSegments called
// during ingest is sufficient.
func (s *Store) PruneAllOrphanRoadSegments(ctx context.Context, sourceSystem string) (int64, error) {
	if sourceSystem == "" {
		return 0, fmt.Errorf("PruneAllOrphanRoadSegments: sourceSystem is required")
	}
	tag, err := s.pool.Exec(ctx, sqlPruneAllOrphanRoadSegments, sourceSystem)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
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
					r.TariffClassCode, string(r.RequiredPermitKind),
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
						w.StartMonth, w.StartDay, w.EndMonth, w.EndDay,
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
