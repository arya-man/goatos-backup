#!/usr/bin/env bash
# Seed a throwaway phone-QA database for role-gated Vaccination + Weighing scans.
#
# This script is intentionally NOT for the canonical local app DB. Use a disposable
# Postgres port (for example 15544) and point the laptop API + phone at that DB.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tenant_id="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
today_sql="(now() AT TIME ZONE 'Asia/Kolkata')::date"
# Upper bound for the tag pool. The widened QA roster itself uses a 2,3,2,3...
# per-shed pattern below, but the base seeder still needs the five physical RFID
# identities to exist.
animals_per_shed="${GOATOS_ANIMALS_PER_SHED:-5}"

die() { echo "phone-qa-throwaway-seed: $*" >&2; exit 1; }

[ -n "${DATABASE_URL:-}" ] || die "DATABASE_URL is required"

case "$animals_per_shed" in
  ''|*[!0-9]*) die "GOATOS_ANIMALS_PER_SHED must be a positive integer, got '${animals_per_shed}'" ;;
esac
[ "$animals_per_shed" -ge 5 ] || die "GOATOS_ANIMALS_PER_SHED must be >= 5 (the fixture always seeds the 5 physical-RFID identities first), got ${animals_per_shed}"

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
  go run ./cmd/seed-vaccination-per-goat-qa -tenant-id "$tenant_id" -animals-per-shed "$animals_per_shed"
)

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<SQL
BEGIN;

SET LOCAL session_replication_role = replica;

INSERT INTO departments (department_id, tenant_id, code, label, status)
VALUES
  ('93000000-0000-4000-8000-000000000001', '${tenant_id}'::uuid, 'preventive_care', 'Preventive Care', 'active'),
  ('93000000-0000-4000-8000-000000000002', '${tenant_id}'::uuid, 'growth', 'Growth', 'active'),
  ('93000000-0000-4000-8000-000000000003', '${tenant_id}'::uuid, 'operations', 'Operations', 'active'),
  ('93000000-0000-4000-8000-000000000004', '${tenant_id}'::uuid, 'verification', 'Verification', 'active'),
  ('93000000-0000-4000-8000-000000000005', '${tenant_id}'::uuid, 'leadership', 'Leadership', 'active'),
  ('93000000-0000-4000-8000-000000000006', '${tenant_id}'::uuid, 'feed', 'Feed', 'active')
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
  ('operations', 'feed_direction'),
  ('verification', 'verification'),
  ('feed', 'feed_direction'),
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
  ('91000000-0000-4000-8000-000000000202', '${tenant_id}'::uuid, 'shed', 'CPT-MANDELA-2-1-2', 'CPT - Mandela 2 Parts 1-2', '92000000-0000-4000-8000-000000000101', 'Karnataka', 'active', 40, now())
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

-- Scope is set once, at creation, in cmd/seed-vaccination-per-goat-qa (TENANT scope so the
-- fixture's CPT sheds are covered too). It cannot be corrected here: a published version is
-- immutable by trigger, so this statement could only ever be a no-op that hid the real value.

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
    ('93000000-0000-4000-8000-000000000105'::uuid, '${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000105'::uuid, 'HEMANG-QA', 'Hemang', 'active', 'feed_director', NULL::uuid, 'feed', '{"seed":"phone-qa","golden_rule":"feed_director sees both parks read-only"}'::jsonb, now()),
    ('93000000-0000-4000-8000-000000000201'::uuid, '${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000201'::uuid, 'AMIT-QA', 'Amit', 'active', 'operator', '92000000-0000-4000-8000-000000000101'::uuid, 'operations', '{"seed":"phone-qa"}'::jsonb, now()),
    ('93000000-0000-4000-8000-000000000202'::uuid, '${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000202'::uuid, 'PRAMOD-QA', 'Pramod', 'active', 'operator', '91000000-0000-4000-8000-000000000101'::uuid, 'operations', '{"seed":"phone-qa"}'::jsonb, now()),
    ('93000000-0000-4000-8000-000000000203'::uuid, '${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000203'::uuid, 'KUMAR-SHARATH-QA', 'Kumar Sharath', 'active', 'operator', '91000000-0000-4000-8000-000000000101'::uuid, 'operations', '{"seed":"phone-qa"}'::jsonb, now()),
    -- OPERATORS are park people: two per park, each scoped to their own park.
    -- DIRECTORS are not. There is ONE director per module, covering BOTH parks: Dinakar runs
    -- weighing across CBE and CPT, Chandrakant runs vaccination across both. A director is split
    -- by MODULE, never by park.
    ('93000000-0000-4000-8000-000000000204'::uuid, '${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000204'::uuid, 'SAGAR-QA', 'Sagar', 'active', 'operator', '92000000-0000-4000-8000-000000000101'::uuid, 'operations', '{"seed":"phone-qa"}'::jsonb, now())
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
    '90000000-0000-4000-8000-000000000105',
    '90000000-0000-4000-8000-000000000201',
    '90000000-0000-4000-8000-000000000202',
    '90000000-0000-4000-8000-000000000203',
    '90000000-0000-4000-8000-000000000204'
  );

INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES
  ('${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000101', 'ceo_internal', 'tenant', '${tenant_id}'::uuid, 'active', now()),
  ('${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000102', 'pc_director', 'tenant', '${tenant_id}'::uuid, 'active', now()),
  -- ONE weighing director for the whole farm: tenant scope, so he covers CBE and CPT both. The
  -- module is what narrows a director (weighing here, vaccination for the PC director), not the park.
  ('${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000103', 'growth_director', 'tenant', '${tenant_id}'::uuid, 'active', now()),
  ('${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000105', 'feed_director', 'tenant', '${tenant_id}'::uuid, 'active', now()),
  ('${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000104', 'verifier', 'tenant', '${tenant_id}'::uuid, 'active', now()),
  ('${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000201', 'operator', 'park', '92000000-0000-4000-8000-000000000101', 'active', now()),
  ('${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000202', 'operator', 'park', '91000000-0000-4000-8000-000000000101', 'active', now()),
  ('${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000203', 'operator', 'park', '91000000-0000-4000-8000-000000000101', 'active', now()),
  ('${tenant_id}'::uuid, '90000000-0000-4000-8000-000000000204', 'operator', 'park', '92000000-0000-4000-8000-000000000101', 'active', now());

-- Workforce positions for the verifier. The verifier must hold active position rows with verify duties
-- to pass authorization checks in backend/internal/workforce/adapters/postgres/repository.go:ListVerifyModuleKeys.
-- position_tier MUST be 'assistant', not 'director', because backend/internal/obligation/adapters/postgres/
-- visit_shot_lock.go branches on "position_tier <> 'director'" and exempts directors from shot-lock enforcement.
-- A real verifier is not a director and must obey all rules applied to her role.
INSERT INTO workforce_positions (
  position_id, tenant_id, workforce_member_id, scope_type, scope_id, position_code,
  position_tier, status, valid_from, valid_to
)
VALUES (
  '93000000-0000-4000-8000-000000010104'::uuid,
  '${tenant_id}'::uuid,
  '93000000-0000-4000-8000-000000000104'::uuid,
  'tenant',
  '${tenant_id}'::uuid,
  'jyothi_verifier',
  'assistant',
  'active',
  now() - interval '1 day',
  NULL
)
ON CONFLICT (tenant_id, scope_type, scope_id, position_code) WHERE status = 'active'
DO UPDATE
SET workforce_member_id = EXCLUDED.workforce_member_id,
    position_tier = EXCLUDED.position_tier,
    valid_from = EXCLUDED.valid_from,
    valid_to = EXCLUDED.valid_to,
    updated_at = now();

-- Position module duties for every module that the verification registry declares a category for.
-- These duties authorize the verifier to review proofs in each module's verify queue.
-- Module codes are position_module_duties.module_code values, which are NOT the navigation keys.
-- The canonical list is notificationbridge's own duty modules (moduleVaccination =
-- "pc.vaccination", moduleWeighing = "weighing", dutyModuleFeed = "feed.direction",
-- dutyModuleCounts = "counts").
--
-- Two vocabularies exist and only ONE of them is forgiving. The verify-queue gate translates
-- ("vaccination" and "pc.vaccination" both resolve -- see
-- backend/internal/verification/adapters/http/duty_module_keys.go), but
-- ResolveModuleDutyRecipients matches module_code EXACTLY. Seeding the navigation spelling
-- therefore produces a verifier who can open the queue and still receives zero pushes: half
-- working, silently. That is the same failure recorded on
-- notificationbridge.PendingNotificationDutyModules, where the seeder emitted only
-- execute/manage rows and every verifier push in the field resolved to zero devices.
INSERT INTO position_module_duties (
  tenant_id, position_code, module_code, duty_type, capability_code,
  effective_from, effective_to, status
)
SELECT
  '${tenant_id}'::uuid,
  'jyothi_verifier',
  m.module_code,
  'verify',
  'vaccination.verify',
  now() - interval '1 day',
  NULL,
  'active'
FROM (
  VALUES
    ('pc.vaccination'),
    ('weighing'),
    ('aas_health'),
    ('counts'),
    ('feed.direction')
) AS m(module_code)
ON CONFLICT (tenant_id, position_code, module_code, duty_type, effective_from)
DO UPDATE
SET status = 'active',
    effective_to = EXCLUDED.effective_to,
    updated_at = now();

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

