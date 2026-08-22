-- Herd Signals: a real 1-hour motion delta column.
--
-- motion_delta on herd_signal_tag_latest is the 15-minute windowed delta (matching
-- motion_window_seconds=900) used for movement_state. The API also serves a distinct
-- motion_delta_1h field, and until this migration the service layer served the SAME 15-minute
-- number under both names (maintainer correctness review, defect 4 / scale review M5) -- a
-- field whose name lies about its grain is worse than a missing one. This adds a real 1-hour
-- (3600s-tier) delta column so the two numbers are actually different when the tag's motion
-- differs 15 minutes ago vs an hour ago.

-- +goose Up
ALTER TABLE public.herd_signal_tag_latest
  ADD COLUMN IF NOT EXISTS motion_delta_1h bigint;

-- +goose Down
ALTER TABLE public.herd_signal_tag_latest
  DROP COLUMN IF EXISTS motion_delta_1h;
