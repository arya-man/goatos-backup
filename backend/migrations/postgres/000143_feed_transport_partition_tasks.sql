-- +goose Up
-- +goose NO TRANSACTION
-- Feed transport is moving from one daily task per shed to one daily task per operational
-- location. This is the EXPAND step: add the partition column and future unique arbiter while
-- keeping the old three-column arbiter so already-running workers remain valid until the
-- contract migration removes it in a later deploy.
SET lock_timeout = '5s';

ALTER TABLE public.feed_transport_tasks
  ADD COLUMN IF NOT EXISTS partition_label text NOT NULL DEFAULT '';

ALTER TABLE public.feed_transport_tasks
  DROP CONSTRAINT IF EXISTS feed_transport_tasks_status_check;

ALTER TABLE public.feed_transport_tasks
  ADD CONSTRAINT feed_transport_tasks_status_check
  CHECK (status IN ('due','verification_due','rework','completed','retired')) NOT VALID;

RESET lock_timeout;

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS feed_transport_tasks_daily_location_uq
  ON public.feed_transport_tasks (
    tenant_id,
    business_date,
    shed_id,
    COALESCE(NULLIF(btrim(partition_label), ''), 'whole')
  );

CREATE INDEX CONCURRENTLY IF NOT EXISTS feed_transport_tasks_today_partition_idx
  ON public.feed_transport_tasks (tenant_id, business_date, status, shed_id, partition_label);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS feed_transport_tasks_today_partition_idx;
DROP INDEX CONCURRENTLY IF EXISTS feed_transport_tasks_daily_location_uq;

SET lock_timeout = '5s';

ALTER TABLE public.feed_transport_tasks
  DROP CONSTRAINT IF EXISTS feed_transport_tasks_status_check;

ALTER TABLE public.feed_transport_tasks
  ADD CONSTRAINT feed_transport_tasks_status_check
  CHECK (status IN ('due','verification_due','rework','completed')) NOT VALID;

ALTER TABLE public.feed_transport_tasks
  DROP COLUMN IF EXISTS partition_label;

RESET lock_timeout;
