-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
  IF to_regclass('public.weighing_shed_observations') IS NOT NULL THEN
    ALTER TABLE public.weighing_shed_observations ADD COLUMN IF NOT EXISTS average_weight_kg numeric(8,3);
    UPDATE public.weighing_shed_observations SET average_weight_kg = weight_kg WHERE average_weight_kg IS NULL;
    ALTER TABLE public.weighing_shed_observations ALTER COLUMN average_weight_kg SET NOT NULL;
    ALTER TABLE public.weighing_shed_observations DROP CONSTRAINT IF EXISTS weighing_shed_observations_average_weight_check;
    ALTER TABLE public.weighing_shed_observations
      ADD CONSTRAINT weighing_shed_observations_average_weight_check CHECK (average_weight_kg > 0);

    CREATE TABLE IF NOT EXISTS public.weighing_shed_observation_proofs (
      shed_observation_id uuid NOT NULL REFERENCES public.weighing_shed_observations(shed_observation_id) ON DELETE CASCADE,
      tenant_id uuid NOT NULL REFERENCES public.tenants(tenant_id),
      proof_artifact_id uuid NOT NULL REFERENCES public.proof_artifacts(proof_id),
      proof_position smallint NOT NULL CHECK (proof_position BETWEEN 1 AND 5),
      created_at timestamptz NOT NULL DEFAULT now(),
      CONSTRAINT weighing_shed_observation_proofs_pk PRIMARY KEY (shed_observation_id, proof_position),
      CONSTRAINT weighing_shed_observation_proofs_unique_artifact UNIQUE (shed_observation_id, proof_artifact_id)
    );

    INSERT INTO public.weighing_shed_observation_proofs (
      shed_observation_id,
      tenant_id,
      proof_artifact_id,
      proof_position
    )
    SELECT shed_observation_id, tenant_id, proof_artifact_id, 1
    FROM public.weighing_shed_observations
    ON CONFLICT (shed_observation_id, proof_position) DO NOTHING;

    CREATE INDEX IF NOT EXISTS weighing_shed_observation_proofs_tenant_observation_idx
      ON public.weighing_shed_observation_proofs (tenant_id, shed_observation_id, proof_position);
  END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
  IF to_regclass('public.weighing_shed_observations') IS NOT NULL THEN
    DROP TABLE IF EXISTS public.weighing_shed_observation_proofs;
    ALTER TABLE public.weighing_shed_observations DROP CONSTRAINT IF EXISTS weighing_shed_observations_average_weight_check;
    ALTER TABLE public.weighing_shed_observations DROP COLUMN IF EXISTS average_weight_kg;
  END IF;
END $$;
-- +goose StatementEnd
