-- +goose Up
ALTER TABLE public.workforce_positions
  ADD COLUMN IF NOT EXISTS vaccination_daily_animal_cap integer;

ALTER TABLE public.workforce_positions
  DROP CONSTRAINT IF EXISTS workforce_positions_vaccination_daily_animal_cap_check,
  ADD CONSTRAINT workforce_positions_vaccination_daily_animal_cap_check
    CHECK (vaccination_daily_animal_cap IS NULL OR vaccination_daily_animal_cap BETWEEN 1 AND 100000);

COMMENT ON COLUMN public.workforce_positions.vaccination_daily_animal_cap IS
  'Optional HRMS-authored vaccination animal capacity for this operator seat. Null means use vaccination_capacity_config.max_per_day.';

-- +goose Down
ALTER TABLE public.workforce_positions
  DROP CONSTRAINT IF EXISTS workforce_positions_vaccination_daily_animal_cap_check;

ALTER TABLE public.workforce_positions
  DROP COLUMN IF EXISTS vaccination_daily_animal_cap;
