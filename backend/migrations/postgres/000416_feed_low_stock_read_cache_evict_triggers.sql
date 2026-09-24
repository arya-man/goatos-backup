-- +goose Up
-- COMMITTED FEED-STOCK WRITES EVICT THE ALERTS LOW-STOCK READ CACHE ON EVERY API INSTANCE.
--
-- /alerts/rows serves the feed low-stock rule from a per-tenant read cache
-- (alerts/adapters/postgres.LowStockReader.WithCache): feedLowStockSQL aggregates every reached
-- purchase and every locked feed issue ever, ~70 ms per alerts load on the stg clone. The value is
-- exact until one of its inputs changes, and those inputs are written from several packages
-- (feed purchase importers, event appliers, the feed-direction issue path, milk preparation, the
-- feed catalog) -- so the tables themselves announce the write instead of threading a notify
-- through every writer and missing the next one.
--
-- Same channel as readcache.NotifyTx (goatos_read_cache_evict), with caches=["alerts"] so the
-- listener evicts only the alerts cache's tenant entries, never the weighing / growth analytics
-- ones (a milk-preparation submit must not cold-start the Weights tab). pg_notify inside
-- the writer's transaction is delivered on COMMIT and dropped on rollback; identical payloads are
-- de-duplicated per transaction. Statement-level with transition tables: one call per statement.
--
-- LOCK SAFETY: CREATE TRIGGER takes SHARE ROW EXCLUSIVE briefly on each table; lock_timeout makes
-- a busy table fail the migration fast instead of queueing writers behind it.
SET lock_timeout = '5s';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.feed_stock_read_cache_notify_new_rows()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
DECLARE
  t uuid;
BEGIN
  FOR t IN SELECT DISTINCT tenant_id FROM new_rows WHERE tenant_id IS NOT NULL LOOP
    PERFORM pg_notify('goatos_read_cache_evict', json_build_object('tenant_id', t::text, 'caches', json_build_array('alerts'))::text);
  END LOOP;
  RETURN NULL;
END
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.feed_stock_read_cache_notify_old_rows()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
DECLARE
  t uuid;
BEGIN
  FOR t IN SELECT DISTINCT tenant_id FROM old_rows WHERE tenant_id IS NOT NULL LOOP
    PERFORM pg_notify('goatos_read_cache_evict', json_build_object('tenant_id', t::text, 'caches', json_build_array('alerts'))::text);
  END LOOP;
  RETURN NULL;
END
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $do$
DECLARE
  tbl text;
BEGIN
  FOREACH tbl IN ARRAY ARRAY[
    'feed_purchases',
    'feed_direction_issues',
    'feed_direction_issue_rows',
    'feed_external_consumption',
    'milk_preparation_completions',
    'milk_preparation_proof_attempts',
    'feed_item_catalog'
  ] LOOP
    EXECUTE format('DROP TRIGGER IF EXISTS feed_stock_read_cache_evict_ins ON public.%I', tbl);
    EXECUTE format('CREATE TRIGGER feed_stock_read_cache_evict_ins AFTER INSERT ON public.%I REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.feed_stock_read_cache_notify_new_rows()', tbl);
    EXECUTE format('DROP TRIGGER IF EXISTS feed_stock_read_cache_evict_upd ON public.%I', tbl);
    EXECUTE format('CREATE TRIGGER feed_stock_read_cache_evict_upd AFTER UPDATE ON public.%I REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.feed_stock_read_cache_notify_new_rows()', tbl);
    EXECUTE format('DROP TRIGGER IF EXISTS feed_stock_read_cache_evict_del ON public.%I', tbl);
    EXECUTE format('CREATE TRIGGER feed_stock_read_cache_evict_del AFTER DELETE ON public.%I REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION public.feed_stock_read_cache_notify_old_rows()', tbl);
  END LOOP;
END
$do$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $do$
DECLARE
  tbl text;
BEGIN
  FOREACH tbl IN ARRAY ARRAY[
    'feed_purchases',
    'feed_direction_issues',
    'feed_direction_issue_rows',
    'feed_external_consumption',
    'milk_preparation_completions',
    'milk_preparation_proof_attempts',
    'feed_item_catalog'
  ] LOOP
    EXECUTE format('DROP TRIGGER IF EXISTS feed_stock_read_cache_evict_ins ON public.%I', tbl);
    EXECUTE format('DROP TRIGGER IF EXISTS feed_stock_read_cache_evict_upd ON public.%I', tbl);
    EXECUTE format('DROP TRIGGER IF EXISTS feed_stock_read_cache_evict_del ON public.%I', tbl);
  END LOOP;
END
$do$;
-- +goose StatementEnd
DROP FUNCTION IF EXISTS public.feed_stock_read_cache_notify_new_rows();
DROP FUNCTION IF EXISTS public.feed_stock_read_cache_notify_old_rows();
