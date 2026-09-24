-- +goose Up
-- +goose NO TRANSACTION
--
-- Indexes for the persisted Herd Signals risk columns (000402), built CONCURRENTLY so ingest
-- keeps writing herd_signal_tag_latest while they build.
--
--   herd_signal_tag_latest_risk_idx / _risk_state_idx: the live risk_state filter
--     ("attention" = any state, or one exact state) in the default last_seen keyset order.
--   herd_signal_tag_latest_risk_dirty_idx: the classifier's change-driven work queue -- tags
--     never classified, queued (risk_evaluated_at NULL), or reporting again after risk_due_at.
--     Partial, so a steady-state herd keeps it tiny.
--   herd_signal_tag_latest_risk_evaluated_idx: the hourly aging sweep (oldest evaluations).

CREATE INDEX CONCURRENTLY IF NOT EXISTS herd_signal_tag_latest_risk_idx
  ON public.herd_signal_tag_latest (tenant_id, last_seen_at DESC, tag_id DESC)
  WHERE risk_state IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS herd_signal_tag_latest_risk_state_idx
  ON public.herd_signal_tag_latest (tenant_id, risk_state, last_seen_at DESC, tag_id DESC)
  WHERE risk_state IS NOT NULL;

CREATE INDEX CONCURRENTLY IF NOT EXISTS herd_signal_tag_latest_risk_dirty_idx
  ON public.herd_signal_tag_latest (tenant_id, tag_id)
  WHERE risk_evaluated_at IS NULL OR last_seen_at > risk_due_at;

CREATE INDEX CONCURRENTLY IF NOT EXISTS herd_signal_tag_latest_risk_evaluated_idx
  ON public.herd_signal_tag_latest (tenant_id, risk_evaluated_at)
  WHERE risk_evaluated_at IS NOT NULL;

-- +goose Down
-- +goose NO TRANSACTION

DROP INDEX CONCURRENTLY IF EXISTS public.herd_signal_tag_latest_risk_evaluated_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.herd_signal_tag_latest_risk_dirty_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.herd_signal_tag_latest_risk_state_idx;
DROP INDEX CONCURRENTLY IF EXISTS public.herd_signal_tag_latest_risk_idx;
