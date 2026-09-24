-- +goose Up
-- Analytics rollup I/O safety (stg db-g1-small saturation, 2026-09-24).
--
-- 1. rollup_day_watermark: durable per-(tenant, business day) watermark. A day
--    is recomputed only when analytics.app_events received rows after the
--    watermark (checked through the existing app_events_received_at_idx range),
--    or when the day crosses its finalization instant. Steady-state reruns scan
--    only rows newer than the watermark instead of rescanning the 3-day lookback.
-- 2. rollup_run.status 'degraded': first-party Postgres summaries committed but
--    an optional Firebase/BigQuery export failed. Exit 0; never certified as
--    'succeeded', and never a reason to re-dispatch the Postgres work.
-- 3. rollup_dispatch.attempts: kernel dispatch exponential backoff with a
--    per-source-date cap, instead of re-firing a failed job every lease.
-- 4. app_events_archive: per-UTC-received-day cold-storage ledger. app_events
--    keeps 15 days hot; older days are exported to GCS, verified (row count +
--    stored object size/MD5), recorded here, and only then deleted in batches.
-- All tables are tiny (one row per day); no large-table rewrite or index build.
CREATE TABLE IF NOT EXISTS analytics.rollup_day_watermark (
 tenant_id uuid NOT NULL,
 event_date date NOT NULL,
 received_through timestamptz NOT NULL,
 rolled_at timestamptz NOT NULL,
 PRIMARY KEY (tenant_id, event_date)
);

CREATE TABLE IF NOT EXISTS analytics.app_events_archive (
 archive_date date PRIMARY KEY,
 object_name text NOT NULL,
 row_count bigint NOT NULL CHECK (row_count >= 0),
 content_sha256 text NOT NULL,
 compressed_bytes bigint NOT NULL CHECK (compressed_bytes >= 0),
 verified_at timestamptz,
 deleted_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE analytics.rollup_dispatch
 ADD COLUMN IF NOT EXISTS attempts integer NOT NULL DEFAULT 1;

ALTER TABLE analytics.rollup_run DROP CONSTRAINT IF EXISTS rollup_run_status_check;
ALTER TABLE analytics.rollup_run ADD CONSTRAINT rollup_run_status_check
 CHECK (status = ANY (ARRAY['running'::text, 'succeeded'::text, 'degraded'::text, 'failed'::text, 'skipped'::text]));

-- +goose Down
ALTER TABLE analytics.rollup_run DROP CONSTRAINT IF EXISTS rollup_run_status_check;
UPDATE analytics.rollup_run SET status = 'succeeded' WHERE status = 'degraded';
ALTER TABLE analytics.rollup_run ADD CONSTRAINT rollup_run_status_check
 CHECK (status = ANY (ARRAY['running'::text, 'succeeded'::text, 'failed'::text, 'skipped'::text]));
ALTER TABLE analytics.rollup_dispatch DROP COLUMN IF EXISTS attempts;
DROP TABLE IF EXISTS analytics.rollup_day_watermark;
DROP TABLE IF EXISTS analytics.app_events_archive;
