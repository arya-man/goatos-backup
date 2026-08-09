-- +goose Up
-- Feed transport is one daily task per operational location. A partitioned shed must not share
-- one task/proof across all of its pens.
ALTER TABLE public.feed_transport_tasks
  ADD COLUMN IF NOT EXISTS partition_label text NOT NULL DEFAULT '';

DROP INDEX IF EXISTS feed_transport_tasks_daily_shed_uq;
CREATE UNIQUE INDEX IF NOT EXISTS feed_transport_tasks_daily_location_uq
  ON public.feed_transport_tasks (
    tenant_id,
    business_date,
    shed_id,
    COALESCE(NULLIF(btrim(partition_label), ''), 'whole')
  );

DROP INDEX IF EXISTS feed_transport_tasks_today_idx;
CREATE INDEX IF NOT EXISTS feed_transport_tasks_today_idx
  ON public.feed_transport_tasks (tenant_id, business_date, status, shed_id, partition_label);

-- +goose Down
DROP INDEX IF EXISTS feed_transport_tasks_daily_location_uq;
CREATE UNIQUE INDEX IF NOT EXISTS feed_transport_tasks_daily_shed_uq
  ON public.feed_transport_tasks (tenant_id, business_date, shed_id);

DROP INDEX IF EXISTS feed_transport_tasks_today_idx;
CREATE INDEX IF NOT EXISTS feed_transport_tasks_today_idx
  ON public.feed_transport_tasks (tenant_id, business_date, status, shed_id);

ALTER TABLE public.feed_transport_tasks
  DROP COLUMN IF EXISTS partition_label;
