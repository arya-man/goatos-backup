-- +goose Up
-- COMMITTED HERD-REGISTER WRITES EVICT THE COUNTS BREAKDOWN FACET CACHE ON EVERY API INSTANCE.
--
-- /counts/breakdown serves its filter facets from a per-(tenant, lifecycle) read cache
-- (counts/adapters/postgres.Repository.WithFacetCache): the facets ignore paging and every
-- dimension filter, so each paging or filter click re-ran the same 6x goats scan. The facets read
-- goats, goat_shed_partitions, locations, shed_partitions and animal_stage_lookup, which are
-- written from many packages (identity, shifting, births/exits, admin UI, imports, repair tools),
-- so the tables themselves announce the write.
--
-- Same channel as readcache.NotifyTx (goatos_read_cache_evict), with caches=["counts"] so the
-- listener evicts only the counts cache's tenant entries, never the weighing / growth analytics or
-- alerts ones. pg_notify inside the writer's transaction is delivered on COMMIT and dropped on
-- rollback; identical payloads are de-duplicated per transaction, so a bulk statement sends one
-- notification per tenant. Statement-level with transition tables: one call per statement.
--
-- LOCK SAFETY: CREATE TRIGGER takes SHARE ROW EXCLUSIVE briefly on each table; lock_timeout makes
-- a busy table fail the migration fast instead of queueing writers behind it.
SET lock_timeout = '5s';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.counts_facet_read_cache_notify_new_rows()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
DECLARE
  t uuid;
BEGIN
  FOR t IN SELECT DISTINCT tenant_id FROM new_rows WHERE tenant_id IS NOT NULL LOOP
    PERFORM pg_notify('goatos_read_cache_evict', json_build_object('tenant_id', t::text, 'caches', json_build_array('counts'))::text);
  END LOOP;
  RETURN NULL;
END
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.counts_facet_read_cache_notify_old_rows()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
DECLARE
  t uuid;
BEGIN
  FOR t IN SELECT DISTINCT tenant_id FROM old_rows WHERE tenant_id IS NOT NULL LOOP
    PERFORM pg_notify('goatos_read_cache_evict', json_build_object('tenant_id', t::text, 'caches', json_build_array('counts'))::text);
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
    'goats',
    'goat_shed_partitions',
    'locations',
    'shed_partitions',
    'animal_stage_lookup'
  ] LOOP
    EXECUTE format('DROP TRIGGER IF EXISTS counts_facet_read_cache_evict_ins ON public.%I', tbl);
    EXECUTE format('CREATE TRIGGER counts_facet_read_cache_evict_ins AFTER INSERT ON public.%I REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.counts_facet_read_cache_notify_new_rows()', tbl);
    EXECUTE format('DROP TRIGGER IF EXISTS counts_facet_read_cache_evict_upd ON public.%I', tbl);
    EXECUTE format('CREATE TRIGGER counts_facet_read_cache_evict_upd AFTER UPDATE ON public.%I REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.counts_facet_read_cache_notify_new_rows()', tbl);
    EXECUTE format('DROP TRIGGER IF EXISTS counts_facet_read_cache_evict_del ON public.%I', tbl);
    EXECUTE format('CREATE TRIGGER counts_facet_read_cache_evict_del AFTER DELETE ON public.%I REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION public.counts_facet_read_cache_notify_old_rows()', tbl);
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
    'goats',
    'goat_shed_partitions',
    'locations',
    'shed_partitions',
    'animal_stage_lookup'
  ] LOOP
    EXECUTE format('DROP TRIGGER IF EXISTS counts_facet_read_cache_evict_ins ON public.%I', tbl);
    EXECUTE format('DROP TRIGGER IF EXISTS counts_facet_read_cache_evict_upd ON public.%I', tbl);
    EXECUTE format('DROP TRIGGER IF EXISTS counts_facet_read_cache_evict_del ON public.%I', tbl);
  END LOOP;
END
$do$;
-- +goose StatementEnd
DROP FUNCTION IF EXISTS public.counts_facet_read_cache_notify_new_rows();
DROP FUNCTION IF EXISTS public.counts_facet_read_cache_notify_old_rows();
