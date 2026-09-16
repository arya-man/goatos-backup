-- +goose Up
-- Calendar configuration is tenant-scoped DB configuration, independent of SOP publication.
-- Replaces the legacy SOP page-settings source without changing SOP documents.
-- seed-fixture-guard:ignore: tenant-scoped weighing calendar settings only.
CREATE TABLE public.weighing_calendar_config (
  tenant_id uuid PRIMARY KEY REFERENCES public.tenants(tenant_id),
  earliest_date date NOT NULL DEFAULT DATE '2026-07-05',
  default_from_mode text NOT NULL DEFAULT 'fixed_date',
  default_from_date date DEFAULT DATE '2026-08-03',
  default_from_weeks integer,
  default_from_days integer,
  updated_at timestamptz NOT NULL DEFAULT now(),
  row_version bigint NOT NULL DEFAULT 1 CHECK (row_version >= 1),
  CONSTRAINT weighing_calendar_mode CHECK (default_from_mode IN ('fixed_date', 'rolling_weeks', 'rolling_days')),
  CONSTRAINT weighing_calendar_weeks CHECK (default_from_weeks IS NULL OR default_from_weeks BETWEEN 1 AND 520),
  CONSTRAINT weighing_calendar_days CHECK (default_from_days IS NULL OR default_from_days BETWEEN 1 AND 3650),
  CONSTRAINT weighing_calendar_selected_value CHECK (
    (default_from_mode = 'fixed_date' AND default_from_date IS NOT NULL AND default_from_date >= earliest_date)
    OR (default_from_mode = 'rolling_weeks' AND default_from_weeks IS NOT NULL)
    OR (default_from_mode = 'rolling_days' AND default_from_days IS NOT NULL)
  ),
  CONSTRAINT weighing_calendar_finite_dates CHECK (earliest_date BETWEEN DATE '0001-01-01' AND DATE '9999-12-31' AND (default_from_date IS NULL OR default_from_date BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'))
);
-- +goose StatementBegin
CREATE FUNCTION public.touch_weighing_calendar_config() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 NEW.updated_at := clock_timestamp();
 NEW.row_version := OLD.row_version + 1;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER weighing_calendar_config_touch
BEFORE UPDATE ON public.weighing_calendar_config
FOR EACH ROW EXECUTE FUNCTION public.touch_weighing_calendar_config();

CREATE TRIGGER admin_ui_weighing_calendar_revision_trg
AFTER INSERT OR UPDATE OR DELETE ON public.weighing_calendar_config
FOR EACH ROW EXECUTE FUNCTION public.admin_ui_bump_row_family_trg('weighing-calendar');

-- The maintainer-selected initial configuration is July 5 / fixed August 3.
-- Historical SOP documents remain untouched and no longer control page dates.
-- seed-migration-guard:ignore owner=codex issue=weighing-calendar-db-config reason=initialize dedicated tenant calendar configuration with explicitly requested defaults; no operational data mutation expiry=2026-10-31
INSERT INTO public.weighing_calendar_config (tenant_id)
SELECT tenant_id FROM public.tenants
ON CONFLICT (tenant_id) DO NOTHING;

-- +goose Down
-- Keep authored configuration on rollback; an older binary ignores this table.
SELECT 1;
