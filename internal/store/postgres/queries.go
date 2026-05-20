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
//
// Each query also joins the parent regulation to pull source_system
// and source_reference. These are denormalised onto domain.Rule.Source
// so the engine can surface them in Reason — the defensible-citation
// audit trail.
//
// All four queries filter out regulations whose effective_to is set
// and in the past. LTF-Tolken doesn't currently surface withdrawal
// dates, so this is mostly inert today, but it makes the read path
// correct ahead of any data source that does (manual overrides,
// future LTF schema versions, multi-city sources).
const sqlRulesByZone = `
SELECT DISTINCT r.id::text, r.regulation_id::text, r.kind, r.max_duration_s,
       r.needs_payment, r.needs_permit, r.vehicle_classes, r.priority,
       reg.source_system, COALESCE(reg.source_reference, ''),
       COALESCE(r.tariff_class_code, '')
FROM rule r
JOIN rule_applies_to a ON a.rule_id = r.id
JOIN regulation reg ON reg.id = r.regulation_id
JOIN zone z ON z.id = a.target_id
WHERE a.target_kind = 'zone'
  AND (reg.effective_to IS NULL OR reg.effective_to > NOW())
  AND ST_DWithin(
    z.geom::geography,
    ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
    $3 + GREATEST(ABS(a.offset_from_meters), ABS(a.offset_to_meters))
  )`

const sqlRulesByParkingArea = `
SELECT DISTINCT r.id::text, r.regulation_id::text, r.kind, r.max_duration_s,
       r.needs_payment, r.needs_permit, r.vehicle_classes, r.priority,
       reg.source_system, COALESCE(reg.source_reference, ''),
       COALESCE(r.tariff_class_code, '')
FROM rule r
JOIN rule_applies_to a ON a.rule_id = r.id
JOIN regulation reg ON reg.id = r.regulation_id
JOIN parking_area pa ON pa.id = a.target_id
WHERE a.target_kind = 'parking_area'
  AND (reg.effective_to IS NULL OR reg.effective_to > NOW())
  AND ST_DWithin(
    pa.geom::geography,
    ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
    $3 + GREATEST(ABS(a.offset_from_meters), ABS(a.offset_to_meters))
  )`

const sqlRulesByRoadSegment = `
SELECT DISTINCT r.id::text, r.regulation_id::text, r.kind, r.max_duration_s,
       r.needs_payment, r.needs_permit, r.vehicle_classes, r.priority,
       reg.source_system, COALESCE(reg.source_reference, ''),
       COALESCE(r.tariff_class_code, '')
FROM rule r
JOIN rule_applies_to a ON a.rule_id = r.id
JOIN regulation reg ON reg.id = r.regulation_id
JOIN road_segment rs ON rs.id = a.target_id
WHERE a.target_kind = 'road_segment'
  AND (reg.effective_to IS NULL OR reg.effective_to > NOW())
  AND ST_DWithin(
    rs.geom::geography,
    ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
    $3 + GREATEST(ABS(a.offset_from_meters), ABS(a.offset_to_meters))
  )`

const sqlRulesByPOI = `
SELECT DISTINCT r.id::text, r.regulation_id::text, r.kind, r.max_duration_s,
       r.needs_payment, r.needs_permit, r.vehicle_classes, r.priority,
       reg.source_system, COALESCE(reg.source_reference, ''),
       COALESCE(r.tariff_class_code, '')
FROM rule r
JOIN rule_applies_to a ON a.rule_id = r.id
JOIN regulation reg ON reg.id = r.regulation_id
JOIN point_of_interest poi ON poi.id = a.target_id
WHERE a.target_kind = 'poi'
  AND (reg.effective_to IS NULL OR reg.effective_to > NOW())
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
//
// Pricing is no longer queried per-zone from the `tariff` table.
// Instead, each rule carries a `tariff_class_code` (see migration
// 0006), which the engine resolves against the in-process tariff
// class registry. The `tariff` table is retained for future operator-
// specific pricing experiments but the read path no longer uses it.

// --- Enrichment: operators ----------------------------------------------

const sqlOperatorsForZone = `
SELECT o.id::text, o.name, COALESCE(oz.external_zone_id, ''),
       COALESCE(oz.deeplink_template, '')
FROM operator o
JOIN operator_zone oz ON oz.operator_id = o.id
WHERE oz.maps_to_zone_id = $1
ORDER BY o.name`

// --- Upserts ------------------------------------------------------------

// UpsertRegulation: idempotent on (source_system, source_reference).
// Returns the generated UUID along with the source_reference so the
// caller can build a map[source_ref]uuid for cross-record resolution.
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
RETURNING id::text, source_reference`

// UpsertRoadSegment: idempotent on the partial unique index over
// (source_system, source_reference) WHERE source_reference IS NOT NULL.
// Geometry is provided as WGS84 WKT.
const sqlUpsertRoadSegment = `
INSERT INTO road_segment
  (street_name, municipality, direction, source_system, source_reference, geom)
VALUES ($1, $2, $3, $4, $5, ST_GeomFromText($6, 4326))
ON CONFLICT (source_system, source_reference) WHERE source_reference IS NOT NULL DO UPDATE SET
  street_name = EXCLUDED.street_name,
  municipality = EXCLUDED.municipality,
  direction = EXCLUDED.direction,
  geom = EXCLUDED.geom,
  updated_at = NOW()
RETURNING id::text, source_reference`

// UpsertRules is destructive per regulation: delete existing rules for
// each affected regulation, then insert. Children (time_windows,
// applies_to) cascade on delete.
const sqlDeleteRulesForRegulation = `DELETE FROM rule WHERE regulation_id = $1`

const sqlInsertRule = `
INSERT INTO rule
  (id, regulation_id, kind, max_duration_s, needs_payment, needs_permit,
   vehicle_classes, priority, tariff_class_code)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''))`

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
