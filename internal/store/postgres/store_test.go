// Integration tests for the postgres store.
//
// These tests require a live Postgres + PostGIS instance and the
// project's migrations applied. They are skipped automatically when
// POSTGRES_TEST_DSN is not set.
//
// Local run:
//
//	make docker-up && make migrate
//	export POSTGRES_TEST_DSN='postgres://parking:parking@localhost:5432/parking?sslmode=disable'
//	go test ./internal/store/postgres/...
//
// The tests are destructive: they truncate the relevant tables before
// running so they're hermetic. Don't point POSTGRES_TEST_DSN at a
// database with real data.
package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/wang-hantao/parking-free/internal/domain"
)

func openTestStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN not set; skipping postgres integration tests")
	}
	ctx := context.Background()
	st, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	truncate(t, st)
	return st, ctx
}

// truncate clears the tables touched by these tests in dependency
// order. Cascade does the heavy lifting via FK constraints.
func truncate(t *testing.T, st *Store) {
	t.Helper()
	stmts := []string{
		`TRUNCATE TABLE permit, parking_session, fine_event, notification, rule_evaluation,
		               operator_zone, tariff, operator,
		               rule_applies_to, rule_time_window, rule, regulation,
		               road_segment, parking_area, zone, point_of_interest CASCADE`,
	}
	for _, s := range stmts {
		if _, err := st.pool.Exec(context.Background(), s); err != nil {
			t.Fatalf("truncate: %v", err)
		}
	}
}

// seedZone inserts a small square zone around a point at (lng,lat) and
// returns its UUID.
func seedZone(t *testing.T, st *Store, lng, lat float64, code, kind string) string {
	t.Helper()
	const q = `
		INSERT INTO zone (city, code, kind, source_system, source_reference, geom)
		VALUES ('Stockholm', $1, $2, 'test', $1,
			ST_Multi(ST_GeomFromText(
				'POLYGON((' ||
					($3 - 0.001) || ' ' || ($4 - 0.001) || ',' ||
					($3 + 0.001) || ' ' || ($4 - 0.001) || ',' ||
					($3 + 0.001) || ' ' || ($4 + 0.001) || ',' ||
					($3 - 0.001) || ' ' || ($4 + 0.001) || ',' ||
					($3 - 0.001) || ' ' || ($4 - 0.001) ||
				'))', 4326)))
		RETURNING id::text`
	var id string
	if err := st.pool.QueryRow(context.Background(), q, code, kind, lng, lat).Scan(&id); err != nil {
		t.Fatalf("seed zone: %v", err)
	}
	return id
}

// seedRoadSegment inserts a tiny line near (lng,lat).
func seedRoadSegment(t *testing.T, st *Store, lng, lat float64, name string) {
	t.Helper()
	const q = `
		INSERT INTO road_segment (street_name, municipality, source_system, source_reference, geom)
		VALUES ($1, 'Stockholm', 'test', $1,
			ST_GeomFromText('LINESTRING(' || ($2 - 0.0005) || ' ' || $3 || ',' || ($2 + 0.0005) || ' ' || $3 || ')', 4326))`
	if _, err := st.pool.Exec(context.Background(), q, name, lng, lat); err != nil {
		t.Fatalf("seed road: %v", err)
	}
}

