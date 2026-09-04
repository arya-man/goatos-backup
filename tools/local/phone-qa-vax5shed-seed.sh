#!/usr/bin/env bash
# Seed a small disposable phone-QA vaccination fixture:
#   5 sheds, 2-3 animals per shed, and 1/2/3 vaccination obligations per animal.
#
# This helper is local-only by construction. It refuses every DATABASE_URL except
# loopback port 15544, which is reserved for throwaway phone QA in this repo.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tenant_id="${GOATOS_TENANT_ID:-00000000-0000-4000-8000-000000000001}"
today_sql="(now() AT TIME ZONE 'Asia/Kolkata')::date"
qa_operator_user_id="${GOATOS_LOCAL_USER_ID:-90000000-0000-4000-8000-000000000204}"
qa_park_id="91000000-0000-4000-8000-000000000101"

die() { echo "phone-qa-vax5shed-seed: $*" >&2; exit 1; }

[ -n "${DATABASE_URL:-}" ] || die "DATABASE_URL is required"

safe_database_url="${DATABASE_URL%%\?*}"
if [[ ! "$safe_database_url" =~ ^postgres(ql)?://([^/@]+(:[^/@]*)?@)?(localhost|127\.0\.0\.1):15544(/|$) ]]; then
  die "refusing DATABASE_URL outside local throwaway port 15544: ${safe_database_url}"
fi

case "${GOATOS_ENV:-}" in
  local|dev|test) ;;
  *) die "GOATOS_ENV must be local/dev/test for this seed" ;;
esac

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt -c "SELECT 1" >/dev/null

qa_operator_member_id="$(
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt -c "
    SELECT workforce_member_id
    FROM workforce_members
    WHERE tenant_id = '${tenant_id}'::uuid
      AND user_id = '${qa_operator_user_id}'::uuid
      AND status = 'active'
      AND primary_role_hint = 'operator'
    LIMIT 1
  "
)"
[ -n "$qa_operator_member_id" ] || die "GOATOS_LOCAL_USER_ID must be an active operator workforce_members.user_id; got ${qa_operator_user_id}"

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -qAt <<SQL >/dev/null
DELETE FROM goat_identifiers
WHERE tenant_id = '${tenant_id}'::uuid
  AND source_system IN ('seed-vaccination-per-goat-qa', 'phone-qa-vax5shed-seed');
SQL

(
  cd "$repo_root/backend"
  GOATOS_ENV="${GOATOS_ENV:-local}" \
    go run ./cmd/seed-vaccination-per-goat-qa -tenant-id "$tenant_id" -animals-per-shed 5
)

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<SQL
BEGIN;

SET LOCAL session_replication_role = replica;

UPDATE user_scope_grants
SET status = 'inactive'
WHERE tenant_id = '${tenant_id}'::uuid
  AND user_id = '${qa_operator_user_id}'::uuid
  AND role = 'operator'
  AND scope_type = 'park'
  AND scope_id <> '${qa_park_id}'::uuid;

UPDATE user_scope_grants
SET status = 'inactive',
    valid_to = COALESCE(valid_to, now())
WHERE tenant_id = '${tenant_id}'::uuid
  AND user_id = '${qa_operator_user_id}'::uuid
  AND role = 'operator'
  AND scope_type = 'park';

INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from, created_at)
VALUES ('${tenant_id}'::uuid, '${qa_operator_user_id}'::uuid, 'operator', 'park', '${qa_park_id}'::uuid, 'active', now(), now());

CREATE TEMP TABLE qa_vax_rules (
  rule_id uuid PRIMARY KEY,
  dose_code text NOT NULL,
  vaccine_code text NOT NULL,
  item_id uuid NOT NULL,
  stock_id uuid NOT NULL,
  vaccine_id uuid NOT NULL,
  disease text NOT NULL,
  sequence int NOT NULL,
  sort_order int NOT NULL
) ON COMMIT DROP;

INSERT INTO qa_vax_rules VALUES
  ('91000000-0000-4000-8000-000000000503', 'ET_TT_QA', 'ET+TT', '91000000-0000-4000-8000-000000000601', '91000000-0000-4000-8000-000000000602', '91000000-0000-4000-8000-000000000603', 'Enterotoxaemia + Tetanus', 1, 10),
  ('9c500000-0000-4000-8000-000000000504', 'PPR_QA', 'PPR', '9c500000-0000-4000-8000-000000000604', '9c500000-0000-4000-8000-000000000704', '9c500000-0000-4000-8000-000000000804', 'Peste des petits ruminants', 2, 20),
  ('9c500000-0000-4000-8000-000000000505', 'FMD_QA', 'FMD', '9c500000-0000-4000-8000-000000000605', '9c500000-0000-4000-8000-000000000705', '9c500000-0000-4000-8000-000000000805', 'Foot-and-mouth disease', 3, 30),
  ('9c500000-0000-4000-8000-000000000506', 'HS_QA', 'HS', '9c500000-0000-4000-8000-000000000606', '9c500000-0000-4000-8000-000000000706', '9c500000-0000-4000-8000-000000000806', 'Haemorrhagic septicaemia', 4, 40);

INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit, status, context)
SELECT item_id, '${tenant_id}'::uuid, 'VAX-QA-' || regexp_replace(vaccine_code, '[^A-Za-z0-9]+', '', 'g'),
       vaccine_code || ' Vaccine QA', 'vaccine', 'dose', 'active', '{"seed":"phone-qa-vax5shed"}'::jsonb
FROM qa_vax_rules
ON CONFLICT (item_id) DO UPDATE
SET item_code = EXCLUDED.item_code,
    name = EXCLUDED.name,
    status = 'active',
    updated_at = now();

