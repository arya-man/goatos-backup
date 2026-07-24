-- +goose Up
-- Date-scoped vaccination operator capacity exceptions. The normal cap remains
-- on workforce_positions/vaccination_capacity_config; this table is for explicit
-- operational exceptions such as the one-time CPT seed catch-up day.
-- seed-fixture-guard:ignore: operational date-scoped scheduler override table;
-- rows are created only from an explicit operator-roster contract block, not
-- authored as raw HRMS/vaccination source rows in the full fixture.

CREATE TABLE IF NOT EXISTS public.vaccination_operator_capacity_overrides (
  tenant_id uuid NOT NULL,
  park_id uuid NOT NULL,
  operator_id uuid NOT NULL,
  capacity_date date NOT NULL,
  max_animals integer NOT NULL,
  reason text NOT NULL,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT vaccination_operator_capacity_overrides_pkey
    PRIMARY KEY (tenant_id, park_id, operator_id, capacity_date),
  CONSTRAINT vaccination_operator_capacity_overrides_max_check CHECK (max_animals >= 1),
  CONSTRAINT vaccination_operator_capacity_overrides_reason_not_blank CHECK (btrim(reason) <> ''),
  CONSTRAINT vaccination_operator_capacity_overrides_park_fk
    FOREIGN KEY (park_id) REFERENCES public.locations (location_id),
  CONSTRAINT vaccination_operator_capacity_overrides_operator_fk
    FOREIGN KEY (operator_id) REFERENCES public.workforce_members (workforce_member_id)
);

CREATE INDEX IF NOT EXISTS vaccination_operator_capacity_overrides_lookup_idx
  ON public.vaccination_operator_capacity_overrides (tenant_id, park_id, capacity_date, operator_id);

COMMENT ON TABLE public.vaccination_operator_capacity_overrides IS
  'Explicit date-scoped vaccination operator animal-cap exceptions. Normal caps stay on HRMS positions; planner reads this table only for the matching operator/date.';

-- +goose Down
DROP TABLE IF EXISTS public.vaccination_operator_capacity_overrides;