func TestUpsertAndQuery_RoundTrip(t *testing.T) {
	st, ctx := openTestStore(t)

	zoneID := seedZone(t, st, 18.0531, 59.3278, "Z14", "paid")
	seedRoadSegment(t, st, 18.0531, 59.3278, "Odengatan")

	// Upsert a regulation + a rule scoped to the zone.
	reg := domain.Regulation{
		Source:            domain.Source{System: "test", Reference: "reg-001"},
		DecisionAuthority: "Stockholms stad",
		Language:          "sv-SE",
		EffectiveFrom:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := st.UpsertRegulations(ctx, []domain.Regulation{reg}); err != nil {
		t.Fatalf("upsert reg: %v", err)
	}

	// Read back the regulation ID via a quick query.
	var regID string
	if err := st.pool.QueryRow(ctx,
		`SELECT id::text FROM regulation WHERE source_system = $1 AND source_reference = $2`,
		"test", "reg-001",
	).Scan(&regID); err != nil {
		t.Fatalf("read regulation: %v", err)
	}

	rule := domain.Rule{
		RegulationID: regID,
		Kind:         domain.RuleAllow,
		NeedsPayment: true,
		MaxDuration:  2 * time.Hour,
		Priority:     5,
		TimeWindows: []domain.TimeWindow{
			{WeekdayMask: 0b01111110, StartMin: 540, EndMin: 1080}, // Mon-Sat 09:00-18:00
		},
		AppliesTo: []domain.AppliesTo{
			{Kind: domain.TargetZone, TargetID: zoneID},
		},
	}
	if err := st.UpsertRules(ctx, []domain.Rule{rule}); err != nil {
		t.Fatalf("upsert rule: %v", err)
	}

	// Query nearby rules.
	rules, err := st.RulesNearby(ctx, domain.Coordinate{Lat: 59.3278, Lng: 18.0531}, 50)
	if err != nil {
		t.Fatalf("rules nearby: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("want 1 rule, got %d", len(rules))
	}
	got := rules[0]
	if got.Kind != domain.RuleAllow {
		t.Errorf("kind: want allow, got %s", got.Kind)
	}
	if !got.NeedsPayment {
		t.Errorf("needs_payment: want true")
	}
	if got.MaxDuration != 2*time.Hour {
		t.Errorf("max_duration: want 2h, got %v", got.MaxDuration)
	}
	if len(got.TimeWindows) != 1 {
		t.Fatalf("want 1 time window, got %d", len(got.TimeWindows))
	}
	if got.TimeWindows[0].StartMin != 540 || got.TimeWindows[0].EndMin != 1080 {
		t.Errorf("time window: want 540-1080, got %d-%d", got.TimeWindows[0].StartMin, got.TimeWindows[0].EndMin)
	}
}

func TestRulesNearby_RadiusFiltering(t *testing.T) {
	st, ctx := openTestStore(t)

	// Two zones: one at the query point, one ~500m east.
	farZoneID := seedZone(t, st, 18.0600, 59.3278, "FAR", "paid") // ~390m east
	nearZoneID := seedZone(t, st, 18.0531, 59.3278, "NEAR", "paid")

	reg := domain.Regulation{
		Source:        domain.Source{System: "test", Reference: "reg-radius"},
		EffectiveFrom: time.Now(),
	}
	if err := st.UpsertRegulations(ctx, []domain.Regulation{reg}); err != nil {
		t.Fatalf("upsert reg: %v", err)
	}
	var regID string
	st.pool.QueryRow(ctx, `SELECT id::text FROM regulation WHERE source_reference = 'reg-radius'`).Scan(&regID)

	rules := []domain.Rule{
		{
			RegulationID: regID, Kind: domain.RuleAllow, Priority: 1,
			AppliesTo: []domain.AppliesTo{{Kind: domain.TargetZone, TargetID: nearZoneID}},
		},
		{
			RegulationID: regID, Kind: domain.RuleAllow, Priority: 2,
			AppliesTo: []domain.AppliesTo{{Kind: domain.TargetZone, TargetID: farZoneID}},
		},
	}
	if err := st.UpsertRules(ctx, rules); err != nil {
		t.Fatalf("upsert rules: %v", err)
	}

	// 50m radius should only catch the near zone.
	got, err := st.RulesNearby(ctx, domain.Coordinate{Lat: 59.3278, Lng: 18.0531}, 50)
	if err != nil {
		t.Fatalf("rules nearby: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 rule within 50m, got %d", len(got))
	}

	// 1000m radius should catch both.
	got, err = st.RulesNearby(ctx, domain.Coordinate{Lat: 59.3278, Lng: 18.0531}, 1000)
	if err != nil {
		t.Fatalf("rules nearby: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rules within 1000m, got %d", len(got))
	}
}

func TestRulesNearby_OffsetExtendsRadius(t *testing.T) {
	st, ctx := openTestStore(t)

	// Insert a POI ~12m south of the query point (latitude offset).
	const insertPOI = `
		INSERT INTO point_of_interest (kind, source_system, source_reference, geom)
		VALUES ('junction', 'test', 'j1', ST_SetSRID(ST_MakePoint($1, $2), 4326))
		RETURNING id::text`
	var poiID string
	// 0.0001 degrees latitude ~= 11m. Two of them ~ 22m.
	if err := st.pool.QueryRow(ctx, insertPOI, 18.0531, 59.3278-0.0002).Scan(&poiID); err != nil {
		t.Fatalf("seed poi: %v", err)
	}

	reg := domain.Regulation{Source: domain.Source{System: "test", Reference: "reg-offset"}, EffectiveFrom: time.Now()}
	if err := st.UpsertRegulations(ctx, []domain.Regulation{reg}); err != nil {
		t.Fatalf("upsert reg: %v", err)
	}
	var regID string
	st.pool.QueryRow(ctx, `SELECT id::text FROM regulation WHERE source_reference = 'reg-offset'`).Scan(&regID)

	// Rule with a 10m offset attached to the POI. Even though the
	// search radius is only 5m, the offset extends the effective
	// reach to 15m and should pick up the POI ~22m away — wait, no,
	// 22m is still farther than 15m. Let me use offset 25m.
	rule := domain.Rule{
		RegulationID: regID, Kind: domain.RuleForbid, Priority: 10,
		AppliesTo: []domain.AppliesTo{
			{Kind: domain.TargetPointOfInterest, TargetID: poiID, OffsetFromMeters: -25, OffsetToMeters: 0},
		},
	}
	if err := st.UpsertRules(ctx, []domain.Rule{rule}); err != nil {
		t.Fatalf("upsert rule: %v", err)
	}

	// 5m base radius + 25m offset = 30m, should catch the ~22m POI.
	got, err := st.RulesNearby(ctx, domain.Coordinate{Lat: 59.3278, Lng: 18.0531}, 5)
	if err != nil {
		t.Fatalf("rules nearby: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("offset should extend reach; want 1 rule, got %d", len(got))
	}
}

func TestPermitsForPlate_FiltersExpired(t *testing.T) {
	st, ctx := openTestStore(t)

	now := time.Now()
	permits := []domain.Permit{
		{Kind: domain.PermitResidential, Plate: "ABC123", ValidFrom: now.AddDate(0, -1, 0), ValidTo: now.AddDate(0, 1, 0)},  // current
		{Kind: domain.PermitResidential, Plate: "ABC123", ValidFrom: now.AddDate(-1, 0, 0), ValidTo: now.AddDate(0, -6, 0)}, // expired
		{Kind: domain.PermitResidential, Plate: "OTHER1", ValidFrom: now, ValidTo: now.AddDate(0, 1, 0)},                    // other plate
	}
	if err := st.UpsertPermits(ctx, permits); err != nil {
		t.Fatalf("upsert permits: %v", err)
	}

	got, err := st.PermitsForPlate(ctx, "ABC123")
	if err != nil {
		t.Fatalf("permits for plate: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 active permit for ABC123, got %d", len(got))
	}
}

func TestUpsertRules_DestructiveReplace(t *testing.T) {
	st, ctx := openTestStore(t)

	zoneID := seedZone(t, st, 18.0531, 59.3278, "Z", "paid")

	reg := domain.Regulation{Source: domain.Source{System: "test", Reference: "reg-replace"}, EffectiveFrom: time.Now()}
	if err := st.UpsertRegulations(ctx, []domain.Regulation{reg}); err != nil {
		t.Fatalf("upsert reg: %v", err)
	}
	var regID string
	st.pool.QueryRow(ctx, `SELECT id::text FROM regulation WHERE source_reference = 'reg-replace'`).Scan(&regID)

	// First batch: 3 rules.
	first := []domain.Rule{
		{RegulationID: regID, Kind: domain.RuleAllow, AppliesTo: []domain.AppliesTo{{Kind: domain.TargetZone, TargetID: zoneID}}},
		{RegulationID: regID, Kind: domain.RuleAllow, AppliesTo: []domain.AppliesTo{{Kind: domain.TargetZone, TargetID: zoneID}}},
		{RegulationID: regID, Kind: domain.RuleAllow, AppliesTo: []domain.AppliesTo{{Kind: domain.TargetZone, TargetID: zoneID}}},
	}
	if err := st.UpsertRules(ctx, first); err != nil {
		t.Fatalf("upsert first batch: %v", err)
	}
	got, _ := st.RulesNearby(ctx, domain.Coordinate{Lat: 59.3278, Lng: 18.0531}, 50)
	if len(got) != 3 {
		t.Fatalf("after first batch want 3, got %d", len(got))
	}

	// Second batch: 1 rule. Should replace, not add.
	second := []domain.Rule{
		{RegulationID: regID, Kind: domain.RuleForbid, AppliesTo: []domain.AppliesTo{{Kind: domain.TargetZone, TargetID: zoneID}}},
	}
	if err := st.UpsertRules(ctx, second); err != nil {
		t.Fatalf("upsert second batch: %v", err)
	}
	got, _ = st.RulesNearby(ctx, domain.Coordinate{Lat: 59.3278, Lng: 18.0531}, 50)
	if len(got) != 1 {
		t.Fatalf("after replace want 1, got %d", len(got))
	}
	if got[0].Kind != domain.RuleForbid {
		t.Errorf("expected the replacement Forbid rule, got %s", got[0].Kind)
	}
}

func TestZoneAt_ReturnsContainingZone(t *testing.T) {
	st, ctx := openTestStore(t)

	id := seedZone(t, st, 18.0531, 59.3278, "Z14", "paid")
	seedRoadSegment(t, st, 18.0531, 59.3278, "Odengatan")

	zone, street, muni, err := st.ZoneAt(ctx, domain.Coordinate{Lat: 59.3278, Lng: 18.0531})
	if err != nil {
		t.Fatalf("zone-at: %v", err)
	}
	if zone == nil {
		t.Fatalf("expected zone, got nil")
	}
	if zone.ID != id {
		t.Errorf("zone id: want %s, got %s", id, zone.ID)
	}
	if zone.Code != "Z14" {
		t.Errorf("code: want Z14, got %s", zone.Code)
	}
	if street != "Odengatan" {
		t.Errorf("street: want Odengatan, got %q", street)
	}
	if muni != "Stockholm" {
		t.Errorf("municipality: want Stockholm, got %q", muni)
	}
}

func TestZoneAt_ReturnsNilOutsideAnyZone(t *testing.T) {
	st, ctx := openTestStore(t)
	// No zones seeded.
	zone, _, _, err := st.ZoneAt(ctx, domain.Coordinate{Lat: 59.3278, Lng: 18.0531})
	if err != nil {
		t.Fatalf("zone-at: %v", err)
	}
	if zone != nil {
		t.Errorf("expected nil zone outside any seeded zone, got %+v", zone)
	}
}

func TestOperatorsForZone(t *testing.T) {
	st, ctx := openTestStore(t)

	zoneID := seedZone(t, st, 18.0531, 59.3278, "Z14", "paid")

	// Insert two operators and their zone mappings via raw SQL.
	const insertOp = `INSERT INTO operator (id, name, kind) VALUES (gen_random_uuid(), $1, 'municipal') RETURNING id::text`
	var opEasy, opPark string
	st.pool.QueryRow(ctx, insertOp, "EasyPark").Scan(&opEasy)
	st.pool.QueryRow(ctx, insertOp, "Parkster").Scan(&opPark)

	const insertOZ = `
		INSERT INTO operator_zone (operator_id, external_zone_id, maps_to_zone_id, deeplink_template)
		VALUES ($1, $2, $3, $4)`
	if _, err := st.pool.Exec(ctx, insertOZ, opEasy, "5012", zoneID, "easypark://start?zone={external}&plate={plate}"); err != nil {
		t.Fatalf("insert op-zone EasyPark: %v", err)
	}
	if _, err := st.pool.Exec(ctx, insertOZ, opPark, "P-12", zoneID, ""); err != nil {
		t.Fatalf("insert op-zone Parkster: %v", err)
	}

	got, err := st.OperatorsForZone(ctx, zoneID, "ABC123")
	if err != nil {
		t.Fatalf("operators: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 operators, got %d", len(got))
	}
	// Sorted by name: EasyPark then Parkster.
	if got[0].Name != "EasyPark" {
		t.Errorf("first operator: want EasyPark, got %s", got[0].Name)
	}
	if got[0].Deeplink != "easypark://start?zone=5012&plate=ABC123" {
		t.Errorf("deeplink expansion: got %q", got[0].Deeplink)
	}
	if got[1].Deeplink != "" {
		t.Errorf("second operator has no template; deeplink should be empty, got %q", got[1].Deeplink)
	}
}
