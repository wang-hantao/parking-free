-- 0007_permit_kind_and_seasonal.sql
--
-- Two related semantic upgrades:
--
-- 1. required_permit_kind on rule (2.2)
--    A NeedsPermit=true rule today is satisfied by ANY valid permit
--    on the plate. That's wrong for disabled-only spots (needs a
--    PermitDisabled specifically, not a PermitResidential). Adding a
--    nullable column on rule lets us say "this rule needs THIS kind
--    of permit". NULL preserves the old any-permit semantics for
--    rules that genuinely accept any permit.
--
-- 2. seasonal month/day on rule_time_window (Seasonal date support)
--    LTF features for pmotorcykel encode rule windows that vary by
--    season (e.g. no street cleaning June 15 – August 15). The
--    schema's date_from/date_to columns hold absolute calendar
--    dates, which would require re-ingest every year. Instead, four
--    new month/day columns encode a recurring annual range that the
--    engine can match against the year-of-query. Cross-year ranges
--    (Aug 16 – June 14, i.e. "everything except summer") work
--    naturally as start > end.
--
-- Both additions are nullable and backward-compatible: existing
-- rules and time-windows keep working unchanged.

ALTER TABLE rule ADD COLUMN IF NOT EXISTS required_permit_kind TEXT;

ALTER TABLE rule_time_window ADD COLUMN IF NOT EXISTS start_month SMALLINT;
ALTER TABLE rule_time_window ADD COLUMN IF NOT EXISTS start_day   SMALLINT;
ALTER TABLE rule_time_window ADD COLUMN IF NOT EXISTS end_month   SMALLINT;
ALTER TABLE rule_time_window ADD COLUMN IF NOT EXISTS end_day     SMALLINT;
