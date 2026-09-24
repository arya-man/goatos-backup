-- +goose Up
-- COMMITTED VACCINATION WRITES EVICT THE VACCINATION READ CACHE ON EVERY API INSTANCE.
--
-- The vaccination board/sheds/execution/operations reads are cached per API instance for one 30 s
-- bucket (vaccinationexecution/adapters/postgres: vaccinationCacheAsOfKey). They must stay correct
-- after a dose record, a verification decision, a drive/batch change or an obligation change, and
-- those writes live in a dozen packages (obligation reconciler, sweeper, verification, sop
-- submissions, repair tools, manual SQL). Rather than thread readcache.NotifyTx through every one
-- of them -- and miss the next one -- the tables themselves announce the write.
--
-- Same channel and payload as readcache.NotifyTx (goatos_read_cache_evict), plus
-- caches=["vaccination"] so the listener evicts only the vaccination cache, never the weighing /
-- growth analytics entries. pg_notify inside the writer's transaction is delivered on COMMIT and
-- dropped on rollback, and identical payloads are de-duplicated per transaction, so a bulk
-- reconciler statement sends one notification per tenant. Statement-level with transition tables:
-- one trigger call per statement, not per row. Tables the board reads WITHOUT a trigger here (goats,
-- partitions, locations, protocol catalogue, workforce) are bounded by the key itself: the as_of
-- component rolls every 30 s, so a moved or retired animal shows within one bucket.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.vaccination_read_cache_notify_new_rows()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
DECLARE
  t uuid;
BEGIN
  FOR t IN SELECT DISTINCT tenant_id FROM new_rows WHERE tenant_id IS NOT NULL LOOP
    PERFORM pg_notify('goatos_read_cache_evict',
      json_build_object('tenant_id', t::text, 'caches', json_build_array('vaccination'))::text);
  END LOOP;
  RETURN NULL;
END
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.vaccination_read_cache_notify_old_rows()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
DECLARE
  t uuid;
BEGIN
  FOR t IN SELECT DISTINCT tenant_id FROM old_rows WHERE tenant_id IS NOT NULL LOOP
    PERFORM pg_notify('goatos_read_cache_evict',
      json_build_object('tenant_id', t::text, 'caches', json_build_array('vaccination'))::text);
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
    'obligation_instances',
    'obligation_batches',
    'vaccination_completions',
    'vaccination_completion_rejections',
    'vaccination_drive_assignments',
    'vaccination_drive_assignment_members',
    'vaccination_drive_date_overrides',
    'vaccination_capacity_config',
    'vaccination_operator_assignment_config'
  ] LOOP
    EXECUTE format('DROP TRIGGER IF EXISTS vaccination_read_cache_evict_ins ON public.%I', tbl);
    EXECUTE format('CREATE TRIGGER vaccination_read_cache_evict_ins AFTER INSERT ON public.%I REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.vaccination_read_cache_notify_new_rows()', tbl);
    EXECUTE format('DROP TRIGGER IF EXISTS vaccination_read_cache_evict_upd ON public.%I', tbl);
    EXECUTE format('CREATE TRIGGER vaccination_read_cache_evict_upd AFTER UPDATE ON public.%I REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.vaccination_read_cache_notify_new_rows()', tbl);
    EXECUTE format('DROP TRIGGER IF EXISTS vaccination_read_cache_evict_del ON public.%I', tbl);
    EXECUTE format('CREATE TRIGGER vaccination_read_cache_evict_del AFTER DELETE ON public.%I REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION public.vaccination_read_cache_notify_old_rows()', tbl);
  END LOOP;
END
$do$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.vaccination_read_cache_notify_verification_rows()
RETURNS trigger
LANGUAGE plpgsql
AS $fn$
DECLARE
  t uuid;
BEGIN
  -- verification_items is shared by every module; only a vaccination proof verdict feeds the
  -- vaccination board (commandboard_sql.go reads source_module='vaccination'), so the other
  -- modules' queues never evict it.
  IF TG_OP = 'DELETE' THEN
    FOR t IN SELECT DISTINCT tenant_id FROM old_rows
             WHERE tenant_id IS NOT NULL AND (source_module = 'vaccination' OR module = 'vaccination') LOOP
      PERFORM pg_notify('goatos_read_cache_evict',
        json_build_object('tenant_id', t::text, 'caches', json_build_array('vaccination'))::text);
    END LOOP;
  ELSE
    FOR t IN SELECT DISTINCT tenant_id FROM new_rows
             WHERE tenant_id IS NOT NULL AND (source_module = 'vaccination' OR module = 'vaccination') LOOP
      PERFORM pg_notify('goatos_read_cache_evict',
        json_build_object('tenant_id', t::text, 'caches', json_build_array('vaccination'))::text);
    END LOOP;
  END IF;
  RETURN NULL;
END
$fn$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $do$
BEGIN
  DROP TRIGGER IF EXISTS vaccination_read_cache_evict_ins ON public.verification_items;
  CREATE TRIGGER vaccination_read_cache_evict_ins AFTER INSERT ON public.verification_items
    REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.vaccination_read_cache_notify_verification_rows();
  DROP TRIGGER IF EXISTS vaccination_read_cache_evict_upd ON public.verification_items;
  CREATE TRIGGER vaccination_read_cache_evict_upd AFTER UPDATE ON public.verification_items
    REFERENCING NEW TABLE AS new_rows FOR EACH STATEMENT EXECUTE FUNCTION public.vaccination_read_cache_notify_verification_rows();
  DROP TRIGGER IF EXISTS vaccination_read_cache_evict_del ON public.verification_items;
  CREATE TRIGGER vaccination_read_cache_evict_del AFTER DELETE ON public.verification_items
    REFERENCING OLD TABLE AS old_rows FOR EACH STATEMENT EXECUTE FUNCTION public.vaccination_read_cache_notify_verification_rows();
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
    'obligation_instances',
    'obligation_batches',
    'vaccination_completions',
    'vaccination_completion_rejections',
    'vaccination_drive_assignments',
    'vaccination_drive_assignment_members',
    'vaccination_drive_date_overrides',
    'vaccination_capacity_config',
    'vaccination_operator_assignment_config'
  ] LOOP
    EXECUTE format('DROP TRIGGER IF EXISTS vaccination_read_cache_evict_ins ON public.%I', tbl);
    EXECUTE format('DROP TRIGGER IF EXISTS vaccination_read_cache_evict_upd ON public.%I', tbl);
    EXECUTE format('DROP TRIGGER IF EXISTS vaccination_read_cache_evict_del ON public.%I', tbl);
  END LOOP;
END
$do$;
-- +goose StatementEnd
DROP TRIGGER IF EXISTS vaccination_read_cache_evict_ins ON public.verification_items;
DROP TRIGGER IF EXISTS vaccination_read_cache_evict_upd ON public.verification_items;
DROP TRIGGER IF EXISTS vaccination_read_cache_evict_del ON public.verification_items;
DROP FUNCTION IF EXISTS public.vaccination_read_cache_notify_verification_rows();
DROP FUNCTION IF EXISTS public.vaccination_read_cache_notify_old_rows();
DROP FUNCTION IF EXISTS public.vaccination_read_cache_notify_new_rows();
