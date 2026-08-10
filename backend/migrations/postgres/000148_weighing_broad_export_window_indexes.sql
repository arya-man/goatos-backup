-- +goose Up
-- Broad CEO weighing export reads a bounded accepted_at window across the caller's
-- park scope. These indexes let Postgres enter by tenant+time before joining to
-- campaign/shed/proof dimensions, instead of walking campaign-oriented history.
CREATE INDEX IF NOT EXISTS weighing_observations_export_window_idx
  ON public.weighing_observations (tenant_id, accepted_at DESC, campaign_shed_id)
  INCLUDE (campaign_id, proof_artifact_id, scanned_identifier, weight_kg, verification_status, verified_at);

CREATE INDEX IF NOT EXISTS weighing_shed_observations_export_window_idx
  ON public.weighing_shed_observations (tenant_id, accepted_at DESC, campaign_shed_id)
  INCLUDE (campaign_id, shed_observation_id, proof_artifact_id, weight_kg, average_weight_kg, animal_count, verification_status, verified_at)
  WHERE withdrawn_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS public.weighing_shed_observations_export_window_idx;
DROP INDEX IF EXISTS public.weighing_observations_export_window_idx;
