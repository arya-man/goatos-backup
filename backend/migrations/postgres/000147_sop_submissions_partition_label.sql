-- +goose Up
-- +goose NO TRANSACTION
ALTER TABLE public.sop_submissions
  ADD COLUMN IF NOT EXISTS partition_label text;

CREATE INDEX CONCURRENTLY IF NOT EXISTS sop_submissions_task_partition_idx
  ON public.sop_submissions (tenant_id, task_id, COALESCE(partition_label, ''), submitted_at DESC);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.sop_submissions_task_partition_idx;

ALTER TABLE public.sop_submissions
  DROP COLUMN IF EXISTS partition_label;
