-- scripts/seed_stockholm_demo.sql
--
-- Demo seed for the Stureplan area (~ 59.3330, 18.0681) in central
-- Stockholm. Populates a zone, the four Stockholm-authorised payment
-- operators, a 25 SEK/hour tariff, and a paid-parking rule, so the
-- /allowed endpoint returns a fully enriched response.
--
-- Run:
--   make seed
-- or:
--   docker compose exec -T postgres psql -U parking -d parking \
--     -v ON_ERROR_STOP=1 < scripts/seed_stockholm_demo.sql
--
-- Re-runnable: deletes prior demo rows before inserting. All seeded
-- rows are tagged either by source_system='demo' or by names prefixed
-- with the four real operator names (idempotent on conflict).
--
-- Try it after seeding:
--   curl 'http://localhost:8080/allowed?lat=59.3330&lng=18.0681&plate=ABC123'
--   curl 'http://localhost:8080/allowed?lat=59.3330&lng=18.0681&plate=ABC123&duration_minutes=180'
--
-- DEMO LIMITATION: The seeded rule fires 24/7 so you see enrichment
-- regardless of the time you query. Real Stockholm parking is paid
-- Mon-Fri 09:00-18:00 with different rules on Saturdays and Sundays.
-- LTF-Tolken ingestion will produce realistic time windows once the
-- transform is implemented.

BEGIN;

-- ---------------------------------------------------------------------------
-- Wipe prior demo data so the script is re-runnable. CASCADE on FKs
-- handles rule_time_window / rule_applies_to / tariff / operator_zone.
-- ---------------------------------------------------------------------------

DELETE FROM regulation WHERE source_system = 'demo';
DELETE FROM operator_zone
 WHERE operator_id IN (SELECT id FROM operator WHERE name IN ('EasyPark','Parkster','Mobill','ePARK'))
   AND maps_to_zone_id IN (SELECT id FROM zone WHERE source_system = 'demo');
DELETE FROM road_segment WHERE source_system = 'demo';
DELETE FROM zone WHERE source_system = 'demo';

-- ---------------------------------------------------------------------------
-- Zone covering the Stureplan area: a small rectangle around the
-- query point (59.3330, 18.0681). PostGIS expects WKT as (lng lat).
-- ---------------------------------------------------------------------------

INSERT INTO zone (city, code, kind, source_system, source_reference, geom)
VALUES (
  'Stockholm', 'DEMO-Z1', 'paid', 'demo', 'stureplan',
  ST_Multi(ST_GeomFromText(
    'POLYGON((18.0660 59.3310, 18.0720 59.3310, 18.0720 59.3350, 18.0660 59.3350, 18.0660 59.3310))',
    4326
  ))
);

-- ---------------------------------------------------------------------------
-- Road segment so ZoneAt can return a street name. Sturegatan runs
-- through the seeded zone.
-- ---------------------------------------------------------------------------

INSERT INTO road_segment (street_name, municipality, source_system, source_reference, geom)
VALUES (
  'Sturegatan', 'Stockholm', 'demo', 'sturegatan',
  ST_GeomFromText('LINESTRING(18.0675 59.3325, 18.0685 59.3335)', 4326)
);

-- ---------------------------------------------------------------------------
-- The four Stockholm-authorised payment operators. Idempotent on name.
-- ---------------------------------------------------------------------------

INSERT INTO operator (name, kind) VALUES
  ('EasyPark', 'municipal'),
  ('Parkster', 'municipal'),
  ('Mobill',   'municipal'),
  ('ePARK',    'municipal')
ON CONFLICT (name) DO UPDATE SET kind = EXCLUDED.kind;

-- ---------------------------------------------------------------------------
-- Operator-zone mappings: each operator's external zone ID for this
-- area, plus a deeplink template. {external} and {plate} placeholders
-- are expanded at query time by the postgres store.
-- ---------------------------------------------------------------------------

