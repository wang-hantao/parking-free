-- 0004_session.sql
-- Operator catalog, permits, parking sessions, fines. The user-facing
-- and commercial subdomains.

CREATE TABLE IF NOT EXISTS operator (
  id   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('municipal','private','mvne')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (name)
);
CREATE OR REPLACE TRIGGER operator_touch
BEFORE UPDATE ON operator FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

CREATE TABLE IF NOT EXISTS operator_zone (
  id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  operator_id       UUID NOT NULL REFERENCES operator(id) ON DELETE CASCADE,
  external_zone_id  TEXT NOT NULL,
  maps_to_zone_id   UUID REFERENCES zone(id) ON DELETE SET NULL,
  deeplink_template TEXT,
  UNIQUE (operator_id, external_zone_id)
);
CREATE INDEX IF NOT EXISTS operator_zone_zone_idx ON operator_zone (maps_to_zone_id);

CREATE TABLE IF NOT EXISTS tariff (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  operator_zone_id UUID NOT NULL REFERENCES operator_zone(id) ON DELETE CASCADE,
  currency         CHAR(3) NOT NULL,
  rate_per_unit    NUMERIC(10,4) NOT NULL,
  time_unit_s      INT NOT NULL CHECK (time_unit_s > 0),
  max_session_cost NUMERIC(10,2),
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS permit (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  kind       TEXT NOT NULL CHECK (kind IN ('residential','disabled','electric','carpool','guest','nytto_a','nytto_b')),
  zone_id    UUID REFERENCES zone(id) ON DELETE SET NULL,
  plate      TEXT NOT NULL,
  holder_ref TEXT,
  valid_from TIMESTAMPTZ NOT NULL,
  valid_to   TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (plate, kind, zone_id, valid_from)
);
CREATE INDEX IF NOT EXISTS permit_plate_idx ON permit (plate);
CREATE INDEX IF NOT EXISTS permit_validity_idx ON permit (valid_from, valid_to);
CREATE OR REPLACE TRIGGER permit_touch
BEFORE UPDATE ON permit FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

CREATE TABLE IF NOT EXISTS parking_session (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  plate               TEXT NOT NULL,
  started_at          TIMESTAMPTZ NOT NULL,
  ended_at            TIMESTAMPTZ,
  position            GEOMETRY(Point, 4326) NOT NULL,
  zone_id             UUID REFERENCES zone(id) ON DELETE SET NULL,
  operator_id         UUID REFERENCES operator(id) ON DELETE SET NULL,
  external_session_id TEXT,
  cost_minor          INT,
  status              TEXT NOT NULL CHECK (status IN ('active','ended','expired'))
);
CREATE INDEX IF NOT EXISTS parking_session_plate_idx ON parking_session (plate);
CREATE INDEX IF NOT EXISTS parking_session_position_gix ON parking_session USING GIST (position);
CREATE INDEX IF NOT EXISTS parking_session_status_idx ON parking_session (status);

-- Cached rule evaluations: avoid re-walking the regulation graph for
-- every notification check. The next-change-timestamp tells the
-- scheduler when to re-evaluate.
CREATE TABLE IF NOT EXISTS rule_evaluation (
  session_id   UUID NOT NULL REFERENCES parking_session(id) ON DELETE CASCADE,
  rule_id      UUID NOT NULL REFERENCES rule(id) ON DELETE CASCADE,
  verdict      TEXT NOT NULL CHECK (verdict IN ('allow','forbid','restrict')),
  evaluated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at   TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (session_id, rule_id)
);
CREATE INDEX IF NOT EXISTS rule_evaluation_expires_idx ON rule_evaluation (expires_at);

CREATE TABLE IF NOT EXISTS fine_event (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id  UUID REFERENCES parking_session(id) ON DELETE SET NULL,
  ticket_no   TEXT NOT NULL,
  issuer      TEXT NOT NULL,
  issuer_kind TEXT NOT NULL CHECK (issuer_kind IN ('yellow','white')),
  amount_sek  INT NOT NULL,
  reason_code TEXT,
  issued_at   TIMESTAMPTZ NOT NULL,
  photo_urls  TEXT[] NOT NULL DEFAULT '{}',
  UNIQUE (issuer, ticket_no)
);
CREATE INDEX IF NOT EXISTS fine_event_session_idx ON fine_event (session_id);
CREATE INDEX IF NOT EXISTS fine_event_issued_idx ON fine_event (issued_at);

CREATE TABLE IF NOT EXISTS notification (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_ref      TEXT NOT NULL,
  channel       TEXT NOT NULL CHECK (channel IN ('push','sms','email')),
  scheduled_at  TIMESTAMPTZ NOT NULL,
  trigger       TEXT NOT NULL CHECK (trigger IN ('expiry','cleaning','max_stay','geofence_exit')),
  dispatched_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS notification_pending_idx ON notification (scheduled_at) WHERE dispatched_at IS NULL;
