\set ON_ERROR_STOP on

BEGIN;
SET LOCAL synchronous_commit = off;

-- Historical filename retained for CI compatibility. This is now a canonical
-- vaccination fixture: it must not insert any copied read-model rows under the
-- 5k-50k envelope.

INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES (
  '00000000-0000-4000-8000-000000003001'::uuid,
  :'tenant_id'::uuid,
  'park',
  'SCALE-CI-PARK',
  'Scale CI Park',
  'active'
)
ON CONFLICT (location_id) DO NOTHING;

INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES (
  'f4000000-0000-4000-8000-000000000001'::uuid,
  :'tenant_id'::uuid,
  'shed',
  'SCALE-CI-SHED',
  'Scale CI Shed',
  '00000000-0000-4000-8000-000000003001'::uuid,
  'active'
)
ON CONFLICT (location_id) DO NOTHING;

INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_vaccination, is_quarantine, is_icu)
VALUES (:'tenant_id'::uuid, 'f4000000-0000-4000-8000-000000000001'::uuid, true, false, false)
ON CONFLICT (tenant_id, location_id) DO UPDATE SET
  usable_for_vaccination = EXCLUDED.usable_for_vaccination,
  is_quarantine = EXCLUDED.is_quarantine,
  is_icu = EXCLUDED.is_icu;

INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
VALUES (
  'f3000000-0000-4000-8000-000000000001'::uuid,
  :'tenant_id'::uuid,
  'vaccination.scale_ci',
  'Scale CI Vaccination',
  'vaccination',
  'draft'
)
ON CONFLICT (protocol_id) DO NOTHING;

INSERT INTO protocol_versions (
  protocol_version_id, tenant_id, protocol_id, scope_type, version, status,
  effective_from, rule_dsl, proof_policy
) VALUES (
  'f3000000-0000-4000-8000-000000000002'::uuid,
  :'tenant_id'::uuid,
  'f3000000-0000-4000-8000-000000000001'::uuid,
  'tenant',
  1,
  'draft',
  current_date,
  '{}'::jsonb,
  '{}'::jsonb
)
ON CONFLICT (protocol_version_id) DO NOTHING;

INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
  eligibility_json, proof_policy
) VALUES (
  'f3000000-0000-4000-8000-000000000003'::uuid,
  :'tenant_id'::uuid,
  'f3000000-0000-4000-8000-000000000002'::uuid,
  'SCALE_CI_D1',
  1,
  'birth_age',
  '{"vaccine":{"display_name":"Scale CI Vaccine"}}'::jsonb,
  '{}'::jsonb
)
ON CONFLICT (rule_id) DO NOTHING;

DELETE FROM vaccination_completions
WHERE tenant_id = :'tenant_id'::uuid
  AND idempotency_key LIKE 'scale-ci-completion:%';

DELETE FROM obligation_instances
WHERE tenant_id = :'tenant_id'::uuid
  AND idempotency_key LIKE 'scale-ci-obligation:%';

DELETE FROM goats
WHERE tenant_id = :'tenant_id'::uuid
  AND goat_id::text LIKE 'f1000000-0000-4000-8000-%';

INSERT INTO goats (
  goat_id, tenant_id, display_id, species, sex, lifecycle_status,
  custodian_party_id, current_location_id, park_id, shed_id, management_stage, health_status
)
SELECT
  ('f1000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
  :'tenant_id'::uuid,
  'G-' || lpad((900000 + n)::text, 6, '0'),
  'goat',
  CASE WHEN n % 2 = 0 THEN 'female' ELSE 'male' END,
  'alive',
  '00000000-0000-4000-8000-000000001001'::uuid,
  'f4000000-0000-4000-8000-000000000001'::uuid,
  '00000000-0000-4000-8000-000000003001'::uuid,
  'f4000000-0000-4000-8000-000000000001'::uuid,
  'K1',
  'healthy'
FROM generate_series(1, :scale_rows::int) AS generated(n);

INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key, sequence
)
SELECT
  ('f2000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
  :'tenant_id'::uuid,
  'f3000000-0000-4000-8000-000000000002'::uuid,
  'f3000000-0000-4000-8000-000000000003'::uuid,
  'goat',
  ('f1000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
  'shed',
  'f4000000-0000-4000-8000-000000000001'::uuid,
  now() + ((n % 21) - 10) * interval '1 day',
  CASE WHEN n % 5 = 0 THEN 'scheduled' ELSE 'due' END,
  'scale-ci-obligation:' || n,
  1
FROM generate_series(1, :scale_rows::int) AS generated(n);

INSERT INTO vaccination_completions (
  completion_id, tenant_id, obligation_id, goat_id, administered_at, status,
  idempotency_key, recorded_by
)
SELECT
  ('f5000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
  :'tenant_id'::uuid,
  ('f2000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
  ('f1000000-0000-4000-8000-' || lpad(n::text, 12, '0'))::uuid,
  now() - ((n % 14) + 1) * interval '1 day',
  'accepted',
  'scale-ci-completion:' || n,
  NULL
FROM generate_series(1, GREATEST(1, :scale_rows::int / 10)) AS generated(n);

COMMIT;

ANALYZE locations;
ANALYZE location_operational_attributes;
ANALYZE protocol_definitions;
ANALYZE protocol_versions;
ANALYZE protocol_rules;
ANALYZE goats;
ANALYZE obligation_instances;
ANALYZE vaccination_completions;
