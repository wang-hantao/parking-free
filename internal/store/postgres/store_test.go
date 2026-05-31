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
	if _, err := st.UpsertRegulations(ctx, []domain.Regulation{reg}); err != nil {
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
	if _, err := st.UpsertRegulations(ctx, []domain.Regulation{reg}); err != nil {
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
	if _, err := st.UpsertRegulations(ctx, []domain.Regulation{reg}); err != nil {
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
	if _, err := st.UpsertRegulations(ctx, []domain.Regulation{reg}); err != nil {
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

func TestPruneOrphanRoadSegments(t *testing.T) {
	st, ctx := openTestStore(t)

	// Insert 3 segments under prefix "ptillaten/":
	//   - 18819/4946: will have a rule attached → must survive
	//   - 290/1:      no rule → orphan, should be deleted
	//   - 18452/1:    no rule → orphan, should be deleted
	// Plus one outside the prefix:
	//   - servicedagar/107/1: no rule, but different prefix → must survive
	// Plus one with rules outside the prefix:
	//   - prorelsehindrad/1875/9204: has a rule → must survive
	//
	// Mirrors the real-world data state the user observed at
	// Sankt Eriksgatan after multiple LTF ingestion runs.

	type segSpec struct {
		ref     string
		hasRule bool
	}
	specs := []segSpec{
		{"ptillaten/18819/4946", true},
		{"ptillaten/290/1", false},
		{"ptillaten/18452/1", false},
		{"servicedagar/107/1", false},
		{"prorelsehindrad/1875/9204", true},
	}

	segIDs := make(map[string]string, len(specs))
	for _, sp := range specs {
		const q = `
			INSERT INTO road_segment (street_name, municipality, source_system, source_reference, geom)
			VALUES ('Sankt Eriksgatan', 'Stockholm', 'stockholm.ltf-tolken', $1,
				ST_GeomFromText('LINESTRING(18.032 59.345, 18.033 59.345)', 4326))
			RETURNING id::text`
		var id string
		if err := st.pool.QueryRow(ctx, q, sp.ref).Scan(&id); err != nil {
			t.Fatalf("insert seg %s: %v", sp.ref, err)
		}
		segIDs[sp.ref] = id
	}

	// Attach a rule to the ones marked hasRule. Each needs a
	// regulation too; share one for simplicity.
	const regQ = `
		INSERT INTO regulation (id, source_system, source_reference, decision_authority, language, effective_from)
		VALUES (gen_random_uuid(), 'stockholm.ltf-tolken', 'test-citation', 'Stockholms stad', 'sv-SE', NOW())
		RETURNING id::text`
	var regID string
	if err := st.pool.QueryRow(ctx, regQ).Scan(&regID); err != nil {
		t.Fatalf("insert regulation: %v", err)
	}

	for _, sp := range specs {
		if !sp.hasRule {
			continue
		}
		const ruleQ = `
			WITH r AS (
				INSERT INTO rule (id, regulation_id, kind, max_duration_s, needs_payment, needs_permit, vehicle_classes, priority)
				VALUES (gen_random_uuid(), $1::uuid, 'allow', 0, false, false, '{}', 5)
				RETURNING id
			)
			INSERT INTO rule_applies_to (rule_id, target_kind, target_id, offset_from_meters, offset_to_meters)
			SELECT id, 'road_segment', $2::uuid, 0, 0 FROM r`
		if _, err := st.pool.Exec(ctx, ruleQ, regID, segIDs[sp.ref]); err != nil {
			t.Fatalf("attach rule to %s: %v", sp.ref, err)
		}
	}

	// Prune ptillaten orphans. Should delete 2 rows.
	deleted, err := st.PruneOrphanRoadSegments(ctx, "stockholm.ltf-tolken", "ptillaten/")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 2 {
		t.Errorf("want 2 ptillaten orphans deleted, got %d", deleted)
	}

	// Verify what's left.
	type remaining struct{ ref string }
	rows, err := st.pool.Query(ctx, `
		SELECT source_reference FROM road_segment
		WHERE source_system = 'stockholm.ltf-tolken' ORDER BY source_reference`)
	if err != nil {
		t.Fatalf("query remaining: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var ref string
		_ = rows.Scan(&ref)
		got = append(got, ref)
	}

	want := []string{
		"prorelsehindrad/1875/9204", // has rule, different prefix — survives
		"ptillaten/18819/4946",      // has rule — survives
		"servicedagar/107/1",        // no rule but different prefix — survives
	}
	if len(got) != len(want) {
		t.Fatalf("want %d remaining, got %d: %v", len(want), len(got), got)
	}
	for i, ref := range want {
		if got[i] != ref {
			t.Errorf("remaining[%d]: got %q, want %q", i, got[i], ref)
		}
	}

	// Idempotency: running prune again should delete 0.
	deleted, err = st.PruneOrphanRoadSegments(ctx, "stockholm.ltf-tolken", "ptillaten/")
	if err != nil {
		t.Fatalf("prune (second call): %v", err)
	}
	if deleted != 0 {
		t.Errorf("second prune should be a no-op; got %d deleted", deleted)
	}
}

func TestPruneAllOrphanRoadSegments(t *testing.T) {
	st, ctx := openTestStore(t)

	// Three orphans across two prefixes + one segment with a rule
	// (must survive).
	insertSeg := func(ref string) string {
		const q = `
			INSERT INTO road_segment (street_name, municipality, source_system, source_reference, geom)
			VALUES ('Test', 'Stockholm', 'stockholm.ltf-tolken', $1,
				ST_GeomFromText('LINESTRING(18.0 59.3, 18.001 59.3)', 4326))
			RETURNING id::text`
		var id string
		if err := st.pool.QueryRow(ctx, q, ref).Scan(&id); err != nil {
			t.Fatalf("insert %s: %v", ref, err)
		}
		return id
	}
	insertSeg("ptillaten/1/1")
	insertSeg("ptillaten/2/1")
	insertSeg("servicedagar/1/1")
	keepID := insertSeg("ptillaten/keep/1")

	// Attach a rule to the "keep" segment.
	var regID string
	if err := st.pool.QueryRow(ctx, `
		INSERT INTO regulation (id, source_system, source_reference, decision_authority, language, effective_from)
		VALUES (gen_random_uuid(), 'stockholm.ltf-tolken', 'cite', 'Stockholms stad', 'sv-SE', NOW())
		RETURNING id::text`).Scan(&regID); err != nil {
		t.Fatalf("regulation: %v", err)
	}
	if _, err := st.pool.Exec(ctx, `
		WITH r AS (
			INSERT INTO rule (id, regulation_id, kind, max_duration_s, needs_payment, needs_permit, vehicle_classes, priority)
			VALUES (gen_random_uuid(), $1::uuid, 'allow', 0, false, false, '{}', 5)
			RETURNING id
		)
		INSERT INTO rule_applies_to (rule_id, target_kind, target_id, offset_from_meters, offset_to_meters)
		SELECT id, 'road_segment', $2::uuid, 0, 0 FROM r`, regID, keepID); err != nil {
		t.Fatalf("attach rule: %v", err)
	}

	deleted, err := st.PruneAllOrphanRoadSegments(ctx, "stockholm.ltf-tolken")
	if err != nil {
		t.Fatalf("prune all: %v", err)
	}
	if deleted != 3 {
		t.Errorf("want 3 deleted, got %d", deleted)
	}

	// Only "keep" remains.
	var remaining int
	if err := st.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM road_segment WHERE source_system = 'stockholm.ltf-tolken'`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 1 {
		t.Errorf("want 1 segment remaining, got %d", remaining)
	}
}

func TestCityOperators_ReturnsSeededStockholmFour(t *testing.T) {
	// Migration 0008 seeds the four Stockholm operators
	// (EasyPark, Parkster, Mobill, ePARK) under
	// service_area_municipality='Stockholm'. CityOperators should
	// return all of them in alphabetic order with deeplink populated.
	st, ctx := openTestStore(t)

	// Note: truncate in openTestStore wipes the operator table, so
	// re-apply the seed for this test. In production runs the
	// migration handles it.
	seed := `
		INSERT INTO operator (name, kind, service_area_municipality, default_deeplink)
		VALUES
		  ('EasyPark', 'private',   'Stockholm', 'https://web.easypark.net/'),
		  ('Parkster', 'private',   'Stockholm', 'https://parkster.com/'),
		  ('Mobill',   'private',   'Stockholm', 'https://mobill.se/'),
		  ('ePARK',    'municipal', 'Stockholm', 'https://www.epark.se/'),
		  ('OtherCity', 'private',  'Göteborg',  'https://other.example/')
		ON CONFLICT (name) DO UPDATE SET
		  service_area_municipality = EXCLUDED.service_area_municipality,
		  default_deeplink          = EXCLUDED.default_deeplink`
	if _, err := st.pool.Exec(ctx, seed); err != nil {
		t.Fatalf("seed operators: %v", err)
	}

	ops, err := st.CityOperators(ctx, "Stockholm", "JAT52Y")
	if err != nil {
		t.Fatalf("city operators: %v", err)
	}
	if len(ops) != 4 {
		t.Fatalf("want 4 Stockholm operators, got %d (%+v)", len(ops), ops)
	}

	wantNames := []string{"EasyPark", "Mobill", "Parkster", "ePARK"}
	for i, op := range ops {
		if op.Name != wantNames[i] {
			t.Errorf("op[%d]: want %q, got %q", i, wantNames[i], op.Name)
		}
		if op.Deeplink == "" {
			t.Errorf("op[%d] (%s): missing deeplink", i, op.Name)
		}
	}

	// Empty municipality → nil result (well-defined).
	none, err := st.CityOperators(ctx, "", "JAT52Y")
	if err != nil {
		t.Fatalf("empty municipality: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("empty municipality should return no operators; got %d", len(none))
	}

	// Different municipality → only its operators.
	gbg, err := st.CityOperators(ctx, "Göteborg", "JAT52Y")
	if err != nil {
		t.Fatalf("göteborg: %v", err)
	}
	if len(gbg) != 1 || gbg[0].Name != "OtherCity" {
		t.Errorf("Göteborg: want [OtherCity]; got %+v", gbg)
	}
}

// =============================================================================
// Strict-mode road-segment resolution — two-step anchor approach.
// =============================================================================

// seedRoadSegmentAt inserts a tiny line at (lng,lat) and returns the
// new road_segment's UUID. Differs from seedRoadSegment by also
// taking a source_reference and returning the ID for direct rule
// attachment.
func seedRoadSegmentAt(t *testing.T, st *Store, lng, lat float64, ref string) string {
	t.Helper()
	const q = `
		INSERT INTO road_segment (street_name, municipality, source_system, source_reference, geom)
		VALUES ($1, 'Stockholm', 'stockholm.ltf-tolken', $1,
			ST_GeomFromText('LINESTRING(' || ($2 - 0.0001) || ' ' || $3 || ',' || ($2 + 0.0001) || ' ' || $3 || ')', 4326))
		RETURNING id::text`
	var id string
	if err := st.pool.QueryRow(context.Background(), q, ref, lng, lat).Scan(&id); err != nil {
		t.Fatalf("seed segment %s: %v", ref, err)
	}
	return id
}

// seedRuleOnSegment attaches a minimal rule to a specific road_segment.
func seedRuleOnSegment(t *testing.T, st *Store, segID, regRef string, kind domain.RuleKind, priority int) {
	t.Helper()
	ctx := context.Background()
	// Idempotent regulation upsert (one regulation can carry many rules).
	const regQ = `
		INSERT INTO regulation (id, source_system, source_reference, decision_authority, language, effective_from)
		VALUES (gen_random_uuid(), 'stockholm.ltf-tolken', $1, 'Stockholms stad', 'sv-SE', NOW())
		ON CONFLICT (source_system, source_reference) DO UPDATE SET updated_at = NOW()
		RETURNING id::text`
	var regID string
	if err := st.pool.QueryRow(ctx, regQ, regRef).Scan(&regID); err != nil {
		t.Fatalf("regulation %s: %v", regRef, err)
	}
	const ruleQ = `
		WITH r AS (
			INSERT INTO rule (id, regulation_id, kind, max_duration_s, needs_payment, needs_permit, vehicle_classes, priority)
			VALUES (gen_random_uuid(), $1::uuid, $2, 0, false, false, '{}', $3)
			RETURNING id
		)
		INSERT INTO rule_applies_to (rule_id, target_kind, target_id, offset_from_meters, offset_to_meters)
		SELECT id, 'road_segment', $4::uuid, 0, 0 FROM r`
	if _, err := st.pool.Exec(ctx, ruleQ, regID, string(kind), priority, segID); err != nil {
		t.Fatalf("attach rule: %v", err)
	}
}

func TestRulesAt_AnchorsAcrossGpsOffset(t *testing.T) {
	// Scenario: rule-bearing segment is ~8m east of the user. A flat
	// 5m radius would silently drop it; the two-step anchor approach
	// locks onto it because it's the nearest rule-bearing segment
	// within the 30m sanity bound.
	st, ctx := openTestStore(t)

	// User at (18.05947, 59.33568). Place a rule-bearing segment ~8m
	// east. At Stockholm's latitude, 1° lng ≈ 56000m, so 8m ≈ 0.00014°.
	segID := seedRoadSegmentAt(t, st, 18.05947+0.00014, 59.33568, "ptillaten/test/1")
	seedRuleOnSegment(t, st, segID, "test-cite-1", domain.RuleAllow, 5)

	rules, err := st.RulesAt(ctx, domain.Coordinate{Lat: 59.33568, Lng: 18.05947})
	if err != nil {
		t.Fatalf("rules-at: %v", err)
	}
	if len(rules) != 1 {
		t.Errorf("want 1 rule from segment ~8m away, got %d", len(rules))
	}
}

func TestRulesAt_CapturesCoLocatedRules(t *testing.T) {
	// Scenario: a disabled bay carved into a paid strip. Two
	// segments, ~1m apart, both with rules. The anchor finds one,
	// the co-located radius (2m) picks up the other.
	st, ctx := openTestStore(t)

	// ~5m east of the user, slightly south
	stripID := seedRoadSegmentAt(t, st, 18.05947+0.00009, 59.33568, "ptillaten/strip/1")
	// ~1m offset from the strip
	bayID := seedRoadSegmentAt(t, st, 18.05947+0.00009, 59.33568+0.000009, "prorelsehindrad/bay/1")

	seedRuleOnSegment(t, st, stripID, "strip-cite", domain.RuleAllow, 5)
	seedRuleOnSegment(t, st, bayID, "bay-cite", domain.RuleAllow, 20)

	rules, err := st.RulesAt(ctx, domain.Coordinate{Lat: 59.33568, Lng: 18.05947})
	if err != nil {
		t.Fatalf("rules-at: %v", err)
	}
	if len(rules) != 2 {
		t.Errorf("want 2 rules (anchor + co-located), got %d", len(rules))
	}
}

func TestRulesAt_DoesNotBleedAcrossStreet(t *testing.T) {
	// Scenario: user is at street A (close), but there's a
	// rule-bearing segment on street B (~15m away, across the
	// street). The 2m co-located radius around the anchor (street A)
	// excludes street B's rules.
	st, ctx := openTestStore(t)

	// Street A: 5m east, with a rule
	streetAID := seedRoadSegmentAt(t, st, 18.05947+0.00009, 59.33568, "ptillaten/streetA/1")
	seedRuleOnSegment(t, st, streetAID, "A-cite", domain.RuleAllow, 5)

	// Street B: 15m west (~0.00027° lng), with a rule of its own
	streetBID := seedRoadSegmentAt(t, st, 18.05947-0.00027, 59.33568, "ptillaten/streetB/1")
	seedRuleOnSegment(t, st, streetBID, "B-cite", domain.RuleAllow, 5)

	rules, err := st.RulesAt(ctx, domain.Coordinate{Lat: 59.33568, Lng: 18.05947})
	if err != nil {
		t.Fatalf("rules-at: %v", err)
	}
	if len(rules) != 1 {
		t.Errorf("want 1 rule (anchored to closer street A), got %d", len(rules))
	}
	if len(rules) == 1 && rules[0].Source.Reference != "A-cite" {
		t.Errorf("want street A's rule, got source=%q", rules[0].Source.Reference)
	}
}

func TestRulesAt_NothingWhenFarFromAnyRuleBearingSegment(t *testing.T) {
	// Scenario: user is 100m from any rule-bearing segment. Past the
	// 30m sanity bound — return nothing rather than reach to a
	// distant road. Important: the user being inside a building, in
	// a park, or at unmapped infrastructure should produce an empty
	// verdict, not a misleading "you're allowed here" derived from
	// a road 100m away.
	st, ctx := openTestStore(t)

	// Segment 100m east of the user (~0.0018° lng at this latitude)
	segID := seedRoadSegmentAt(t, st, 18.05947+0.0018, 59.33568, "ptillaten/distant/1")
	seedRuleOnSegment(t, st, segID, "distant-cite", domain.RuleAllow, 5)

	rules, err := st.RulesAt(ctx, domain.Coordinate{Lat: 59.33568, Lng: 18.05947})
	if err != nil {
		t.Fatalf("rules-at: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("want 0 rules (segment beyond sanity bound), got %d", len(rules))
	}
}

func TestRulesAt_IgnoresOrphanSegmentsAsAnchor(t *testing.T) {
	// Scenario: an orphan segment (no rule_applies_to entries) sits
	// closer to the user than a rule-bearing segment. The anchor
	// must skip the orphan and lock onto the rule-bearing one —
	// otherwise the original v1 bug returns.
	st, ctx := openTestStore(t)

	// Orphan: 3m east, no rules
	seedRoadSegmentAt(t, st, 18.05947+0.000054, 59.33568, "ptillaten/orphan/1")

	// Rule-bearing: 12m east, with rules
	realID := seedRoadSegmentAt(t, st, 18.05947+0.00021, 59.33568, "ptillaten/real/1")
	seedRuleOnSegment(t, st, realID, "real-cite", domain.RuleAllow, 5)

	rules, err := st.RulesAt(ctx, domain.Coordinate{Lat: 59.33568, Lng: 18.05947})
	if err != nil {
		t.Fatalf("rules-at: %v", err)
	}
	if len(rules) != 1 {
		t.Errorf("want 1 rule (anchor must skip orphan), got %d", len(rules))
	}
}

// seedRoadSegmentWithStreet inserts a segment with explicit street_name
// distinct from source_reference. Needed for tests that exercise the
// same-street matching path in strict mode.
func seedRoadSegmentWithStreet(t *testing.T, st *Store, lng, lat float64, ref, street string) string {
	t.Helper()
	const q = `
		INSERT INTO road_segment (street_name, municipality, source_system, source_reference, geom)
		VALUES ($1, 'Stockholm', 'stockholm.ltf-tolken', $2,
			ST_GeomFromText('LINESTRING(' || ($3 - 0.0001) || ' ' || $4 || ',' || ($3 + 0.0001) || ' ' || $4 || ')', 4326))
		RETURNING id::text`
	var id string
	if err := st.pool.QueryRow(context.Background(), q, street, ref, lng, lat).Scan(&id); err != nil {
		t.Fatalf("seed segment %s: %v", ref, err)
	}
	return id
}

func TestRulesAt_SameStreetCapturesDistantRule(t *testing.T) {
	// Real-world scenario: Gamla Brogatan has reserved bays (the
	// anchor) plus a general paid-parking feature ~15m away on the
	// same street. The 8m co-located radius can't reach 15m, but
	// the same-street path catches it.
	st, ctx := openTestStore(t)

	// Reserved bay 5m east of GPS, on Gamla Brogatan
	bayID := seedRoadSegmentWithStreet(t, st,
		18.05947+0.00009, 59.33568,
		"ptillaten/bay/1", "Gamla Brogatan")
	seedRuleOnSegment(t, st, bayID, "bay-cite", domain.RuleAllow, 20)

	// General paid parking 15m east of GPS, same street — past the
	// co-located radius but within the same-street radius
	generalID := seedRoadSegmentWithStreet(t, st,
		18.05947+0.00027, 59.33568,
		"ptillaten/general/1", "Gamla Brogatan")
	seedRuleOnSegment(t, st, generalID, "general-cite", domain.RuleAllow, 5)

	rules, err := st.RulesAt(ctx, domain.Coordinate{Lat: 59.33568, Lng: 18.05947})
	if err != nil {
		t.Fatalf("rules-at: %v", err)
	}
	if len(rules) != 2 {
		t.Errorf("want 2 rules (bay anchor + same-street general), got %d", len(rules))
	}
}

func TestRulesAt_SameStreetDoesNotBleedAcrossStreets(t *testing.T) {
	// Sanity: a feature 30m away on a DIFFERENT street is excluded
	// by the street_name filter, even though it's within the 50m
	// same-street radius.
	st, ctx := openTestStore(t)

	// Anchor on Gamla Brogatan, 5m east
	anchorID := seedRoadSegmentWithStreet(t, st,
		18.05947+0.00009, 59.33568,
		"ptillaten/anchor/1", "Gamla Brogatan")
	seedRuleOnSegment(t, st, anchorID, "anchor-cite", domain.RuleAllow, 5)

	// Different street, 30m away. Within the same-street radius
	// distance-wise but excluded by name.
	otherID := seedRoadSegmentWithStreet(t, st,
		18.05947+0.00054, 59.33568,
		"ptillaten/other/1", "Mäster Samuelsgatan")
	seedRuleOnSegment(t, st, otherID, "other-cite", domain.RuleAllow, 5)

	rules, err := st.RulesAt(ctx, domain.Coordinate{Lat: 59.33568, Lng: 18.05947})
	if err != nil {
		t.Fatalf("rules-at: %v", err)
	}
	if len(rules) != 1 {
		t.Errorf("want 1 rule (anchor only, other street excluded), got %d", len(rules))
	}
}

func TestRulesAt_SameStreetWithEmptyStreetNameFallsBackToCoLocated(t *testing.T) {
	// Edge case: anchor has empty street_name. The same-street path
	// must be disabled (otherwise it would match every road_segment
	// with empty street_name as "same street"). Falls back to the
	// co-located path alone — distant features stay excluded.
	st, ctx := openTestStore(t)

	// Anchor with empty street_name, 5m east
	anchorID := seedRoadSegmentWithStreet(t, st,
		18.05947+0.00009, 59.33568,
		"ptillaten/anchor/1", "")
	seedRuleOnSegment(t, st, anchorID, "anchor-cite", domain.RuleAllow, 5)

	// Distant feature also with empty street_name, 30m away
	distantID := seedRoadSegmentWithStreet(t, st,
		18.05947+0.00054, 59.33568,
		"ptillaten/distant/1", "")
	seedRuleOnSegment(t, st, distantID, "distant-cite", domain.RuleAllow, 5)

	rules, err := st.RulesAt(ctx, domain.Coordinate{Lat: 59.33568, Lng: 18.05947})
	if err != nil {
		t.Fatalf("rules-at: %v", err)
	}
	if len(rules) != 1 {
		t.Errorf("want 1 rule (anchor only, empty street_name doesn't enable same-street), got %d", len(rules))
	}
}
