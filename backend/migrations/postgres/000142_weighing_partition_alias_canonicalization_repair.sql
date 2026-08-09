-- +goose Up
-- +goose NO TRANSACTION
-- seed-fixture-guard:ignore: Weighing operational-location repair only; no Vaccination HRMS seed contract change
--
-- Repair any open weighing buckets left behind by the first operational-identity migration when
-- legacy inactive location aliases used the numeric "Castro 2" convention rather than the canonical
-- "Castro - 2" display.

WITH alias_matches AS (
  SELECT
    wcs.campaign_shed_id,
    parent.location_id AS canonical_location_id,
    sp.partition_label
  FROM public.weighing_campaign_sheds wcs
  JOIN public.locations alias
    ON alias.tenant_id=wcs.tenant_id
   AND alias.location_id=wcs.location_id
   AND alias.location_type='shed'
   AND alias.status <> 'active'
  JOIN public.locations parent
    ON parent.tenant_id=alias.tenant_id
   AND parent.parent_location_id=alias.parent_location_id
   AND parent.location_type='shed'
   AND parent.status='active'
   AND parent.retired_at IS NULL
  JOIN public.shed_partitions sp
    ON sp.tenant_id=parent.tenant_id
   AND sp.shed_id=parent.location_id
   AND sp.status='active'
  WHERE wcs.status NOT IN ('canceled', 'closed', 'completed')
    AND (
      lower(alias.name)=lower(concat_ws(' - ', parent.name, NULLIF(BTRIM(sp.partition_label), '')))
      OR lower(alias.name)=lower(concat_ws(' ', parent.name, NULLIF(BTRIM(sp.partition_label), '')))
    )
)
UPDATE public.weighing_campaign_sheds wcs
SET location_id=am.canonical_location_id,
    partition_label=am.partition_label,
    updated_at=now()
FROM alias_matches am
WHERE wcs.campaign_shed_id=am.campaign_shed_id;

-- +goose StatementBegin
DO $$
DECLARE
  blocked_count integer;
BEGIN
  SELECT COUNT(*) INTO blocked_count
  FROM public.weighing_campaign_sheds wcs
  JOIN public.locations loc
    ON loc.tenant_id=wcs.tenant_id
   AND loc.location_id=wcs.location_id
   AND loc.location_type='shed'
   AND loc.status <> 'active'
  WHERE wcs.status NOT IN ('canceled', 'closed', 'completed');

  IF blocked_count > 0 THEN
    RAISE EXCEPTION 'weighing partition alias repair blocked: % open inactive shed alias rows remain', blocked_count;
  END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose NO TRANSACTION
-- Irreversible data repair.
