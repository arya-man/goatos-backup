-- seed-fixture-guard:ignore: health_cases is health-owned; this adds a partition_label snapshot column + backfill. It touches no vaccination seed source, fixture shape, SOP/config semantic or source column.
-- +goose Up
-- health_cases.partition_label: snapshot the goat's partition at the moment the case is opened,
-- alongside the pre-existing park_id/shed_id snapshot columns (OpenCase already reads those from
-- `goats` under FOR SHARE and stores them once; partition_label follows the same snapshot design
-- rather than a live read-through).
--
-- WHY A SNAPSHOT, NOT READ-THROUGH: health_cases.shed_id is captured once at diagnosis time and is
-- never updated as the animal moves sheds afterward (health has no shifting-rescope consumer, unlike
-- vaccination's obligation rescope on goat.location.changed). A case's location display must stay
-- self-consistent with its own shed_id snapshot: reading the goat's CURRENT partition at query time
-- would risk showing partition_label for a different shed than the one recorded on the case (e.g.
-- shed_id snapshot = 'Castro' but a live read-through partition belongs to whatever shed the goat is
-- in today, possibly a different park after a transfer). Backfilling/joining live goat_shed_partitions
-- at read time was rejected for this reason; see AGENTS.md operational-location rule + the obligation
-- read-through precedent, which does not apply here because obligations track a still-open goat-shed
-- assignment while a health case's shed_id is already a point-in-time diagnosis fact.
--
-- LOCK SAFETY: ADD COLUMN nullable is a fast catalog-only change (no table rewrite).
-- Index creation uses CONCURRENTLY to avoid blocking concurrent writes (requires NO TRANSACTION).
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE public.health_cases
    ADD COLUMN partition_label text;

-- Backfill using goat_shed_partitions CURRENT partition, matched only where the goat's current shed
-- still equals the case's own shed_id snapshot (so we never mix a stale shed with a live partition).
-- Cases whose goat has since moved shed, or with no goat_shed_partitions row (non-partitioned shed),
-- are left NULL -- never invented.
UPDATE public.health_cases hc
SET partition_label = gsp.partition_label
FROM public.goat_shed_partitions gsp
WHERE gsp.tenant_id = hc.tenant_id
  AND gsp.goat_id = hc.goat_id
  AND hc.shed_id IS NOT NULL
  AND gsp.shed_id = hc.shed_id
  AND hc.partition_label IS NULL;

-- Index for filter/list paths that group or filter by (shed_id, partition_label).
-- +goose NO TRANSACTION
CREATE INDEX CONCURRENTLY health_cases_shed_partition_idx
    ON public.health_cases (tenant_id, shed_id, partition_label)
    WHERE shed_id IS NOT NULL;

-- +goose Down
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '30s';

-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.health_cases_shed_partition_idx;

ALTER TABLE public.health_cases
    DROP COLUMN IF EXISTS partition_label;