INSERT INTO vaccines (vaccine_id, tenant_id, item_id, disease, manufacturer, doses_per_vial, withdrawal_days, context)
SELECT vaccine_id, '${tenant_id}'::uuid, item_id, disease, 'QA', 10, 0, '{"seed":"phone-qa-vax5shed"}'::jsonb
FROM qa_vax_rules
ON CONFLICT (tenant_id, item_id) DO UPDATE
SET disease = EXCLUDED.disease,
    manufacturer = EXCLUDED.manufacturer,
    updated_at = now();

INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, lot_code, expiry_date, quantity_in_stock, quantity_reserved, quantity_unit, status)
SELECT stock_id, '${tenant_id}'::uuid, item_id, '91000000-0000-4000-8000-000000000101'::uuid,
       'QA-LOT-' || vaccine_code, DATE '2027-12-31', 100, 0, 'dose', 'active'
FROM qa_vax_rules
ON CONFLICT (stock_id) DO UPDATE
SET quantity_in_stock = 100,
    quantity_reserved = 0,
    status = 'active',
    updated_at = now();

INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
  offset_days, due_window_days, min_gap_days, repeat, catch_up,
  eligibility_json, sop_version_id, proof_policy, sort_order
)
SELECT rule_id, '${tenant_id}'::uuid, '91000000-0000-4000-8000-000000000502'::uuid,
       dose_code, sequence, 'manual_campaign', 0, 3, 0, 'none', 'immediate',
       '{"stage":"K2","lifecycle":"alive"}'::jsonb,
       '91000000-0000-4000-8000-000000000402'::uuid,
       '{"types":["video"],"required":true,"proof_mode":"per_goat_video","subject_scope":"goat","expected_subjects":["goat"],"minimum_count":1,"minimum_count_per_subject":1,"maximum_count":25,"maximum_count_per_subject":5,"capture_source":"in_app_camera","allowed_capture_sources":["in_app_camera","gallery_picker"],"verify_capability":"proof.verify","verify_before_apply":true,"retention_policy":"operational_90d"}'::jsonb,
       sort_order
FROM qa_vax_rules
ON CONFLICT ON CONSTRAINT protocol_rules_version_dose_unique DO UPDATE
SET dose_code = EXCLUDED.dose_code,
    sequence = EXCLUDED.sequence,
    trigger_type = EXCLUDED.trigger_type,
    proof_policy = EXCLUDED.proof_policy,
    sort_order = EXCLUDED.sort_order;

UPDATE qa_vax_rules q
SET rule_id = actual.rule_id
FROM protocol_rules actual
WHERE actual.tenant_id = '${tenant_id}'::uuid
  AND actual.protocol_version_id = '91000000-0000-4000-8000-000000000502'::uuid
  AND actual.dose_code = q.dose_code;

DELETE FROM protocol_rule_dimensions dim
USING qa_vax_rules q
WHERE dim.tenant_id = '${tenant_id}'::uuid
  AND dim.protocol_version_id = '91000000-0000-4000-8000-000000000502'::uuid
  AND dim.dose_code = q.dose_code;

INSERT INTO protocol_rule_dimensions (
  tenant_id, protocol_version_id, rule_id, category, ruleset_family, matrix_row_id,
  selector_key, dose_code, source_dose_code, vaccine_code, vaccine_type,
  pathogen_class, compatibility_group, species, animal_stage, sex, breed,
  lifecycle, health, reproductive, min_age_days, max_age_days, trigger_type,
  sequence, offset_days, due_window_days, min_gap_days, repeat, catch_up,
  eligibility_json, vaccine_json, schedule_json
)
SELECT '${tenant_id}'::uuid,
       '91000000-0000-4000-8000-000000000502'::uuid,
       rule_id,
       'vaccination',
       'phone-qa-vax5shed',
       'phone-qa-vax5shed-' || lower(regexp_replace(vaccine_code, '[^A-Za-z0-9]+', '_', 'g')),
       'phone-qa-vax5shed-' || lower(regexp_replace(vaccine_code, '[^A-Za-z0-9]+', '_', 'g')),
       dose_code,
       dose_code,
       vaccine_code,
       CASE WHEN vaccine_code = 'PPR' THEN 'live' ELSE 'killed' END,
       CASE WHEN vaccine_code = 'PPR' THEN 'viral' ELSE 'bacterial' END,
       vaccine_code,
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
       sequence,
       0,
       3,
       0,
       'none',
       'immediate',
       '{"stage":"K2","lifecycle":"alive"}'::jsonb,
       jsonb_build_object('code', vaccine_code),
       jsonb_build_object('dose_code', dose_code)
FROM qa_vax_rules;

CREATE TEMP TABLE qa_sheds (
  seq int PRIMARY KEY,
  shed_id uuid NOT NULL,
  shed_name text NOT NULL,
  partition_label text NOT NULL,
  prefix text NOT NULL,
  animal_count int NOT NULL,
  rule_ids uuid[] NOT NULL
) ON COMMIT DROP;

INSERT INTO qa_sheds VALUES
  (1, '91000000-0000-4000-8000-000000000201', 'Godel 1',   'Part 1', '',    2, ARRAY[(SELECT rule_id FROM qa_vax_rules WHERE dose_code = 'ET_TT_QA')]::uuid[]),
  (2, '91000000-0000-4000-8000-000000000203', 'Yashoda 1', 'Part 2', 'Y1-', 3, ARRAY[(SELECT rule_id FROM qa_vax_rules WHERE dose_code = 'PPR_QA'), (SELECT rule_id FROM qa_vax_rules WHERE dose_code = 'FMD_QA')]::uuid[]),
  (3, '9c000000-0000-4000-8000-000000000301', 'Gandhi 1',  'Part 1', 'G1-', 2, ARRAY[(SELECT rule_id FROM qa_vax_rules WHERE dose_code = 'PPR_QA'), (SELECT rule_id FROM qa_vax_rules WHERE dose_code = 'FMD_QA')]::uuid[]),
  (4, '9c000000-0000-4000-8000-000000000302', 'Gandhi 2',  'Part 3', 'G2-', 3, ARRAY[(SELECT rule_id FROM qa_vax_rules WHERE dose_code = 'PPR_QA'), (SELECT rule_id FROM qa_vax_rules WHERE dose_code = 'FMD_QA'), (SELECT rule_id FROM qa_vax_rules WHERE dose_code = 'HS_QA')]::uuid[]),
  (5, '91000000-0000-4000-8000-000000000202', 'Mandela 2', 'Part 7', 'M2-', 2, ARRAY[(SELECT rule_id FROM qa_vax_rules WHERE dose_code = 'PPR_QA'), (SELECT rule_id FROM qa_vax_rules WHERE dose_code = 'FMD_QA'), (SELECT rule_id FROM qa_vax_rules WHERE dose_code = 'HS_QA')]::uuid[]);

