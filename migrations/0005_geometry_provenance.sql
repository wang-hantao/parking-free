-- 0005_geometry_provenance.sql
-- Stable provenance keys for geometry tables so ingested external
-- features (LTF-Tolken, NVDB, etc.) can be re-ingested idempotently.
--
-- Partial unique index because source_reference is nullable — manually
-- inserted geometries (e.g. tests, demo seed) may legitimately leave
-- it null without conflicting with each other.

CREATE UNIQUE INDEX IF NOT EXISTS road_segment_source_unique
  ON road_segment (source_system, source_reference)
  WHERE source_reference IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS parking_area_source_unique
  ON parking_area (source_system, source_reference)
  WHERE source_reference IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS point_of_interest_source_unique
  ON point_of_interest (source_system, source_reference)
  WHERE source_reference IS NOT NULL;
