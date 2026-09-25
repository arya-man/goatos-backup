-- +goose Up
-- +goose NO TRANSACTION
--
-- INDEX-ONLY 24H WINDOW READS FOR THE LIVE MONITOR PAGE.
--
-- herdsignals/adapters/postgres/repository.go GetBaselineDeltas and motionDeltas24hSQL run on
-- every GET /herd-signals/live page: for the page's tag ids, the 300s tier over the last 24h
-- (motion_delta filtered by packet_count > 0 and gap_delta). The pkey / tag index lead with
-- (tenant_id, tag_id, bucket_start) and carry no bucket_seconds, so each read walked every tier
-- (60s rows outnumber 300s ~5:1) and fetched each heap row for the filter columns: ~5.9k buffers
-- and ~33 ms per query on goatos-stg for a 19-tag page, twice per request.
--
-- (tenant_id, bucket_seconds, tag_id, bucket_start) matches both predicates exactly and
-- INCLUDE (motion_delta, packet_count, gap_delta) makes them index-only (~2 ms each on a stg
-- copy, 0 heap fetches).
--
-- LOCK SAFETY: CONCURRENTLY + NO TRANSACTION. herd_signal_activity_windows is upserted by every
-- packet ingest; a blocking build would stall ingest.
CREATE INDEX CONCURRENTLY IF NOT EXISTS herd_signal_activity_windows_grain_tag_start_idx
ON public.herd_signal_activity_windows (tenant_id, bucket_seconds, tag_id, bucket_start)
INCLUDE (motion_delta, packet_count, gap_delta);

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS public.herd_signal_activity_windows_grain_tag_start_idx;