CREATE TEMP TABLE qa_tags (idx int PRIMARY KEY, tag text NOT NULL) ON COMMIT DROP;
INSERT INTO qa_tags VALUES
  (1, '901007000504418'),
  (2, '901007000504332'),
  (3, '901007000504407');

CREATE TEMP TABLE qa_fixture_goats (
  shed_seq int NOT NULL,
  tag_idx int NOT NULL,
  goat_id uuid PRIMARY KEY,
  display_id text NOT NULL,
  shed_id uuid NOT NULL,
  shed_name text NOT NULL,
  tag text NOT NULL
) ON COMMIT DROP;

INSERT INTO qa_fixture_goats
SELECT s.seq,
       t.idx,
       CASE
         WHEN s.seq = 1 THEN ('91000000-0000-4000-8000-' || lpad((1000 + t.idx)::text, 12, '0'))::uuid
         WHEN s.seq = 2 THEN ('91000000-0000-4000-8000-' || lpad((1002 + t.idx)::text, 12, '0'))::uuid
         ELSE ('9c510000-0000-4000-8000-' || lpad(s.seq::text, 6, '0') || lpad(t.idx::text, 6, '0'))::uuid
       END,
       CASE
         WHEN s.seq = 1 THEN 'G-910' || lpad(t.idx::text, 3, '0')
         WHEN s.seq = 2 THEN 'G-910' || lpad((2 + t.idx)::text, 3, '0')
         ELSE 'G-5' || lpad(s.seq::text, 2, '0') || lpad(t.idx::text, 3, '0')
       END,
       s.shed_id,
       s.shed_name,
       s.prefix || t.tag
FROM qa_sheds s
JOIN qa_tags t ON t.idx <= s.animal_count;

UPDATE locations
SET location_code = CASE location_id
      WHEN '91000000-0000-4000-8000-000000000201' THEN 'QA-VAX-GODEL-1'
      WHEN '91000000-0000-4000-8000-000000000203' THEN 'QA-VAX-YASHODA-1'
      ELSE location_code
    END,
    name = CASE location_id
      WHEN '91000000-0000-4000-8000-000000000201' THEN 'Godel 1'
      WHEN '91000000-0000-4000-8000-000000000203' THEN 'Yashoda 1'
      ELSE name
    END,
    parent_location_id = '91000000-0000-4000-8000-000000000101',
    status = 'active',
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND location_id IN ('91000000-0000-4000-8000-000000000201','91000000-0000-4000-8000-000000000203');

UPDATE locations
SET location_code = location_code || '-OLD-' || right(location_id::text, 4),
    status = 'inactive',
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND location_id IN (
    '9c500000-0000-4000-8000-000000000301',
    '9c500000-0000-4000-8000-000000000302',
    '9c500000-0000-4000-8000-000000000303'
  )
  AND location_code IN ('QA-VAX-GANDHI-1', 'QA-VAX-GANDHI-2', 'QA-VAX-MANDELA-2');

INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, state_region, status, display_order, updated_at)
SELECT shed_id, '${tenant_id}'::uuid, 'shed', 'QA-VAX-' || upper(replace(shed_name, ' ', '-')),
       shed_name, '91000000-0000-4000-8000-000000000101'::uuid, 'Karnataka', 'active', 50 + seq, now()
FROM qa_sheds
ON CONFLICT (location_id) DO UPDATE
SET location_code = EXCLUDED.location_code,
    name = EXCLUDED.name,
    parent_location_id = EXCLUDED.parent_location_id,
    status = 'active',
    updated_at = now();

INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_counts, usable_for_feed, usable_for_vaccination, usable_for_sop, is_holding, is_quarantine, is_icu, display_order, notes)
SELECT '${tenant_id}'::uuid, shed_id, true, true, true, true, false, false, false, 50 + seq,
       'Phone QA 5-shed vaccination fixture'
FROM qa_sheds
ON CONFLICT (location_id) DO UPDATE
SET usable_for_vaccination = true,
    usable_for_sop = true,
    notes = EXCLUDED.notes,
    updated_at = now();

INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, health_status, custodian_party_id, current_location_id, farm_id, park_id, shed_id, dob, origin_type, entry_date)
SELECT fg.goat_id,
       '${tenant_id}'::uuid,
       fg.display_id,
       CASE WHEN fg.tag_idx % 2 = 0 THEN 'male' ELSE 'female' END,
       'kid', 'alive', 'K2', 'healthy',
       '91000000-0000-4000-8000-000000000301',
       fg.shed_id, '91000000-0000-4000-8000-000000000100', '91000000-0000-4000-8000-000000000101', fg.shed_id,
       DATE '2026-05-15' + fg.tag_idx, 'birth', DATE '2026-05-15' + fg.tag_idx
FROM qa_fixture_goats fg
ON CONFLICT (goat_id) DO UPDATE
SET display_id = EXCLUDED.display_id,
    lifecycle_status = 'alive',
    management_stage = 'K2',
    health_status = 'healthy',
    current_location_id = EXCLUDED.current_location_id,
    farm_id = EXCLUDED.farm_id,
    park_id = EXCLUDED.park_id,
    shed_id = EXCLUDED.shed_id,
    exited_at = NULL,
    exit_reason = NULL,
    updated_at = now();

