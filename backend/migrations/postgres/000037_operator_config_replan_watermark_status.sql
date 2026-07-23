-- +goose Up
-- Add status column to operator_config_replan_watermarks to track pending/succeeded state.
-- The two-phase watermark pattern: claim as 'pending' before recompute, mark 'succeeded' only
-- if recompute succeeds. On retry/redelivery, if status != 'succeeded', retry recompute.
-- This makes the consumer durable and idempotent: exactly-once effect, at-least-once attempt.
ALTER TABLE public.obligation_operator_config_replan_watermarks
ADD COLUMN status text DEFAULT 'pending' NOT NULL;

-- Index to support querying pending events (not yet fully processed)
CREATE INDEX IF NOT EXISTS obligation_operator_config_replan_watermarks_status_idx
  ON public.obligation_operator_config_replan_watermarks (tenant_id, status)
  WHERE status = 'pending';

-- +goose Down
DROP INDEX IF EXISTS public.obligation_operator_config_replan_watermarks_status_idx;
ALTER TABLE public.obligation_operator_config_replan_watermarks
DROP COLUMN IF EXISTS status;
