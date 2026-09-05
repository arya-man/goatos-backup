-- +goose Up
-- +goose NO TRANSACTION
-- Read-path indexes for the Weight Analytics demographics tabs. These match
-- the query's normalized RFID grain and whole-shed observation window; they do
-- not change or backfill weighing data.
CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_observations_demo_tag_window_idx
  ON public.weighing_observations (
    tenant_id,
    lower(btrim(scanned_identifier)),
    accepted_at DESC,
    observation_id DESC
  )
  INCLUDE (campaign_shed_id, weight_kg, verification_status)
  WHERE btrim(scanned_identifier) <> '' AND verification_status <> 'rejected';

CREATE INDEX CONCURRENTLY IF NOT EXISTS goat_identifiers_demo_tag_lookup_idx
  ON public.goat_identifiers (
    tenant_id,
    lower(btrim(identifier_value)),
    created_at DESC
  )
  INCLUDE (goat_id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_shed_observations_demo_window_idx
  ON public.weighing_shed_observations (
    tenant_id,
    accepted_at DESC,
    campaign_shed_id,
    shed_observation_id DESC
  )
  INCLUDE (average_weight_kg, animal_count, verification_status)
  WHERE withdrawn_at IS NULL AND verification_status <> 'rejected';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_shed_observations_demo_window_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.goat_identifiers_demo_tag_lookup_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_observations_demo_tag_window_idx;
