-- +goose Up
-- seed-fixture-guard:ignore: additive tenant-scoped reporting assumptions for the Weighing area;
-- no vaccination/HRMS seed contract change and no app-visible projection table.
--
-- MORE WEIGHING ASSUMPTIONS (maintainer decision 2026-09-19, same day as 000364). After the first
-- three figures moved into the drawer the maintainer asked for the rest of what ADG Analytics still
-- carried as constants. Two are not scalars -- the weight-band edges are a LIST and the window
-- dates are DATES -- so the row gains two typed columns; exactly one of the three holds a value.
--
--   sale_ready_lower_kg            the "Over 30 kg" line (weighing/domain.SaleThresholdLowerKg)
--   slow_growth_target_g_per_day   the slow-growth rule of thumb (growthdirector SlowGrowthTargetGPerDay)
--   bad_scan_loss_g_per_day        a pair losing MORE than this per day is a bad scan (SQL literal -300)
--   default_period_days            the Growth Director window when no dates are given (15)
--   weight_band_edges_kg           the band board / Weight-wise / FCR-by-band edges (15,20,25,30,35)
--
-- The Weights pages' landing date and picker floor are NOT here: they already live on the
-- Weighing SOP's weights_pages block (maintainer request 2026-09-16), and one figure must have
-- one home. value_date stays for a future dated figure.
ALTER TABLE public.growth_assumptions
  ALTER COLUMN value DROP NOT NULL,
  DROP CONSTRAINT IF EXISTS growth_assumptions_value_check,
  ADD COLUMN value_list numeric(12, 3)[] NULL,
  ADD COLUMN value_date date NULL,
  ADD CONSTRAINT growth_assumptions_value_positive CHECK (value IS NULL OR value > 0),
  ADD CONSTRAINT growth_assumptions_one_value CHECK (
    (value IS NOT NULL)::int + (value_list IS NOT NULL)::int + (value_date IS NOT NULL)::int = 1
  );

INSERT INTO public.growth_assumptions (tenant_id, key, value, value_list, value_date, unit, set_by)
SELECT t.tenant_id, v.key, v.value, v.value_list, v.value_date, v.unit, 'maintainer'
FROM tenants t
CROSS JOIN (VALUES
  ('sale_ready_lower_kg',          30.000::numeric, NULL::numeric[],                       NULL::date,         'kg'),
  ('slow_growth_target_g_per_day', 200.000,         NULL,                                  NULL,               'g/day'),
  ('bad_scan_loss_g_per_day',      300.000,         NULL,                                  NULL,               'g/day'),
  ('default_period_days',          15.000,          NULL,                                  NULL,               'days'),
  ('weight_band_edges_kg',         NULL,            ARRAY[15,20,25,30,35]::numeric(12,3)[], NULL,              'kg')
) AS v(key, value, value_list, value_date, unit)
ON CONFLICT (tenant_id, key) DO NOTHING;

-- +goose Down
DELETE FROM public.growth_assumptions
 WHERE key IN ('sale_ready_lower_kg','slow_growth_target_g_per_day','bad_scan_loss_g_per_day','default_period_days','weight_band_edges_kg');
ALTER TABLE public.growth_assumptions
  DROP CONSTRAINT IF EXISTS growth_assumptions_one_value,
  DROP CONSTRAINT IF EXISTS growth_assumptions_value_positive,
  DROP COLUMN IF EXISTS value_list,
  DROP COLUMN IF EXISTS value_date,
  ALTER COLUMN value SET NOT NULL,
  ADD CONSTRAINT growth_assumptions_value_check CHECK (value > 0);
