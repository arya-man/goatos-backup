-- +goose Up
-- Persist the vaccination operator shift + N-active-operators-per-day default assignment config.
-- The admin config screen authors these rows, and the drive/obligation scheduler consumes them
-- when choosing the operator set for each business day.

CREATE TABLE IF NOT EXISTS public.vaccination_operator_assignment_config (
  tenant_id uuid NOT NULL,
  park_id uuid NOT NULL,
  active_operators_per_day integer NOT NULL DEFAULT 1,
  default_operator_id uuid NOT NULL,
  row_version bigint NOT NULL DEFAULT 1,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT vaccination_operator_assignment_config_pkey PRIMARY KEY (tenant_id, park_id),
  CONSTRAINT vaccination_operator_assignment_config_n_check
    CHECK (active_operators_per_day >= 1 AND active_operators_per_day <= 3),
  CONSTRAINT vaccination_operator_assignment_config_row_version_check
    CHECK (row_version >= 1),
  CONSTRAINT vaccination_operator_assignment_config_park_fk
    FOREIGN KEY (park_id) REFERENCES public.locations (location_id),
  CONSTRAINT vaccination_operator_assignment_config_default_operator_fk
    FOREIGN KEY (default_operator_id)
    REFERENCES public.workforce_members (workforce_member_id)
);

CREATE INDEX IF NOT EXISTS vaccination_operator_assignment_config_tenant_park_idx
  ON public.vaccination_operator_assignment_config (tenant_id, park_id);

CREATE TABLE IF NOT EXISTS public.vaccination_operator_shift_config (
  operator_id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  park_id uuid NOT NULL,
  shift_start_minute integer NOT NULL,
  shift_end_minute integer NOT NULL,
  shift_label text NOT NULL,
  week_off_weekday text,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT vaccination_operator_shift_config_pkey PRIMARY KEY (tenant_id, operator_id, park_id),
  CONSTRAINT vaccination_operator_shift_config_start_minute_check
    CHECK (shift_start_minute >= 0 AND shift_start_minute <= 1439),
  CONSTRAINT vaccination_operator_shift_config_end_minute_check
    CHECK (shift_end_minute >= 0 AND shift_end_minute <= 1439),
  CONSTRAINT vaccination_operator_shift_config_label_check
    CHECK (shift_label IN ('am', 'pm', 'rover')),
  CONSTRAINT vaccination_operator_shift_config_week_off_check
    CHECK (week_off_weekday IS NULL OR week_off_weekday IN
      ('monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday', 'sunday')),
  CONSTRAINT vaccination_operator_shift_config_park_fk
    FOREIGN KEY (park_id) REFERENCES public.locations (location_id),
  CONSTRAINT vaccination_operator_shift_config_operator_fk
    FOREIGN KEY (operator_id)
    REFERENCES public.workforce_members (workforce_member_id)
);

CREATE INDEX IF NOT EXISTS vaccination_operator_shift_config_tenant_park_idx
  ON public.vaccination_operator_shift_config (tenant_id, park_id);

COMMENT ON TABLE public.vaccination_operator_assignment_config IS
  'N active operators/day + CEO-set default operator per park. Consumed by the drive scheduler when planning daily operator assignment.';
COMMENT ON TABLE public.vaccination_operator_shift_config IS
  'Per-operator shift window (minutes-of-day) + week-off weekday, per park. Consumed by the drive scheduler when resolving default/PM/shift fallback.';

-- +goose Down
DROP TABLE IF EXISTS public.vaccination_operator_shift_config;
DROP TABLE IF EXISTS public.vaccination_operator_assignment_config;
