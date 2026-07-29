-- +goose Up
ALTER TABLE public.weighing_shed_observations
  ADD COLUMN IF NOT EXISTS animal_count integer;

UPDATE public.weighing_shed_observations
SET animal_count = 1
WHERE animal_count IS NULL;

ALTER TABLE public.weighing_shed_observations
  ALTER COLUMN animal_count SET NOT NULL;

ALTER TABLE public.weighing_shed_observations
  DROP CONSTRAINT IF EXISTS weighing_shed_observations_animal_count_check;

ALTER TABLE public.weighing_shed_observations
  ADD CONSTRAINT weighing_shed_observations_animal_count_check
  CHECK (animal_count > 0);

-- +goose Down
ALTER TABLE public.weighing_shed_observations
  DROP CONSTRAINT IF EXISTS weighing_shed_observations_animal_count_check;

ALTER TABLE public.weighing_shed_observations
  DROP COLUMN IF EXISTS animal_count;
