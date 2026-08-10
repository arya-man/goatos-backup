-- +goose Up
-- +goose NO TRANSACTION
-- Feed transport is one daily task per operational location. A partitioned shed must not share
-- one task/proof across all of its pens. The deploy task quiesces old writers before this
-- contract migration removes the legacy shed-only arbiter.
SET lock_timeout = '5s';

ALTER TABLE public.feed_transport_tasks
  ADD COLUMN IF NOT EXISTS partition_label text NOT NULL DEFAULT '';

ALTER TABLE public.feed_transport_tasks
  DROP CONSTRAINT IF EXISTS feed_transport_tasks_status_check;

ALTER TABLE public.feed_transport_tasks
  ADD CONSTRAINT feed_transport_tasks_status_check
  CHECK (status IN ('due','verification_due','rework','completed','retired')) NOT VALID;

ALTER TABLE public.feed_transport_tasks
  VALIDATE CONSTRAINT feed_transport_tasks_status_check;

RESET lock_timeout;

-- An unstarted whole-shed task is ambiguous once the shed has real partitions. Retire it and let
-- the partition-aware materializer create the replacement tasks. Submitted verification/rework
-- rows keep their original state and evidence until operators finish them. Never invent completion.
UPDATE public.feed_transport_tasks task
SET status = 'retired',
    updated_at = now(),
    row_version = task.row_version + 1
WHERE COALESCE(NULLIF(BTRIM(task.partition_label), ''), 'whole') = 'whole'
  AND task.status = 'due'
  AND task.current_attempt_id IS NULL
  AND task.completed_at IS NULL
  AND EXISTS (
    SELECT 1
    FROM public.shed_partitions sp
    WHERE sp.tenant_id = task.tenant_id
      AND sp.shed_id = task.shed_id
      AND sp.status = 'active'
      AND COALESCE(NULLIF(BTRIM(sp.partition_label), ''), 'whole') <> 'whole'
  );

-- A failed concurrent build leaves an invalid relation with the intended name. Drop first so a
-- retry cannot let IF NOT EXISTS silently accept an invalid uniqueness arbiter.
DROP INDEX CONCURRENTLY IF EXISTS public.feed_transport_tasks_daily_location_uq;
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS feed_transport_tasks_daily_location_uq
  ON public.feed_transport_tasks (
    tenant_id,
    business_date,
    shed_id,
    COALESCE(NULLIF(btrim(partition_label), ''), 'whole')
  );

DROP INDEX CONCURRENTLY IF EXISTS public.feed_transport_tasks_today_partition_idx;
CREATE INDEX CONCURRENTLY IF NOT EXISTS feed_transport_tasks_today_partition_idx
  ON public.feed_transport_tasks (tenant_id, business_date, status, shed_id, partition_label);

DROP INDEX CONCURRENTLY IF EXISTS feed_transport_tasks_daily_shed_uq;
DROP INDEX CONCURRENTLY IF EXISTS feed_transport_tasks_today_idx;

-- +goose Down
-- +goose NO TRANSACTION
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS feed_transport_tasks_daily_shed_uq
  ON public.feed_transport_tasks (tenant_id, business_date, shed_id);

CREATE INDEX CONCURRENTLY IF NOT EXISTS feed_transport_tasks_today_idx
  ON public.feed_transport_tasks (tenant_id, business_date, status, shed_id);

DROP INDEX CONCURRENTLY IF EXISTS feed_transport_tasks_today_partition_idx;
DROP INDEX CONCURRENTLY IF EXISTS feed_transport_tasks_daily_location_uq;

ALTER TABLE public.feed_transport_tasks
  DROP COLUMN IF EXISTS partition_label;
