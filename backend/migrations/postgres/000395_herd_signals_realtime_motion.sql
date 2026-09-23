-- Live movement fields for Herd Signals.
--
-- Existing motion_delta remains the sustained 15-minute classifier. These columns separate
-- "tag is alive but animal is still" from "animal moved in the last seconds/minute".

-- +goose Up

ALTER TABLE public.herd_signal_tag_latest
  ADD COLUMN IF NOT EXISTS last_packet_motion_delta bigint,
  ADD COLUMN IF NOT EXISTS last_packet_window_seconds integer,
  ADD COLUMN IF NOT EXISTS motion_delta_30s bigint,
  ADD COLUMN IF NOT EXISTS motion_delta_60s bigint,
  ADD COLUMN IF NOT EXISTS motion_delta_5m bigint,
  ADD COLUMN IF NOT EXISTS last_moved_at timestamptz;

COMMENT ON COLUMN public.herd_signal_tag_latest.last_packet_motion_delta IS
  'Motion-count delta between this latest packet and the previous packet for the same tag; zero means tag is fresh but animal may be still.';
COMMENT ON COLUMN public.herd_signal_tag_latest.last_packet_window_seconds IS
  'Server-clock seconds between previous_seen_at and last_seen_at for the last packet delta.';
COMMENT ON COLUMN public.herd_signal_tag_latest.motion_delta_30s IS
  'Motion-count delta over the last 30 seconds, computed from raw cumulative readings at ingest.';
COMMENT ON COLUMN public.herd_signal_tag_latest.motion_delta_60s IS
  'Motion-count delta over the last 60 seconds, computed from raw cumulative readings at ingest.';
COMMENT ON COLUMN public.herd_signal_tag_latest.motion_delta_5m IS
  'Motion-count delta over the last 5 minutes, computed from raw cumulative readings at ingest.';
COMMENT ON COLUMN public.herd_signal_tag_latest.last_moved_at IS
  'Last server receive time when the motion counter increased versus the previous reading; NULL until movement is observed.';

CREATE INDEX IF NOT EXISTS herd_signal_tag_latest_last_moved_idx
  ON public.herd_signal_tag_latest (tenant_id, last_moved_at DESC)
  WHERE last_moved_at IS NOT NULL;

-- +goose Down

DROP INDEX IF EXISTS public.herd_signal_tag_latest_last_moved_idx;
ALTER TABLE public.herd_signal_tag_latest
  DROP COLUMN IF EXISTS last_moved_at,
  DROP COLUMN IF EXISTS motion_delta_5m,
  DROP COLUMN IF EXISTS motion_delta_60s,
  DROP COLUMN IF EXISTS motion_delta_30s,
  DROP COLUMN IF EXISTS last_packet_window_seconds,
  DROP COLUMN IF EXISTS last_packet_motion_delta;
