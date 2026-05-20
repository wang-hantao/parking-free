-- 0006_tariff_class.sql — attach a tariff class to each rule.
--
-- Stockholm pricing is not zone-driven but class-driven: every
-- PARKING_RATE string in LTF-Tolken starts with "taxa N:", and N is
-- one of a small set of stable Stockholm conventions. Two streets
-- with the same taxa class share identical pricing schedules. The
-- class definitions themselves live in code (internal/engine/tariffs.go);
-- this column is just the foreign key into that registry.
--
-- Nullable because non-LTF rule sources (and pre-1.2 ingest snapshots)
-- have no class to point at. Indexed only on non-null values to skip
-- the long tail of cleaning/forbid rules where pricing is irrelevant.

ALTER TABLE rule ADD COLUMN IF NOT EXISTS tariff_class_code TEXT;

CREATE INDEX IF NOT EXISTS rule_tariff_class_code_idx
    ON rule(tariff_class_code) WHERE tariff_class_code IS NOT NULL;
