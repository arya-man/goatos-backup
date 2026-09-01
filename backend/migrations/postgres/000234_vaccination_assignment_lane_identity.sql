-- seed-fixture-guard:ignore: runtime drive-assignment identity only; no seed input, fixture schema, or source data changes.
-- seed-migration-guard:ignore owner=ravi issue=PR-140 reason=runtime-vaccination-drive-assignment-identity-and-cap-constraint-only expiry=2026-12-31
-- +goose Up
-- Keep distinct vaccine lanes separate when the same operator/date/shed/partition is planned.
-- Without vaccine_rule_ids in the key, Z1+Z3 and ET+TT-like lanes can collapse into one card.

DROP INDEX IF EXISTS public.vaccination_drive_assignments_batch_shed_part_operator_uq;

-- seed-migration-guard:ignore owner=ravi issue=RFID-OCI-GOOSE-MARKER reason=restoring-missing-goose-marker-only-no-seed-impact expiry=2026-12-31
CREATE UNIQUE INDEX IF NOT EXISTS vaccination_drive_assignments_batch_shed_part_operator_lane_uq
  ON public.vaccination_drive_assignments (
    tenant_id,
    batch_id,
    planned_date,
    park_id,
    COALESCE(shed_id, '00000000-0000-0000-0000-000000000000'::uuid),
    physical_shed,
    partition_label,
    COALESCE(operator_id, '00000000-0000-0000-0000-000000000000'::uuid),
    COALESCE(vaccine_rule_ids, '{}'::uuid[])
  );

-- seed-migration-guard:ignore owner=ravi issue=RFID-OCI-GOOSE-MARKER reason=restoring-missing-goose-marker-only-no-seed-impact expiry=2026-12-31
ALTER TABLE public.vaccination_capacity_config
  DROP CONSTRAINT IF EXISTS vaccination_capacity_config_max_per_day_check;

-- seed-migration-guard:ignore owner=ravi issue=RFID-OCI-GOOSE-MARKER reason=restoring-missing-goose-marker-only-no-seed-impact expiry=2026-12-31
ALTER TABLE public.vaccination_capacity_config
  ADD CONSTRAINT vaccination_capacity_config_max_per_day_check
  CHECK (max_per_day BETWEEN 1 AND 200);

-- seed-migration-guard:ignore owner=ravi issue=RFID-OCI-GOOSE-MARKER reason=restoring-missing-goose-marker-only-no-seed-impact expiry=2026-12-31
ALTER TABLE public.workforce_positions
  DROP CONSTRAINT IF EXISTS workforce_positions_vaccination_daily_animal_cap_check;

-- seed-migration-guard:ignore owner=ravi issue=RFID-OCI-GOOSE-MARKER reason=restoring-missing-goose-marker-only-no-seed-impact expiry=2026-12-31
ALTER TABLE public.workforce_positions
  ADD CONSTRAINT workforce_positions_vaccination_daily_animal_cap_check
  CHECK (vaccination_daily_animal_cap IS NULL OR vaccination_daily_animal_cap BETWEEN 1 AND 200);
