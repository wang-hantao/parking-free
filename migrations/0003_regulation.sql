-- 0003_regulation.sql
-- Regulation graph: the core abstraction. A Regulation is a legal
-- instrument with provenance; it contains Rules; each Rule has time
-- windows and applies-to links.

CREATE TABLE regulation (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  source_system      TEXT NOT NULL,
  source_reference   TEXT NOT NULL,
  decision_authority TEXT,
  language           TEXT NOT NULL DEFAULT 'sv-SE',
  effective_from     TIMESTAMPTZ NOT NULL,
  effective_to       TIMESTAMPTZ,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (source_system, source_reference)
);
CREATE INDEX regulation_effective_idx ON regulation (effective_from, effective_to);
CREATE TRIGGER regulation_touch
BEFORE UPDATE ON regulation FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

CREATE TABLE rule (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  regulation_id   UUID NOT NULL REFERENCES regulation(id) ON DELETE CASCADE,
  kind            TEXT NOT NULL CHECK (kind IN ('allow','forbid','restrict')),
  max_duration_s  INT,
  needs_payment   BOOLEAN NOT NULL DEFAULT FALSE,
  needs_permit    BOOLEAN NOT NULL DEFAULT FALSE,
  vehicle_classes TEXT[] NOT NULL DEFAULT '{}', -- empty = all classes
  priority        INT NOT NULL DEFAULT 0,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX rule_regulation_idx ON rule (regulation_id);
CREATE INDEX rule_kind_priority_idx ON rule (kind, priority DESC);
CREATE TRIGGER rule_touch
BEFORE UPDATE ON rule FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- Time-window grammar. weekday_mask is bit Sun=1, Mon=2, ..., Sat=64.
-- start_min and end_min are minutes since 00:00 (0..1440); end < start
-- means the window crosses midnight.
CREATE TABLE rule_time_window (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  rule_id      UUID NOT NULL REFERENCES rule(id) ON DELETE CASCADE,
  weekday_mask SMALLINT NOT NULL DEFAULT 0,
  day_type     TEXT CHECK (day_type IN ('normal','pre_holiday','holiday')),
  start_min    INT NOT NULL CHECK (start_min BETWEEN 0 AND 1440),
  end_min      INT NOT NULL CHECK (end_min BETWEEN 0 AND 1440),
  date_from    DATE,
  date_to      DATE
);
CREATE INDEX rule_time_window_rule_idx ON rule_time_window (rule_id);

-- Rule applies-to: the geometric scope. A rule may bind to many
-- targets across kinds. offset_from/offset_to_meters express n-metre
-- buffers (negative = before, positive = after) along linear/point
-- features.
CREATE TABLE rule_applies_to (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  rule_id            UUID NOT NULL REFERENCES rule(id) ON DELETE CASCADE,
  target_kind        TEXT NOT NULL CHECK (target_kind IN ('road_segment','zone','parking_area','poi')),
  target_id          UUID NOT NULL,
  offset_from_meters DOUBLE PRECISION NOT NULL DEFAULT 0,
  offset_to_meters   DOUBLE PRECISION NOT NULL DEFAULT 0
);
CREATE INDEX rule_applies_to_rule_idx ON rule_applies_to (rule_id);
CREATE INDEX rule_applies_to_target_idx ON rule_applies_to (target_kind, target_id);

-- Per-country/region holiday calendar. Used by the engine to map a
-- date to {normal, pre_holiday, holiday}. Pre-populate per year.
CREATE TABLE holiday (
  country  TEXT NOT NULL,
  region   TEXT,
  on_date  DATE NOT NULL,
  name     TEXT NOT NULL,
  PRIMARY KEY (country, region, on_date)
);
