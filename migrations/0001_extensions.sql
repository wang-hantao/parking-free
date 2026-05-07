-- 0001_extensions.sql
-- Bring up PostGIS and any shared helper functions.

CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS pgcrypto;  -- for gen_random_uuid()

-- A trigger function that maintains updated_at timestamps. Reused by
-- subsequent migrations.
CREATE OR REPLACE FUNCTION touch_updated_at()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END;
$$;