-- A vaccination drive is a PARK VISIT: one batch + one park-scoped sop_task per park.
-- The Go seed creates the CBE drive (batch ...0701 / task ...0702). This fixture adds the
-- second park (CPT), so CPT needs its OWN drive too. Hanging CPT goats off the CBE task made
-- taskExecutionIdentity() return no row for a CPT shed (the task's park never matches the
-- goat's park), which the app surfaced as an unscannable, empty shed.
INSERT INTO sop_tasks (
  task_id, tenant_id, sop_id, sop_version_id, task_type, title, description, state,
  assigned_to, scope_type, scope_id, priority, due_at, context, created_by, created_at, updated_at
)
VALUES (
  '92000000-0000-4000-8000-000000000712',
  '${tenant_id}'::uuid,
  '91000000-0000-4000-8000-000000000401',
  '91000000-0000-4000-8000-000000000402',
  'vaccination',
  'Per-animal vaccination proof QA (CPT)',
  'CPT park drive for the phone-QA throwaway fixture.',
  'assigned',
  '90000000-0000-4000-8000-000000000201',
  'park',
  '92000000-0000-4000-8000-000000000101',
  'normal',
  now() + interval '6 hours',
  jsonb_build_object('obligation_batch_id', '92000000-0000-4000-8000-000000000711'),
  '90000000-0000-4000-8000-000000000201',
  now(), now()
)
ON CONFLICT (task_id) DO UPDATE
SET assigned_to = EXCLUDED.assigned_to,
    scope_type = EXCLUDED.scope_type,
    scope_id = EXCLUDED.scope_id,
    state = EXCLUDED.state,
    due_at = EXCLUDED.due_at,
    context = EXCLUDED.context,
    updated_at = now();

INSERT INTO obligation_batches (
  batch_id, tenant_id, protocol_version_id, scope_type, scope_id, session, planned_date,
  window_start, window_end, status, estimated_targets, planned_quantity, quantity_unit,
  primary_inventory_lot_id, sop_task_id, conducted_by, created_at, updated_at
)
VALUES (
  '92000000-0000-4000-8000-000000000711',
  '${tenant_id}'::uuid,
  '91000000-0000-4000-8000-000000000502',
  'park',
  '92000000-0000-4000-8000-000000000101',
  'qa-per-goat-proof-cpt',
  ${today_sql},
  now() - interval '1 hour',
  now() + interval '3 days',
  'in_progress',
  5, 5, 'dose',
  '91000000-0000-4000-8000-000000000602',
  '92000000-0000-4000-8000-000000000712',
  '93000000-0000-4000-8000-000000000201',
  now(), now()
)
ON CONFLICT (batch_id) DO UPDATE
SET scope_type = EXCLUDED.scope_type,
    scope_id = EXCLUDED.scope_id,
    planned_date = EXCLUDED.planned_date,
    window_start = EXCLUDED.window_start,
    window_end = EXCLUDED.window_end,
    status = EXCLUDED.status,
    sop_task_id = EXCLUDED.sop_task_id,
    conducted_by = EXCLUDED.conducted_by,
    updated_at = now();

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
  -- Seeded as DRAFT on purpose. weighing_work_items -- the per-shed task rows that surface on an
  -- assignee's landing page -- are created ONLY by the publish path
  -- (createWorkItemsForPublishTx). Inserting these campaigns already-published skipped that
  -- kernel step, which is why the work-item table stayed empty and nobody saw weighing work.
  -- tools/local/phone-qa-throwaway-run.sh publishes them through the real service afterwards.
  ('92000000-0000-4000-8000-000000000701', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000101', 'week', ${today_sql}, ${today_sql} + 6, 'weekly_kids', ${today_sql}, 'draft', 100, '90000000-0000-4000-8000-000000000202', NULL, '90000000-0000-4000-8000-000000000103', now()),
  ('92000000-0000-4000-8000-000000000702', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000000101', 'week', ${today_sql}, ${today_sql} + 6, 'weekly_kids', ${today_sql}, 'draft', 100, '90000000-0000-4000-8000-000000000201', NULL, '90000000-0000-4000-8000-000000000103', now())
ON CONFLICT (campaign_id) DO UPDATE
SET period_start_date = EXCLUDED.period_start_date,
    period_end_date = EXCLUDED.period_end_date,
    start_business_date = EXCLUDED.start_business_date,
    status = CASE WHEN weighing_campaigns.status = 'completed' THEN 'published' ELSE weighing_campaigns.status END,
    operator_user_id = EXCLUDED.operator_user_id,
    updated_at = now();

INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, expected_animal_count, weighing_category, operator_user_id, status, park_id, start_business_date, updated_at)
VALUES
  ('92000000-0000-4000-8000-000000000801', '92000000-0000-4000-8000-000000000701', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000201', 'shed', 'CBE - Godel 1 Parts 1-3', 0, 'individual_animal', '90000000-0000-4000-8000-000000000202', 'pending', '91000000-0000-4000-8000-000000000101', ${today_sql}, now()),
  ('92000000-0000-4000-8000-000000000802', '92000000-0000-4000-8000-000000000702', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000202', 'shed', 'CPT - Mandela 2 Parts 3-5', 0, 'individual_animal', '90000000-0000-4000-8000-000000000201', 'pending', '92000000-0000-4000-8000-000000000101', ${today_sql}, now())
ON CONFLICT (tenant_id, campaign_id, location_id, COALESCE(partition_label, ''::text)) DO UPDATE
SET display_name = EXCLUDED.display_name,
    expected_animal_count = 0,
    weighing_category = 'individual_animal',
    operator_user_id = EXCLUDED.operator_user_id,
    park_id = EXCLUDED.park_id,
    start_business_date = EXCLUDED.start_business_date,
    status = CASE WHEN weighing_campaign_sheds.status = 'completed' THEN 'pending' ELSE weighing_campaign_sheds.status END,
    updated_at = now();

-- Phone QA matrix:
--   CBE has 5 real-RFID goats split 2 + 3 across two sheds.
--   CPT has 5 separate synthetic-tag goats split 2 + 3 across two sheds.
-- Goat identity stays honest: goat_identifiers is unique by (tenant_id, normalized_value),
-- so the same physical RFID is never assigned to two goats. Weighing remains free-flow,
-- so the same physical RFID text may still be scanned into any weighing shed bucket.
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, state_region, status, display_order, updated_at)
VALUES
  ('91000000-0000-4000-8000-000000000201', '${tenant_id}'::uuid, 'shed', 'CBE-GODEL-1-1-2', 'CBE - Godel 1 Parts 1-2', '91000000-0000-4000-8000-000000000101', 'Karnataka', 'active', 30, now()),
  ('91000000-0000-4000-8000-000000000203', '${tenant_id}'::uuid, 'shed', 'CBE-GODEL-1-3-5', 'CBE - Godel 1 Parts 3-5', '91000000-0000-4000-8000-000000000101', 'Karnataka', 'active', 35, now()),
  ('91000000-0000-4000-8000-000000000202', '${tenant_id}'::uuid, 'shed', 'CPT-MANDELA-2-1-2', 'CPT - Mandela 2 Parts 1-2', '92000000-0000-4000-8000-000000000101', 'Karnataka', 'active', 40, now()),
  ('92000000-0000-4000-8000-000000000203', '${tenant_id}'::uuid, 'shed', 'CPT-MANDELA-2-3-5', 'CPT - Mandela 2 Parts 3-5', '92000000-0000-4000-8000-000000000101', 'Karnataka', 'active', 45, now())
ON CONFLICT (location_id) DO UPDATE
SET location_code = EXCLUDED.location_code,
    name = EXCLUDED.name,
    parent_location_id = EXCLUDED.parent_location_id,
    status = 'active',
    display_order = EXCLUDED.display_order,
    updated_at = now();

INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_counts, usable_for_feed, usable_for_vaccination, usable_for_sop, is_holding, is_quarantine, is_icu, display_order, notes)
VALUES
  ('${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000201', true, true, true, true, false, false, false, 30, 'Phone QA CBE 2-animal shed'),
  ('${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000203', true, true, true, true, false, false, false, 35, 'Phone QA CBE 3-animal shed'),
  ('${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000202', true, true, true, true, false, false, false, 40, 'Phone QA CPT 2-animal shed'),
  ('${tenant_id}'::uuid, '92000000-0000-4000-8000-000000000203', true, true, true, true, false, false, false, 45, 'Phone QA CPT 3-animal shed')
ON CONFLICT (location_id) DO UPDATE
SET usable_for_vaccination = true,
    usable_for_sop = true,
    notes = EXCLUDED.notes,
    display_order = EXCLUDED.display_order,
    updated_at = now();

UPDATE goats
SET park_id = CASE
      WHEN goat_id IN ('91000000-0000-4000-8000-000000001001','91000000-0000-4000-8000-000000001002','91000000-0000-4000-8000-000000001003','91000000-0000-4000-8000-000000001004','91000000-0000-4000-8000-000000001005')
        THEN '91000000-0000-4000-8000-000000000101'::uuid
      ELSE park_id
    END,
    shed_id = CASE
      WHEN goat_id IN ('91000000-0000-4000-8000-000000001001','91000000-0000-4000-8000-000000001002')
        THEN '91000000-0000-4000-8000-000000000201'::uuid
      WHEN goat_id IN ('91000000-0000-4000-8000-000000001003','91000000-0000-4000-8000-000000001004','91000000-0000-4000-8000-000000001005')
        THEN '91000000-0000-4000-8000-000000000203'::uuid
      ELSE shed_id
    END,
    current_location_id = CASE
      WHEN goat_id IN ('91000000-0000-4000-8000-000000001001','91000000-0000-4000-8000-000000001002')
        THEN '91000000-0000-4000-8000-000000000201'::uuid
      WHEN goat_id IN ('91000000-0000-4000-8000-000000001003','91000000-0000-4000-8000-000000001004','91000000-0000-4000-8000-000000001005')
        THEN '91000000-0000-4000-8000-000000000203'::uuid
      ELSE current_location_id
    END,
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND goat_id IN (
    '91000000-0000-4000-8000-000000001001',
    '91000000-0000-4000-8000-000000001002',
    '91000000-0000-4000-8000-000000001003',
    '91000000-0000-4000-8000-000000001004',
    '91000000-0000-4000-8000-000000001005'
  );

INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, health_status, custodian_party_id, current_location_id, farm_id, park_id, shed_id, dob, origin_type, entry_date)
VALUES
  ('92000000-0000-4000-8000-000000001001', '${tenant_id}'::uuid, 'G-920001', 'female', 'kid', 'alive', 'K2', 'healthy', '91000000-0000-4000-8000-000000000301', '91000000-0000-4000-8000-000000000202', '91000000-0000-4000-8000-000000000100', '92000000-0000-4000-8000-000000000101', '91000000-0000-4000-8000-000000000202', DATE '2026-05-15', 'birth', DATE '2026-05-15'),
  ('92000000-0000-4000-8000-000000001002', '${tenant_id}'::uuid, 'G-920002', 'male', 'kid', 'alive', 'K2', 'healthy', '91000000-0000-4000-8000-000000000301', '91000000-0000-4000-8000-000000000202', '91000000-0000-4000-8000-000000000100', '92000000-0000-4000-8000-000000000101', '91000000-0000-4000-8000-000000000202', DATE '2026-05-16', 'birth', DATE '2026-05-16'),
  ('92000000-0000-4000-8000-000000001003', '${tenant_id}'::uuid, 'G-920003', 'female', 'kid', 'alive', 'K2', 'healthy', '91000000-0000-4000-8000-000000000301', '92000000-0000-4000-8000-000000000203', '91000000-0000-4000-8000-000000000100', '92000000-0000-4000-8000-000000000101', '92000000-0000-4000-8000-000000000203', DATE '2026-05-17', 'birth', DATE '2026-05-17'),
  ('92000000-0000-4000-8000-000000001004', '${tenant_id}'::uuid, 'G-920004', 'female', 'kid', 'alive', 'K2', 'healthy', '91000000-0000-4000-8000-000000000301', '92000000-0000-4000-8000-000000000203', '91000000-0000-4000-8000-000000000100', '92000000-0000-4000-8000-000000000101', '92000000-0000-4000-8000-000000000203', DATE '2026-05-18', 'birth', DATE '2026-05-18'),
  ('92000000-0000-4000-8000-000000001005', '${tenant_id}'::uuid, 'G-920005', 'male', 'kid', 'alive', 'K2', 'healthy', '91000000-0000-4000-8000-000000000301', '92000000-0000-4000-8000-000000000203', '91000000-0000-4000-8000-000000000100', '92000000-0000-4000-8000-000000000101', '92000000-0000-4000-8000-000000000203', DATE '2026-05-19', 'birth', DATE '2026-05-19')
ON CONFLICT (goat_id) DO UPDATE
SET lifecycle_status = 'alive',
    management_stage = 'K2',
    health_status = 'healthy',
    current_location_id = EXCLUDED.current_location_id,
    farm_id = EXCLUDED.farm_id,
    park_id = EXCLUDED.park_id,
    shed_id = EXCLUDED.shed_id,
    updated_at = now();

INSERT INTO goat_identifiers (identifier_id, tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, source_system, source_record_id, normalizer_version, confidence)
VALUES
  ('92000000-0000-4000-8000-000000002001', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000001001', 'temporary_tag', 'QA-CPT-0001', 'QA-CPT-0001', 'tenant:${tenant_id}', true, 'active', now(), 'phone-qa-throwaway-seed', 'qa-cpt-1', 'seed-v1', 1.0),
  ('92000000-0000-4000-8000-000000002002', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000001002', 'temporary_tag', 'QA-CPT-0002', 'QA-CPT-0002', 'tenant:${tenant_id}', true, 'active', now(), 'phone-qa-throwaway-seed', 'qa-cpt-2', 'seed-v1', 1.0),
  ('92000000-0000-4000-8000-000000002003', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000001003', 'temporary_tag', 'QA-CPT-0003', 'QA-CPT-0003', 'tenant:${tenant_id}', true, 'active', now(), 'phone-qa-throwaway-seed', 'qa-cpt-3', 'seed-v1', 1.0),
  ('92000000-0000-4000-8000-000000002004', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000001004', 'temporary_tag', 'QA-CPT-0004', 'QA-CPT-0004', 'tenant:${tenant_id}', true, 'active', now(), 'phone-qa-throwaway-seed', 'qa-cpt-4', 'seed-v1', 1.0),
  ('92000000-0000-4000-8000-000000002005', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000001005', 'temporary_tag', 'QA-CPT-0005', 'QA-CPT-0005', 'tenant:${tenant_id}', true, 'active', now(), 'phone-qa-throwaway-seed', 'qa-cpt-5', 'seed-v1', 1.0)
ON CONFLICT (tenant_id, normalized_value) DO UPDATE
SET goat_id = EXCLUDED.goat_id,
    identifier_value = EXCLUDED.identifier_value,
    is_primary_for_goat = EXCLUDED.is_primary_for_goat,
    status = 'active',
    updated_at = now();

-- The CBE batch stays PARK-scoped. It was previously widened to tenant scope only because it
-- straddled both parks; now that CPT owns its own drive this batch holds CBE animals only, and a
-- drive is a park visit. Five CBE animals, not ten.
UPDATE obligation_batches
SET estimated_targets = 5,
    planned_quantity = 5,
    scope_type = 'park',
    scope_id = '91000000-0000-4000-8000-000000000101',
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND batch_id = '91000000-0000-4000-8000-000000000701';

INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, window_start, window_end, status, sop_task_id, idempotency_key, sequence)
VALUES
  ('92000000-0000-4000-8000-000000003001', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000502', '91000000-0000-4000-8000-000000000503', '92000000-0000-4000-8000-000000000711', 'goat', '92000000-0000-4000-8000-000000001001', 'shed', '91000000-0000-4000-8000-000000000202', now(), now() - interval '1 hour', now() + interval '3 days', 'due', '92000000-0000-4000-8000-000000000712', 'qa-vax-per-goat-cpt-1', 1),
  ('92000000-0000-4000-8000-000000003002', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000502', '91000000-0000-4000-8000-000000000503', '92000000-0000-4000-8000-000000000711', 'goat', '92000000-0000-4000-8000-000000001002', 'shed', '91000000-0000-4000-8000-000000000202', now(), now() - interval '1 hour', now() + interval '3 days', 'due', '92000000-0000-4000-8000-000000000712', 'qa-vax-per-goat-cpt-2', 1),
  ('92000000-0000-4000-8000-000000003003', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000502', '91000000-0000-4000-8000-000000000503', '92000000-0000-4000-8000-000000000711', 'goat', '92000000-0000-4000-8000-000000001003', 'shed', '92000000-0000-4000-8000-000000000203', now(), now() - interval '1 hour', now() + interval '3 days', 'due', '92000000-0000-4000-8000-000000000712', 'qa-vax-per-goat-cpt-3', 1),
  ('92000000-0000-4000-8000-000000003004', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000502', '91000000-0000-4000-8000-000000000503', '92000000-0000-4000-8000-000000000711', 'goat', '92000000-0000-4000-8000-000000001004', 'shed', '92000000-0000-4000-8000-000000000203', now(), now() - interval '1 hour', now() + interval '3 days', 'due', '92000000-0000-4000-8000-000000000712', 'qa-vax-per-goat-cpt-4', 1),
  ('92000000-0000-4000-8000-000000003005', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000502', '91000000-0000-4000-8000-000000000503', '92000000-0000-4000-8000-000000000711', 'goat', '92000000-0000-4000-8000-000000001005', 'shed', '92000000-0000-4000-8000-000000000203', now(), now() - interval '1 hour', now() + interval '3 days', 'due', '92000000-0000-4000-8000-000000000712', 'qa-vax-per-goat-cpt-5', 1)
ON CONFLICT (obligation_id) DO UPDATE
SET batch_id = EXCLUDED.batch_id,
    status = 'due',
    scope_type = EXCLUDED.scope_type,
    scope_id = EXCLUDED.scope_id,
    sop_task_id = EXCLUDED.sop_task_id,
    due_at = now(),
    updated_at = now();

UPDATE obligation_instances
SET scope_id = CASE
      WHEN target_id IN ('91000000-0000-4000-8000-000000001001','91000000-0000-4000-8000-000000001002')
        THEN '91000000-0000-4000-8000-000000000201'::uuid
      WHEN target_id IN ('91000000-0000-4000-8000-000000001003','91000000-0000-4000-8000-000000001004','91000000-0000-4000-8000-000000001005')
        THEN '91000000-0000-4000-8000-000000000203'::uuid
      ELSE scope_id
    END,
    status = 'due',
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND obligation_id IN (
    '91000000-0000-4000-8000-000000003001',
    '91000000-0000-4000-8000-000000003002',
    '91000000-0000-4000-8000-000000003003',
    '91000000-0000-4000-8000-000000003004',
    '91000000-0000-4000-8000-000000003005'
  );

DELETE FROM vaccination_drive_assignment_members
WHERE tenant_id = '${tenant_id}'::uuid;

DELETE FROM vaccination_drive_assignments
WHERE tenant_id = '${tenant_id}'::uuid;

INSERT INTO vaccination_drive_assignments (assignment_id, tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count, capacity_status, warnings, vaccine_rule_ids, total_doses)
VALUES
  ('91000000-0000-4000-8000-000000000801', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000701', ${today_sql}, '93000000-0000-4000-8000-000000000202', '91000000-0000-4000-8000-000000000101', '91000000-0000-4000-8000-000000000201', 'CBE - Godel 1 Parts 1-2', 'whole', 2, 'within_cap', '[]'::jsonb, ARRAY['91000000-0000-4000-8000-000000000503']::uuid[], 2),
  ('91000000-0000-4000-8000-000000000803', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000701', ${today_sql}, '93000000-0000-4000-8000-000000000202', '91000000-0000-4000-8000-000000000101', '91000000-0000-4000-8000-000000000203', 'CBE - Godel 1 Parts 3-5', 'whole', 3, 'within_cap', '[]'::jsonb, ARRAY['91000000-0000-4000-8000-000000000503']::uuid[], 3),
  ('91000000-0000-4000-8000-000000000802', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000000711', ${today_sql}, '93000000-0000-4000-8000-000000000201', '92000000-0000-4000-8000-000000000101', '91000000-0000-4000-8000-000000000202', 'CPT - Mandela 2 Parts 1-2', 'whole', 2, 'within_cap', '[]'::jsonb, ARRAY['91000000-0000-4000-8000-000000000503']::uuid[], 2),
  ('91000000-0000-4000-8000-000000000804', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000000711', ${today_sql}, '93000000-0000-4000-8000-000000000201', '92000000-0000-4000-8000-000000000101', '92000000-0000-4000-8000-000000000203', 'CPT - Mandela 2 Parts 3-5', 'whole', 3, 'within_cap', '[]'::jsonb, ARRAY['91000000-0000-4000-8000-000000000503']::uuid[], 3)
ON CONFLICT (assignment_id) DO UPDATE
SET batch_id = EXCLUDED.batch_id,
    planned_date = EXCLUDED.planned_date,
    operator_id = EXCLUDED.operator_id,
    park_id = EXCLUDED.park_id,
    shed_id = EXCLUDED.shed_id,
    physical_shed = EXCLUDED.physical_shed,
    animal_count = EXCLUDED.animal_count,
    total_doses = EXCLUDED.total_doses,
    updated_at = now();

DELETE FROM vaccination_drive_assignment_members
WHERE tenant_id = '${tenant_id}'::uuid
  AND assignment_id IN (
    '91000000-0000-4000-8000-000000000801',
    '91000000-0000-4000-8000-000000000802',
    '91000000-0000-4000-8000-000000000803',
    '91000000-0000-4000-8000-000000000804'
  );

INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
SELECT '${tenant_id}'::uuid,
       CASE
         WHEN oi.scope_id = '91000000-0000-4000-8000-000000000201' THEN '91000000-0000-4000-8000-000000000801'::uuid
         WHEN oi.scope_id = '91000000-0000-4000-8000-000000000203' THEN '91000000-0000-4000-8000-000000000803'::uuid
         WHEN oi.scope_id = '91000000-0000-4000-8000-000000000202' THEN '91000000-0000-4000-8000-000000000802'::uuid
         ELSE '91000000-0000-4000-8000-000000000804'::uuid
       END,
       oi.obligation_id,
       oi.target_id
FROM obligation_instances oi
WHERE oi.tenant_id = '${tenant_id}'::uuid
  AND oi.batch_id IN ('91000000-0000-4000-8000-000000000701', '92000000-0000-4000-8000-000000000711')
  AND oi.target_id IN (
    '91000000-0000-4000-8000-000000001001',
    '91000000-0000-4000-8000-000000001002',
    '91000000-0000-4000-8000-000000001003',
    '91000000-0000-4000-8000-000000001004',
    '91000000-0000-4000-8000-000000001005',
    '92000000-0000-4000-8000-000000001001',
    '92000000-0000-4000-8000-000000001002',
    '92000000-0000-4000-8000-000000001003',
    '92000000-0000-4000-8000-000000001004',
    '92000000-0000-4000-8000-000000001005'
  )
ON CONFLICT (tenant_id, obligation_id) DO UPDATE
SET assignment_id = EXCLUDED.assignment_id;

INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, expected_animal_count, weighing_category, operator_user_id, status, park_id, start_business_date, updated_at)
VALUES
  -- ONE weighing task per park, sheds SPLIT between its assignees (the CEO decides the split).
  -- A shed carries exactly one assignee (1:1); the task carries many. Here the CBE task is shared
  -- by the CBE operator and the Growth Director, so the operator/director split can be exercised
  -- side by side: only the director may reopen a submitted scope, and only he closes it.
  ('92000000-0000-4000-8000-000000000801', '92000000-0000-4000-8000-000000000701', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000201', 'shed', 'Godel 1', 0, 'individual_animal', '90000000-0000-4000-8000-000000000202', 'pending', '91000000-0000-4000-8000-000000000101', ${today_sql}, now()),
  ('92000000-0000-4000-8000-000000000803', '92000000-0000-4000-8000-000000000701', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000203', 'shed', 'Yashoda 1', 0, 'individual_animal', '90000000-0000-4000-8000-000000000103', 'pending', '91000000-0000-4000-8000-000000000101', ${today_sql}, now()),
  ('92000000-0000-4000-8000-000000000802', '92000000-0000-4000-8000-000000000702', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000202', 'shed', 'Mandela 2', 0, 'individual_animal', '90000000-0000-4000-8000-000000000201', 'pending', '92000000-0000-4000-8000-000000000101', ${today_sql}, now()),
  ('92000000-0000-4000-8000-000000000804', '92000000-0000-4000-8000-000000000702', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000000203', 'shed', 'Castro 1', 0, 'individual_animal', '90000000-0000-4000-8000-000000000201', 'pending', '92000000-0000-4000-8000-000000000101', ${today_sql}, now())
ON CONFLICT (tenant_id, campaign_id, location_id, COALESCE(partition_label, ''::text)) DO UPDATE
SET location_id = EXCLUDED.location_id,
    display_name = EXCLUDED.display_name,
    expected_animal_count = 0,
    weighing_category = 'individual_animal',
    operator_user_id = EXCLUDED.operator_user_id,
    park_id = EXCLUDED.park_id,
    start_business_date = EXCLUDED.start_business_date,
    status = CASE WHEN weighing_campaign_sheds.status = 'completed' THEN 'pending' ELSE weighing_campaign_sheds.status END,
    updated_at = now();

COMMIT;
SQL

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<SQL
BEGIN;

-- Final phone-test matrix:
--   CBE/Godel 1:     plain shed, 2 goats, real physical RFIDs.
--   CBE/Yashoda 1:   partition Parts 1-3, 3 goats, real physical RFIDs.
--   CPT/Mandela 2:   plain shed, 2 goats, transformed CPT-<RFID> identifiers.
--   CPT/Castro 1:    partition Parts 1-3, 3 goats, transformed CPT-<RFID> identifiers.
-- The Android dev build rewrites vaccination reads for CPT shed IDs only. Weighing keeps raw RFID.
UPDATE locations
SET location_code = CASE location_id
      WHEN '91000000-0000-4000-8000-000000000201' THEN 'CBE-GODEL-1'
      WHEN '91000000-0000-4000-8000-000000000203' THEN 'CBE-YASHODA-1'
      WHEN '91000000-0000-4000-8000-000000000202' THEN 'CPT-MANDELA-2'
      WHEN '92000000-0000-4000-8000-000000000203' THEN 'CPT-CASTRO-1'
      ELSE location_code
    END,
    name = CASE location_id
      WHEN '91000000-0000-4000-8000-000000000201' THEN 'Godel 1'
      WHEN '91000000-0000-4000-8000-000000000203' THEN 'Yashoda 1'
      WHEN '91000000-0000-4000-8000-000000000202' THEN 'Mandela 2'
      WHEN '92000000-0000-4000-8000-000000000203' THEN 'Castro 1'
      ELSE name
    END,
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND location_id IN (
    '91000000-0000-4000-8000-000000000201',
    '91000000-0000-4000-8000-000000000203',
    '91000000-0000-4000-8000-000000000202',
    '92000000-0000-4000-8000-000000000203'
  );

UPDATE goats
SET park_id = CASE
      WHEN goat_id::text LIKE '91000000-0000-4000-8000-000000001%' THEN '91000000-0000-4000-8000-000000000101'::uuid
      WHEN goat_id::text LIKE '92000000-0000-4000-8000-000000001%' THEN '92000000-0000-4000-8000-000000000101'::uuid
      ELSE park_id
    END,
    shed_id = CASE
      WHEN goat_id IN ('91000000-0000-4000-8000-000000001001','91000000-0000-4000-8000-000000001002') THEN '91000000-0000-4000-8000-000000000201'::uuid
      WHEN goat_id IN ('91000000-0000-4000-8000-000000001003','91000000-0000-4000-8000-000000001004','91000000-0000-4000-8000-000000001005') THEN '91000000-0000-4000-8000-000000000203'::uuid
      WHEN goat_id IN ('92000000-0000-4000-8000-000000001001','92000000-0000-4000-8000-000000001002') THEN '91000000-0000-4000-8000-000000000202'::uuid
      WHEN goat_id IN ('92000000-0000-4000-8000-000000001003','92000000-0000-4000-8000-000000001004','92000000-0000-4000-8000-000000001005') THEN '92000000-0000-4000-8000-000000000203'::uuid
      ELSE shed_id
    END,
    current_location_id = CASE
      WHEN goat_id IN ('91000000-0000-4000-8000-000000001001','91000000-0000-4000-8000-000000001002') THEN '91000000-0000-4000-8000-000000000201'::uuid
      WHEN goat_id IN ('91000000-0000-4000-8000-000000001003','91000000-0000-4000-8000-000000001004','91000000-0000-4000-8000-000000001005') THEN '91000000-0000-4000-8000-000000000203'::uuid
      WHEN goat_id IN ('92000000-0000-4000-8000-000000001001','92000000-0000-4000-8000-000000001002') THEN '91000000-0000-4000-8000-000000000202'::uuid
      WHEN goat_id IN ('92000000-0000-4000-8000-000000001003','92000000-0000-4000-8000-000000001004','92000000-0000-4000-8000-000000001005') THEN '92000000-0000-4000-8000-000000000203'::uuid
      ELSE current_location_id
    END,
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND goat_id IN (
    '91000000-0000-4000-8000-000000001001',
    '91000000-0000-4000-8000-000000001002',
    '91000000-0000-4000-8000-000000001003',
    '91000000-0000-4000-8000-000000001004',
    '91000000-0000-4000-8000-000000001005',
    '92000000-0000-4000-8000-000000001001',
    '92000000-0000-4000-8000-000000001002',
    '92000000-0000-4000-8000-000000001003',
    '92000000-0000-4000-8000-000000001004',
    '92000000-0000-4000-8000-000000001005'
  );

DELETE FROM goat_identifiers
WHERE tenant_id = '${tenant_id}'::uuid
  AND (
    normalized_value IN (
      'QA-CPT-0001','QA-CPT-0002','QA-CPT-0003','QA-CPT-0004','QA-CPT-0005',
      'CPT-901007000504418','CPT-901007000504332','CPT-901007000504407',
      'CPT-901007000504419','CPT-901007000504392'
    )
    OR identifier_id IN (
      '92000000-0000-4000-8000-000000002101',
      '92000000-0000-4000-8000-000000002102',
      '92000000-0000-4000-8000-000000002103',
      '92000000-0000-4000-8000-000000002104',
      '92000000-0000-4000-8000-000000002105'
    )
  );

INSERT INTO goat_identifiers (identifier_id, tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, source_system, source_record_id, normalizer_version, confidence)
VALUES
  ('92000000-0000-4000-8000-000000002101', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000001001', 'animal_identifier_1', 'CPT-901007000504418', 'CPT-901007000504418', 'tenant:${tenant_id}', true, 'active', now(), 'phone-qa-throwaway-seed', 'cpt-rfid-1', 'seed-v1', 1.0),
  ('92000000-0000-4000-8000-000000002102', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000001002', 'animal_identifier_1', 'CPT-901007000504332', 'CPT-901007000504332', 'tenant:${tenant_id}', true, 'active', now(), 'phone-qa-throwaway-seed', 'cpt-rfid-2', 'seed-v1', 1.0),
  ('92000000-0000-4000-8000-000000002103', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000001003', 'animal_identifier_1', 'CPT-901007000504407', 'CPT-901007000504407', 'tenant:${tenant_id}', true, 'active', now(), 'phone-qa-throwaway-seed', 'cpt-rfid-3', 'seed-v1', 1.0),
  ('92000000-0000-4000-8000-000000002104', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000001004', 'animal_identifier_1', 'CPT-901007000504419', 'CPT-901007000504419', 'tenant:${tenant_id}', true, 'active', now(), 'phone-qa-throwaway-seed', 'cpt-rfid-4', 'seed-v1', 1.0),
  ('92000000-0000-4000-8000-000000002105', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000001005', 'animal_identifier_1', 'CPT-901007000504392', 'CPT-901007000504392', 'tenant:${tenant_id}', true, 'active', now(), 'phone-qa-throwaway-seed', 'cpt-rfid-5', 'seed-v1', 1.0)
ON CONFLICT (tenant_id, normalized_value) DO UPDATE
SET goat_id = EXCLUDED.goat_id,
    identifier_value = EXCLUDED.identifier_value,
    identifier_type = EXCLUDED.identifier_type,
    is_primary_for_goat = true,
    status = 'active',
    updated_at = now();

INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name, updated_at)
VALUES
  ('${tenant_id}'::uuid, '91000000-0000-4000-8000-000000001001', '91000000-0000-4000-8000-000000000201', 'whole', 'Godel 1', now()),
  ('${tenant_id}'::uuid, '91000000-0000-4000-8000-000000001002', '91000000-0000-4000-8000-000000000201', 'whole', 'Godel 1', now()),
  ('${tenant_id}'::uuid, '91000000-0000-4000-8000-000000001003', '91000000-0000-4000-8000-000000000203', 'Parts 1-3', 'Yashoda 1', now()),
  ('${tenant_id}'::uuid, '91000000-0000-4000-8000-000000001004', '91000000-0000-4000-8000-000000000203', 'Parts 1-3', 'Yashoda 1', now()),
  ('${tenant_id}'::uuid, '91000000-0000-4000-8000-000000001005', '91000000-0000-4000-8000-000000000203', 'Parts 1-3', 'Yashoda 1', now()),
  ('${tenant_id}'::uuid, '92000000-0000-4000-8000-000000001001', '91000000-0000-4000-8000-000000000202', 'whole', 'Mandela 2', now()),
  ('${tenant_id}'::uuid, '92000000-0000-4000-8000-000000001002', '91000000-0000-4000-8000-000000000202', 'whole', 'Mandela 2', now()),
  ('${tenant_id}'::uuid, '92000000-0000-4000-8000-000000001003', '92000000-0000-4000-8000-000000000203', 'Parts 1-3', 'Castro 1', now()),
  ('${tenant_id}'::uuid, '92000000-0000-4000-8000-000000001004', '92000000-0000-4000-8000-000000000203', 'Parts 1-3', 'Castro 1', now()),
  ('${tenant_id}'::uuid, '92000000-0000-4000-8000-000000001005', '92000000-0000-4000-8000-000000000203', 'Parts 1-3', 'Castro 1', now())
ON CONFLICT (tenant_id, goat_id) DO UPDATE
SET shed_id = EXCLUDED.shed_id,
    partition_label = EXCLUDED.partition_label,
    source_shed_name = EXCLUDED.source_shed_name,
    updated_at = now();

UPDATE obligation_instances oi
SET scope_id = g.shed_id,
    updated_at = now()
FROM goats g
WHERE oi.tenant_id = '${tenant_id}'::uuid
  AND g.tenant_id = oi.tenant_id
  AND g.goat_id = oi.target_id
  AND oi.batch_id IN ('91000000-0000-4000-8000-000000000701', '92000000-0000-4000-8000-000000000711')
  AND oi.target_id IN (
    '91000000-0000-4000-8000-000000001001',
    '91000000-0000-4000-8000-000000001002',
    '91000000-0000-4000-8000-000000001003',
    '91000000-0000-4000-8000-000000001004',
    '91000000-0000-4000-8000-000000001005',
    '92000000-0000-4000-8000-000000001001',
    '92000000-0000-4000-8000-000000001002',
    '92000000-0000-4000-8000-000000001003',
    '92000000-0000-4000-8000-000000001004',
    '92000000-0000-4000-8000-000000001005'
  );

UPDATE vaccination_drive_assignments
SET physical_shed = CASE assignment_id
      WHEN '91000000-0000-4000-8000-000000000801' THEN 'Godel 1'
      WHEN '91000000-0000-4000-8000-000000000803' THEN 'Yashoda 1'
      WHEN '91000000-0000-4000-8000-000000000802' THEN 'Mandela 2'
      WHEN '91000000-0000-4000-8000-000000000804' THEN 'Castro 1'
      ELSE physical_shed
    END,
    partition_label = CASE assignment_id
      WHEN '91000000-0000-4000-8000-000000000801' THEN 'whole'
      WHEN '91000000-0000-4000-8000-000000000803' THEN 'Parts 1-3'
      WHEN '91000000-0000-4000-8000-000000000802' THEN 'whole'
      WHEN '91000000-0000-4000-8000-000000000804' THEN 'Parts 1-3'
      ELSE partition_label
    END,
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND assignment_id IN (
    '91000000-0000-4000-8000-000000000801',
    '91000000-0000-4000-8000-000000000802',
    '91000000-0000-4000-8000-000000000803',
    '91000000-0000-4000-8000-000000000804'
  );

UPDATE weighing_campaign_sheds
SET display_name = CASE campaign_shed_id
      WHEN '92000000-0000-4000-8000-000000000801' THEN 'Godel 1'
      WHEN '92000000-0000-4000-8000-000000000803' THEN 'Yashoda 1 · Parts 1-3'
      WHEN '92000000-0000-4000-8000-000000000802' THEN 'Mandela 2'
      WHEN '92000000-0000-4000-8000-000000000804' THEN 'Castro 1 · Parts 1-3'
      ELSE display_name
    END,
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND campaign_shed_id IN (
    '92000000-0000-4000-8000-000000000801',
    '92000000-0000-4000-8000-000000000802',
    '92000000-0000-4000-8000-000000000803',
    '92000000-0000-4000-8000-000000000804'
  );

COMMIT;
SQL

cat <<EOF
Seeded throwaway phone QA DB on ${DATABASE_URL%%\?*}
Vaccination RFID setup: CBE raw RFID, CPT uses CPT-<RFID> for local dev scan transform.

Use these GOATOS_LOCAL_USER_ID values with tools/dev/android-dev-run.sh:
  CEO QA            90000000-0000-4000-8000-000000000101  ceo_internal       all modules + PA card
  Chandrakant       90000000-0000-4000-8000-000000000102  pc_director        Vaccination execute only
  Dinakar           90000000-0000-4000-8000-000000000103  growth_director    Weighing execute/reopen only
  Jyothi            90000000-0000-4000-8000-000000000104  verifier           Video review only
  Hemang            90000000-0000-4000-8000-000000000105  feed_director      Feed read/status across CBE+CPT, no execute
  Amit              90000000-0000-4000-8000-000000000201  operator/CPT       CPT Mandela 2 Parts 3-5
  Pramod            90000000-0000-4000-8000-000000000202  operator/CBE       CBE Godel 1 Parts 1-3
  Kumar Sharath     90000000-0000-4000-8000-000000000203  operator/CBE       spare CBE operator

Physical RFIDs:
  CBE vaccination:
    Godel 1 / whole: 901007000504418, 901007000504332
    Yashoda 1 / Parts 1-3: 901007000504407, 901007000504419, 901007000504392
  CPT vaccination:
    Mandela 2 / whole: CPT-901007000504418, CPT-901007000504332
    Castro 1 / Parts 1-3: CPT-901007000504407, CPT-901007000504419, CPT-901007000504392
  Weighing free-flow can scan these same five physical RFIDs in any CBE/CPT shed bucket.

Weighing stays free-flow: expected_animal_count=0 and no weighing_expected_animals rows are seeded.
EOF

# ---------------------------------------------------------------------------
# Widening: 8 weighing sheds and 40 vaccination identities.
#
# Weighing and vaccination want different things from this fixture, so they are
# widened differently:
#
#   Weighing is free-flow (raw RFID, no herd-animal join), so a shed only needs
#   an assignee. Eight sheds are split across three people so the my-work vs
#   oversight split can be seen on one phone.
#
#   Vaccination resolves the scanned tag to a goat, and goat_identifiers is
#   unique by (tenant, normalized_value), so the same physical tag cannot belong
#   to eight goats. Each shed therefore owns a PREFIXED copy of the five
#   physical tags -- Godel 1 keeps them raw -- giving 8 x 5 = 40 distinct
#   identities from five real tags. The dev build applies the matching prefix.
# ---------------------------------------------------------------------------
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<SQL
BEGIN;

CREATE TEMP TABLE qa_sheds (
  seq int PRIMARY KEY,
  shed_id uuid NOT NULL,
  park_id uuid NOT NULL,
  shed_name text NOT NULL,
  prefix text NOT NULL,
  animal_count int NOT NULL,
  weighing_operator uuid NOT NULL,
  batch_id uuid NOT NULL,
  task_id uuid NOT NULL,
  is_new boolean NOT NULL
) ON COMMIT DROP;

-- Godel 1 and Mandela 2 already exist and already hold goats; the other six are
-- created or re-pointed here. CBE work is Pramod's and Dinakar's, CPT work is
-- Amit's and Dinakar's, so Dinakar owns sheds in BOTH parks and every other
-- shed on his Operators list belongs to somebody else.
INSERT INTO qa_sheds VALUES
  (1, '91000000-0000-4000-8000-000000000201', '91000000-0000-4000-8000-000000000101', 'Godel 1',   '',    2, '90000000-0000-4000-8000-000000000202', '91000000-0000-4000-8000-000000000701', '91000000-0000-4000-8000-000000000702', false),
  (2, '91000000-0000-4000-8000-000000000203', '91000000-0000-4000-8000-000000000101', 'Yashoda 1', 'Y1-', 3, '90000000-0000-4000-8000-000000000202', '91000000-0000-4000-8000-000000000701', '91000000-0000-4000-8000-000000000702', false),
  (3, '9c000000-0000-4000-8000-000000000301', '91000000-0000-4000-8000-000000000101', 'Gandhi 1',  'G1-', 2, '90000000-0000-4000-8000-000000000103', '91000000-0000-4000-8000-000000000701', '91000000-0000-4000-8000-000000000702', true),
  (4, '9c000000-0000-4000-8000-000000000302', '91000000-0000-4000-8000-000000000101', 'Gandhi 2',  'G2-', 3, '90000000-0000-4000-8000-000000000103', '91000000-0000-4000-8000-000000000701', '91000000-0000-4000-8000-000000000702', true),
  (5, '91000000-0000-4000-8000-000000000202', '92000000-0000-4000-8000-000000000101', 'Mandela 2', 'M2-', 2, '90000000-0000-4000-8000-000000000201', '92000000-0000-4000-8000-000000000711', '92000000-0000-4000-8000-000000000712', false),
  (6, '92000000-0000-4000-8000-000000000203', '92000000-0000-4000-8000-000000000101', 'Castro 1',  'C1-', 3, '90000000-0000-4000-8000-000000000201', '92000000-0000-4000-8000-000000000711', '92000000-0000-4000-8000-000000000712', false),
  (7, '9c000000-0000-4000-8000-000000000303', '92000000-0000-4000-8000-000000000101', 'Castro 2',  'C2-', 2, '90000000-0000-4000-8000-000000000103', '92000000-0000-4000-8000-000000000711', '92000000-0000-4000-8000-000000000712', true),
  (8, '9c000000-0000-4000-8000-000000000304', '92000000-0000-4000-8000-000000000101', 'Castro 3',  'C3-', 3, '90000000-0000-4000-8000-000000000103', '92000000-0000-4000-8000-000000000711', '92000000-0000-4000-8000-000000000712', true);

-- qa_tags has ${animals_per_shed} rows per shed. Only the maintainer's 5 physical
-- RFIDs exist, so slots 1-5 map to them (scannable = true) and slots 6..N get a
-- synthetic, non-RFID identifier (SYN###) so the shed can hold N distinct animal
-- identities without inventing fake physical tags or colliding with a real one.
-- Vaccination still resolves 1 identifier -> 1 goat either way; only the 5
-- scannable slots per shed can actually be walked with a phone.
CREATE TEMP TABLE qa_tags (idx int PRIMARY KEY, tag text NOT NULL, scannable boolean NOT NULL) ON COMMIT DROP;
INSERT INTO qa_tags
SELECT s.idx,
       COALESCE(rt.tag, 'SYN' || lpad(s.idx::text, 3, '0')),
       rt.tag IS NOT NULL
FROM generate_series(1, ${animals_per_shed}) AS s(idx)
LEFT JOIN (VALUES
  (1, '901007000504418'),
  (2, '901007000504332'),
  (3, '901007000504407'),
  (4, '901007000504419'),
  (5, '901007000504392')
) AS rt(idx, tag) ON rt.idx = s.idx;

CREATE TEMP TABLE qa_vaccine_rules (
  rule_id uuid PRIMARY KEY,
  dose_code text NOT NULL,
  vaccine_code text NOT NULL,
  sequence int NOT NULL
) ON COMMIT DROP;

INSERT INTO qa_vaccine_rules VALUES
  ('91000000-0000-4000-8000-000000000503', 'ET_TT_QA', 'ET+TT', 1),
  ('91000000-0000-4000-8000-000000000504', 'PPR_QA', 'PPR', 2);

ALTER TABLE protocol_rules DISABLE TRIGGER ALL;
ALTER TABLE protocol_rule_dimensions DISABLE TRIGGER ALL;

INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
  offset_days, due_window_days, min_gap_days, repeat, catch_up,
  eligibility_json, sop_version_id, proof_policy, sort_order
)
VALUES (
  '91000000-0000-4000-8000-000000000504',
  '${tenant_id}'::uuid,
  '91000000-0000-4000-8000-000000000502',
  'PPR_QA',
  2,
  'manual_campaign',
  0,
  3,
  0,
  'none',
  'immediate',
  '{"stage":"K2","lifecycle":"alive"}'::jsonb,
  '91000000-0000-4000-8000-000000000402',
  '{"types":["video"],"required":true,"proof_mode":"per_goat_video","subject_scope":"goat","expected_subjects":["goat"],"minimum_count":1,"minimum_count_per_subject":1,"maximum_count":25,"maximum_count_per_subject":5,"capture_source":"in_app_camera","allowed_capture_sources":["in_app_camera","gallery_picker"],"verify_capability":"proof.verify","verify_before_apply":true,"retention_policy":"operational_90d"}'::jsonb,
  20
)
ON CONFLICT (rule_id) DO UPDATE
SET dose_code = EXCLUDED.dose_code,
    sequence = EXCLUDED.sequence,
    trigger_type = EXCLUDED.trigger_type,
    proof_policy = EXCLUDED.proof_policy,
    sort_order = EXCLUDED.sort_order;

INSERT INTO protocol_rule_dimensions (
  tenant_id, protocol_version_id, rule_id, category, ruleset_family, matrix_row_id,
  selector_key, dose_code, source_dose_code, vaccine_code, vaccine_type,
  pathogen_class, compatibility_group, species, animal_stage, sex, breed,
  lifecycle, health, reproductive, min_age_days, max_age_days, trigger_type,
  sequence, offset_days, due_window_days, min_gap_days, repeat, catch_up,
  eligibility_json, vaccine_json, schedule_json
)
VALUES (
  '${tenant_id}'::uuid,
  '91000000-0000-4000-8000-000000000502',
  '91000000-0000-4000-8000-000000000504',
  'vaccination',
  'qa',
  'qa-row-ppr',
  'qa-selector-ppr',
  'PPR_QA',
  'PPR_QA',
  'PPR',
  'live',
  'viral',
  'PPR',
  'goat',
  'K2',
  'all',
  'all',
  'alive',
  'any',
  'any',
  0,
  180,
  'manual_campaign',
  2,
  0,
  3,
  0,
  'none',
  'immediate',
  '{"stage":"K2","lifecycle":"alive"}'::jsonb,
  '{"code":"PPR"}'::jsonb,
  '{"dose_code":"PPR_QA"}'::jsonb
)
ON CONFLICT DO NOTHING;

ALTER TABLE protocol_rule_dimensions ENABLE TRIGGER ALL;
ALTER TABLE protocol_rules ENABLE TRIGGER ALL;

INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, state_region, status, display_order, updated_at)
SELECT s.shed_id, '${tenant_id}'::uuid, 'shed', upper(replace(s.shed_name, ' ', '-')), s.shed_name, s.park_id, 'Karnataka', 'active', 50 + s.seq, now()
FROM qa_sheds s
WHERE s.is_new
ON CONFLICT (location_id) DO UPDATE
SET name = EXCLUDED.name, parent_location_id = EXCLUDED.parent_location_id, status = 'active', updated_at = now();

INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_counts, usable_for_feed, usable_for_vaccination, usable_for_sop, is_holding, is_quarantine, is_icu, display_order, notes)
SELECT '${tenant_id}'::uuid, s.shed_id, true, true, true, true, false, false, false, 50 + s.seq, 'Phone QA widened shed'
FROM qa_sheds s
WHERE s.is_new
ON CONFLICT (location_id) DO UPDATE
SET usable_for_vaccination = true, usable_for_sop = true, updated_at = now();

-- Shed 1 keeps the five RAW tags, so its five goats are the existing CBE fixture
-- animals pulled together into Godel 1. Shed 5 keeps the existing CPT animals,
-- whose identifiers are re-prefixed from CPT- to M2- below.
UPDATE goats
SET park_id = '91000000-0000-4000-8000-000000000101',
    shed_id = '91000000-0000-4000-8000-000000000201',
    current_location_id = '91000000-0000-4000-8000-000000000201',
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND goat_id::text LIKE '91000000-0000-4000-8000-0000000010%';

UPDATE goats
SET park_id = '92000000-0000-4000-8000-000000000101',
    shed_id = '91000000-0000-4000-8000-000000000202',
    current_location_id = '91000000-0000-4000-8000-000000000202',
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND goat_id::text LIKE '92000000-0000-4000-8000-0000000010%';

UPDATE goat_identifiers
SET identifier_value = 'M2-' || substring(identifier_value from 5),
    normalized_value = 'M2-' || substring(normalized_value from 5),
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND normalized_value LIKE 'CPT-%';

-- Sheds 2,3,4,6,7,8 get thirty freshly minted goats: one per (shed, tag).
INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, health_status, custodian_party_id, current_location_id, farm_id, park_id, shed_id, dob, origin_type, entry_date)
SELECT ('9a000000-0000-4000-8000-' || lpad(s.seq::text, 6, '0') || lpad(t.idx::text, 6, '0'))::uuid,
       '${tenant_id}'::uuid,
       -- display_id is constrained to ^G-[0-9]{6,}\$: three digits of shed, three of tag.
       'G-' || lpad(s.seq::text, 3, '0') || lpad(t.idx::text, 3, '0'),
       CASE WHEN t.idx % 2 = 0 THEN 'male' ELSE 'female' END,
       'kid', 'alive', 'K2', 'healthy',
       '91000000-0000-4000-8000-000000000301',
       s.shed_id, '91000000-0000-4000-8000-000000000100', s.park_id, s.shed_id,
       DATE '2026-05-15' + t.idx, 'birth', DATE '2026-05-15' + t.idx
FROM qa_sheds s
CROSS JOIN qa_tags t
-- Sheds 1 (Godel 1) and 5 (Mandela 2) already own goats 1-5 from the base fixture
-- (real RFIDs for Godel 1, M2-<RFID> for Mandela 2) -- only their slots 6..N are
-- freshly minted here. Every other shed is entirely new, so it gets all N slots.
WHERE (s.seq NOT IN (1, 5)) OR (t.idx > 5)
ON CONFLICT (goat_id) DO UPDATE
SET lifecycle_status = 'alive', park_id = EXCLUDED.park_id, shed_id = EXCLUDED.shed_id,
    current_location_id = EXCLUDED.current_location_id, updated_at = now();

INSERT INTO goat_identifiers (identifier_id, tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, source_system, source_record_id, normalizer_version, confidence)
SELECT ('9b000000-0000-4000-8000-' || lpad(s.seq::text, 6, '0') || lpad(t.idx::text, 6, '0'))::uuid,
       '${tenant_id}'::uuid,
       ('9a000000-0000-4000-8000-' || lpad(s.seq::text, 6, '0') || lpad(t.idx::text, 6, '0'))::uuid,
       'animal_identifier_1',
       s.prefix || t.tag, s.prefix || t.tag,
       'tenant:${tenant_id}', true, 'active', now(),
       'phone-qa-throwaway-seed', 'qa-' || s.seq || '-' || t.idx, 'seed-v1', 1.0
FROM qa_sheds s
CROSS JOIN qa_tags t
-- Sheds 1 (Godel 1) and 5 (Mandela 2) already own goats 1-5 from the base fixture
-- (real RFIDs for Godel 1, M2-<RFID> for Mandela 2) -- only their slots 6..N are
-- freshly minted here. Every other shed is entirely new, so it gets all N slots.
WHERE (s.seq NOT IN (1, 5)) OR (t.idx > 5)
ON CONFLICT (tenant_id, normalized_value) DO UPDATE
SET goat_id = EXCLUDED.goat_id, identifier_value = EXCLUDED.identifier_value,
    identifier_type = EXCLUDED.identifier_type, is_primary_for_goat = true, status = 'active', updated_at = now();

INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name, updated_at)
SELECT '${tenant_id}'::uuid, g.goat_id, g.shed_id, 'whole', s.shed_name, now()
FROM goats g
JOIN qa_sheds s ON s.shed_id = g.shed_id
WHERE g.tenant_id = '${tenant_id}'::uuid
  AND (g.goat_id::text LIKE '9a000000%' OR g.goat_id::text LIKE '91000000-0000-4000-8000-0000000010%' OR g.goat_id::text LIKE '92000000-0000-4000-8000-0000000010%')
ON CONFLICT (tenant_id, goat_id) DO UPDATE
SET shed_id = EXCLUDED.shed_id, partition_label = EXCLUDED.partition_label,
    source_shed_name = EXCLUDED.source_shed_name, updated_at = now();

-- The eight per-shed assignments below supersede the four the base fixture made,
-- which collide with them on (batch, date, park, shed, partition, operator).
-- Members go first: they carry the FK.
DELETE FROM vaccination_drive_assignment_members WHERE tenant_id = '${tenant_id}'::uuid;
DELETE FROM vaccination_drive_assignments WHERE tenant_id = '${tenant_id}'::uuid;

DELETE FROM obligation_instances
WHERE tenant_id = '${tenant_id}'::uuid
  AND batch_id IN (
    '91000000-0000-4000-8000-000000000701',
    '92000000-0000-4000-8000-000000000711'
  );

CREATE TEMP TABLE qa_due_goats ON COMMIT DROP AS
SELECT g.goat_id, s.seq AS shed_seq, s.shed_id, s.park_id, s.batch_id, s.task_id
FROM goats g
JOIN qa_sheds s ON s.shed_id = g.shed_id
JOIN goat_identifiers gi
  ON gi.tenant_id = g.tenant_id
 AND gi.goat_id = g.goat_id
 AND gi.is_primary_for_goat
 AND gi.status = 'active'
JOIN qa_tags t
  ON gi.normalized_value = s.prefix || t.tag
 AND t.idx <= s.animal_count
WHERE g.tenant_id = '${tenant_id}'::uuid;

-- ET+TT for every due QA goat; PPR is added too for selected sheds, so the phone
-- can prove that two vaccine titles appear in the roster/overlay.
INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, target_type,
  target_id, scope_type, scope_id, due_at, window_start, window_end, status,
  sop_task_id, idempotency_key, sequence
)
SELECT (
         CASE WHEN r.sequence = 1 THEN '9d000000' ELSE '9d100000' END ||
         '-0000-4000-8000-' ||
         lpad(d.shed_seq::text, 6, '0') ||
         lpad(row_number() OVER (PARTITION BY d.shed_seq, r.sequence ORDER BY d.goat_id)::text, 6, '0')
       )::uuid,
       '${tenant_id}'::uuid,
       '91000000-0000-4000-8000-000000000502',
       r.rule_id,
       d.batch_id,
       'goat',
       d.goat_id,
       'shed',
       d.shed_id,
       now(),
       now() - interval '1 hour',
       now() + interval '3 days',
       'due',
       d.task_id,
       'qa-vax-' || r.dose_code || '-' || d.goat_id::text,
       r.sequence
FROM qa_due_goats d
JOIN qa_vaccine_rules r
  ON r.sequence = 1
  OR (r.sequence = 2 AND d.shed_seq IN (2, 6))
ON CONFLICT (obligation_id) DO UPDATE
SET batch_id = EXCLUDED.batch_id,
    rule_id = EXCLUDED.rule_id,
    status = 'due',
    scope_id = EXCLUDED.scope_id,
    sop_task_id = EXCLUDED.sop_task_id,
    due_at = now(),
    updated_at = now();

-- One drive assignment per shed, owned by that park's vaccination operator.
INSERT INTO vaccination_drive_assignments (assignment_id, tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count, capacity_status, warnings, vaccine_rule_ids, total_doses)
SELECT ('9e000000-0000-4000-8000-' || lpad(s.seq::text, 12, '0'))::uuid,
       '${tenant_id}'::uuid, s.batch_id, ${today_sql},
       CASE WHEN s.park_id = '91000000-0000-4000-8000-000000000101'
            THEN '93000000-0000-4000-8000-000000000202'::uuid
            ELSE '93000000-0000-4000-8000-000000000201'::uuid END,
       s.park_id, s.shed_id, s.shed_name, 'whole',
       (SELECT count(DISTINCT d.goat_id) FROM qa_due_goats d WHERE d.shed_id = s.shed_id),
       'within_cap', '[]'::jsonb,
       CASE WHEN s.seq IN (2, 6)
            THEN ARRAY['91000000-0000-4000-8000-000000000503','91000000-0000-4000-8000-000000000504']::uuid[]
            ELSE ARRAY['91000000-0000-4000-8000-000000000503']::uuid[] END,
       (SELECT count(*) FROM obligation_instances oi WHERE oi.tenant_id = '${tenant_id}'::uuid AND oi.batch_id = s.batch_id AND oi.scope_id = s.shed_id AND oi.status = 'due')
FROM qa_sheds s
ON CONFLICT (assignment_id) DO UPDATE
SET batch_id = EXCLUDED.batch_id, planned_date = EXCLUDED.planned_date, operator_id = EXCLUDED.operator_id,
    park_id = EXCLUDED.park_id, shed_id = EXCLUDED.shed_id, physical_shed = EXCLUDED.physical_shed,
    animal_count = EXCLUDED.animal_count, vaccine_rule_ids = EXCLUDED.vaccine_rule_ids,
    total_doses = EXCLUDED.total_doses, updated_at = now();

INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
SELECT '${tenant_id}'::uuid,
       ('9e000000-0000-4000-8000-' || lpad(s.seq::text, 12, '0'))::uuid,
       oi.obligation_id, oi.target_id
FROM obligation_instances oi
JOIN goats g ON g.tenant_id = oi.tenant_id AND g.goat_id = oi.target_id
JOIN qa_sheds s ON s.shed_id = g.shed_id
WHERE oi.tenant_id = '${tenant_id}'::uuid
  AND oi.status = 'due'
ON CONFLICT (tenant_id, obligation_id) DO UPDATE
SET assignment_id = EXCLUDED.assignment_id;

-- The base fixture's four buckets are superseded by the eight below and collide
-- with them on (campaign, location), so they go first.
DELETE FROM weighing_campaign_sheds
WHERE tenant_id = '${tenant_id}'::uuid
  AND campaign_shed_id IN (
    '92000000-0000-4000-8000-000000000801',
    '92000000-0000-4000-8000-000000000802',
    '92000000-0000-4000-8000-000000000803',
    '92000000-0000-4000-8000-000000000804'
  );

-- Weighing: every shed gets a bucket on its park's campaign, with ONE assignee.
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, expected_animal_count, weighing_category, operator_user_id, status, park_id, start_business_date, updated_at)
SELECT ('9f000000-0000-4000-8000-' || lpad(s.seq::text, 12, '0'))::uuid,
       CASE WHEN s.park_id = '91000000-0000-4000-8000-000000000101'
            THEN '92000000-0000-4000-8000-000000000701'::uuid
            ELSE '92000000-0000-4000-8000-000000000702'::uuid END,
       '${tenant_id}'::uuid, s.shed_id, 'shed', s.shed_name, 0,
       -- Castro 3 is deliberately LUMP-SUM so both weighing categories are testable on one phone:
       -- a lump-sum bucket takes one shed-level weight and video instead of per-animal capture.
       CASE WHEN s.seq = 8 THEN 'per_shed_partition' ELSE 'individual_animal' END,
       s.weighing_operator, 'pending', s.park_id, ${today_sql}, now()
FROM qa_sheds s
ON CONFLICT (tenant_id, campaign_id, location_id, COALESCE(partition_label, ''::text)) DO UPDATE
SET location_id = EXCLUDED.location_id, display_name = EXCLUDED.display_name,
    expected_animal_count = 0, weighing_category = EXCLUDED.weighing_category,
    operator_user_id = EXCLUDED.operator_user_id, park_id = EXCLUDED.park_id,
    start_business_date = EXCLUDED.start_business_date, updated_at = now();

-- This fixture is a TWO-PARK world: CBE and CPT, four sheds each. That is the whole
-- point of it -- vaccination scheduled for one operator per park, and per park two
-- weighing sheds to the growth director and two to that park's operator, all driven by
-- five physical RFIDs behind per-shed prefixes.
--
-- The base seed also ships the canonical Coimbatore and Channapatna parks (78 and 76
-- sheds), which hold no animals and no weighing buckets here and only bury the real
-- fixture in every park and shed picker. Retire them for the phone.
--
-- locations carries a guard trigger that refuses to rescope the seeded CBE/CPT/HF ids
-- without an explicit approved plan. That gate is deliberate, so the reason is stated
-- rather than worked around, and it is scoped to THIS disposable database only -- never
-- run this against a shared or staging environment.
SET LOCAL goatos.approved_location_migration_plan = 'phone-qa-throwaway: retire empty seeded parks, fixture is CBE+CPT only';

UPDATE locations
SET status = 'inactive', updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND (
    location_id IN (
      '00000000-0000-4000-8000-000000003001',
      '00000000-0000-4000-8000-000000003002',
      '00000000-0000-4000-8000-000000003003'
    )
    OR parent_location_id IN (
      '00000000-0000-4000-8000-000000003001',
      '00000000-0000-4000-8000-000000003002'
    )
  );

COMMIT;
SQL

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<SQL
BEGIN;

DELETE FROM feed_packing_completions
WHERE tenant_id = '${tenant_id}'::uuid
  AND idempotency_key LIKE 'phone-qa-feed-%';

UPDATE feed_transport_tasks
SET current_attempt_id = NULL,
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND task_id IN (
    '8f000000-0000-4000-8000-000000000001',
    '8f000000-0000-4000-8000-000000000002',
    '8f000000-0000-4000-8000-000000000003',
    '8f000000-0000-4000-8000-000000000004'
  );

DELETE FROM feed_transport_attempts
WHERE tenant_id = '${tenant_id}'::uuid
  AND task_id IN (
    '8f000000-0000-4000-8000-000000000001',
    '8f000000-0000-4000-8000-000000000002',
    '8f000000-0000-4000-8000-000000000003',
    '8f000000-0000-4000-8000-000000000004'
  );

DELETE FROM feed_transport_tasks
WHERE tenant_id = '${tenant_id}'::uuid
  AND task_id IN (
    '8f000000-0000-4000-8000-000000000001',
    '8f000000-0000-4000-8000-000000000002',
    '8f000000-0000-4000-8000-000000000003',
    '8f000000-0000-4000-8000-000000000004'
  );

DELETE FROM feed_direction_issues
WHERE tenant_id = '${tenant_id}'::uuid
  AND feed_direction_issue_id IN (
    '8d000000-0000-4000-8000-000000000001',
    '8d000000-0000-4000-8000-000000000002'
  );

INSERT INTO feed_direction_issues (
  feed_direction_issue_id, tenant_id, park_id, feed_day, workflow, state, issued_at,
  generation_input_fingerprint, idempotency_key, request_fingerprint,
  source_contract, source_contract_version, generated_by
) VALUES
  ('8d000000-0000-4000-8000-000000000001', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000101', current_date, 'normal', 'issued', now(), 'phone-qa-cbe', 'phone-qa-feed-cbe', 'phone-qa-cbe', 'phone-qa', '1', 'phone-qa-seed'),
  ('8d000000-0000-4000-8000-000000000002', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000000101', current_date, 'normal', 'issued', now(), 'phone-qa-cpt', 'phone-qa-feed-cpt', 'phone-qa-cpt', 'phone-qa', '1', 'phone-qa-seed');

INSERT INTO feed_direction_issue_rows (
  tenant_id, feed_direction_issue_id, park_id, park_label, shed_id, shed_label, shed_tag,
  breed, ration_group, experiment_arm, session_no, session_label, head_count,
  head_count_informational, workflow, feed_item_label, quantity_kg, grams_per_head,
  shed_factor, session_total_kg, overdue_pending, row_seq, item_seq, partition_label
) VALUES
  ('${tenant_id}'::uuid, '8d000000-0000-4000-8000-000000000001', '91000000-0000-4000-8000-000000000101', 'CBE', '91000000-0000-4000-8000-000000000201', 'Godel 1', 'CBE-GODEL-1', 'Boer', 'grower', '', 1, 'Morning', 5, false, 'normal', 'Dry Feed', 12.500, 2500.000, 1.0000, 12.500, false, 1, 1, NULL),
  ('${tenant_id}'::uuid, '8d000000-0000-4000-8000-000000000001', '91000000-0000-4000-8000-000000000101', 'CBE', '91000000-0000-4000-8000-000000000203', 'Yashoda 1', 'CBE-YASHODA-1', 'Boer', 'grower', '', 1, 'Morning', 5, false, 'normal', 'Dry Feed', 8.750, 1750.000, 1.0000, 8.750, false, 2, 1, NULL),
  ('${tenant_id}'::uuid, '8d000000-0000-4000-8000-000000000002', '92000000-0000-4000-8000-000000000101', 'CPT', '91000000-0000-4000-8000-000000000202', 'Mandela 2', 'CPT-MANDELA-2', 'Boer', 'grower', '', 1, 'Morning', 5, false, 'normal', 'Dry Feed', 12.500, 2500.000, 1.0000, 12.500, false, 3, 1, NULL),
  ('${tenant_id}'::uuid, '8d000000-0000-4000-8000-000000000002', '92000000-0000-4000-8000-000000000101', 'CPT', '92000000-0000-4000-8000-000000000203', 'Castro 1', 'CPT-CASTRO-1', 'Boer', 'grower', '', 1, 'Morning', 5, false, 'normal', 'Dry Feed', 8.750, 1750.000, 1.0000, 8.750, false, 4, 1, NULL);

INSERT INTO feed_packing_completions (
  completion_id, tenant_id, park_id, shed_id, session_no, target_date, workflow, status,
  packing_proof_ref, completed_by, idempotency_key
) VALUES
  ('8e000000-0000-4000-8000-000000000001', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000101', '91000000-0000-4000-8000-000000000201', 1, current_date, 'normal', 'pending_verification', 'phone-qa-pending-1', '90000000-0000-4000-8000-000000000202', 'phone-qa-feed-pack-1'),
  ('8e000000-0000-4000-8000-000000000002', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000101', '91000000-0000-4000-8000-000000000203', 1, current_date, 'normal', 'completed', 'phone-qa-proof-2', '90000000-0000-4000-8000-000000000202', 'phone-qa-feed-pack-2'),
  ('8e000000-0000-4000-8000-000000000003', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000000101', '91000000-0000-4000-8000-000000000202', 1, current_date, 'normal', 'pending_verification', 'phone-qa-pending-3', '90000000-0000-4000-8000-000000000201', 'phone-qa-feed-pack-3'),
  ('8e000000-0000-4000-8000-000000000004', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000000101', '92000000-0000-4000-8000-000000000203', 1, current_date, 'normal', 'completed', 'phone-qa-proof-4', '90000000-0000-4000-8000-000000000201', 'phone-qa-feed-pack-4');

INSERT INTO feed_transport_tasks (
  task_id, tenant_id, park_id, shed_id, business_date, scheduled_at, status, operator_id
) VALUES
  ('8f000000-0000-4000-8000-000000000001', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000101', '91000000-0000-4000-8000-000000000201', current_date, current_date + time '15:30', 'due', '90000000-0000-4000-8000-000000000202'),
  ('8f000000-0000-4000-8000-000000000002', '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000101', '91000000-0000-4000-8000-000000000203', current_date, current_date + time '15:30', 'completed', '90000000-0000-4000-8000-000000000202'),
  ('8f000000-0000-4000-8000-000000000003', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000000101', '91000000-0000-4000-8000-000000000202', current_date, current_date + time '15:30', 'due', '90000000-0000-4000-8000-000000000201'),
  ('8f000000-0000-4000-8000-000000000004', '${tenant_id}'::uuid, '92000000-0000-4000-8000-000000000101', '92000000-0000-4000-8000-000000000203', current_date, current_date + time '15:30', 'completed', '90000000-0000-4000-8000-000000000201');

COMMIT;
SQL

# ---------------------------------------------------------------------------
# Self-check. A previous revision of this script re-pointed the five RAW physical
# tags onto the CPT goats, which both stripped the CBE goats of their vaccination
# identity and left each CPT goat holding two primary animal_identifier_1 rows --
# so the seed died on goat_identifiers_primary_per_goat_unique. Neither the
# INSERTs' ON CONFLICT (tenant_id, normalized_value) nor psql's exit code caught
# the identity half of that, so assert the fixture's invariants explicitly.
# ---------------------------------------------------------------------------
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt <<SQL >/dev/null
DO \$check\$
DECLARE
  bad int;
BEGIN
  SELECT count(*) INTO bad
  FROM goats g
  WHERE g.tenant_id = '${tenant_id}'::uuid
    AND g.lifecycle_status = 'alive'
    AND NOT EXISTS (
      SELECT 1 FROM goat_identifiers i
      WHERE i.goat_id = g.goat_id
        AND i.identifier_type = 'animal_identifier_1'
        AND i.is_primary_for_goat
        AND i.status = 'active'
    );
  IF bad > 0 THEN
    RAISE EXCEPTION 'phone-qa seed: % alive goats have no primary animal_identifier_1', bad;
  END IF;

  SELECT count(*) INTO bad
  FROM (
    SELECT 1 FROM user_scope_grants
    WHERE tenant_id = '${tenant_id}'::uuid
      AND status = 'active'
      AND user_id IN (
        '90000000-0000-4000-8000-000000000101',
        '90000000-0000-4000-8000-000000000102',
        '90000000-0000-4000-8000-000000000103',
        '90000000-0000-4000-8000-000000000104',
        '90000000-0000-4000-8000-000000000105',
        '90000000-0000-4000-8000-000000000201',
        '90000000-0000-4000-8000-000000000202'
      )
    GROUP BY user_id
  ) q;
  IF bad <> 7 THEN
    RAISE EXCEPTION 'phone-qa seed: % of the 7 QA identities have an active scope grant, want 7 (pending-only grants cause runtime 403s)', bad;
  END IF;
END
\$check\$;
SQL

cat <<EOF

Widened phone-QA fixture: 8 weighing sheds, vaccination due roster is 2,3,2,3,2,3,2,3 animals.
Yashoda 1 and Castro 1 have TWO due vaccines per animal: ET+TT and PPR.
Each shed still has up to ${animals_per_shed} seeded RFID identities; only due obligations show in the scan roster.

  Shed        Park  Due animals  Vaccines       Weighing assignee
  Godel 1     CBE   2            ET+TT          Pramod
  Yashoda 1   CBE   3            ET+TT, PPR     Pramod
  Gandhi 1    CBE   2            ET+TT          Dinakar
  Gandhi 2    CBE   3            ET+TT          Dinakar
  Mandela 2   CPT   2            ET+TT          Amit
  Castro 1    CPT   3            ET+TT, PPR     Amit
  Castro 2    CPT   2            ET+TT          Dinakar
  Castro 3    CPT   3            ET+TT          Dinakar (LUMP-SUM)

The five physical tags are 901007000504418, 901007000504332, 901007000504407,
901007000504419 and 901007000504392. Vaccination applies the shed prefix in the
dev build for prefixed sheds; weighing is free-flow and takes the raw tag in any shed.
EOF
