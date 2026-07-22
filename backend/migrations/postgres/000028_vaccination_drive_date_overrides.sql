-- +goose Up
CREATE TABLE IF NOT EXISTS public.vaccination_drive_date_overrides (
  override_id uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id uuid NOT NULL,
  park_id uuid NOT NULL,
  vaccine_code text NOT NULL,
  original_drive_date date NOT NULL,
  override_date date NOT NULL,
  reason text NOT NULL,
  created_by uuid NOT NULL,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  canceled_at timestamp with time zone,
  canceled_by uuid,
  cancel_reason text,
  CONSTRAINT vaccination_drive_date_overrides_pkey PRIMARY KEY (override_id),
  CONSTRAINT vaccination_drive_date_overrides_vaccine_code_not_blank CHECK (btrim(vaccine_code) <> ''),
  CONSTRAINT vaccination_drive_date_overrides_reason_not_blank CHECK (btrim(reason) <> ''),
  CONSTRAINT vaccination_drive_date_overrides_postpone_check CHECK (override_date > original_drive_date),
  CONSTRAINT vaccination_drive_date_overrides_tenant_park_fk FOREIGN KEY (tenant_id, park_id) REFERENCES public.locations(tenant_id, location_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS vaccination_drive_date_overrides_active_uq
  ON public.vaccination_drive_date_overrides (tenant_id, park_id, (lower(btrim(vaccine_code))), original_drive_date)
  WHERE canceled_at IS NULL;

CREATE INDEX IF NOT EXISTS vaccination_drive_date_overrides_lookup_idx
  ON public.vaccination_drive_date_overrides (tenant_id, park_id, original_drive_date, (lower(btrim(vaccine_code))))
  WHERE canceled_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS public.vaccination_drive_date_overrides;
