-- +goose Up
-- +goose NO TRANSACTION
-- seed-fixture-guard:ignore: operational Weighing planner identity only; no Vaccination HRMS seed contract change
--
-- projection-review: membership=open weighing_campaign_sheds rows plus their active
-- shed_partitions catalog match when a legacy alias row represents a partition; group_key=
-- (tenant_id,campaign_id,location_id,partition_label) and (tenant_id,park_id,start_business_date,
-- location_id,partition_label); join_cardinality=shed_partitions is a bounded 1:N catalog expansion
-- used only to canonicalize legacy aliases before duplicate checks and unique indexes are created;
-- pagination=none, this migration rewrites/validates the whole relation in one deploy step; scope=
-- every tenant, but only open weighing buckets affect the open-date uniqueness guard.
--
-- Partitioned sheds are operational sheds. A weighing bucket is identified by
-- (physical shed, partition_label), with NULL/empty partition_label reserved for
-- truly unpartitioned sheds. The old indexes keyed only location_id, so choosing
-- Castro - 1 occupied all of Castro and same-campaign edits could not carry two
-- partitions of one physical shed.
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_campaign_sheds_campaign_location_uidx;
DROP INDEX CONCURRENTLY IF EXISTS public.uq_weighing_open_shed_per_park_date_v2;
DROP INDEX CONCURRENTLY IF EXISTS public.uq_weighing_open_shed_per_park_date;
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_campaign_sheds_open_date_v2_idx;

ALTER TABLE public.weighing_campaign_sheds
  ADD COLUMN IF NOT EXISTS partition_label text;

WITH alias_matches AS (
  SELECT
    wcs.campaign_shed_id,
    parent.location_id AS canonical_location_id,
    parent.name AS parent_shed_name,
    sp.partition_label
  FROM public.weighing_campaign_sheds wcs
  JOIN public.locations alias
    ON alias.tenant_id=wcs.tenant_id
   AND alias.location_id=wcs.location_id
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
  WHERE (
      lower(alias.name)=lower(concat_ws(' - ', parent.name, NULLIF(BTRIM(sp.partition_label), '')))
      OR lower(alias.name)=lower(concat_ws(' ', parent.name, NULLIF(BTRIM(sp.partition_label), '')))
    )
    AND wcs.status NOT IN ('canceled', 'closed', 'completed')
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
  -- projection-review: membership=open weighing_campaign_sheds rows joined to active
  -- shed_partitions for parent-shed rows plus duplicate scans over weighing bucket identities;
  -- group_key=(tenant_id,campaign_id,location_id,partition_label) and
  -- (tenant_id,park_id,start_business_date,location_id,partition_label); join_cardinality=
  -- shed_partitions is a bounded 1:N catalog expansion used only for a count assertion before
  -- uniqueness is enforced; pagination=none, migration validation scans complete tables; scope=
  -- all tenants with open rows only for the open-task status matrix.
  SELECT COUNT(*) INTO blocked_count
  FROM public.weighing_campaign_sheds wcs
  JOIN public.shed_partitions sp
    ON sp.tenant_id=wcs.tenant_id
   AND sp.shed_id=wcs.location_id
   AND sp.status='active'
  WHERE wcs.status NOT IN ('canceled', 'closed', 'completed')
    AND NULLIF(BTRIM(COALESCE(wcs.partition_label, '')), '') IS NULL;

  IF blocked_count > 0 THEN
    RAISE EXCEPTION 'weighing partition migration blocked: % open parent-shed rows without partition_label remain', blocked_count;
  END IF;

  SELECT COUNT(*) INTO blocked_count
  FROM (
    SELECT tenant_id, campaign_id, location_id, COALESCE(partition_label, ''), COUNT(*)
    FROM public.weighing_campaign_sheds
    GROUP BY tenant_id, campaign_id, location_id, COALESCE(partition_label, '')
    HAVING COUNT(*) > 1
  ) dupes;
  IF blocked_count > 0 THEN
    RAISE EXCEPTION 'weighing partition migration blocked: % duplicate campaign operational buckets remain', blocked_count;
  END IF;

  SELECT COUNT(*) INTO blocked_count
  FROM (
    SELECT tenant_id, park_id, start_business_date, location_id, COALESCE(partition_label, ''), COUNT(*)
    FROM public.weighing_campaign_sheds
    WHERE status NOT IN ('canceled', 'closed', 'completed')
    GROUP BY tenant_id, park_id, start_business_date, location_id, COALESCE(partition_label, '')
    HAVING COUNT(*) > 1
  ) dupes;
  IF blocked_count > 0 THEN
    RAISE EXCEPTION 'weighing partition migration blocked: % duplicate open operational buckets remain', blocked_count;
  END IF;
END
$$;
-- +goose StatementEnd

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS weighing_campaign_sheds_campaign_location_partition_uidx
  ON public.weighing_campaign_sheds (tenant_id, campaign_id, location_id, COALESCE(partition_label, ''));

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_weighing_open_shed_partition_per_park_date
  ON public.weighing_campaign_sheds (tenant_id, park_id, start_business_date, location_id, COALESCE(partition_label, ''))
  WHERE status NOT IN ('canceled', 'closed', 'completed');

CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_campaign_sheds_open_date_partition_idx
  ON public.weighing_campaign_sheds (tenant_id, start_business_date, location_id, COALESCE(partition_label, ''))
  INCLUDE (campaign_id, park_id, operator_user_id, weighing_category, status)
  WHERE status NOT IN ('canceled', 'closed', 'completed');

DROP INDEX CONCURRENTLY IF EXISTS public.weighing_campaign_sheds_campaign_location_uidx;
DROP INDEX CONCURRENTLY IF EXISTS public.uq_weighing_open_shed_per_park_date_v2;
DROP INDEX CONCURRENTLY IF EXISTS public.uq_weighing_open_shed_per_park_date;
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_campaign_sheds_open_date_v2_idx;

-- +goose Down
-- +goose NO TRANSACTION
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS weighing_campaign_sheds_campaign_location_uidx
  ON public.weighing_campaign_sheds (tenant_id, campaign_id, location_id);

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_weighing_open_shed_per_park_date_v2
  ON public.weighing_campaign_sheds (tenant_id, park_id, start_business_date, location_id)
  WHERE status NOT IN ('canceled', 'closed', 'completed');

CREATE INDEX CONCURRENTLY IF NOT EXISTS weighing_campaign_sheds_open_date_v2_idx
  ON public.weighing_campaign_sheds (tenant_id, start_business_date, location_id)
  INCLUDE (campaign_id, park_id, operator_user_id, weighing_category, status)
  WHERE status NOT IN ('canceled', 'closed', 'completed');

DROP INDEX CONCURRENTLY IF EXISTS public.weighing_campaign_sheds_open_date_partition_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.uq_weighing_open_shed_partition_per_park_date;
DROP INDEX CONCURRENTLY IF EXISTS public.weighing_campaign_sheds_campaign_location_partition_uidx;
