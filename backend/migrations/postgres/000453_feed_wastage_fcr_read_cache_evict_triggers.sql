-- +goose Up
-- AN APPROVED FEED-WASTAGE READING EVICTS THE FCR READ CACHE ON EVERY API INSTANCE.
--
-- Growth Director's FCR tab now takes the verifier's approved leftover (feed_wastage_completions,
-- status 'completed' with a wastage_kg) off each pen's directed feed (maintainer instruction
-- 2026-09-26, docs/decisions/weights-fcr.md). The tab is served from the shared "analytics" read
-- cache, which feed-issue writes already evict; without a trigger here an approval -- or a reading
-- corrected afterwards -- would leave FCR serving the pre-approval ratio until the entry aged out.
--
-- The table announces its OWN writes, like the feed-stock inputs in 000416/000427, so every writer
-- (the verdict consumer, the standalone measurement route, a repair script) is covered without
-- touching its Go. Only rows that are or were APPROVED matter to FCR, so a submit, a re-shoot or a
-- rejection of a clip nobody approved notifies nothing. Scoped to the rows' tenant + parks and to
-- caches=["analytics"] so a wastage write never cold-starts another module's cache. pg_notify
-- inside the writer's transaction is delivered on commit only.
--
-- LOCK SAFETY: CREATE TRIGGER takes SHARE ROW EXCLUSIVE briefly on one small table.
SET lock_timeout = '5s';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.feed_wastage_fcr_read_cache_notify(rows_json jsonb)
RETURNS void
LANGUAGE plpgsql
AS $fn$
DECLARE
  r record;
BEGIN
  FOR r IN
    SELECT x.tenant_id, array_agg(DISTINCT x.park_id::text) AS park_ids
    FROM jsonb_to_recordset(rows_json) AS x(tenant_id uuid, park_id uuid, status text)
    WHERE x.tenant_id IS NOT NULL AND x.status = 'completed'
    GROUP BY x.tenant_id
  LOOP
    PERFORM pg_notify('goatos_read_cache_evict', json_build_object(
      'tenant_id', r.tenant_id::text, 'park_ids', r.park_ids, 'caches', json_build_array('analytics'))::text);
  END LOOP;
END
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.feed_wastage_fcr_read_cache_notify_new_rows()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
BEGIN
  PERFORM public.feed_wastage_fcr_read_cache_notify(
    (SELECT COALESCE(jsonb_agg(jsonb_build_object('tenant_id', n.tenant_id, 'park_id', n.park_id, 'status', n.status)), '[]'::jsonb) FROM new_rows n));
  RETURN NULL;
END
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.feed_wastage_fcr_read_cache_notify_old_rows()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
BEGIN
  PERFORM public.feed_wastage_fcr_read_cache_notify(
    (SELECT COALESCE(jsonb_agg(jsonb_build_object('tenant_id', o.tenant_id, 'park_id', o.park_id, 'status', o.status)), '[]'::jsonb) FROM old_rows o));
  RETURN NULL;
END
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
-- An UPDATE reads both sides: a row becoming approved (new side) and a row leaving approved (old
-- side) both move FCR.
CREATE OR REPLACE FUNCTION public.feed_wastage_fcr_read_cache_notify_both_rows()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
BEGIN
  PERFORM public.feed_wastage_fcr_read_cache_notify(
    (SELECT COALESCE(jsonb_agg(jsonb_build_object('tenant_id', x.tenant_id, 'park_id', x.park_id, 'status', x.status)), '[]'::jsonb)
     FROM (SELECT tenant_id, park_id, status FROM new_rows UNION ALL SELECT tenant_id, park_id, status FROM old_rows) x));
  RETURN NULL;
END
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $do$
BEGIN
  DROP TRIGGER IF EXISTS feed_wastage_fcr_read_cache_evict_ins ON public.feed_wastage_completions;
  CREATE TRIGGER feed_wastage_fcr_read_cache_evict_ins AFTER INSERT ON public.feed_wastage_completions
    REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.feed_wastage_fcr_read_cache_notify_new_rows();
  DROP TRIGGER IF EXISTS feed_wastage_fcr_read_cache_evict_upd ON public.feed_wastage_completions;
  CREATE TRIGGER feed_wastage_fcr_read_cache_evict_upd AFTER UPDATE ON public.feed_wastage_completions
    REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.feed_wastage_fcr_read_cache_notify_both_rows();
  DROP TRIGGER IF EXISTS feed_wastage_fcr_read_cache_evict_del ON public.feed_wastage_completions;
  CREATE TRIGGER feed_wastage_fcr_read_cache_evict_del AFTER DELETE ON public.feed_wastage_completions
    REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION public.feed_wastage_fcr_read_cache_notify_old_rows();
END
$do$;
-- +goose StatementEnd

-- +goose Down
DROP TRIGGER IF EXISTS feed_wastage_fcr_read_cache_evict_ins ON public.feed_wastage_completions;
DROP TRIGGER IF EXISTS feed_wastage_fcr_read_cache_evict_upd ON public.feed_wastage_completions;
DROP TRIGGER IF EXISTS feed_wastage_fcr_read_cache_evict_del ON public.feed_wastage_completions;
DROP FUNCTION IF EXISTS public.feed_wastage_fcr_read_cache_notify_new_rows();
DROP FUNCTION IF EXISTS public.feed_wastage_fcr_read_cache_notify_old_rows();
DROP FUNCTION IF EXISTS public.feed_wastage_fcr_read_cache_notify_both_rows();
DROP FUNCTION IF EXISTS public.feed_wastage_fcr_read_cache_notify(jsonb);
