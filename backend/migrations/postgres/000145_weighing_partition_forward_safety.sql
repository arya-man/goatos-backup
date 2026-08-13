-- +goose Up
-- +goose NO TRANSACTION
-- seed-fixture-guard:ignore: weighing partition forward repair only; no seed contract change
--
-- Forward-only companion to 000141. 000141 is already in released migration history, so it must
-- remain byte-stable for checksum validation; extra idempotent safety lives here.

ALTER TABLE public.weighing_campaign_sheds
  ADD COLUMN IF NOT EXISTS partition_label text;

WITH alias_matches AS (
  SELECT
    wcs.campaign_shed_id,
    parent.location_id AS canonical_location_id,
    sp.partition_label
  FROM public.weighing_campaign_sheds wcs
  JOIN public.locations alias
    ON alias.tenant_id = wcs.tenant_id
   AND alias.location_id = wcs.location_id
   AND alias.location_type = 'shed'
   AND alias.status = 'inactive'
  JOIN public.locations parent
    ON parent.tenant_id = alias.tenant_id
   AND parent.parent_location_id = alias.parent_location_id
   AND parent.location_type = 'shed'
   AND parent.status = 'active'
   AND parent.retired_at IS NULL
  JOIN public.shed_partitions sp
    ON sp.tenant_id = parent.tenant_id
   AND sp.shed_id = parent.location_id
   AND sp.status = 'active'
  WHERE lower(alias.name) = lower(concat_ws(' ', parent.name, NULLIF(BTRIM(sp.partition_label), '')))
    AND wcs.status NOT IN ('canceled', 'closed', 'completed')
)
UPDATE public.weighing_campaign_sheds wcs
SET location_id = am.canonical_location_id,
    partition_label = am.partition_label,
    updated_at = now()
FROM alias_matches am
WHERE wcs.campaign_shed_id = am.campaign_shed_id
  AND (
    wcs.location_id IS DISTINCT FROM am.canonical_location_id
    OR COALESCE(wcs.partition_label, '') IS DISTINCT FROM COALESCE(am.partition_label, '')
  );

-- +goose Down
-- +goose NO TRANSACTION
-- Intentionally no-op: 000141 owns the partition-aware weighing indexes and data shape.