UPDATE goats g
SET lifecycle_status = 'inactive',
    exited_at = COALESCE(g.exited_at, now()),
    exit_reason = COALESCE(g.exit_reason, 'transferred'),
    updated_at = now()
WHERE g.tenant_id = '${tenant_id}'::uuid
  AND (
    g.goat_id::text LIKE '9a000000-%'
    OR g.goat_id::text LIKE '9c510000-%'
    OR g.goat_id::text LIKE '92000000-0000-4000-8000-0000000010%'
  )
  AND NOT EXISTS (SELECT 1 FROM qa_fixture_goats fg WHERE fg.goat_id = g.goat_id);

DELETE FROM goat_identifiers gi
WHERE gi.tenant_id = '${tenant_id}'::uuid
  AND gi.source_system IN ('seed-vaccination-per-goat-qa', 'phone-qa-vax5shed-seed', 'phone-qa-throwaway-seed')
  AND (
    gi.goat_id IN (SELECT goat_id FROM qa_fixture_goats)
    OR gi.goat_id::text LIKE '9a000000-%'
    OR gi.goat_id::text LIKE '9c510000-%'
    OR gi.goat_id::text LIKE '92000000-0000-4000-8000-0000000010%'
  );

INSERT INTO goat_identifiers (identifier_id, tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, source_system, source_record_id, normalizer_version, confidence)
SELECT ('9c520000-0000-4000-8000-' || lpad(fg.shed_seq::text, 6, '0') || lpad(fg.tag_idx::text, 6, '0'))::uuid,
       '${tenant_id}'::uuid,
       fg.goat_id,
       'animal_identifier_1',
       fg.tag,
       fg.tag,
       'tenant:${tenant_id}',
       true,
       'active',
       now(),
       'phone-qa-vax5shed-seed',
       'vax5shed-' || fg.shed_seq || '-' || fg.tag_idx,
       'seed-v1',
       1.0
FROM qa_fixture_goats fg
ON CONFLICT (tenant_id, normalized_value) DO UPDATE
SET goat_id = EXCLUDED.goat_id,
    identifier_value = EXCLUDED.identifier_value,
    identifier_type = EXCLUDED.identifier_type,
    is_primary_for_goat = true,
    status = 'active',
    updated_at = now();

INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name, updated_at)
SELECT '${tenant_id}'::uuid, g.goat_id, g.shed_id, s.partition_label, s.shed_name, now()
FROM goats g
JOIN qa_sheds s ON s.shed_id = g.shed_id
WHERE g.tenant_id = '${tenant_id}'::uuid
  AND g.goat_id IN (SELECT goat_id FROM qa_fixture_goats)
ON CONFLICT (tenant_id, goat_id) DO UPDATE
SET shed_id = EXCLUDED.shed_id,
    partition_label = EXCLUDED.partition_label,
    source_shed_name = EXCLUDED.source_shed_name,
    updated_at = now();

INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, display_order, source, updated_at)
SELECT '${tenant_id}'::uuid,
       shed_id,
       partition_label,
       regexp_replace(lower(btrim(partition_label)), '^part[[:space:]]+', ''),
       'active',
       seq,
       'manual',
       now()
FROM qa_sheds
ON CONFLICT (tenant_id, shed_id, normalized_label) DO UPDATE
SET partition_label = EXCLUDED.partition_label,
    status = 'active',
    display_order = EXCLUDED.display_order,
    source = EXCLUDED.source,
    updated_at = now();

UPDATE obligation_batches
SET scope_type = 'park',
    scope_id = '91000000-0000-4000-8000-000000000101'::uuid,
    planned_date = ${today_sql},
    window_start = now() - interval '1 hour',
    window_end = now() + interval '3 days',
    status = 'in_progress',
    estimated_targets = 12,
    planned_quantity = 27,
    primary_inventory_lot_id = '91000000-0000-4000-8000-000000000602',
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND batch_id = '91000000-0000-4000-8000-000000000701';

UPDATE sop_tasks
SET title = '5-shed vaccination obligation QA',
    description = 'Five sheds covering one, two, and three vaccine obligations per animal.',
    assigned_to = '${qa_operator_member_id}'::uuid,
    scope_type = 'park',
    scope_id = '91000000-0000-4000-8000-000000000101'::uuid,
    state = 'assigned',
    due_at = now() + interval '6 hours',
    context = jsonb_build_object('seed', 'phone-qa-vax5shed', 'obligation_batch_id', '91000000-0000-4000-8000-000000000701'),
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND task_id = '91000000-0000-4000-8000-000000000702';

DELETE FROM vaccination_completions
WHERE tenant_id = '${tenant_id}'::uuid
  AND (
    batch_id IN (
      '91000000-0000-4000-8000-000000000701',
      '92000000-0000-4000-8000-000000000711'
    )
    OR obligation_id::text LIKE '9c530000%'
    OR sop_submission_item_id IN (
      SELECT item_id
      FROM sop_submission_items
      WHERE tenant_id = '${tenant_id}'::uuid
        AND task_id IN (
          '91000000-0000-4000-8000-000000000702',
          '92000000-0000-4000-8000-000000000712'
        )
    )
  );

DELETE FROM sop_task_submission_fanouts
WHERE tenant_id = '${tenant_id}'::uuid
  AND task_id IN (
    '91000000-0000-4000-8000-000000000702',
    '92000000-0000-4000-8000-000000000712'
  );

DELETE FROM sop_submission_items
WHERE tenant_id = '${tenant_id}'::uuid
  AND task_id IN (
    '91000000-0000-4000-8000-000000000702',
    '92000000-0000-4000-8000-000000000712'
  );

