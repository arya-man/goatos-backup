-- +goose Up
-- seed-fixture-guard:ignore: updates the runtime vaccination planner shot-cap override only; no HRMS source rows, fixture inputs, or import contract change
-- User-confirmed vaccination drive policy: a same-day compatible session may
-- include up to 3 vaccines for one animal. Published protocol versions are
-- immutable, so do not rewrite stored rule_dsl JSON here. The kernel/sweeper
-- already gives vaccination_capacity_config.max_shots_per_animal_per_drive
-- precedence over the DSL value; set the tenant override so existing published
-- protocols enforce cap 3 after deploy.

UPDATE public.vaccination_capacity_config
SET max_shots_per_animal_per_drive = 3,
    updated_at = now(),
    row_version = row_version + 1
WHERE max_shots_per_animal_per_drive IS DISTINCT FROM 3;

-- +goose Down
UPDATE public.vaccination_capacity_config
SET max_shots_per_animal_per_drive = NULL,
    updated_at = now(),
    row_version = row_version + 1
WHERE max_shots_per_animal_per_drive = 3;
