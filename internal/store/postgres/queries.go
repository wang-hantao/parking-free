package postgres

// SQL queries used by the Store. Kept in a separate file so the
// implementation in store.go reads as control flow, not text.

// --- Spatial queries ----------------------------------------------------

// Each rulesByXxx query joins one of the four target geometry tables.
// They share a skeleton: pull rule columns, join applies_to, join the
// specific geometry table, and filter with ST_DWithin in metres
// (geography cast). The radius is the user-supplied search radius
// PLUS the rule's own offset extent (for the 10m-before-junction class
// of rule, where offset_from = -10).
const sqlRulesByZone = `
SELECT DISTINCT r.id::text, r.regulation_id::text, r.kind, r.max_duration_s,
       r.needs_payment, r.needs_permit, r.vehicle_classes, r.priority
FROM rule r
JOIN rule_applies_to a ON a.rule_id = r.id
JOIN zone z ON z.id = a.target_id
WHERE a.target_kind = 'zone'
  AND ST_DWithin(
    z.geom::geography,
    ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
    $3 + GREATEST(ABS(a.offset_from_meters), ABS(a.offset_to_meters))
  )`

const sqlRulesByParkingArea = `
SELECT DISTINCT r.id::text, r.regulation_id::text, r.kind, r.max_duration_s,
       r.needs_payment, r.needs_permit, r.vehicle_classes, r.priority
FROM rule r
JOIN rule_applies_to a ON a.rule_id = r.id
JOIN parking_area pa ON pa.id = a.target_id
WHERE a.target_kind = 'parking_area'
  AND ST_DWithin(
    pa.geom::geography,
    ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
    $3 + GREATEST(ABS(a.offset_from_meters), ABS(a.offset_to_meters))
  )`

const sqlRulesByRoadSegment = `
SELECT DISTINCT r.id::text, r.regulation_id::text, r.kind, r.max_duration_s,
       r.needs_payment, r.needs_permit, r.vehicle_classes, r.priority
FROM rule r
JOIN rule_applies_to a ON a.rule_id = r.id
JOIN road_segment rs ON rs.id = a.target_id
WHERE a.target_kind = 'road_segment'
  AND ST_DWithin(
    rs.geom::geography,
    ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
    $3 + GREATEST(ABS(a.offset_from_meters), ABS(a.offset_to_meters))
  )`

const sqlRulesByPOI = `
SELECT DISTINCT r.id::text, r.regulation_id::text, r.kind, r.max_duration_s,
       r.needs_payment, r.needs_permit, r.vehicle_classes, r.priority
FROM rule r
JOIN rule_applies_to a ON a.rule_id = r.id
JOIN point_of_interest poi ON poi.id = a.target_id
WHERE a.target_kind = 'poi'
  AND ST_DWithin(
    poi.geom::geography,
    ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
    $3 + GREATEST(ABS(a.offset_from_meters), ABS(a.offset_to_meters))
  )`

const sqlTimeWindowsForRules = `
SELECT rule_id::text, weekday_mask, COALESCE(day_type, ''),
       start_min, end_min
FROM rule_time_window
WHERE rule_id = ANY($1)`

// --- Permits ------------------------------------------------------------

const sqlPermitsByPlate = `
SELECT id::text, kind, COALESCE(zone_id::text, ''),
       plate, COALESCE(holder_ref, ''), valid_from, valid_to
FROM permit
WHERE plate = $1 AND valid_to > NOW()`

// --- Enrichment: zone, street, municipality ----------------------------

const sqlZoneAt = `
SELECT id::text, code, city, kind
FROM zone
WHERE ST_Contains(geom, ST_SetSRID(ST_MakePoint($1, $2), 4326))
ORDER BY ST_Area(geom) ASC
LIMIT 1`

const sqlStreetAt = `
SELECT COALESCE(street_name, ''), municipality
FROM road_segment
WHERE ST_DWithin(
    geom::geography,
    ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
    30
)
ORDER BY ST_Distance(
    geom::geography,
    ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography
) ASC
LIMIT 1`

// --- Enrichment: tariffs ------------------------------------------------

// v1 returns one open-ended TariffWindow per distinct tariff active
// for the position's zone. DISTINCT collapses the case where multiple
// authorised operators (Stockholm parity model) carry identical
// tariffs — without it, the engine sees them as separate windows and
// emits a spurious next_rate_change. Future schema changes will add
// weekday/time-of-day columns to tariff so the store can return real
// per-window pricing.
const sqlTariffsAt = `
SELECT DISTINCT t.currency, t.rate_per_unit::float8, t.time_unit_s, t.max_session_cost::float8
FROM tariff t
JOIN operator_zone oz ON oz.id = t.operator_zone_id
JOIN zone z ON z.id = oz.maps_to_zone_id
WHERE ST_Contains(z.geom, ST_SetSRID(ST_MakePoint($1, $2), 4326))`

// --- Enrichment: operators ----------------------------------------------

const sqlOperatorsForZone = `
SELECT o.id::text, o.name, COALESCE(oz.external_zone_id, ''),
       COALESCE(oz.deeplink_template, '')
FROM operator o
JOIN operator_zone oz ON oz.operator_id = o.id
WHERE oz.maps_to_zone_id = $1
ORDER BY o.name`

// --- Upserts ------------------------------------------------------------

const sqlUpsertRegulation = `
INSERT INTO regulation
  (id, source_system, source_reference, decision_authority,
   language, effective_from, effective_to)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (source_system, source_reference) DO UPDATE SET
  decision_authority = EXCLUDED.decision_authority,
  language = EXCLUDED.language,
  effective_from = EXCLUDED.effective_from,
  effective_to = EXCLUDED.effective_to,
  updated_at = NOW()
RETURNING id::text`

// UpsertRules is destructive per regulation: delete existing rules for
// each affected regulation, then insert. Children (time_windows,
// applies_to) cascade on delete.
const sqlDeleteRulesForRegulation = `DELETE FROM rule WHERE regulation_id = $1`

const sqlInsertRule = `
INSERT INTO rule
  (id, regulation_id, kind, max_duration_s, needs_payment, needs_permit,
   vehicle_classes, priority)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

const sqlInsertTimeWindow = `
INSERT INTO rule_time_window
  (rule_id, weekday_mask, day_type, start_min, end_min, date_from, date_to)
VALUES ($1, $2, $3, $4, $5, $6, $7)`

const sqlInsertAppliesTo = `
INSERT INTO rule_applies_to
  (rule_id, target_kind, target_id, offset_from_meters, offset_to_meters)
VALUES ($1, $2, $3, $4, $5)`

const sqlUpsertPermit = `
INSERT INTO permit
  (id, kind, zone_id, plate, holder_ref, valid_from, valid_to)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO UPDATE SET
  kind = EXCLUDED.kind,
  zone_id = EXCLUDED.zone_id,
  plate = EXCLUDED.plate,
  holder_ref = EXCLUDED.holder_ref,
  valid_from = EXCLUDED.valid_from,
  valid_to = EXCLUDED.valid_to,
  updated_at = NOW()`
