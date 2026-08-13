-- +goose Up
-- +goose NO TRANSACTION
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS sop_task_scan_captures_task_field_tag_obligation_unique_idx
ON public.sop_task_scan_captures USING btree (
  tenant_id,
  task_id,
  field_key,
  normalized_tag,
  COALESCE(obligation_id, '00000000-0000-0000-0000-000000000000'::uuid)
);

DROP INDEX CONCURRENTLY IF EXISTS public.sop_task_scan_captures_task_field_tag_unique_idx;

-- +goose Down
-- +goose NO TRANSACTION
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS sop_task_scan_captures_task_field_tag_unique_idx
ON public.sop_task_scan_captures USING btree (
  tenant_id,
  task_id,
  field_key,
  normalized_tag
);

DROP INDEX CONCURRENTLY IF EXISTS public.sop_task_scan_captures_task_field_tag_obligation_unique_idx;
