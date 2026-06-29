\set ON_ERROR_STOP on

BEGIN;

INSERT INTO inventory_items (
  item_id, tenant_id, item_code, name, category, base_unit, status, context
) VALUES (
  '00000000-0000-4000-8000-00000000b001',
  :'tenant_id'::uuid,
  'VAC-ET-PHC',
  'Enterotoxaemia Vaccine',
  'vaccine',
  'dose',
  'active',
  '{"seed":"google-dev-clean-slate-vaccination","source_ref":"docs/phc-vaccination/PRD.md"}'::jsonb
)
ON CONFLICT (tenant_id, item_code) DO UPDATE
SET name = EXCLUDED.name,
    status = 'active',
    context = EXCLUDED.context,
    updated_at = now();

INSERT INTO vaccines (tenant_id, item_id, disease, manufacturer, doses_per_vial, withdrawal_days, context)
SELECT
  i.tenant_id,
  i.item_id,
  'Enterotoxaemia',
  'Mesha source-derived dev baseline',
  10,
  0,
  '{"seed":"google-dev-clean-slate-vaccination","source_ref":"docs/phc-vaccination/PRD.md"}'::jsonb
FROM inventory_items i
WHERE i.tenant_id = :'tenant_id'::uuid
  AND i.item_code = 'VAC-ET-PHC'
ON CONFLICT (tenant_id, item_id) DO UPDATE
SET disease = EXCLUDED.disease,
    manufacturer = EXCLUDED.manufacturer,
    doses_per_vial = EXCLUDED.doses_per_vial,
    withdrawal_days = EXCLUDED.withdrawal_days,
    context = EXCLUDED.context,
    updated_at = now();

WITH item AS (
  SELECT tenant_id, item_id
  FROM inventory_items
  WHERE tenant_id = :'tenant_id'::uuid
    AND item_code = 'VAC-ET-PHC'
),
park AS (
  SELECT tenant_id, location_id
  FROM locations
  WHERE tenant_id = :'tenant_id'::uuid
    AND location_type = 'park'
    AND location_code = 'CBE'
),
desired (stock_id, lot_code, expiry_date, qty, reserved, status) AS (
  VALUES
    ('00000000-0000-4000-8000-00000000d101'::uuid, 'GDEV-ET-FEFO-2026-07', DATE '2026-07-15', 200::numeric, 0::numeric, 'active'),
    ('00000000-0000-4000-8000-00000000d102'::uuid, 'GDEV-ET-LONG-2027-12', DATE '2027-12-31', 1000::numeric, 0::numeric, 'active'),
    ('00000000-0000-4000-8000-00000000d103'::uuid, 'GDEV-ET-EXPIRED-2026-06', DATE '2026-06-01', 100::numeric, 0::numeric, 'expired'),
    ('00000000-0000-4000-8000-00000000d104'::uuid, 'GDEV-ET-QUAR-2026-08', DATE '2026-08-31', 100::numeric, 0::numeric, 'quarantined'),
    ('00000000-0000-4000-8000-00000000d105'::uuid, 'GDEV-ET-LOW-2026-09', DATE '2026-09-30', 1::numeric, 0::numeric, 'active')
)
INSERT INTO inventory_stock (
  stock_id, tenant_id, item_id, location_id, lot_code, expiry_date,
  quantity_in_stock, quantity_reserved, quantity_unit, status
)
SELECT
  d.stock_id,
  i.tenant_id,
  i.item_id,
  p.location_id,
  d.lot_code,
  d.expiry_date,
  d.qty,
  d.reserved,
  'dose',
  d.status
FROM desired d
CROSS JOIN item i
CROSS JOIN park p
ON CONFLICT (stock_id) DO UPDATE
SET lot_code = EXCLUDED.lot_code,
    expiry_date = EXCLUDED.expiry_date,
    quantity_in_stock = EXCLUDED.quantity_in_stock,
    quantity_reserved = EXCLUDED.quantity_reserved,
    quantity_unit = EXCLUDED.quantity_unit,
    status = EXCLUDED.status,
    updated_at = now();

SELECT
  CASE WHEN count(*) = 5 THEN 'true' ELSE 'false' END AS gdev_et_lot_ok,
  count(*) AS gdev_et_lot_count
FROM inventory_stock st
JOIN inventory_items i
  ON i.tenant_id = st.tenant_id
 AND i.item_id = st.item_id
WHERE st.tenant_id = :'tenant_id'::uuid
  AND i.item_code = 'VAC-ET-PHC'
  AND st.lot_code LIKE 'GDEV-ET-%'
\gset

\if :gdev_et_lot_ok
\else
  \echo 'expected 5 google-dev ET lot rows but found' :gdev_et_lot_count
  \quit 1
\endif

COMMIT;