DELETE FROM sop_submissions
WHERE tenant_id = '${tenant_id}'::uuid
  AND task_id IN (
    '91000000-0000-4000-8000-000000000702',
    '92000000-0000-4000-8000-000000000712'
  );

DELETE FROM sop_task_scan_captures
WHERE tenant_id = '${tenant_id}'::uuid
  AND task_id IN (
    '91000000-0000-4000-8000-000000000702',
    '92000000-0000-4000-8000-000000000712'
  );

DELETE FROM proof_artifacts
WHERE tenant_id = '${tenant_id}'::uuid
  AND scope_type = 'task'
  AND scope_id IN (
    '91000000-0000-4000-8000-000000000702',
    '92000000-0000-4000-8000-000000000712'
  );

DELETE FROM vaccination_drive_assignment_members WHERE tenant_id = '${tenant_id}'::uuid;
DELETE FROM vaccination_drive_assignments WHERE tenant_id = '${tenant_id}'::uuid;
DELETE FROM obligation_instances
WHERE tenant_id = '${tenant_id}'::uuid
  AND (
    batch_id = '91000000-0000-4000-8000-000000000701'
    OR batch_id = '92000000-0000-4000-8000-000000000711'
    OR idempotency_key LIKE 'qa-vax5shed-%'
    OR idempotency_key LIKE 'qa-vax-per-goat-%'
    OR obligation_id::text LIKE '9c530000%'
  );

UPDATE obligation_batches
SET status = 'canceled',
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND batch_id = '92000000-0000-4000-8000-000000000711';

UPDATE sop_tasks
SET state = 'canceled',
    updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND task_id = '92000000-0000-4000-8000-000000000712';

CREATE TEMP TABLE qa_due_goats ON COMMIT DROP AS
SELECT g.goat_id, s.seq AS shed_seq, s.shed_id, s.shed_name, s.rule_ids, gi.normalized_value AS tag
FROM goats g
JOIN qa_sheds s ON s.shed_id = g.shed_id
JOIN goat_identifiers gi
  ON gi.tenant_id = g.tenant_id
 AND gi.goat_id = g.goat_id
 AND gi.identifier_type = 'animal_identifier_1'
 AND gi.is_primary_for_goat
 AND gi.status = 'active'
WHERE g.tenant_id = '${tenant_id}'::uuid
  AND g.goat_id IN (SELECT goat_id FROM qa_fixture_goats);

INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, target_type,
  target_id, scope_type, scope_id, due_at, window_start, window_end, status,
  sop_task_id, idempotency_key, sequence
)
SELECT ('9c530000-0000-4000-8000-' || lpad(d.shed_seq::text, 3, '0') || lpad(dense_rank() OVER (PARTITION BY d.shed_seq ORDER BY d.goat_id)::text, 3, '0') || lpad(r.sequence::text, 6, '0'))::uuid,
       '${tenant_id}'::uuid,
       '91000000-0000-4000-8000-000000000502'::uuid,
       r.rule_id,
       '91000000-0000-4000-8000-000000000701'::uuid,
       'goat',
       d.goat_id,
       'shed',
       d.shed_id,
       now(),
       now() - interval '1 hour',
       now() + interval '3 days',
       'due',
       '91000000-0000-4000-8000-000000000702'::uuid,
       'qa-vax5shed-' || d.shed_seq || '-' || d.goat_id::text || '-' || r.dose_code,
       r.sequence
FROM qa_due_goats d
JOIN qa_vax_rules r ON r.rule_id = ANY(d.rule_ids)
ON CONFLICT (obligation_id) DO UPDATE
SET rule_id = EXCLUDED.rule_id,
    batch_id = EXCLUDED.batch_id,
    status = 'due',
    scope_id = EXCLUDED.scope_id,
    sop_task_id = EXCLUDED.sop_task_id,
    due_at = now(),
    updated_at = now();

INSERT INTO vaccination_drive_assignments (
  assignment_id, tenant_id, batch_id, planned_date, operator_id, park_id, shed_id,
  physical_shed, partition_label, animal_count, capacity_status, warnings,
  vaccine_rule_ids, total_doses
)
SELECT ('9c540000-0000-4000-8000-' || lpad(s.seq::text, 12, '0'))::uuid,
       '${tenant_id}'::uuid,
       '91000000-0000-4000-8000-000000000701'::uuid,
       ${today_sql},
       '${qa_operator_member_id}'::uuid,
       '91000000-0000-4000-8000-000000000101'::uuid,
       s.shed_id,
       s.shed_name,
       s.partition_label,
       s.animal_count,
       'within_cap',
       '[]'::jsonb,
       s.rule_ids,
       s.animal_count * cardinality(s.rule_ids)
FROM qa_sheds s
ON CONFLICT (assignment_id) DO UPDATE
SET planned_date = EXCLUDED.planned_date,
    operator_id = EXCLUDED.operator_id,
    park_id = EXCLUDED.park_id,
    shed_id = EXCLUDED.shed_id,
    physical_shed = EXCLUDED.physical_shed,
    partition_label = EXCLUDED.partition_label,
    animal_count = EXCLUDED.animal_count,
    vaccine_rule_ids = EXCLUDED.vaccine_rule_ids,
    total_doses = EXCLUDED.total_doses,
    updated_at = now();

INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
SELECT '${tenant_id}'::uuid,
       ('9c540000-0000-4000-8000-' || lpad(d.shed_seq::text, 12, '0'))::uuid,
       oi.obligation_id,
       oi.target_id
FROM obligation_instances oi
JOIN qa_due_goats d ON d.goat_id = oi.target_id AND d.shed_id = oi.scope_id
WHERE oi.tenant_id = '${tenant_id}'::uuid
  AND oi.batch_id = '91000000-0000-4000-8000-000000000701'
ON CONFLICT (tenant_id, obligation_id) DO UPDATE
SET assignment_id = EXCLUDED.assignment_id;