INSERT INTO operator_zone (operator_id, external_zone_id, maps_to_zone_id, deeplink_template)
SELECT o.id, '5012', z.id, 'easypark://start?zone={external}&plate={plate}'
  FROM operator o, zone z WHERE o.name = 'EasyPark' AND z.source_system = 'demo' AND z.source_reference = 'stureplan'
UNION ALL
SELECT o.id, 'P-12', z.id, 'parkster://start?zone={external}&plate={plate}'
  FROM operator o, zone z WHERE o.name = 'Parkster' AND z.source_system = 'demo' AND z.source_reference = 'stureplan'
UNION ALL
SELECT o.id, 'M-7',  z.id, ''
  FROM operator o, zone z WHERE o.name = 'Mobill'   AND z.source_system = 'demo' AND z.source_reference = 'stureplan'
UNION ALL
SELECT o.id, 'E-1',  z.id, ''
  FROM operator o, zone z WHERE o.name = 'ePARK'    AND z.source_system = 'demo' AND z.source_reference = 'stureplan';

-- ---------------------------------------------------------------------------
-- Tariff: 25 SEK / hour, attached to each operator_zone (Stockholm
-- authorisation parity means all four operators charge the same).
-- ---------------------------------------------------------------------------

INSERT INTO tariff (operator_zone_id, currency, rate_per_unit, time_unit_s)
SELECT oz.id, 'SEK', 25, 3600
  FROM operator_zone oz
  JOIN zone z ON z.id = oz.maps_to_zone_id
 WHERE z.source_system = 'demo' AND z.source_reference = 'stureplan';

-- ---------------------------------------------------------------------------
-- Regulation + Rule: paid parking, max 2 hours.
-- ---------------------------------------------------------------------------

WITH new_reg AS (
  INSERT INTO regulation (source_system, source_reference, decision_authority, language, effective_from)
  VALUES ('demo', 'stureplan-paid-parking', 'Stockholms stad', 'sv-SE', '2024-01-01 00:00:00+00')
  RETURNING id
),
new_rule AS (
  INSERT INTO rule (regulation_id, kind, max_duration_s, needs_payment, needs_permit, vehicle_classes, priority)
  SELECT id, 'allow', 7200, TRUE, FALSE, ARRAY[]::TEXT[], 5
    FROM new_reg
  RETURNING id
),
inserted_window AS (
  -- DEMO: 24/7. weekday_mask 127 = all days; start_min 0, end_min 1440.
  INSERT INTO rule_time_window (rule_id, weekday_mask, start_min, end_min)
  SELECT id, 127, 0, 1440 FROM new_rule
  RETURNING rule_id
)
INSERT INTO rule_applies_to (rule_id, target_kind, target_id, offset_from_meters, offset_to_meters)
SELECT nr.id, 'zone', z.id, 0, 0
  FROM new_rule nr, zone z
 WHERE z.source_system = 'demo' AND z.source_reference = 'stureplan';

COMMIT;

-- ---------------------------------------------------------------------------
-- Verification: row counts.
-- ---------------------------------------------------------------------------

SELECT 'zone'         AS object, COUNT(*) FROM zone WHERE source_system = 'demo'
UNION ALL SELECT 'road_segment',  COUNT(*) FROM road_segment WHERE source_system = 'demo'
UNION ALL SELECT 'operator',      COUNT(*) FROM operator WHERE name IN ('EasyPark','Parkster','Mobill','ePARK')
UNION ALL SELECT 'operator_zone', COUNT(*) FROM operator_zone oz
                                  JOIN zone z ON z.id = oz.maps_to_zone_id
                                  WHERE z.source_system = 'demo'
UNION ALL SELECT 'tariff',        COUNT(*) FROM tariff t
                                  JOIN operator_zone oz ON oz.id = t.operator_zone_id
                                  JOIN zone z ON z.id = oz.maps_to_zone_id
                                  WHERE z.source_system = 'demo'
UNION ALL SELECT 'regulation',    COUNT(*) FROM regulation WHERE source_system = 'demo'
UNION ALL SELECT 'rule',          COUNT(*) FROM rule WHERE regulation_id IN
                                  (SELECT id FROM regulation WHERE source_system = 'demo');
