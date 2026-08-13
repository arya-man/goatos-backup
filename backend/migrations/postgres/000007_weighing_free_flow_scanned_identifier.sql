-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
  IF to_regclass('public.weighing_observations') IS NOT NULL THEN
    ALTER TABLE public.weighing_observations ADD COLUMN IF NOT EXISTS scanned_identifier text NOT NULL DEFAULT '';
    ALTER TABLE public.weighing_observations ALTER COLUMN animal_id DROP NOT NULL;
    ALTER TABLE public.weighing_observations DROP CONSTRAINT IF EXISTS weighing_observations_animal_or_identifier_check;
    ALTER TABLE public.weighing_observations
      ADD CONSTRAINT weighing_observations_animal_or_identifier_check
      CHECK (animal_id IS NOT NULL OR btrim(scanned_identifier) <> '');
    CREATE INDEX IF NOT EXISTS weighing_observations_campaign_scanned_identifier_idx
      ON public.weighing_observations (tenant_id, campaign_id, scanned_identifier, accepted_at DESC);
  END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
  IF to_regclass('public.weighing_observations') IS NOT NULL THEN
    DROP INDEX IF EXISTS public.weighing_observations_campaign_scanned_identifier_idx;
    ALTER TABLE public.weighing_observations DROP CONSTRAINT IF EXISTS weighing_observations_animal_or_identifier_check;
    DELETE FROM public.weighing_observations WHERE animal_id IS NULL;
    ALTER TABLE public.weighing_observations ALTER COLUMN animal_id SET NOT NULL;
    ALTER TABLE public.weighing_observations DROP COLUMN IF EXISTS scanned_identifier;
  END IF;
END $$;
-- +goose StatementEnd