SET LOCAL goatos.approved_location_migration_plan = 'phone-qa-vax5shed: retire empty seeded parks/sheds in disposable DB only';

UPDATE locations
SET status = 'inactive', updated_at = now()
WHERE tenant_id = '${tenant_id}'::uuid
  AND location_type IN ('park','shed')
  AND location_id NOT IN (
    '91000000-0000-4000-8000-000000000101',
    '91000000-0000-4000-8000-000000000201',
    '91000000-0000-4000-8000-000000000202',
    '91000000-0000-4000-8000-000000000203',
    '9c000000-0000-4000-8000-000000000301',
    '9c000000-0000-4000-8000-000000000302'
  );

COMMIT;
SQL

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<SQL
\pset pager off

DO \$\$
DECLARE
  n int;
BEGIN
  WITH expected_sheds(shed_id) AS (
    VALUES
      ('91000000-0000-4000-8000-000000000201'::uuid),
      ('91000000-0000-4000-8000-000000000203'::uuid),
      ('9c000000-0000-4000-8000-000000000301'::uuid),
      ('9c000000-0000-4000-8000-000000000302'::uuid),
      ('91000000-0000-4000-8000-000000000202'::uuid)
  )
  SELECT count(*) INTO n
  FROM locations l
  JOIN location_operational_attributes loa
    ON loa.tenant_id = l.tenant_id
   AND loa.location_id = l.location_id
  WHERE l.tenant_id = '${tenant_id}'::uuid
    AND l.location_type = 'shed'
    AND l.status = 'active'
    AND loa.usable_for_vaccination;
  IF n <> 5 THEN
    RAISE EXCEPTION 'phone-qa-vax5shed expected 5 active vaccination sheds, found %', n;
  END IF;

  WITH expected_counts(shed_id, animals, obligations) AS (
    VALUES
      ('91000000-0000-4000-8000-000000000201'::uuid, 2, 2),
      ('91000000-0000-4000-8000-000000000203'::uuid, 3, 6),
      ('9c000000-0000-4000-8000-000000000301'::uuid, 2, 4),
      ('9c000000-0000-4000-8000-000000000302'::uuid, 3, 9),
      ('91000000-0000-4000-8000-000000000202'::uuid, 2, 6)
  ),
  actual_counts AS (
    SELECT e.shed_id,
           count(DISTINCT g.goat_id)::int AS animals,
           count(oi.obligation_id)::int AS obligations
    FROM expected_counts e
    LEFT JOIN goats g
      ON g.tenant_id = '${tenant_id}'::uuid
     AND g.shed_id = e.shed_id
     AND g.lifecycle_status = 'alive'
    LEFT JOIN obligation_instances oi
      ON oi.tenant_id = '${tenant_id}'::uuid
     AND oi.target_id = g.goat_id
     AND oi.batch_id = '91000000-0000-4000-8000-000000000701'
     AND oi.status = 'due'
    GROUP BY e.shed_id
  )
  SELECT count(*) INTO n
  FROM expected_counts e
  JOIN actual_counts a ON a.shed_id = e.shed_id
  WHERE a.animals <> e.animals OR a.obligations <> e.obligations;
  IF n <> 0 THEN
    RAISE EXCEPTION 'phone-qa-vax5shed shed animal/obligation counts drifted';
  END IF;

  WITH expected_sheds(shed_id) AS (
    VALUES
      ('91000000-0000-4000-8000-000000000201'::uuid),
      ('91000000-0000-4000-8000-000000000203'::uuid),
      ('9c000000-0000-4000-8000-000000000301'::uuid),
      ('9c000000-0000-4000-8000-000000000302'::uuid),
      ('91000000-0000-4000-8000-000000000202'::uuid)
  )
  SELECT count(*) INTO n
  FROM goats g
  WHERE g.tenant_id = '${tenant_id}'::uuid
    AND g.lifecycle_status = 'alive'
    AND g.shed_id IN (SELECT shed_id FROM expected_sheds);
  IF n <> 12 THEN
    RAISE EXCEPTION 'phone-qa-vax5shed expected 12 alive animals, found %', n;
  END IF;

  WITH fixture_goats AS (
    SELECT g.goat_id
    FROM goats g
    WHERE g.tenant_id = '${tenant_id}'::uuid
      AND g.lifecycle_status = 'alive'
      AND g.shed_id IN (
        '91000000-0000-4000-8000-000000000201',
        '91000000-0000-4000-8000-000000000203',
        '9c000000-0000-4000-8000-000000000301',
        '9c000000-0000-4000-8000-000000000302',
        '91000000-0000-4000-8000-000000000202'
      )
  )
  SELECT count(*) INTO n
  FROM fixture_goats g
  JOIN goat_identifiers gi
    ON gi.tenant_id = '${tenant_id}'::uuid
   AND gi.goat_id = g.goat_id
   AND gi.identifier_type = 'animal_identifier_1'
   AND gi.is_primary_for_goat
   AND gi.status = 'active';
  IF n <> 12 THEN
    RAISE EXCEPTION 'phone-qa-vax5shed expected 12 primary animal_identifier_1 rows, found %', n;
  END IF;

  SELECT count(*) INTO n
  FROM (
    SELECT gi.goat_id
    FROM goat_identifiers gi
    WHERE gi.tenant_id = '${tenant_id}'::uuid
      AND gi.identifier_type = 'animal_identifier_1'
      AND gi.is_primary_for_goat
      AND gi.status = 'active'
    GROUP BY gi.goat_id
    HAVING count(*) <> 1
  ) bad;
  IF n <> 0 THEN
    RAISE EXCEPTION 'phone-qa-vax5shed found % goats with duplicate primary animal_identifier_1 rows', n;
  END IF;

  SELECT count(*) INTO n
  FROM obligation_instances oi
  WHERE oi.tenant_id = '${tenant_id}'::uuid
    AND oi.batch_id = '91000000-0000-4000-8000-000000000701'
    AND oi.status = 'due';
  IF n <> 27 THEN
    RAISE EXCEPTION 'phone-qa-vax5shed expected 27 due obligations, found %', n;
  END IF;

  SELECT count(*) INTO n
  FROM vaccination_drive_assignments a
  WHERE a.tenant_id = '${tenant_id}'::uuid
    AND a.batch_id = '91000000-0000-4000-8000-000000000701';
  IF n <> 5 THEN
    RAISE EXCEPTION 'phone-qa-vax5shed expected 5 vaccination assignments, found %', n;
  END IF;

  SELECT count(*) INTO n
  FROM vaccination_drive_assignments a
  WHERE a.tenant_id = '${tenant_id}'::uuid
    AND a.batch_id = '91000000-0000-4000-8000-000000000701'
    AND (a.operator_id <> '${qa_operator_member_id}'::uuid OR lower(btrim(a.partition_label)) = 'whole');
  IF n <> 0 THEN
    RAISE EXCEPTION 'phone-qa-vax5shed assignments must target operator % and real partitions, found % bad rows', '${qa_operator_user_id}', n;
  END IF;

  SELECT count(*) INTO n
  FROM user_scope_grants g
  WHERE g.tenant_id = '${tenant_id}'::uuid
    AND g.user_id = '${qa_operator_user_id}'::uuid
    AND g.role = 'operator'
    AND g.scope_type = 'park'
    AND g.scope_id = '${qa_park_id}'::uuid
    AND g.status = 'active'
    AND (g.valid_to IS NULL OR g.valid_to > now());
  IF n = 0 THEN
    RAISE EXCEPTION 'phone-qa-vax5shed operator % must have active operator grant on fixture park %', '${qa_operator_user_id}', '${qa_park_id}';
  END IF;

  SELECT count(*) INTO n
  FROM user_scope_grants g
  WHERE g.tenant_id = '${tenant_id}'::uuid
    AND g.user_id = '${qa_operator_user_id}'::uuid
    AND g.role = 'operator'
    AND g.scope_type = 'park'
    AND g.scope_id <> '${qa_park_id}'::uuid
    AND g.status = 'active'
    AND (g.valid_to IS NULL OR g.valid_to > now());
  IF n <> 0 THEN
    RAISE EXCEPTION 'phone-qa-vax5shed operator % must not keep another active operator park grant during this fixture; found %', '${qa_operator_user_id}', n;
  END IF;

  SELECT count(*) INTO n
  FROM vaccination_drive_assignment_members m
  JOIN vaccination_drive_assignments a
    ON a.assignment_id = m.assignment_id
   AND a.tenant_id = m.tenant_id
  WHERE m.tenant_id = '${tenant_id}'::uuid
    AND a.batch_id = '91000000-0000-4000-8000-000000000701';
  IF n <> 27 THEN
    RAISE EXCEPTION 'phone-qa-vax5shed expected 27 assignment members, found %', n;
  END IF;
