-- +goose Up
-- seed-fixture-guard:ignore: additive tenant-scoped reporting assumptions for the Weighing area;
-- no vaccination/HRMS seed contract change and no app-visible projection table.
--
-- CONFIGURABLE WEIGHING ASSUMPTIONS (maintainer decision 2026-09-19).
--
-- 000249 made the assumed live-weight sale price DATA instead of a constant, but it could still
-- only be changed with SQL. The maintainer's instruction on 2026-09-19: the price "should be
-- configurable ... I need to change it in future", from a button on the Weighing screens, and
-- the same button should carry the OTHER figures that were still hard-coded in the product:
--
--   sale_ready_threshold_kg  the "Over 35 kg" sale-ready line (weighing/domain.SaleThresholdUpperKg,
--                            and a literal 35 in the Farm value and Weights page copy)
--   load_age_alert_days      the 90-day load-age alert (procurement/domain.LoadAgeAlertDays)
--
-- One row per (tenant, key), edited IN PLACE under a row_version fence: unlike the sale price,
-- neither of these values prices a past window, so no history is needed -- an audit row records
-- who changed it. Bounds live in growthdirector/domain.AssumptionKeys and are enforced on the
-- write path; the CHECK here is the floor (a value must be positive), not the business band.
CREATE TABLE public.growth_assumptions (
  tenant_id    uuid NOT NULL REFERENCES tenants (tenant_id),
  key          text NOT NULL,
  value        numeric(12, 3) NOT NULL CHECK (value > 0),
  unit         text NOT NULL DEFAULT '',
  set_by       text NOT NULL DEFAULT '',
  updated_at   timestamptz NOT NULL DEFAULT now(),
  row_version  integer NOT NULL DEFAULT 1,
  PRIMARY KEY (tenant_id, key)
);

COMMENT ON TABLE public.growth_assumptions IS
  'Tenant-scoped figures the Weighing area reads that used to be code constants: the sale-ready weight line and the load-age alert. Edited from the ADG Analytics Assumptions drawer, never a code constant.';

INSERT INTO public.growth_assumptions (tenant_id, key, value, unit, set_by)
SELECT t.tenant_id, v.key, v.value, v.unit, 'maintainer'
FROM tenants t
CROSS JOIN (VALUES
  ('sale_ready_threshold_kg', 35.000, 'kg'),
  ('load_age_alert_days', 90.000, 'days')
) AS v(key, value, unit)
ON CONFLICT (tenant_id, key) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS public.growth_assumptions;
