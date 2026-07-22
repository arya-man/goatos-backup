-- +goose Up
CREATE TABLE IF NOT EXISTS public.vaccination_drive_assignments (
  assignment_id uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id uuid NOT NULL,
  batch_id uuid NOT NULL,
  planned_date date NOT NULL,
  operator_id uuid,
  park_id uuid NOT NULL,
  shed_id uuid,
  physical_shed text NOT NULL,
  partition_label text NOT NULL DEFAULT 'whole',
  animal_count integer NOT NULL,
  capacity_status text NOT NULL DEFAULT 'within_cap',
  warnings jsonb NOT NULL DEFAULT '[]'::jsonb,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT vaccination_drive_assignments_pkey PRIMARY KEY (assignment_id),
  CONSTRAINT vaccination_drive_assignments_animals_check CHECK (animal_count >= 0),
  CONSTRAINT vaccination_drive_assignments_capacity_check CHECK (capacity_status = ANY (ARRAY['within_cap'::text, 'over_cap_required'::text, 'capacity_action'::text])),
  CONSTRAINT vaccination_drive_assignments_warnings_array_check CHECK (jsonb_typeof(warnings) = 'array'),
  CONSTRAINT vaccination_drive_assignments_tenant_batch_fk FOREIGN KEY (tenant_id, batch_id) REFERENCES public.obligation_batches(tenant_id, batch_id) ON DELETE CASCADE,
  CONSTRAINT vaccination_drive_assignments_tenant_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id),
  CONSTRAINT vaccination_drive_assignments_tenant_shed_fk FOREIGN KEY (tenant_id, shed_id) REFERENCES public.locations(tenant_id, location_id),
  CONSTRAINT vaccination_drive_assignments_operator_fk FOREIGN KEY (operator_id) REFERENCES public.workforce_members(workforce_member_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS vaccination_drive_assignments_batch_shed_part_uq
  ON public.vaccination_drive_assignments (tenant_id, batch_id, planned_date, park_id, COALESCE(shed_id, '00000000-0000-0000-0000-000000000000'::uuid), physical_shed, partition_label);

CREATE INDEX IF NOT EXISTS vaccination_drive_assignments_operator_day_idx
  ON public.vaccination_drive_assignments (tenant_id, operator_id, planned_date)
  WHERE operator_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS vaccination_drive_assignments_park_day_idx
  ON public.vaccination_drive_assignments (tenant_id, park_id, planned_date);

-- +goose Down
DROP TABLE IF EXISTS public.vaccination_drive_assignments;
