-- +goose Up
-- FEED & WATER REMOVAL CUTOFF IS CONFIG, NOT CODE (maintainer decision 2026-09-07).
--
-- The feed & water removal precondition (docs/decisions/feed-water-removal-precondition.md)
-- turns on ONE wall-clock boundary: the evening's removal window opens at a fixed IST time.
-- Before it, tomorrow can still be planned (tonight's removal can still be staffed); at or
-- after it, the earliest plannable day is the day after tomorrow, and the removal card
-- surfaces on the operator's list. Until now that time was the literal 20:00 in FOUR places
-- (weighing Go + SQL, PC Care Go + SQL) and a fifth on the phone, so changing the farm's
-- evening meant a code change and a release on two surfaces.
--
-- This table is the single source. One row per tenant: the rule is a property of the FARM's
-- evening ("one evening, one crew"), shared by weighing and deworming, never per park or
-- per module -- two modules disagreeing about when the evening starts would offer a date
-- one of them refuses.
--
-- TIME SEMANTICS -- INDIA BUSINESS CALENDAR (Asia/Kolkata) LOCAL wall-clock time, the
-- feed_schedule_config shape: `time` WITHOUT time zone, no offset stored. "20:00" means eight
-- in the evening at the park on whatever date the reader is scheduling. Readers combine
-- (business date, this time, Asia/Kolkata) to get an instant; do not add a UTC column.
--
-- NO SILENT DEFAULT: the readers fail closed (feedwaterremoval/ports.ErrCutoffNotConfigured)
-- when a tenant has no row, rather than inventing an evening. Every existing tenant is seeded
-- below with the value the code carried (20:00) so nothing changes on deploy. Tenants are
-- created only by the baseline (no seed command inserts tenants), so there is no seed path
-- to couple; a future tenant-creation path must insert this row too.
CREATE TABLE IF NOT EXISTS feed_water_removal_config (
    tenant_id   uuid PRIMARY KEY REFERENCES tenants (tenant_id),
    cutoff_time time NOT NULL,
    updated_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

COMMENT ON TABLE feed_water_removal_config IS
  'Per-tenant feed & water removal evening cutoff (maintainer decision 2026-09-07). One LOCAL Asia/Kolkata wall-clock time shared by weighing and PC Care deworming: before it, tomorrow is plannable; at/after it, the earliest plannable day is the day after tomorrow and the removal card becomes visible. Readers fail closed when the row is missing.';
COMMENT ON COLUMN feed_water_removal_config.cutoff_time IS
  'LOCAL Asia/Kolkata wall-clock time the removal evening opens. No UTC offset is stored: a recurring business-calendar rule, not an instant. Combine with the business date in Asia/Kolkata to get the instant.';

-- Seed every existing tenant with the value the code carried until now, so deploying this
-- migration changes no offered date and no card visibility. Read-only over tenants.
-- seed-migration-guard:ignore owner=manohark issue=feed-water-removal-cutoff-config reason=read-only-select-over-tenants-to-seed-the-new-config-row expiry=2026-12-31
INSERT INTO feed_water_removal_config (tenant_id, cutoff_time)
SELECT tenant_id, TIME '20:00'
FROM tenants
ON CONFLICT (tenant_id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS feed_water_removal_config;