END
\$\$;

\echo 'Validation totals'
WITH fixture_goats AS (
  SELECT g.goat_id, g.shed_id
  FROM goats g
  WHERE g.tenant_id = '${tenant_id}'::uuid
    AND g.lifecycle_status = 'alive'
    AND g.shed_id IN (
      '91000000-0000-4000-8000-000000000201',
      '91000000-0000-4000-8000-000000000203',
      '9c000000-0000-4000-8000-000000000301',
      '9c000000-0000-4000-8000-000000000302',
      '91000000-0000-4000-8000-000000000202'
    )
),
fixture_obligations AS (
  SELECT oi.*
  FROM obligation_instances oi
  JOIN fixture_goats g ON g.goat_id = oi.target_id
  WHERE oi.tenant_id = '${tenant_id}'::uuid
    AND oi.status = 'due'
    AND oi.batch_id = '91000000-0000-4000-8000-000000000701'
)
SELECT 'active_vaccination_sheds' AS metric, count(*)
FROM locations l
JOIN location_operational_attributes loa
  ON loa.tenant_id = l.tenant_id
 AND loa.location_id = l.location_id
WHERE l.tenant_id = '${tenant_id}'::uuid
  AND l.location_type = 'shed'
  AND l.status = 'active'
  AND loa.usable_for_vaccination
UNION ALL
SELECT 'alive_animals', count(*) FROM fixture_goats
UNION ALL
SELECT 'due_obligations', count(*) FROM fixture_obligations
UNION ALL
SELECT 'assignments', count(*) FROM vaccination_drive_assignments WHERE tenant_id = '${tenant_id}'::uuid AND batch_id = '91000000-0000-4000-8000-000000000701'
UNION ALL
SELECT 'assignment_members', count(*)
FROM vaccination_drive_assignment_members m
JOIN vaccination_drive_assignments a ON a.tenant_id = m.tenant_id AND a.assignment_id = m.assignment_id
WHERE m.tenant_id = '${tenant_id}'::uuid AND a.batch_id = '91000000-0000-4000-8000-000000000701';

