-- 0008_operator_service_area.sql
--
-- City-wide operator catalog.
--
-- The original schema (migration 0004) modeled operators as
-- payment providers attached to specific zones via operator_zone.
-- That fits cities where different operators have different
-- contracts per zone, but Stockholm doesn't work that way: the
-- four authorised operators (EasyPark, Parkster, Mobill, ePARK)
-- each serve the entire municipality under one city contract.
-- See parkering.stockholm/betala-parkering/.
--
-- For zone-less locations (which is most of the city — only
-- residential and a few special zones are modeled today), we need
-- a fallback that returns "the operators that serve this city" so
-- the verdict can offer the user payment buttons. Two additions:
--
--   1. service_area_municipality TEXT on operator
--      Empty when an operator is zone-scoped (legacy model).
--      Set when the operator serves the entire named city.
--
--   2. default_deeplink TEXT on operator
--      The URL to send the user to when no zone-specific deeplink
--      is known. For Stockholm's operators these are landing URLs
--      for the operator's web app or homepage — the user types the
--      area code from the parking sign once they arrive.
--
-- Both columns are nullable / additive so the legacy zone-based
-- flow keeps working unchanged.

ALTER TABLE operator ADD COLUMN IF NOT EXISTS service_area_municipality TEXT;
ALTER TABLE operator ADD COLUMN IF NOT EXISTS default_deeplink TEXT;

CREATE INDEX IF NOT EXISTS operator_service_area_idx
    ON operator (service_area_municipality);

-- Seed Stockholm's four authorised parking operators. EasyPark's
-- "web app" is the City of Stockholm's free service tier — same
-- URL serves the mobile-installed app via universal link, falls
-- back to a browser-based parking flow otherwise. ePARK is
-- Stockholms Stad's own service (Stockholm Parkering's app).
INSERT INTO operator (name, kind, service_area_municipality, default_deeplink)
VALUES
  ('EasyPark', 'private',   'Stockholm', 'https://web.easypark.net/'),
  ('Parkster', 'private',   'Stockholm', 'https://parkster.com/'),
  ('Mobill',   'private',   'Stockholm', 'https://mobill.se/'),
  ('ePARK',    'municipal', 'Stockholm', 'https://www.epark.se/')
ON CONFLICT (name) DO UPDATE SET
  service_area_municipality = EXCLUDED.service_area_municipality,
  default_deeplink          = EXCLUDED.default_deeplink,
  kind                      = EXCLUDED.kind;
