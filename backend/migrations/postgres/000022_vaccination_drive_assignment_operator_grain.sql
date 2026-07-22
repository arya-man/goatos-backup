-- +goose Up
DROP INDEX IF EXISTS public.vaccination_drive_assignments_batch_shed_part_uq;

CREATE UNIQUE INDEX IF NOT EXISTS vaccination_drive_assignments_batch_shed_part_operator_uq
  ON public.vaccination_drive_assignments (
    tenant_id,
    batch_id,
    planned_date,
    park_id,
    COALESCE(shed_id, '00000000-0000-0000-0000-000000000000'::uuid),
    physical_shed,
    partition_label,
    COALESCE(operator_id, '00000000-0000-0000-0000-000000000000'::uuid)
  );

-- +goose Down
DROP INDEX IF EXISTS public.vaccination_drive_assignments_batch_shed_part_operator_uq;

CREATE UNIQUE INDEX IF NOT EXISTS vaccination_drive_assignments_batch_shed_part_uq
  ON public.vaccination_drive_assignments (
    tenant_id,
    batch_id,
    planned_date,
    park_id,
    COALESCE(shed_id, '00000000-0000-0000-0000-000000000000'::uuid),
    physical_shed,
    partition_label
  );
