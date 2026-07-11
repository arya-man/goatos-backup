-- +goose Up
-- Precomputed vaccination eligibility rollup for FAST, million-scale config impact preview / planning
-- aggregates ONLY. This table exists so the Config "Preview impact" button (and any planning aggregate)
-- can read simple top-level numbers WITHOUT scanning the goats table on the UI request path.
--
-- READ-ONLY for the UI + impact-preview endpoint: they may only SELECT/aggregate here. The ONLY writer
-- is the backend projector/recompute code (cmd/vaccination-eligibility-rollup-recompute today; an
-- event-driven projector later). Never write this table from a request handler.
--
-- Grain: one row per (tenant, park, shed, species, management_stage, sex, breed, health_status,
-- usable_for_vaccination). `usable_for_vaccination` encodes the clinical + location eligibility gate
-- (alive, healthy, location usable, not quarantine/ICU) so the preview computes eligible_animals as
-- SUM(animal_count) WHERE usable_for_vaccination. The time-relative warmup window is intentionally NOT
-- part of the grain: the config preview is an aggregate estimate; per-animal warmup/next-due timing is
-- resolved later by the planner/sweeper after publish.
CREATE TABLE vaccination_eligibility_rollups (
  rollup_id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id             uuid NOT NULL REFERENCES tenants(tenant_id),
  park_id               uuid,
  shed_id               uuid,
  species               text NOT NULL DEFAULT '',
  management_stage      text NOT NULL DEFAULT '',
  sex                   text NOT NULL DEFAULT '',
  breed                 text NOT NULL DEFAULT '',
  health_status         text NOT NULL DEFAULT '',
  usable_for_vaccination boolean NOT NULL,
  animal_count          bigint NOT NULL,
  -- Provenance: source_revision is a monotonic recompute stamp (epoch ms of the recompute run);
  -- recomputed_at is when this grain bucket was last rebuilt; updated_at tracks the row write.
  source_revision       bigint NOT NULL DEFAULT 0,
  recomputed_at         timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT vaccination_eligibility_rollups_count_check CHECK (animal_count >= 0)
);

COMMENT ON TABLE vaccination_eligibility_rollups IS
  'Read model. Precomputed alive-animal eligibility counts per grain for FAST config impact preview / planning aggregates only. UI + impact-preview endpoint are read-only; only the backend projector/recompute writes it. Not a source of truth for per-animal state.';
COMMENT ON COLUMN vaccination_eligibility_rollups.usable_for_vaccination IS
  'Derived eligibility gate: alive + healthy (not sick/under_treatment/quarantine/icu) + location usable_for_vaccination + not quarantine/ICU. eligible_animals = SUM(animal_count) WHERE usable_for_vaccination.';
COMMENT ON COLUMN vaccination_eligibility_rollups.source_revision IS
  'Monotonic recompute stamp (epoch ms) of the run that produced this grain bucket. Lets preview report read-model freshness without scanning source tables.';

-- Grain uniqueness guard. park_id/shed_id are nullable (a goat may lack a shed/park), so COALESCE to a
-- zero UUID keeps the grain key total. The full recompute is delete-then-insert per tenant, so this is a
-- correctness guard, not an upsert target.
CREATE UNIQUE INDEX vaccination_eligibility_rollups_grain_uidx
  ON vaccination_eligibility_rollups (
    tenant_id,
    COALESCE(park_id, '00000000-0000-0000-0000-000000000000'::uuid),
    COALESCE(shed_id, '00000000-0000-0000-0000-000000000000'::uuid),
    species, management_stage, sex, breed, health_status, usable_for_vaccination
  );

-- Impact-preview read path: aggregate SUM(animal_count) + COUNT(DISTINCT shed) scoped by tenant, the
-- usable gate, an optional park, and optional stage/sex/breed/health filters. Leading (tenant_id,
-- usable_for_vaccination) serves the common "eligible" aggregate; the trailing dims cover filtered
-- previews without touching goats.
CREATE INDEX vaccination_eligibility_rollups_preview_idx
  ON vaccination_eligibility_rollups (
    tenant_id, usable_for_vaccination, park_id, management_stage, sex, breed, health_status
  );

-- +goose Down
DROP TABLE IF EXISTS vaccination_eligibility_rollups;