\echo 'Counts by shed'
WITH expected_sheds(seq, shed_id, shed_name, combo) AS (
  VALUES
    (1, '91000000-0000-4000-8000-000000000201'::uuid, 'Godel 1', 'ET+TT'),
    (2, '91000000-0000-4000-8000-000000000203'::uuid, 'Yashoda 1', 'PPR+FMD'),
    (3, '9c000000-0000-4000-8000-000000000301'::uuid, 'Gandhi 1', 'PPR+FMD'),
    (4, '9c000000-0000-4000-8000-000000000302'::uuid, 'Gandhi 2', 'PPR+FMD+HS'),
    (5, '91000000-0000-4000-8000-000000000202'::uuid, 'Mandela 2', 'PPR+FMD+HS')
),
expected_rules(rule_id, vaccine_code, rule_order) AS (
  SELECT pr.rule_id, v.vaccine_code, v.rule_order
  FROM (VALUES
    ('ET_TT_QA', 'ET+TT', 1),
    ('PPR_QA', 'PPR', 2),
    ('FMD_QA', 'FMD', 3),
    ('HS_QA', 'HS', 4)
  ) AS v(dose_code, vaccine_code, rule_order)
  JOIN protocol_rules pr
    ON pr.tenant_id = '${tenant_id}'::uuid
   AND pr.protocol_version_id = '91000000-0000-4000-8000-000000000502'::uuid
   AND pr.dose_code = v.dose_code
),
fixture_goats AS (
  SELECT g.goat_id, g.shed_id, gi.normalized_value
  FROM goats g
  JOIN goat_identifiers gi
    ON gi.tenant_id = g.tenant_id
   AND gi.goat_id = g.goat_id
   AND gi.identifier_type = 'animal_identifier_1'
   AND gi.is_primary_for_goat
   AND gi.status = 'active'
  WHERE g.tenant_id = '${tenant_id}'::uuid
    AND g.lifecycle_status = 'alive'
),
due AS (
  SELECT oi.scope_id AS shed_id, er.vaccine_code, er.rule_order, oi.obligation_id
  FROM obligation_instances oi
  JOIN expected_rules er ON er.rule_id = oi.rule_id
  WHERE oi.tenant_id = '${tenant_id}'::uuid
    AND oi.batch_id = '91000000-0000-4000-8000-000000000701'
    AND oi.status = 'due'
)
SELECT e.seq,
       e.shed_name,
       e.combo,
       count(DISTINCT g.goat_id) AS animals,
       count(DISTINCT d.obligation_id) AS due_obligations,
       (
         SELECT string_agg(v.vaccine_code, '+' ORDER BY v.rule_order)
         FROM (
           SELECT DISTINCT d2.vaccine_code, d2.rule_order
           FROM due d2
           WHERE d2.shed_id = e.shed_id
         ) v
       ) AS vaccines,
       string_agg(DISTINCT g.normalized_value, ', ' ORDER BY g.normalized_value) AS roster_identifiers
FROM expected_sheds e
LEFT JOIN fixture_goats g ON g.shed_id = e.shed_id
LEFT JOIN due d ON d.shed_id = e.shed_id
GROUP BY e.seq, e.shed_id, e.shed_name, e.combo
ORDER BY e.seq;

\echo 'Counts by vaccine'
WITH expected_rules(rule_id, vaccine_code, rule_order) AS (
  SELECT pr.rule_id, v.vaccine_code, v.rule_order
  FROM (VALUES
    ('ET_TT_QA', 'ET+TT', 1),
    ('PPR_QA', 'PPR', 2),
    ('FMD_QA', 'FMD', 3),
    ('HS_QA', 'HS', 4)
  ) AS v(dose_code, vaccine_code, rule_order)
  JOIN protocol_rules pr
    ON pr.tenant_id = '${tenant_id}'::uuid
   AND pr.protocol_version_id = '91000000-0000-4000-8000-000000000502'::uuid
   AND pr.dose_code = v.dose_code
)
SELECT er.vaccine_code,
       count(DISTINCT oi.target_id) AS animals,
       count(DISTINCT oi.scope_id) AS sheds,
       count(*) AS due_obligations
FROM obligation_instances oi
JOIN expected_rules er ON er.rule_id = oi.rule_id
WHERE oi.tenant_id = '${tenant_id}'::uuid
  AND oi.batch_id = '91000000-0000-4000-8000-000000000701'
  AND oi.status = 'due'
GROUP BY er.vaccine_code
ORDER BY min(er.rule_order);

\echo 'Counts by assignment'
SELECT a.assignment_id,
       a.physical_shed AS shed_name,
       a.partition_label,
       a.animal_count,
       cardinality(a.vaccine_rule_ids) AS vaccines_per_animal,
       a.total_doses,
       count(m.obligation_id) AS assignment_members
FROM vaccination_drive_assignments a
LEFT JOIN vaccination_drive_assignment_members m
  ON m.tenant_id = a.tenant_id
 AND m.assignment_id = a.assignment_id
WHERE a.tenant_id = '${tenant_id}'::uuid
  AND a.batch_id = '91000000-0000-4000-8000-000000000701'
GROUP BY a.assignment_id, a.physical_shed, a.partition_label, a.animal_count, a.vaccine_rule_ids, a.total_doses
ORDER BY a.physical_shed;
SQL

cat <<EOF
Seeded local-only phone QA vax5shed fixture on ${safe_database_url}
Phone QA operator user id:
  ${qa_operator_user_id}
Phone QA operator workforce_member_id:
  ${qa_operator_member_id}

Fixture:
  Godel 1    2 animals  ET+TT
  Yashoda 1  3 animals  PPR+FMD
  Gandhi 1   2 animals  PPR+FMD
  Gandhi 2   3 animals  PPR+FMD+HS
  Mandela 2  2 animals  PPR+FMD+HS

Android dev sample card IDs (DebugSampleTagAliaser normalized ids):
  TEMP-CPT-CASTRO1-001, TEMP-CPT-CASTRO1-002, TEMP-CPT-CASTRO1-003
  TEMP-CPT-CASTRO1-004, TEMP-CPT-CASTRO1-005

Stored roster identifiers:
  Godel 1:    901007000504332, 901007000504418
  Yashoda 1:  Y1-901007000504332, Y1-901007000504407, Y1-901007000504418
  Gandhi 1:   G1-901007000504332, G1-901007000504418
  Gandhi 2:   G2-901007000504332, G2-901007000504407, G2-901007000504418
  Mandela 2:  M2-901007000504332, M2-901007000504418

Validation tags without Android dev aliasing:
  Wrong shed/orange: scan G1-901007000504418 while opened in Yashoda 1 - Part 2.
  Unknown/red:       QA-UNKNOWN-RED-0000
EOF
