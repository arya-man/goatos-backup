-- +goose Up
-- Seed the neutral functional department vocabulary used by HR roster rows.
-- These rows do not grant module boundaries, navigation, or permissions.
WITH tenant_scope AS (
  SELECT tenant_id
  FROM tenants
  WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
     OR name ILIKE '%Mesha%'
  ORDER BY tenant_id = '00000000-0000-4000-8000-000000000001'::uuid DESC, created_at ASC
  LIMIT 1
)
INSERT INTO departments (tenant_id, code, label, status)
SELECT tenant_id, code, label, 'active'
FROM tenant_scope
CROSS JOIN (VALUES
  ('procurement', 'Procurement'),
  ('preventive_care', 'Preventive Care'),
  ('breeding', 'Breeding'),
  ('health', 'Health'),
  ('growth', 'Growth'),
  ('infrastructure', 'Infrastructure'),
  ('feed', 'Feed'),
  ('milk', 'Milk'),
  ('sales', 'Sales')
) AS seed(code, label)
ON CONFLICT (tenant_id, code) DO UPDATE SET
  label = EXCLUDED.label,
  status = 'active',
  updated_at = now();

-- +goose Down
WITH tenant_scope AS (
  SELECT tenant_id
  FROM tenants
  WHERE tenant_id = '00000000-0000-4000-8000-000000000001'::uuid
     OR name ILIKE '%Mesha%'
)
DELETE FROM departments d
USING tenant_scope ts
WHERE d.tenant_id = ts.tenant_id
  AND d.code IN (
    'procurement',
    'preventive_care',
    'breeding',
    'health',
    'growth',
    'infrastructure',
    'feed',
    'milk',
    'sales'
  );
