-- 0002_geospatial.sql
-- Geospatial subdomain: roads, zones, parking areas, points of interest.
-- All geometries stored in WGS84 (SRID 4326). Adapters that ingest
-- SWEREF99 TM data must transform on insert.

CREATE TABLE road_segment (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  street_name  TEXT,
  municipality TEXT NOT NULL,
  direction    TEXT, -- "forward" | "reverse" | "both"
  source_system    TEXT NOT NULL,
  source_reference TEXT,
  geom         GEOMETRY(LineString, 4326) NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX road_segment_geom_gix ON road_segment USING GIST (geom);
CREATE INDEX road_segment_street_idx ON road_segment (municipality, street_name);
CREATE TRIGGER road_segment_touch
BEFORE UPDATE ON road_segment FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

CREATE TABLE zone (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  city         TEXT NOT NULL,
  code         TEXT NOT NULL,
  kind         TEXT NOT NULL CHECK (kind IN ('paid','residential','mixed','loading','forbidden')),
  source_system    TEXT NOT NULL,
  source_reference TEXT,
  geom         GEOMETRY(MultiPolygon, 4326) NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (city, code, kind)
);
CREATE INDEX zone_geom_gix ON zone USING GIST (geom);
CREATE TRIGGER zone_touch
BEFORE UPDATE ON zone FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

CREATE TABLE parking_area (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  type         TEXT NOT NULL CHECK (type IN ('street','garage','private')),
  capacity     INT,
  operator_id  UUID,
  source_system    TEXT NOT NULL,
  source_reference TEXT,
  geom         GEOMETRY(Polygon, 4326) NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX parking_area_geom_gix ON parking_area USING GIST (geom);
CREATE TRIGGER parking_area_touch
BEFORE UPDATE ON parking_area FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- Discrete features used by offset rules (the 10m-before-junction
-- pattern). Junctions, crosswalks, hydrants, bus stops.
CREATE TABLE point_of_interest (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  kind         TEXT NOT NULL,
  source_system    TEXT NOT NULL,
  source_reference TEXT,
  geom         GEOMETRY(Point, 4326) NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX poi_geom_gix ON point_of_interest USING GIST (geom);
CREATE INDEX poi_kind_idx ON point_of_interest (kind);
CREATE TRIGGER poi_touch
BEFORE UPDATE ON point_of_interest FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
