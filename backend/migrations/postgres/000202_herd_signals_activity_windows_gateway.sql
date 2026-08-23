-- Migration 000202: Add gateway_id to herd_signal_activity_windows and index for GetGatewayWindowStats.
--
-- RATIONALE FOR DEFECT FIXES:
--
-- Defect 2: GetGatewayWindowStats currently joins herd_signal_activity_windows to herd_signal_tag_latest
-- to get the gateway_id. This attributes old packets heard by a previous gateway to the CURRENT gateway
-- when a tag moves. This column carries the gateway that actually heard each window of packets, so the
-- aggregate is honest.
--
-- Defect 3: The GetGatewayWindowStats query filters on bucket_seconds = 300 and bucket_start >= now() - 15 minutes.
-- Without an index on (tenant_id, bucket_seconds, bucket_start), it scans all activity_windows tiers at
-- high cardinality. The compound index lets the query seek by tenant and bucket_seconds, then descend on
-- bucket_start to find recent buckets efficiently.

-- +goose Up

ALTER TABLE public.herd_signal_activity_windows
ADD COLUMN IF NOT EXISTS gateway_id text;

-- Index for GetGatewayWindowStats: efficiently fetch 15-minute windows by tenant and bucket grain.
-- The query filters tenant_id, bucket_seconds = 300, and bucket_start >= now() - 15 minutes.
-- This index allows seeking by tenant and bucket_seconds, then efficient range scan on bucket_start.
CREATE INDEX IF NOT EXISTS herd_signal_activity_windows_tenant_bucket_grain_start_idx
  ON public.herd_signal_activity_windows (tenant_id, bucket_seconds, bucket_start DESC)
  WHERE gateway_id IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS public.herd_signal_activity_windows_tenant_bucket_grain_start_idx;

ALTER TABLE public.herd_signal_activity_windows
DROP COLUMN IF EXISTS gateway_id;
