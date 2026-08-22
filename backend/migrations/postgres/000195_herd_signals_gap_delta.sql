-- Herd Signals: flag a delta computed across a reception gap.
--
-- Maintainer decision on offline device behaviour: the gateway is a BLE scanner + network
-- forwarder that does NOT buffer scan reports through a WAN outage, and a tag only broadcasts
-- its CURRENT cumulative motion_count, never history. So a network outage means the backend
-- receives NOTHING for that period, and on reconnect the next packet's delta (new cumulative
-- minus the last one we saw) is the TOTAL movement across the whole outage, with NO information
-- about how it was distributed in time. That lump must be flagged wherever a delta is stored or
-- served, so it is never silently treated as ordinary in-window movement, smeared across the
-- buckets it spans, or fed into the p75 baseline / spike comparison (see domain.Baseline75 and
-- domain.PatternStateFromHistory, which now both take this into account explicitly).

-- +goose Up
ALTER TABLE public.herd_signal_tag_latest
  ADD COLUMN IF NOT EXISTS gap_delta boolean NOT NULL DEFAULT false;

ALTER TABLE public.herd_signal_activity_windows
  ADD COLUMN IF NOT EXISTS gap_delta boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE public.herd_signal_tag_latest
  DROP COLUMN IF EXISTS gap_delta;

ALTER TABLE public.herd_signal_activity_windows
  DROP COLUMN IF EXISTS gap_delta;
