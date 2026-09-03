-- +goose Up
--
-- HRMS person access is part of the admin-web permissions contract. The old
-- user_scope_grants table already bumps the permissions family; these newer
-- per-person access tables must do the same or /admin-web/bootstrap may
-- legitimately revalidate an old ETag after an HRMS edit.

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_trigger
    WHERE tgname = 'admin_ui_person_access_revision_trg'
      AND tgrelid = 'public.person_access'::regclass
  ) THEN
    CREATE TRIGGER admin_ui_person_access_revision_trg
    AFTER INSERT OR DELETE OR UPDATE ON public.person_access
    FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_row_family_trg('permissions');
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_trigger
    WHERE tgname = 'admin_ui_person_module_access_revision_trg'
      AND tgrelid = 'public.person_module_access'::regclass
  ) THEN
    CREATE TRIGGER admin_ui_person_module_access_revision_trg
    AFTER INSERT OR DELETE OR UPDATE ON public.person_module_access
    FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_row_family_trg('permissions');
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_trigger
    WHERE tgname = 'admin_ui_person_park_scope_revision_trg'
      AND tgrelid = 'public.person_park_scope'::regclass
  ) THEN
    CREATE TRIGGER admin_ui_person_park_scope_revision_trg
    AFTER INSERT OR DELETE OR UPDATE ON public.person_park_scope
    FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_row_family_trg('permissions');
  END IF;
END $$;

SELECT public.admin_ui_bump_config_family(
  tenant_id,
  'permissions',
  NULL,
  'migration:000246_person_access_revision_triggers',
  '{"reason":"person access revision triggers installed"}'::jsonb
)
FROM (
  SELECT DISTINCT tenant_id FROM public.person_access
  UNION
  SELECT DISTINCT tenant_id FROM public.person_module_access
  UNION
  SELECT DISTINCT tenant_id FROM public.person_park_scope
) AS tenants;

-- +goose Down
DROP TRIGGER IF EXISTS admin_ui_person_park_scope_revision_trg ON public.person_park_scope;
DROP TRIGGER IF EXISTS admin_ui_person_module_access_revision_trg ON public.person_module_access;
DROP TRIGGER IF EXISTS admin_ui_person_access_revision_trg ON public.person_access;
