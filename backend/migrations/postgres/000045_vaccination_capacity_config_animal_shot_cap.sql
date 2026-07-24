-- +goose Up
-- Editable animal shot-cap override on the tenant's vaccination_capacity_config row. NULL means "no
-- override, use the published rule_dsl drive_policy.max_shots_per_animal_per_drive / code default"
-- (domain.DefaultMaxShotsPerAnimalPerDrive). A non-null value is a tenant-wide admin override enforced
-- by the obligation sweeper's same-day shot cap and honored ahead of the DSL value. Lock-safe: a bare
-- ADD COLUMN with a NULL default takes only a brief metadata lock, no table rewrite/scan.
--
-- No seed companion: the column defaults NULL and the planner falls back to the published rule_dsl /
-- code default, so existing vaccination-config / HRMS seed correctly leaves it unset.
-- seed-fixture-guard:ignore: nullable shot-cap override column, default NULL falls back to rule_dsl/default; HRMS/config seed fixtures carry no value for it and need no companion update
-- seed-migration-guard:ignore owner=ravi issue=caps-editable reason=nullable-shot-cap-override-defaults-null-planner-falls-back-to-rule_dsl-so-seed-leaves-it-unset expiry=2026-10-31
ALTER TABLE public.vaccination_capacity_config
  ADD COLUMN IF NOT EXISTS max_shots_per_animal_per_drive integer
  CONSTRAINT vaccination_capacity_config_max_shots_check
    CHECK (max_shots_per_animal_per_drive IS NULL OR max_shots_per_animal_per_drive >= 1);

COMMENT ON COLUMN public.vaccination_capacity_config.max_shots_per_animal_per_drive IS
  'Admin-editable override of the same-day per-animal shot cap. NULL = fall back to the published rule_dsl drive_policy value / code default (domain.DefaultMaxShotsPerAnimalPerDrive).';

-- +goose Down
-- seed-migration-guard:ignore owner=ravi issue=caps-editable reason=drops-the-nullable-shot-cap-override-column-no-seed-impact expiry=2026-10-31
ALTER TABLE public.vaccination_capacity_config
  DROP COLUMN IF EXISTS max_shots_per_animal_per_drive;
