-- +goose Up
-- app_events archive safety follow-up to 000419.
-- 1. The archive ledger records the last exported (received_at, event_id) so
--    deletes are bounded by exactly what the verified object holds; a row
--    restored/backfilled into an archived day later is never deleted unarchived.
--    Ledger rows without a bound are re-verified before any delete.
-- 2. Batched archive deletes remove a whole day (~25-120k rows) per run; the
--    default 20% scale factor on a 1.5M-row table would leave that bloat
--    unvacuumed for days. Per-table reloptions only (brief SHARE UPDATE
--    EXCLUSIVE lock, no rewrite).
ALTER TABLE analytics.app_events_archive
 ADD COLUMN IF NOT EXISTS last_received_at timestamptz,
 ADD COLUMN IF NOT EXISTS last_event_id uuid;

ALTER TABLE analytics.app_events SET (
 autovacuum_vacuum_scale_factor = 0.02,
 autovacuum_analyze_scale_factor = 0.02
);

-- +goose Down
ALTER TABLE analytics.app_events RESET (autovacuum_vacuum_scale_factor, autovacuum_analyze_scale_factor);
ALTER TABLE analytics.app_events_archive
 DROP COLUMN IF EXISTS last_event_id,
 DROP COLUMN IF EXISTS last_received_at;
