-- +goose Up
-- Canonical herd animals must have a real biological sex. Legacy rows with
-- blank/unknown sex must be corrected before they can enter accepted GoatOS
-- herd identity or vaccination matching.

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM goats
    WHERE sex IS NULL OR sex NOT IN ('female', 'male')
  ) THEN
    RAISE EXCEPTION 'goats contains missing or invalid sex; correct rows before applying 000135';
  END IF;
END $$;

ALTER TABLE goats DROP CONSTRAINT IF EXISTS goats_sex_check;
ALTER TABLE goats ALTER COLUMN sex SET NOT NULL;
ALTER TABLE goats ADD CONSTRAINT goats_sex_check CHECK (sex IN ('female', 'male'));

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM counts_current_snapshot_rows
    WHERE sex IS NOT NULL AND sex NOT IN ('female', 'male')
  ) THEN
    RAISE EXCEPTION 'counts_current_snapshot_rows contains invalid sex; correct source rows before applying 000135';
  END IF;
END $$;

ALTER TABLE counts_current_snapshot_rows DROP CONSTRAINT IF EXISTS counts_snapshot_sex_check;
ALTER TABLE counts_current_snapshot_rows
  ADD CONSTRAINT counts_snapshot_sex_check CHECK (sex IS NULL OR sex IN ('female', 'male'));

DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM mortality_events
    WHERE sex IS NOT NULL AND sex NOT IN ('female', 'male')
  ) THEN
    RAISE EXCEPTION 'mortality_events contains invalid sex; correct source rows before applying 000135';
  END IF;
END $$;

ALTER TABLE mortality_events DROP CONSTRAINT IF EXISTS mortality_events_sex_check;
ALTER TABLE mortality_events
  ADD CONSTRAINT mortality_events_sex_check CHECK (sex IS NULL OR sex IN ('female', 'male'));

-- +goose Down
-- Intentionally no-op. Unknown/blank canonical animal sex is permanently
-- forbidden after this migration. Rolling application code back must remain
-- compatible with the female/male-only schema instead of reopening unknown sex.
DO $$
BEGIN
  RAISE NOTICE '000135 down is intentionally no-op: unknown animal sex remains forbidden';
END $$;
