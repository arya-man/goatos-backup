-- +goose Up
-- +goose NO TRANSACTION
-- The VIDEO LOG on /verify (maintainer decision 2026-08-14) answers ONE business day: for that day,
-- per shed, when each proof was uploaded. It reads verification_items over a half-open captured_at
-- range with NO status filter, because the question is "what arrived from this shed today" and a
-- rejected or approved proof arrived just as much as a pending one.
--
-- No existing index serves that shape:
--   verification_items_queue_idx (tenant_id, status, category, captured_at, item_id)
--     leads with status, so an all-statuses day read binds tenant_id and stops.
--   verification_items_pending_scope_idx  is partial to status='pending'.
--   verification_items_verified_day_idx   is keyed on verified_at, not captured_at.
--   verification_items_shed_partition_idx (tenant_id, shed_id, partition_label)
--     carries no time column, so a shed's day still filters captured_at in the heap.
-- So the day slice was a tenant-wide scan of an append-only table that grows with
-- animals x modules x years.
--
-- Leading (tenant_id, captured_at) is deliberate over (tenant_id, park_id, captured_at): leadership
-- reads the whole tenant with no park bound, and a park-leading index cannot serve that. A
-- park-scoped caller's park_id = ANY(...) clamp and the optional shed_id filter both apply as heap
-- filters over ONE day's rows, which is a bounded set at the 5k-50k envelope.
--
-- item_id is the tiebreak so the ordering matches the read's ORDER BY exactly, and the INCLUDE
-- columns carry everything the shed-summary aggregate needs so that level is index-only.
CREATE INDEX CONCURRENTLY IF NOT EXISTS verification_items_video_log_day_idx
  ON public.verification_items (tenant_id, captured_at, item_id)
  INCLUDE (shed_id, partition_label, park_id, category, module, status);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.verification_items_video_log_day_idx;
