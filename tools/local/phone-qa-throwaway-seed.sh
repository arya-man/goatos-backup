#!/usr/bin/env bash
# Seed a throwaway phone-QA database for role-gated Vaccination + Weighing scans.
#
# This script is intentionally NOT for the canonical local app DB. Use a disposable
# Postgres port (for example 15544) and point the laptop API + phone at that DB.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tenant_id="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
today_sql="(now() AT TIME ZONE 'Asia/Kolkata')::date"

die() { echo "phone-qa-throwaway-seed: $*" >&2; exit 1; }

[ -n "${DATABASE_URL:-}" ] || die "DATABASE_URL is required"

case "$DATABASE_URL" in
  *127.0.0.1:15544/*|*localhost:15544/*) ;;
  *) die "refusing DATABASE_URL outside throwaway port 15544: ${DATABASE_URL%%\?*}" ;;
esac

case "${GOATOS_ENV:-}" in
  local|dev|test) ;;
  *) die "GOATOS_ENV must be local/dev/test for this seed" ;;
esac

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt -c "SELECT 1" >/dev/null

(
  cd "$repo_root/backend"
  go run ./cmd/seed-vaccination-per-goat-qa -tenant-id "$tenant_id"
)

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<SQL
BEGIN;

INSERT INTO departments (department_id, tenant_id, code, label, status)
VALUES
  ('93000000-0000-4000-8000-000000000001', '${tenant_id}'::uuid, 'preventive_care', 'Preventive Care', 'active'),
  ('93000000-0000-4000-8000-000000000002', '${tenant_id}'::uuid, 'growth', 'Growth', 'active'),
  ('93000000-0000-4000-8000-000000000003', '${tenant_id}'::uuid, 'operations', 'Operations', 'active'),
  ('93000000-0000-4000-8000-000000000004', '${tenant_id}'::uuid, 'verification', 'Verification', 'active'),
  ('93000000-0000-4000-8000-000000000005', '${tenant_id}'::uuid, 'leadership', 'Leadership', 'active')
ON CONFLICT (tenant_id, code) DO UPDATE
SET label = EXCLUDED.label, status = 'active', updated_at = now();

INSERT INTO department_module_grants (tenant_id, department_id, module_key, status)
SELECT '${tenant_id}'::uuid, d.department_id, m.module_key, 'active'
FROM departments d
JOIN (VALUES
  ('preventive_care', 'vaccination'),
  ('growth', 'weighing'),
  ('operations', 'vaccination'),
  ('operations', 'weighing'),
  ('verification', 'verification'),
  ('leadership', 'vaccination'),
  ('leadership', 'weighing'),
  ('leadership', 'counts')
) AS m(code, module_key) ON m.code = d.code
WHERE d.tenant_id = '${tenant_id}'::uuid
ON CONFLICT (tenant_id, department_id, module_key) DO UPDATE
SET status = 'active', updated_at = now();

INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, state_region, status, display_order, updated_at)
VALUES
  ('91000000-0000-4000-8000-000000000101', '${tenant_id}'::uuid, 'park', 'CBE-QA', 'CBE', '91000000-0000-4000-8000-000000000100', 'Karnataka', 'active', 20, now()),
  ('92000000-0000-4000-8000-000000000101', '${tenant_id}'::uuid, 'park', 'CPT-QA', 'CPT', '91000000-0000-4000-8000-000000000100', 'Karnataka', 'active', 25, now()),
  ('91000000-0000-4000-8000-000000000201', '${tenant_id}'::uuid, 'shed', 'CBE-GODEL-1-1-3', 'CBE - Godel 1 Parts 1-3', '91000000-0000-4000-8000-000000000101', 'Karnataka', 'active', 30, now()),
  ('91000000-0000-4000-8000-000000000202', '${tenant_id}'::uuid, 'shed', 'CPT-MANDELA-2-3-5', 'CPT - Mandela 2 Parts 3-5', '92000000-0000-4000-8000-000000000101', 'Karnataka', 'active', 40, now())
ON CONFLICT (location_id) DO UPDATE
SET location_code = EXCLUDED.location_code,
    name = EXCLUDED.name,
    parent_location_id = EXCLUDED.parent_location_id,
    status = 'active',
    updated_at = now();

INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_counts, usable_for_feed, usable_for_vaccination, usable_for_sop, is_holding, is_quarantine, is_icu, display_order, notes)
VALUES
  ('${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000101', true, true, true, true, false, false, false, 20, 'Phone QA CBE park'),
  ('${tenant_id}'::uuid, '92000000-0000-4000-8000-000000000101', true, true, true, true, false, false, false, 25, 'Phone QA CPT park'),
  ('${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000201', true, true, true, true, false, false, false, 30, 'Phone QA CBE weighing/vaccination shed'),
  ('${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000202', true, true, true, true, false, false, false, 40, 'Phone QA CPT weighing/vaccination shed')
ON CONFLICT (location_id) DO UPDATE
SET usable_for_vaccination = true, usable_for_sop = true, updated_at = now();

UPDATE protocol_versions
SET scope_id = '91000000-0000-4000-8000-000000000101',
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND protocol_version_id = '91000000-0000-4000-8000-000000000502';

UPDATE goats
SET park_id = CASE WHEN goat_id IN ('91000000-0000-4000-8000-000000001004', '91000000-0000-4000-8000-000000001005')
                   THEN '92000000-0000-4000-8000-000000000101'::uuid
                   ELSE '91000000-0000-4000-8000-000000000101'::uuid END,
    shed_id = CASE WHEN goat_id IN ('91000000-0000-4000-8000-000000001004', '91000000-0000-4000-8000-000000001005')
                   THEN '91000000-0000-4000-8000-000000000202'::uuid
                   ELSE '91000000-0000-4000-8000-000000000201'::uuid END,
    current_location_id = CASE WHEN goat_id IN ('91000000-0000-4000-8000-000000001004', '91000000-0000-4000-8000-000000001005')
                               THEN '91000000-0000-4000-8000-000000000202'::uuid
                               ELSE '91000000-0000-4000-8000-000000000201'::uuid END,
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND goat_id IN (
    '91000000-0000-4000-8000-000000001001',
    '91000000-0000-4000-8000-000000001002',
    '91000000-0000-4000-8000-000000001003',
    '91000000-0000-4000-8000-000000001004',
    '91000000-0000-4000-8000-000000001005'
  );

INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id, department_id, metadata, updated_at)
SELECT
  seed.workforce_member_id,
  seed.tenant_id,
  seed.user_id,
  seed.display_code,
  seed.display_name,
  seed.status,
  seed.primary_role_hint,
  seed.primary_location_id,
  d.department_id,
  seed.metadata,
  seed.updated_at
FROM (
  VALUES
    ('93000000-0000-4000-8000-000000000101'::uuid, '${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000101'::uuid, 'CEO-QA', 'CEO QA', 'active', 'cxo', NULL::uuid, 'leadership', '{"seed":"phone-qa"}'::jsonb, now()),
    ('93000000-0000-4000-8000-000000000102'::uuid, '${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000102'::uuid, 'CHANDRAKANT-QA', 'Chandrakant', 'active', 'pc_director', NULL::uuid, 'preventive_care', '{"seed":"phone-qa"}'::jsonb, now()),
    ('93000000-0000-4000-8000-000000000103'::uuid, '${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000103'::uuid, 'DINAKAR-QA', 'Dinakar', 'active', 'growth_director', NULL::uuid, 'growth', '{"seed":"phone-qa"}'::jsonb, now()),
    ('93000000-0000-4000-8000-000000000104'::uuid, '${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000104'::uuid, 'JYOTHI-QA', 'Jyothi', 'active', 'verifier', NULL::uuid, 'verification', '{"seed":"phone-qa"}'::jsonb, now()),
    ('93000000-0000-4000-8000-000000000201'::uuid, '${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000201'::uuid, 'AMIT-QA', 'Amit', 'active', 'operator', '92000000-0000-4000-8000-000000000101'::uuid, 'operations', '{"seed":"phone-qa"}'::jsonb, now()),
    ('93000000-0000-4000-8000-000000000202'::uuid, '${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000202'::uuid, 'PRAMOD-QA', 'Pramod', 'active', 'operator', '91000000-0000-4000-8000-000000000101'::uuid, 'operations', '{"seed":"phone-qa"}'::jsonb, now()),
    ('93000000-0000-4000-8000-000000000203'::uuid, '${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000203'::uuid, 'KUMAR-SHARATH-QA', 'Kumar Sharath', 'active', 'operator', '91000000-0000-4000-8000-000000000101'::uuid, 'operations', '{"seed":"phone-qa"}'::jsonb, now())
) AS seed(workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id, department_code, metadata, updated_at)
JOIN departments d ON d.tenant_id = seed.tenant_id AND d.code = seed.department_code
ON CONFLICT (tenant_id, display_code) DO UPDATE
SET user_id = EXCLUDED.user_id,
    display_name = EXCLUDED.display_name,
    status = 'active',
    primary_role_hint = EXCLUDED.primary_role_hint,
    primary_location_id = EXCLUDED.primary_location_id,
    department_id = EXCLUDED.department_id,
    metadata = EXCLUDED.metadata,
    updated_at = now();

UPDATE workforce_members
SET status = 'inactive',
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND user_id = '90000000-0000-4000-8000-000000000001'
  AND display_code = 'AMIT-QA-VAX';

DELETE FROM user_scope_grants
WHERE tenant_id = '${tenant_id}'::uuid
  AND user_id IN (
    '90000000-0000-4000-8000-000000000001',
    '90000000-0000-4000-8000-000000000101',
    '90000000-0000-4000-8000-000000000102',
    '90000000-0000-4000-8000-000000000103',
    '90000000-0000-4000-8000-000000000104',
    '90000000-0000-4000-8000-000000000201',
    '90000000-0000-4000-8000-000000000202',
    '90000000-0000-4000-8000-000000000203'
  );

INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES
  ('${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000101', 'ceo_internal', 'tenant', '${tenant_id}'::uuid, 'active', now()),
  ('${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000102', 'pc_director', 'tenant', '${tenant_id}'::uuid, 'active', now()),
  ('${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000103', 'growth_director', 'tenant', '${tenant_id}'::uuid, 'active', now()),
  ('${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000104', 'verifier', 'tenant', '${tenant_id}'::uuid, 'active', now()),
  ('${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000201', 'operator', 'park', '92000000-0000-4000-8000-000000000101', 'active', now()),
  ('${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000202', 'operator', 'park', '91000000-0000-4000-8000-000000000101', 'active', now()),
  ('${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000203', 'operator', 'park', '91000000-0000-4000-8000-000000000101', 'active', now());

UPDATE sop_tasks
SET assigned_to = '90000000-0000-4000-8000-000000000202',
    scope_id = '91000000-0000-4000-8000-000000000101',
    due_at = now() + interval '6 hours',
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND task_id = '91000000-0000-4000-8000-000000000702';

UPDATE obligation_batches
SET scope_id = '91000000-0000-4000-8000-000000000101',
    conducted_by = '93000000-0000-4000-8000-000000000202',
    planned_date = ${today_sql},
    window_start = now() - interval '1 hour',
    window_end = now() + interval '3 days',
    status = 'in_progress',
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND batch_id = '91000000-0000-4000-8000-000000000701';

UPDATE vaccination_drive_assignments
SET planned_date = ${today_sql},
    operator_id = CASE WHEN assignment_id = '91000000-0000-4000-8000-000000000801'
                       THEN '93000000-0000-4000-8000-000000000202'::uuid
                       ELSE '93000000-0000-4000-8000-000000000201'::uuid END,
    park_id = CASE WHEN assignment_id = '91000000-0000-4000-8000-000000000801'
                   THEN '91000000-0000-4000-8000-000000000101'::uuid
                   ELSE '92000000-0000-4000-8000-000000000101'::uuid END,
    shed_id = CASE WHEN assignment_id = '91000000-0000-4000-8000-000000000801'
                   THEN '91000000-0000-4000-8000-000000000201'::uuid
                   ELSE '91000000-0000-4000-8000-000000000202'::uuid END,
    physical_shed = CASE WHEN assignment_id = '91000000-0000-4000-8000-000000000801'
                         THEN 'CBE - Godel 1 Parts 1-3'
                         ELSE 'CPT - Mandela 2 Parts 3-5' END,
    animal_count = CASE WHEN assignment_id = '91000000-0000-4000-8000-000000000801'
                        THEN 3 ELSE 2 END,
    total_doses = CASE WHEN assignment_id = '91000000-0000-4000-8000-000000000801'
                       THEN 3 ELSE 2 END,
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND assignment_id IN ('91000000-0000-4000-8000-000000000801', '91000000-0000-4000-8000-000000000802');

INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_type, period_start_date, period_end_date, cadence_type, start_business_date, status, planned_cap_per_day, operator_user_id, published_at, created_by, updated_at)
VALUES
  ('92000000-0000-4000-8000-000000000701', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000101', 'week', ${today_sql}, ${today_sql} + 6, 'weekly_kids', ${today_sql}, 'published', 100, '90000000-0000-4000-8000-000000000202', now(), '90000000-0000-4000-8000-000000000103', now()),
  ('92000000-0000-4000-8000-000000000702', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000000101', 'week', ${today_sql}, ${today_sql} + 6, 'weekly_kids', ${today_sql}, 'published', 100, '90000000-0000-4000-8000-000000000201', now(), '90000000-0000-4000-8000-000000000103', now())
ON CONFLICT (campaign_id) DO UPDATE
SET period_start_date = EXCLUDED.period_start_date,
    period_end_date = EXCLUDED.period_end_date,
    start_business_date = EXCLUDED.start_business_date,
    status = CASE WHEN weighing_campaigns.status = 'completed' THEN 'published' ELSE weighing_campaigns.status END,
    operator_user_id = EXCLUDED.operator_user_id,
    updated_at = now();

INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, expected_animal_count, weighing_category, operator_user_id, status, updated_at)
VALUES
  ('92000000-0000-4000-8000-000000000801', '92000000-0000-4000-8000-000000000701', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000201', 'shed', 'CBE - Godel 1 Parts 1-3', 0, 'individual_animal', '90000000-0000-4000-8000-000000000202', 'pending', now()),
  ('92000000-0000-4000-8000-000000000802', '92000000-0000-4000-8000-000000000702', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000202', 'shed', 'CPT - Mandela 2 Parts 3-5', 0, 'individual_animal', '90000000-0000-4000-8000-000000000201', 'pending', now())
ON CONFLICT (campaign_shed_id) DO UPDATE
SET display_name = EXCLUDED.display_name,
    expected_animal_count = 0,
    weighing_category = 'individual_animal',
    operator_user_id = EXCLUDED.operator_user_id,
    status = CASE WHEN weighing_campaign_sheds.status = 'completed' THEN 'pending' ELSE weighing_campaign_sheds.status END,
    updated_at = now();

COMMIT;
SQL

cat <<EOF
Seeded throwaway phone QA DB on ${DATABASE_URL%%\?*}

Use these GOATOS_LOCAL_USER_ID values with tools/dev/android-dev-run.sh:
  CEO QA            90000000-0000-4000-8000-000000000101  ceo_internal       all modules + PA card
  Chandrakant       90000000-0000-4000-8000-000000000102  pc_director        Vaccination execute only
  Dinakar           90000000-0000-4000-8000-000000000103  growth_director    Weighing execute/reopen only
  Jyothi            90000000-0000-4000-8000-000000000104  verifier           Video review only
  Amit              90000000-0000-4000-8000-000000000201  operator/CPT       CPT Mandela 2 Parts 3-5
  Pramod            90000000-0000-4000-8000-000000000202  operator/CBE       CBE Godel 1 Parts 1-3
  Kumar Sharath     90000000-0000-4000-8000-000000000203  operator/CBE       spare CBE operator

Physical RFIDs:
  CBE/Godel:    901007000504418, 901007000504332, 901007000504407
  CPT/Mandela:  901007000504419, 901007000504392

Weighing stays free-flow: expected_animal_count=0 and no weighing_expected_animals rows are seeded.
EOF
