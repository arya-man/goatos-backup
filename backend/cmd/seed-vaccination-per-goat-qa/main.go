package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const (
	defaultTenantID = "00000000-0000-4000-8000-000000000001"
	localActorID    = "90000000-0000-4000-8000-000000000001"
	qaOperatorID    = "91000000-0000-4000-8000-000000000001"
	qaFarmID        = "91000000-0000-4000-8000-000000000100"
	qaParkID        = "91000000-0000-4000-8000-000000000101"
	qaShed1ID       = "91000000-0000-4000-8000-000000000201"
	qaShed2ID       = "91000000-0000-4000-8000-000000000202"
	qaPartyID       = "91000000-0000-4000-8000-000000000301"
	qaSOPID         = "91000000-0000-4000-8000-000000000401"
	qaSOPVersionID  = "91000000-0000-4000-8000-000000000402"
	qaProtocolID    = "91000000-0000-4000-8000-000000000501"
	qaVersionID     = "91000000-0000-4000-8000-000000000502"
	qaRuleID        = "91000000-0000-4000-8000-000000000503"
	qaItemID        = "91000000-0000-4000-8000-000000000601"
	qaStockID       = "91000000-0000-4000-8000-000000000602"
	qaBatchID       = "91000000-0000-4000-8000-000000000701"
	qaTaskID        = "91000000-0000-4000-8000-000000000702"
	qaAssign1ID     = "91000000-0000-4000-8000-000000000801"
	qaAssign2ID     = "91000000-0000-4000-8000-000000000802"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("seed-vaccination-per-goat-qa", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID", defaultTenantID), "tenant id")
	timeout := fs.Duration("timeout", 30*time.Second, "seed timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	pgCfg := platformpg.ConfigFromEnv()
	if err := localtarget.ValidateLocalDatabaseTarget("seed-vaccination-per-goat-qa", os.Getenv("GOATOS_ENV"), pgCfg.DatabaseURL, "local", "dev", "test"); err != nil {
		return err
	}
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := execSeedSQL(ctx, pool, seedSQL, *tenantID); err != nil {
		return err
	}
	fmt.Printf("seeded per-goat vaccination QA tenant=%s park=%s sheds=%s,%s rfids=901007000504418,901007000504332,901007000504407,901007000504419,901007000504392 task=%s\n", *tenantID, qaParkID, qaShed1ID, qaShed2ID, qaTaskID)
	return nil
}

type seedExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func execSeedSQL(ctx context.Context, execer seedExecutor, sql string, tenantID string) error {
	for _, statement := range strings.Split(sql, ";\n\n") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if strings.Contains(statement, "$1") {
			if _, err := execer.Exec(ctx, statement, tenantID); err != nil { // scale-guard:ignore: local QA seed runner executes a fixed statement bundle, not request-path data fanout
				return fmt.Errorf("seed statement failed near %q: %w", previewStatement(statement), err)
			}
			continue
		}
		if _, err := execer.Exec(ctx, statement); err != nil { // scale-guard:ignore: local QA seed runner executes a fixed statement bundle, not request-path data fanout
			return fmt.Errorf("seed statement failed near %q: %w", previewStatement(statement), err)
		}
	}
	return nil
}

func previewStatement(statement string) string {
	preview := strings.Join(strings.Fields(statement), " ")
	if len(preview) > 160 {
		return preview[:160]
	}
	return preview
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

const perGoatProofPolicy = `{"types":["video"],"required":true,"proof_mode":"per_goat_video","subject_scope":"goat","expected_subjects":["goat"],"minimum_count":1,"minimum_count_per_subject":1,"maximum_count":25,"maximum_count_per_subject":5,"capture_source":"in_app_camera","allowed_capture_sources":["in_app_camera","gallery_picker"],"verify_capability":"proof.verify","verify_before_apply":true,"retention_policy":"operational_90d"}`

const seedSQL = `
INSERT INTO parties (party_id, party_type, display_name, status)
VALUES ('` + qaPartyID + `', 'org', 'Per Goat Proof QA', 'active')
ON CONFLICT (party_id) DO UPDATE SET display_name = EXCLUDED.display_name, status = 'active', updated_at = now();

INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, state_region, status, display_order, updated_at)
VALUES
  ('` + qaFarmID + `', $1::uuid, 'farm', 'QA-FARM', 'QA Farm', NULL, 'Karnataka', 'active', 10, now()),
  ('` + qaParkID + `', $1::uuid, 'park', 'QA-PARK', 'QA Park', '` + qaFarmID + `', 'Karnataka', 'active', 20, now()),
  ('` + qaShed1ID + `', $1::uuid, 'shed', 'QA-SHED-1', 'Shed 1', '` + qaParkID + `', 'Karnataka', 'active', 30, now()),
  ('` + qaShed2ID + `', $1::uuid, 'shed', 'QA-SHED-2', 'Shed 2', '` + qaParkID + `', 'Karnataka', 'active', 40, now())
ON CONFLICT (location_id) DO UPDATE
SET name = EXCLUDED.name, parent_location_id = EXCLUDED.parent_location_id, status = 'active', updated_at = now();

INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_counts, usable_for_feed, usable_for_vaccination, usable_for_sop, is_holding, is_quarantine, is_icu, display_order, notes)
VALUES
  ($1::uuid, '` + qaParkID + `', true, true, true, true, false, false, false, 20, 'Per-goat proof QA park'),
  ($1::uuid, '` + qaShed1ID + `', true, true, true, true, false, false, false, 30, 'Per-goat proof QA shed 1'),
  ($1::uuid, '` + qaShed2ID + `', true, true, true, true, false, false, false, 40, 'Per-goat proof QA shed 2')
ON CONFLICT (location_id) DO UPDATE
SET usable_for_vaccination = true, usable_for_sop = true, is_quarantine = false, is_icu = false, updated_at = now();

INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id, metadata, updated_at)
VALUES ('` + qaOperatorID + `', $1::uuid, '` + localActorID + `', 'AMIT-QA-VAX', 'Amit Kumar', 'active', 'operator', '` + qaParkID + `', '{"seed":"vaccination-per-goat-qa"}'::jsonb, now())
ON CONFLICT (tenant_id, display_code) DO UPDATE
SET user_id = EXCLUDED.user_id, display_name = EXCLUDED.display_name, status = 'active', primary_role_hint = 'operator', primary_location_id = EXCLUDED.primary_location_id, metadata = EXCLUDED.metadata, updated_at = now();

INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES
  ($1::uuid, '` + localActorID + `', 'operator', 'tenant', $1::uuid, 'active', now()),
  ($1::uuid, '` + localActorID + `', 'operator', 'park', '` + qaParkID + `', 'active', now())
ON CONFLICT DO NOTHING;

WITH dept AS (
  SELECT department_id
  FROM departments
  WHERE tenant_id = $1::uuid AND code = 'preventive_care'
  LIMIT 1
)
UPDATE workforce_members wm
SET department_id = dept.department_id, updated_at = now()
FROM dept
WHERE wm.tenant_id = $1::uuid AND wm.workforce_member_id = '` + qaOperatorID + `';

WITH dept AS (
  SELECT department_id
  FROM departments
  WHERE tenant_id = $1::uuid AND code = 'preventive_care'
  LIMIT 1
)
INSERT INTO department_module_grants (tenant_id, department_id, module_key, status)
SELECT $1::uuid, department_id, 'vaccination', 'active'
FROM dept
ON CONFLICT (tenant_id, department_id, module_key) DO UPDATE SET status = 'active', updated_at = now();

INSERT INTO sop_definitions (sop_id, tenant_id, code, name, description, status)
VALUES ('` + qaSOPID + `', $1::uuid, 'vaccination.per_goat_video_qa', 'Vaccination per animal proof QA', 'Per-animal vaccination proof QA SOP', 'active')
ON CONFLICT (sop_id) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description, status = 'active', updated_at = now();

INSERT INTO sop_versions (sop_version_id, tenant_id, sop_id, version, version_label, status, form_dsl, proof_policy, compatibility, validation_report, published_at)
VALUES (
  '` + qaSOPVersionID + `', $1::uuid, '` + qaSOPID + `', 1, 'Per animal video proof', 'published',
  '{"schema_version":"goatos.sop-form.v1","fields":[{"key":"goat_ids","label":"Goats","type":"goat_scan","required":true,"repeat":true}],"rules":[]}'::jsonb,
  '` + perGoatProofPolicy + `'::jsonb,
  '{}'::jsonb,
  '{"seed":"vaccination-per-goat-qa"}'::jsonb,
  now()
)
ON CONFLICT (sop_version_id) DO NOTHING;

INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit, status, context)
VALUES ('` + qaItemID + `', $1::uuid, 'VAX-QA-ETTT', 'ET+TT Vaccine QA', 'vaccine', 'dose', 'active', '{"seed":"vaccination-per-goat-qa"}'::jsonb)
ON CONFLICT (item_id) DO UPDATE SET item_code = EXCLUDED.item_code, name = EXCLUDED.name, status = 'active', updated_at = now();

INSERT INTO vaccines (tenant_id, item_id, disease, manufacturer, doses_per_vial, withdrawal_days, context)
VALUES ($1::uuid, '` + qaItemID + `', 'Enterotoxaemia + Tetanus', 'QA', 10, 0, '{"seed":"vaccination-per-goat-qa"}'::jsonb)
ON CONFLICT (tenant_id, item_id) DO UPDATE SET disease = EXCLUDED.disease, manufacturer = EXCLUDED.manufacturer, updated_at = now();

INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, lot_code, expiry_date, quantity_in_stock, quantity_reserved, quantity_unit, status)
VALUES ('` + qaStockID + `', $1::uuid, '` + qaItemID + `', '` + qaParkID + `', 'QA-LOT-001', DATE '2027-12-31', 100, 0, 'dose', 'active')
ON CONFLICT (stock_id) DO UPDATE SET quantity_in_stock = 100, quantity_reserved = 0, status = 'active', updated_at = now();

INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
VALUES ('` + qaProtocolID + `', $1::uuid, 'vaccination.per_goat_video_qa', 'Per Animal Proof Vaccination QA', 'vaccination', 'active')
ON CONFLICT (protocol_id) DO UPDATE SET code = EXCLUDED.code, name = EXCLUDED.name, status = 'active', updated_at = now();

INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version, version_label, status, effective_from, effective_to, rule_dsl, proof_policy, sop_version_id, published_at)
VALUES (
  '` + qaVersionID + `', $1::uuid, '` + qaProtocolID + `', 'park', '` + qaParkID + `', 1, 'Per animal proof QA', 'draft',
  DATE '2026-01-01', DATE '2028-01-01',
  '{"vaccine":{"code":"ET+TT","name":"ET+TT","inventory_item_id":"` + qaItemID + `"},"schedule":[{"dose_code":"ET_TT_QA","sequence":1,"trigger_type":"manual_campaign","proof_policy":` + perGoatProofPolicy + `}]}'::jsonb,
  '` + perGoatProofPolicy + `'::jsonb,
  '` + qaSOPVersionID + `',
  NULL
)
ON CONFLICT (protocol_version_id) DO NOTHING;

INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type, offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json, sop_version_id, proof_policy, sort_order)
VALUES ('` + qaRuleID + `', $1::uuid, '` + qaVersionID + `', 'ET_TT_QA', 1, 'manual_campaign', 0, 3, 0, 'none', 'immediate', '{"stage":"K2","lifecycle":"alive"}'::jsonb, '` + qaSOPVersionID + `', '` + perGoatProofPolicy + `'::jsonb, 10)
ON CONFLICT (rule_id) DO NOTHING;

INSERT INTO protocol_rule_dimensions (tenant_id, protocol_version_id, rule_id, category, ruleset_family, matrix_row_id, selector_key, dose_code, source_dose_code, vaccine_code, vaccine_type, pathogen_class, compatibility_group, species, animal_stage, sex, breed, lifecycle, health, reproductive, min_age_days, max_age_days, trigger_type, sequence, offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json, vaccine_json, schedule_json)
VALUES ($1::uuid, '` + qaVersionID + `', '` + qaRuleID + `', 'vaccination', 'qa', 'qa-row', 'qa-selector', 'ET_TT_QA', 'ET_TT_QA', 'ET+TT', 'killed', 'bacterial', 'ET+TT', 'goat', 'K2', 'all', 'all', 'alive', 'any', 'any', 0, 180, 'manual_campaign', 1, 0, 3, 0, 'none', 'immediate', '{"stage":"K2","lifecycle":"alive"}'::jsonb, '{"code":"ET+TT"}'::jsonb, '{"dose_code":"ET_TT_QA"}'::jsonb)
ON CONFLICT DO NOTHING;

UPDATE protocol_versions
SET status = 'published',
    published_at = COALESCE(published_at, now()),
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND protocol_version_id = '` + qaVersionID + `'
  AND status = 'draft';

INSERT INTO goats (goat_id, tenant_id, display_id, sex, age_band, lifecycle_status, management_stage, health_status, custodian_party_id, current_location_id, farm_id, park_id, shed_id, dob, origin_type, entry_date)
VALUES
  ('91000000-0000-4000-8000-000000001001', $1::uuid, 'G-910001', 'female', 'kid', 'alive', 'K2', 'healthy', '` + qaPartyID + `', '` + qaShed1ID + `', '` + qaFarmID + `', '` + qaParkID + `', '` + qaShed1ID + `', DATE '2026-05-15', 'birth', DATE '2026-05-15'),
  ('91000000-0000-4000-8000-000000001002', $1::uuid, 'G-910002', 'male', 'kid', 'alive', 'K2', 'healthy', '` + qaPartyID + `', '` + qaShed1ID + `', '` + qaFarmID + `', '` + qaParkID + `', '` + qaShed1ID + `', DATE '2026-05-16', 'birth', DATE '2026-05-16'),
  ('91000000-0000-4000-8000-000000001003', $1::uuid, 'G-910003', 'female', 'kid', 'alive', 'K2', 'healthy', '` + qaPartyID + `', '` + qaShed1ID + `', '` + qaFarmID + `', '` + qaParkID + `', '` + qaShed1ID + `', DATE '2026-05-17', 'birth', DATE '2026-05-17'),
  ('91000000-0000-4000-8000-000000001004', $1::uuid, 'G-910004', 'female', 'kid', 'alive', 'K2', 'healthy', '` + qaPartyID + `', '` + qaShed2ID + `', '` + qaFarmID + `', '` + qaParkID + `', '` + qaShed2ID + `', DATE '2026-05-18', 'birth', DATE '2026-05-18'),
  ('91000000-0000-4000-8000-000000001005', $1::uuid, 'G-910005', 'male', 'kid', 'alive', 'K2', 'healthy', '` + qaPartyID + `', '` + qaShed2ID + `', '` + qaFarmID + `', '` + qaParkID + `', '` + qaShed2ID + `', DATE '2026-05-19', 'birth', DATE '2026-05-19')
ON CONFLICT (goat_id) DO UPDATE
SET lifecycle_status = 'alive', management_stage = 'K2', health_status = 'healthy', current_location_id = EXCLUDED.current_location_id, farm_id = EXCLUDED.farm_id, park_id = EXCLUDED.park_id, shed_id = EXCLUDED.shed_id, updated_at = now();

INSERT INTO goat_identifiers (identifier_id, tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, source_system, source_record_id, normalizer_version, confidence)
VALUES
  ('91000000-0000-4000-8000-000000002001', $1::uuid, '91000000-0000-4000-8000-000000001001', 'animal_identifier_1', '901007000504418', '901007000504418', 'tenant:' || $1::text, true, 'active', now(), 'seed-vaccination-per-goat-qa', 'qa-1', 'seed-v1', 1.0),
  ('91000000-0000-4000-8000-000000002002', $1::uuid, '91000000-0000-4000-8000-000000001002', 'animal_identifier_1', '901007000504332', '901007000504332', 'tenant:' || $1::text, true, 'active', now(), 'seed-vaccination-per-goat-qa', 'qa-2', 'seed-v1', 1.0),
  ('91000000-0000-4000-8000-000000002003', $1::uuid, '91000000-0000-4000-8000-000000001003', 'animal_identifier_1', '901007000504407', '901007000504407', 'tenant:' || $1::text, true, 'active', now(), 'seed-vaccination-per-goat-qa', 'qa-3', 'seed-v1', 1.0),
  ('91000000-0000-4000-8000-000000002004', $1::uuid, '91000000-0000-4000-8000-000000001004', 'animal_identifier_1', '901007000504419', '901007000504419', 'tenant:' || $1::text, true, 'active', now(), 'seed-vaccination-per-goat-qa', 'qa-4', 'seed-v1', 1.0),
  ('91000000-0000-4000-8000-000000002005', $1::uuid, '91000000-0000-4000-8000-000000001005', 'animal_identifier_1', '901007000504392', '901007000504392', 'tenant:' || $1::text, true, 'active', now(), 'seed-vaccination-per-goat-qa', 'qa-5', 'seed-v1', 1.0),
  ('91000000-0000-4000-8000-000000002102', $1::uuid, '91000000-0000-4000-8000-000000001002', 'animal_identifier_2', '901007000503785', '901007000503785', 'tenant:' || $1::text, false, 'active', now(), 'seed-vaccination-per-goat-qa', 'qa-2b', 'seed-v1', 1.0),
  ('91000000-0000-4000-8000-000000002103', $1::uuid, '91000000-0000-4000-8000-000000001003', 'animal_identifier_2', '901007000504377', '901007000504377', 'tenant:' || $1::text, false, 'active', now(), 'seed-vaccination-per-goat-qa', 'qa-3b', 'seed-v1', 1.0)
ON CONFLICT (tenant_id, normalized_value) DO UPDATE
SET goat_id = EXCLUDED.goat_id, identifier_value = EXCLUDED.identifier_value, is_primary_for_goat = EXCLUDED.is_primary_for_goat, status = 'active', updated_at = now();

INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, description, state, assigned_to, scope_type, scope_id, priority, due_at, context, created_by)
VALUES ('` + qaTaskID + `', $1::uuid, '` + qaSOPID + `', '` + qaSOPVersionID + `', 'vaccination', 'Per-animal vaccination proof QA', 'Five animals, goat-level video proof', 'assigned', '` + localActorID + `', 'park', '` + qaParkID + `', 'normal', now() + interval '6 hours', '{"seed":"vaccination-per-goat-qa","proof_mode":"per_goat_video","obligation_batch_id":"` + qaBatchID + `"}'::jsonb, '` + localActorID + `')
ON CONFLICT (task_id) DO UPDATE
SET sop_version_id = EXCLUDED.sop_version_id, state = 'assigned', assigned_to = EXCLUDED.assigned_to, context = EXCLUDED.context, updated_at = now();

INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, session, planned_date, window_start, window_end, status, estimated_targets, planned_quantity, reserved_quantity, used_quantity, quantity_unit, primary_inventory_lot_id, sop_task_id, conducted_by, context)
VALUES ('` + qaBatchID + `', $1::uuid, '` + qaVersionID + `', 'park', '` + qaParkID + `', 'qa-per-goat-proof', (now() AT TIME ZONE 'Asia/Kolkata')::date, now() - interval '1 hour', now() + interval '3 days', 'in_progress', 5, 5, 0, 0, 'dose', '` + qaStockID + `', '` + qaTaskID + `', '` + qaOperatorID + `', '{"seed":"vaccination-per-goat-qa"}'::jsonb)
ON CONFLICT (batch_id) DO UPDATE
SET planned_date = EXCLUDED.planned_date, window_start = EXCLUDED.window_start, window_end = EXCLUDED.window_end, status = 'in_progress', estimated_targets = 5, sop_task_id = EXCLUDED.sop_task_id, conducted_by = EXCLUDED.conducted_by, updated_at = now();

INSERT INTO vaccination_drive_assignments (assignment_id, tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count, capacity_status, warnings, vaccine_rule_ids, total_doses)
VALUES
  ('` + qaAssign1ID + `', $1::uuid, '` + qaBatchID + `', (now() AT TIME ZONE 'Asia/Kolkata')::date, '` + qaOperatorID + `', '` + qaParkID + `', '` + qaShed1ID + `', 'Shed 1', 'whole', 3, 'within_cap', '[]'::jsonb, ARRAY['` + qaRuleID + `']::uuid[], 3),
  ('` + qaAssign2ID + `', $1::uuid, '` + qaBatchID + `', (now() AT TIME ZONE 'Asia/Kolkata')::date, '` + qaOperatorID + `', '` + qaParkID + `', '` + qaShed2ID + `', 'Shed 2', 'whole', 2, 'within_cap', '[]'::jsonb, ARRAY['` + qaRuleID + `']::uuid[], 2)
ON CONFLICT (tenant_id, batch_id, planned_date, park_id, COALESCE(shed_id, '00000000-0000-0000-0000-000000000000'::uuid), physical_shed, partition_label, COALESCE(operator_id, '00000000-0000-0000-0000-000000000000'::uuid)) DO UPDATE
SET animal_count = EXCLUDED.animal_count, vaccine_rule_ids = EXCLUDED.vaccine_rule_ids, total_doses = EXCLUDED.total_doses, updated_at = now();

WITH obligations AS (
  INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, window_start, window_end, status, sop_task_id, idempotency_key, sequence)
  VALUES
    ('91000000-0000-4000-8000-000000003001', $1::uuid, '` + qaVersionID + `', '` + qaRuleID + `', '` + qaBatchID + `', 'goat', '91000000-0000-4000-8000-000000001001', 'shed', '` + qaShed1ID + `', now(), now() - interval '1 hour', now() + interval '3 days', 'due', '` + qaTaskID + `', 'qa-vax-per-goat-1', 1),
    ('91000000-0000-4000-8000-000000003002', $1::uuid, '` + qaVersionID + `', '` + qaRuleID + `', '` + qaBatchID + `', 'goat', '91000000-0000-4000-8000-000000001002', 'shed', '` + qaShed1ID + `', now(), now() - interval '1 hour', now() + interval '3 days', 'due', '` + qaTaskID + `', 'qa-vax-per-goat-2', 1),
    ('91000000-0000-4000-8000-000000003003', $1::uuid, '` + qaVersionID + `', '` + qaRuleID + `', '` + qaBatchID + `', 'goat', '91000000-0000-4000-8000-000000001003', 'shed', '` + qaShed1ID + `', now(), now() - interval '1 hour', now() + interval '3 days', 'due', '` + qaTaskID + `', 'qa-vax-per-goat-3', 1),
    ('91000000-0000-4000-8000-000000003004', $1::uuid, '` + qaVersionID + `', '` + qaRuleID + `', '` + qaBatchID + `', 'goat', '91000000-0000-4000-8000-000000001004', 'shed', '` + qaShed2ID + `', now(), now() - interval '1 hour', now() + interval '3 days', 'due', '` + qaTaskID + `', 'qa-vax-per-goat-4', 1),
    ('91000000-0000-4000-8000-000000003005', $1::uuid, '` + qaVersionID + `', '` + qaRuleID + `', '` + qaBatchID + `', 'goat', '91000000-0000-4000-8000-000000001005', 'shed', '` + qaShed2ID + `', now(), now() - interval '1 hour', now() + interval '3 days', 'due', '` + qaTaskID + `', 'qa-vax-per-goat-5', 1)
  ON CONFLICT (obligation_id) DO UPDATE
  SET batch_id = EXCLUDED.batch_id, status = 'due', sop_task_id = EXCLUDED.sop_task_id, due_at = now(), updated_at = now()
  RETURNING obligation_id, target_id
)
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
SELECT $1::uuid,
       CASE WHEN g.shed_id = '` + qaShed1ID + `' THEN '` + qaAssign1ID + `'::uuid ELSE '` + qaAssign2ID + `'::uuid END,
       o.obligation_id,
       o.target_id
FROM obligations o
JOIN goats g ON g.tenant_id = $1::uuid AND g.goat_id = o.target_id
ON CONFLICT (tenant_id, obligation_id) DO UPDATE SET assignment_id = EXCLUDED.assignment_id;
`
