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
	cbeParkID       = "00000000-0000-4000-8000-000000003001"
	cptParkID       = "86000000-0000-4000-8000-000000000002"
	cbeShedID       = "86000000-0000-4000-8000-000000000101"
	cptShedID       = "86000000-0000-4000-8000-000000000201"
	cbeCohortID     = "86000000-0000-4000-8000-000000000301"
	cptCohortID     = "86000000-0000-4000-8000-000000000302"
	itemID          = "86000000-0000-4000-8000-000000000401"
	stockID         = "86000000-0000-4000-8000-000000000402"
	protocolID      = "86000000-0000-4000-8000-000000000501"
	versionID       = "86000000-0000-4000-8000-000000000502"
	ruleID          = "86000000-0000-4000-8000-000000000503"
	draftProtocolID = "86000000-0000-4000-8000-000000000511"
	draftVersionID  = "86000000-0000-4000-8000-000000000512"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("seed-calendar-vaccination-dev", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID", defaultTenantID), "tenant id")
	timeout := fs.Duration("timeout", 30*time.Second, "seed timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	pgCfg := platformpg.ConfigFromEnv()
	if err := localtarget.ValidateLocalDatabaseTarget("seed-calendar-vaccination-dev", os.Getenv("GOATOS_ENV"), pgCfg.DatabaseURL, "local", "dev", "test"); err != nil {
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
	fmt.Printf("seeded calendar vaccination fixtures tenant=%s parks=CBE,CPT protocol_version=%s\n", *tenantID, versionID)
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
		if _, err := execer.Exec(ctx, statement, tenantID); err != nil {
			return err
		}
	}
	return nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

const seedSQL = `
UPDATE calendar_snoozes
SET status = 'replaced'
WHERE tenant_id = $1::uuid
  AND status = 'active'
  AND calendar_event_id IN (
    'obligation:86000000-0000-4000-8000-000000001001',
    'batch:86000000-0000-4000-8000-000000001002:rule:` + ruleID + `:shed:` + cbeShedID + `',
    'calendar:86000000-0000-4000-8000-000000001003',
    'calendar:86000000-0000-4000-8000-000000001004',
    'calendar:86000000-0000-4000-8000-000000001005',
    'calendar:86000000-0000-4000-8000-000000001006',
    'calendar:86000000-0000-4000-8000-000000001007',
    'calendar:86000000-0000-4000-8000-000000001008',
    'calendar:86000000-0000-4000-8000-000000001009',
    'calendar:86000000-0000-4000-8000-000000001010',
    'calendar:86000000-0000-4000-8000-000000001011',
    'calendar:86000000-0000-4000-8000-000000001012'
  );

INSERT INTO locations (
  location_id, tenant_id, location_type, location_code, name, parent_location_id,
  country, state_region, timezone, status, updated_at
) VALUES
  ('` + cptParkID + `', $1::uuid, 'park', 'CPT-CAL', 'Chennai Pilot', NULL, 'IN', 'Tamil Nadu', 'Asia/Kolkata', 'active', now()),
  ('` + cbeShedID + `', $1::uuid, 'shed', 'CBE-CAL-S1', 'CBE Calendar Shed 1', '` + cbeParkID + `', 'IN', 'Tamil Nadu', 'Asia/Kolkata', 'active', now()),
  ('` + cptShedID + `', $1::uuid, 'shed', 'CPT-CAL-S1', 'CPT Calendar Shed 1', '` + cptParkID + `', 'IN', 'Tamil Nadu', 'Asia/Kolkata', 'active', now()),
  ('` + cbeCohortID + `', $1::uuid, 'cohort', 'CBE-KID-CAL', 'CBE Kid Cohort', '` + cbeShedID + `', 'IN', 'Tamil Nadu', 'Asia/Kolkata', 'active', now()),
  ('` + cptCohortID + `', $1::uuid, 'cohort', 'CPT-ADULT-CAL', 'CPT Adult Cohort', '` + cptShedID + `', 'IN', 'Tamil Nadu', 'Asia/Kolkata', 'active', now())
ON CONFLICT (location_id) DO UPDATE
SET location_code = EXCLUDED.location_code,
    name = EXCLUDED.name,
    parent_location_id = EXCLUDED.parent_location_id,
    status = 'active',
    updated_at = now();

INSERT INTO location_operational_attributes (
  tenant_id, location_id, usable_for_counts, usable_for_feed, usable_for_vaccination,
  usable_for_sop, is_holding, is_quarantine, is_icu, display_order, notes
) VALUES
  ($1::uuid, '` + cbeParkID + `', true, true, true, true, false, false, false, 10, 'Calendar vaccination seed CBE'),
  ($1::uuid, '` + cptParkID + `', true, true, true, true, false, false, false, 20, 'Calendar vaccination seed CPT'),
  ($1::uuid, '` + cbeShedID + `', true, true, true, true, false, false, false, 30, 'Calendar vaccination seed shed'),
  ($1::uuid, '` + cptShedID + `', true, true, true, true, false, false, false, 40, 'Calendar vaccination seed shed')
ON CONFLICT (location_id) DO UPDATE
SET usable_for_vaccination = true,
    usable_for_sop = true,
    updated_at = now();

INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit, status, context)
VALUES ('` + itemID + `', $1::uuid, 'VAC-CAL-ET-TT', 'ET+TT Vaccine Calendar Seed', 'vaccine', 'dose', 'active',
        '{"seed":"calendar-vaccination-dev","reference":"docs/preventive-care-vaccination/vaccination-rules.md"}'::jsonb)
ON CONFLICT (item_id) DO UPDATE
SET item_code = EXCLUDED.item_code,
    name = EXCLUDED.name,
    status = 'active',
    context = EXCLUDED.context,
    updated_at = now();

INSERT INTO vaccines (tenant_id, item_id, disease, manufacturer, doses_per_vial, withdrawal_days, context)
VALUES ($1::uuid, '` + itemID + `', 'Enterotoxaemia + Tetanus', 'Mesha matrix dev baseline', 10, 0,
        '{"seed":"calendar-vaccination-dev","reference":"docs/preventive-care-vaccination/vaccination-rules.md"}'::jsonb)
ON CONFLICT (tenant_id, item_id) DO UPDATE
SET disease = EXCLUDED.disease,
    manufacturer = EXCLUDED.manufacturer,
    context = EXCLUDED.context,
    updated_at = now();

INSERT INTO inventory_stock (
  stock_id, tenant_id, item_id, location_id, lot_code, expiry_date,
  quantity_in_stock, quantity_reserved, quantity_unit, status
) VALUES (
  '` + stockID + `', $1::uuid, '` + itemID + `', '` + cbeParkID + `', 'CAL-ETTT-001',
  DATE '2027-12-31', 1000, 120, 'dose', 'active'
)
ON CONFLICT (stock_id) DO UPDATE
SET quantity_in_stock = GREATEST(inventory_stock.quantity_in_stock, 1000),
    quantity_reserved = 120,
    status = 'active',
    updated_at = now();

INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
VALUES
  ('` + protocolID + `', $1::uuid, 'vaccination.calendar.matrix', 'Calendar Vaccination Matrix', 'vaccination', 'active'),
  ('` + draftProtocolID + `', $1::uuid, 'vaccination.calendar.draft_matrix', 'Draft Vaccination Matrix', 'vaccination', 'active')
ON CONFLICT (protocol_id) DO UPDATE
SET code = EXCLUDED.code,
    name = EXCLUDED.name,
    status = EXCLUDED.status,
    updated_at = now();

INSERT INTO protocol_versions (
  protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
  version_label, status, effective_from, effective_to, rule_dsl, proof_policy,
  sop_version_id, published_at
) VALUES
  ('` + versionID + `', $1::uuid, '` + protocolID + `', 'park', '` + cbeParkID + `', 1,
   'Calendar active matrix seed', 'published', DATE '2026-01-01', DATE '2028-01-01',
   '{"category":"vaccination","vaccine":{"code":"ET+TT","name":"ET+TT","type":"toxoid"},"eligibility":{"stage":"K2"},"schedule":[{"dose_code":"ET_TT_4W","trigger_type":"birth_age","offset_days":28,"due_window_days":7,"dose_amount":2,"dose_unit":"ml","route_site":"subcutaneous","max_delay_days":7,"course_lapse_policy":"pc_review"}]}'::jsonb,
   '{"required_proofs":["shed","vial_lot","administration"]}'::jsonb,
   'b0000000-0000-4000-8000-000000000002', now()),
  ('` + draftVersionID + `', $1::uuid, '` + draftProtocolID + `', 'park', '` + cbeParkID + `', 1,
   'Draft matrix excluded seed', 'draft', DATE '2026-01-01', DATE '2028-01-01',
   '{"category":"vaccination"}'::jsonb,
   '{}'::jsonb,
   NULL, NULL)
ON CONFLICT (protocol_version_id) DO UPDATE
SET status = EXCLUDED.status,
    rule_dsl = EXCLUDED.rule_dsl,
    proof_policy = EXCLUDED.proof_policy,
    published_at = EXCLUDED.published_at,
    updated_at = now();

INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
  offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json,
  sop_version_id, proof_policy, sort_order
) VALUES (
  '` + ruleID + `', $1::uuid, '` + versionID + `', 'ET_TT_4W', 1, 'birth_age',
  28, 7, 0, 'none', 'immediate',
  '{"stage":"K2","seed":"calendar-vaccination-dev"}'::jsonb,
  'b0000000-0000-4000-8000-000000000002',
  '{"required_proofs":["shed","vial_lot","administration"]}'::jsonb, 10
)
ON CONFLICT (rule_id) DO UPDATE
SET dose_code = EXCLUDED.dose_code,
    eligibility_json = EXCLUDED.eligibility_json,
    proof_policy = EXCLUDED.proof_policy;

WITH rows(event_id, event_type, owner_key, title, subtitle, status, severity, due_at, window_start, window_end,
          park_id, park_code, shed_id, shed_name, cohort_id, cohort_name, target_type, target_count,
          source_target_id, assignee_label, executor_role, verifier_label, reminder_state,
          primary_notification_channel, escalation_state, source_label, links, detail) AS (
  VALUES
  ('obligation:86000000-0000-4000-8000-000000001001','vaccination_dose_due','pc','ET+TT 4-week dose due','CBE Calendar Shed 1','due','warning', now() + interval '2 hours', now(), now() + interval '1 day','` + cbeParkID + `','CBE','` + cbeShedID + `','CBE Calendar Shed 1','` + cbeCohortID + `','CBE Kid Cohort','cohort',48,'86000000-0000-4000-8000-000000001001','PC vaccinator','pc_vaccinator',NULL,'scheduled','local-stub','none','Active vaccination matrix rule','{"vaccination":"/vaccination/operations","workflow":"/vaccination/workflows/obligation:86000000-0000-4000-8000-000000001001","action_center":"/vaccination/action-center"}'::jsonb,'{"summary":{"owner":"PC","target_count":48},"source_and_rule":{"protocol_version_id":"` + versionID + `","rule_id":"` + ruleID + `"},"execution":{"obligation_id":"86000000-0000-4000-8000-000000001001","work_state":"due"},"stock":{"lot":"CAL-ETTT-001","reserved_qty":48,"shortfall":0},"proof":{},"verification":{},"notification_channels":["local-stub","slack"],"notification_policy":{"reminder":"due_minus_1h"},"links":{"history":"/calendar/vaccination/events/obligation:86000000-0000-4000-8000-000000001001/history"}}'::jsonb),
  ('batch:86000000-0000-4000-8000-000000001002:rule:` + ruleID + `:shed:` + cbeShedID + `','vaccination_drive','pc','CBE ET+TT vaccination drive','Single-shed drive','in_progress','info', now() + interval '4 hours', now(), now() + interval '8 hours','` + cbeParkID + `','CBE','` + cbeShedID + `','CBE Calendar Shed 1',NULL,NULL,'shed',120,'86000000-0000-4000-8000-000000001002','PC drive team','pc_vaccinator','PC verifier','not_scheduled','local-stub','none','Active vaccination matrix drive batch','{"vaccination":"/vaccination/operations","drive":"/vaccination/execution/sheds/` + cbeShedID + `"}'::jsonb,'{"summary":{"owner":"PC","target_count":120},"source_and_rule":{"protocol_version_id":"` + versionID + `","rule_id":"` + ruleID + `"},"execution":{"batch_id":"86000000-0000-4000-8000-000000001002","sop_task_id":"86000000-0000-4000-8000-000000002002","work_state":"in_progress"},"stock":{"lot":"CAL-ETTT-001","reserved_qty":120,"shortfall":0},"proof":{"state":"not_submitted"},"verification":{"verifier":"PC verifier"},"notification_channels":["local-stub"],"notification_policy":{"nudge_allowed":true},"links":{}}'::jsonb),
  ('calendar:86000000-0000-4000-8000-000000001003','vaccination_booster_due','pc','ET+TT booster due from accepted completion','CPT Adult Cohort','scheduled','info', now() + interval '3 days', now() + interval '3 days', now() + interval '4 days','` + cptParkID + `','CPT','` + cptShedID + `','CPT Calendar Shed 1','` + cptCohortID + `','CPT Adult Cohort','cohort',35,'86000000-0000-4000-8000-000000001003','PC booster owner','pc_vaccinator',NULL,'scheduled','local-stub','none','Accepted completion SM-7 booster basis','{"workflow":"/vaccination/workflows/calendar:86000000-0000-4000-8000-000000001003"}'::jsonb,'{"summary":{"previous_dose":"ET_TT_4W"},"source_and_rule":{"booster_basis":"accepted administered_at"},"execution":{},"stock":{},"proof":{},"verification":{},"notification_channels":["local-stub"],"notification_policy":{},"links":{}}'::jsonb),
  ('calendar:86000000-0000-4000-8000-000000001004','vaccination_proof_verification','pc','Verify CBE drive proof','Shed/vial/admin proof pending','verification_pending','warning', now() - interval '1 hour', now() - interval '2 hours', now() + interval '2 hours','` + cbeParkID + `','CBE','` + cbeShedID + `','CBE Calendar Shed 1',NULL,NULL,'shed',120,'86000000-0000-4000-8000-000000001004','PC verifier',NULL,'PC verifier','not_scheduled','local-stub','none','SOP proof verification task','{"workflow":"/vaccination/workflows/calendar:86000000-0000-4000-8000-000000001004"}'::jsonb,'{"summary":{"owner":"PC verifier"},"source_and_rule":{},"execution":{"sop_task_id":"86000000-0000-4000-8000-000000002004"},"stock":{},"proof":{"shed_video":"pending","vial_video":"pending","administration_video":"pending"},"verification":{"state":"verification_pending"},"notification_channels":["local-stub"],"notification_policy":{},"links":{}}'::jsonb),
  ('calendar:86000000-0000-4000-8000-000000001005','vaccination_rework_due','pc','Rework rejected vaccination proof','Administration video unclear','rework_due','critical', now() + interval '1 day', now(), now() + interval '2 days','` + cbeParkID + `','CBE','` + cbeShedID + `','CBE Calendar Shed 1',NULL,NULL,'shed',12,'86000000-0000-4000-8000-000000001005','PC drive team','pc_vaccinator','PC verifier','snoozed','local-stub','none','Rejected proof rework task','{"workflow":"/vaccination/workflows/calendar:86000000-0000-4000-8000-000000001005"}'::jsonb,'{"summary":{"rejection_reason":"administration video unclear"},"source_and_rule":{},"execution":{"work_state":"rework_due"},"stock":{},"proof":{"state":"rejected"},"verification":{"state":"rework_requested","rejection_reason":"administration video unclear"},"notification_channels":["local-stub","slack"],"notification_policy":{},"links":{}}'::jsonb),
  ('calendar:86000000-0000-4000-8000-000000001006','vaccine_stock_readiness','inventory','Resolve ET+TT stock shortfall','CPT drive lacks 30 doses','blocked','critical', now() + interval '6 hours', now(), now() + interval '1 day','` + cptParkID + `','CPT','` + cptShedID + `','CPT Calendar Shed 1',NULL,NULL,'shed',80,'86000000-0000-4000-8000-000000001006','Inventory keeper','inventory_keeper',NULL,'not_scheduled','local-stub','pending','Batch stock readiness task','{"vaccination":"/vaccination/operations"}'::jsonb,'{"summary":{"owner":"Inventory / Stock"},"source_and_rule":{},"execution":{"batch_id":"86000000-0000-4000-8000-000000001006"},"stock":{"shortfall":30,"fefo_state":"shortfall","cold_chain_state":"ok"},"proof":{},"verification":{},"notification_channels":["local-stub"],"notification_policy":{},"links":{}}'::jsonb),
  ('calendar:86000000-0000-4000-8000-000000001007','vaccine_cold_chain_check','inventory','Cold-chain temperature check','ET+TT lots before drive','due','warning', now() + interval '30 minutes', now(), now() + interval '2 hours','` + cbeParkID + `','CBE',NULL,NULL,NULL,NULL,'park',1,'86000000-0000-4000-8000-000000001007','Inventory keeper','inventory_keeper',NULL,'not_scheduled','local-stub','none','Cold-chain check task','{"vaccination":"/vaccination/operations"}'::jsonb,'{"summary":{"owner":"Inventory / Stock"},"source_and_rule":{},"execution":{},"stock":{"lot":"CAL-ETTT-001","cold_chain_state":"check_due"},"proof":{},"verification":{},"notification_channels":["local-stub"],"notification_policy":{},"links":{}}'::jsonb),
  ('calendar:86000000-0000-4000-8000-000000001008','vaccine_reorder_expiry_grn','inventory','Review ET+TT reorder and expiry','GRN and FEFO review','scheduled','info', now() + interval '5 days', now() + interval '5 days', now() + interval '6 days','` + cbeParkID + `','CBE',NULL,NULL,NULL,NULL,'park',1,'86000000-0000-4000-8000-000000001008','Inventory keeper','inventory_keeper',NULL,'not_scheduled','email','none','Inventory readiness review','{"vaccination":"/vaccination/operations"}'::jsonb,'{"summary":{"owner":"Inventory / Stock"},"source_and_rule":{},"execution":{},"stock":{"expiry_date":"2027-12-31","quantity":1000},"proof":{},"verification":{},"notification_channels":["local-stub","email"],"notification_policy":{},"links":{}}'::jsonb),
  ('calendar:86000000-0000-4000-8000-000000001009','pc_stock_anti_misuse','pc','Investigate vaccine variance','Expected vs actual dose variance','overdue','critical', now() - interval '1 day', now() - interval '2 days', now() - interval '12 hours','` + cbeParkID + `','CBE',NULL,NULL,NULL,NULL,'park',1,'86000000-0000-4000-8000-000000001009','PC Director','pc_director',NULL,'queued','slack','pending','PC stock discrepancy follow-up','{"audit":"/operations/audit"}'::jsonb,'{"summary":{"variance":18},"source_and_rule":{},"execution":{},"stock":{"expected_use":120,"actual_use":138},"proof":{},"verification":{},"notification_channels":["local-stub","slack"],"notification_policy":{"escalates":"PC Director"},"links":{}}'::jsonb),
  ('calendar:86000000-0000-4000-8000-000000001010','vaccination_config_activation_review','admin_data_ops','Review ET+TT matrix activation','Protocol activation review due','due','warning', now() + interval '12 hours', now(), now() + interval '1 day','` + cbeParkID + `','CBE',NULL,NULL,NULL,NULL,'protocol_version',1,'` + versionID + `','Admin Data Ops reviewer','admin_data_ops_reviewer',NULL,'not_scheduled','local-stub','none','Config activation review task','{"protocol":"/protocols/versions/` + versionID + `"}'::jsonb,'{"summary":{"owner":"Admin / Data Ops"},"source_and_rule":{"protocol_version_id":"` + versionID + `","review_state":"activation_due"},"execution":{},"stock":{},"proof":{},"verification":{},"notification_channels":["local-stub"],"notification_policy":{},"links":{}}'::jsonb),
  ('calendar:86000000-0000-4000-8000-000000001011','vaccination_defer_review','pc','Review deferred vaccination','Quarantine cohort needs PC decision','deferred','warning', now() + interval '2 days', now() + interval '2 days', now() + interval '3 days','` + cptParkID + `','CPT','` + cptShedID + `','CPT Calendar Shed 1','` + cptCohortID + `','CPT Adult Cohort','cohort',22,'86000000-0000-4000-8000-000000001011','PC reviewer','pc_reviewer',NULL,'not_scheduled','local-stub','none','Dated defer review task','{"workflow":"/vaccination/workflows/calendar:86000000-0000-4000-8000-000000001011"}'::jsonb,'{"summary":{"defer_reason":"quarantine"},"source_and_rule":{},"execution":{"work_state":"deferred"},"stock":{},"proof":{},"verification":{},"notification_channels":["local-stub"],"notification_policy":{},"links":{}}'::jsonb),
  ('calendar:86000000-0000-4000-8000-000000001012','vaccination_evidence_review','pc','Review holding-farm vaccination evidence','Supplier ET+TT proof requires PC trust decision','proof_pending','warning', now() + interval '8 hours', now(), now() + interval '1 day','` + cbeParkID + `','CBE','` + cbeShedID + `','CBE Calendar Shed 1',NULL,NULL,'goat',1,'86000000-0000-4000-8000-000000001012','PC reviewer','pc_reviewer',NULL,'not_scheduled','local-stub','none','Procurement evidence review task','{"workflow":"/vaccination/workflows/calendar:86000000-0000-4000-8000-000000001012"}'::jsonb,'{"summary":{"evidence_source":"holding_farm"},"source_and_rule":{},"execution":{"review_state":"imported"},"stock":{},"proof":{"state":"pending"},"verification":{"action":"accept_or_reject"},"notification_channels":["local-stub"],"notification_policy":{},"links":{}}'::jsonb)
)
INSERT INTO calendar_event_projections (
  tenant_id, event_id, slice_key, event_type, owner_key, title, subtitle, status, severity,
  due_at, window_start, window_end, timezone, timezone_source, park_id, park_code, shed_id, shed_name,
  cohort_id, cohort_name, target_type, target_count, protocol_id, protocol_version_id, rule_id,
  vaccine_name, dose_code, source_backed, source_label, source_target_type, source_target_id,
  assignee_label, executor_role, verifier_label, reminder_state, primary_notification_channel,
  escalation_state, system, cross_cutting, links, detail, updated_at
)
SELECT
  $1::uuid, event_id, 'vaccination', event_type, owner_key, title, subtitle, status, severity,
  due_at, window_start, window_end, 'Asia/Kolkata', 'india_only', park_id::uuid, park_code,
  NULLIF(shed_id, '')::uuid, shed_name, NULLIF(cohort_id, '')::uuid, cohort_name, target_type,
  target_count, '` + protocolID + `'::uuid, '` + versionID + `'::uuid, '` + ruleID + `'::uuid,
  'ET+TT Vaccine', 'ET_TT_4W', true, source_label, target_type,
  source_target_id::uuid, assignee_label, executor_role, verifier_label, reminder_state,
  primary_notification_channel, escalation_state, false, false, links, detail, now()
FROM rows
ON CONFLICT (tenant_id, event_id) DO UPDATE
SET event_type = EXCLUDED.event_type,
    owner_key = EXCLUDED.owner_key,
    title = EXCLUDED.title,
    subtitle = EXCLUDED.subtitle,
    status = EXCLUDED.status,
    severity = EXCLUDED.severity,
    due_at = EXCLUDED.due_at,
    window_start = EXCLUDED.window_start,
    window_end = EXCLUDED.window_end,
    park_id = EXCLUDED.park_id,
    park_code = EXCLUDED.park_code,
    shed_id = EXCLUDED.shed_id,
    shed_name = EXCLUDED.shed_name,
    cohort_id = EXCLUDED.cohort_id,
    cohort_name = EXCLUDED.cohort_name,
    target_type = EXCLUDED.target_type,
    target_count = EXCLUDED.target_count,
    source_backed = true,
    source_label = EXCLUDED.source_label,
    assignee_label = EXCLUDED.assignee_label,
    executor_role = EXCLUDED.executor_role,
    verifier_label = EXCLUDED.verifier_label,
    reminder_state = EXCLUDED.reminder_state,
    primary_notification_channel = EXCLUDED.primary_notification_channel,
    escalation_state = EXCLUDED.escalation_state,
    links = EXCLUDED.links,
    detail = EXCLUDED.detail,
    updated_at = now();

INSERT INTO calendar_event_projections (
  tenant_id, event_id, slice_key, event_type, owner_key, title, subtitle, status, severity,
  due_at, timezone, timezone_source, park_id, park_code, target_type, target_count,
  source_backed, source_label, source_target_type, source_target_id, assignee_label,
  executor_role, reminder_state, primary_notification_channel, escalation_state,
  system, cross_cutting, links, detail, updated_at
) VALUES
  ($1::uuid, 'calendar:86000000-0000-4000-8000-000000009001', 'vaccination', 'vaccination_dose_due', 'pc',
   'Excluded system reminder ping', 'Negative fixture', 'due', 'info', now(), 'Asia/Kolkata', 'india_only',
   '` + cbeParkID + `', 'CBE', 'system_job', 0, true, 'negative system fixture',
   'calendar_event', '86000000-0000-4000-8000-000000009001', 'System', 'system', 'queued',
   'local-stub', 'none', true, false, '{}'::jsonb, '{"negative":"system rows are excluded"}'::jsonb, now())
ON CONFLICT (tenant_id, event_id) DO UPDATE
SET system = true,
    updated_at = now();

INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, target_id, notification_type, channel,
  title, body, status, idempotency_key, request_fingerprint, context
) VALUES (
  $1::uuid, 'calendar:86000000-0000-4000-8000-000000001009', 'park', '` + cbeParkID + `',
  'reminder', 'slack', 'Seed reminder: vaccine variance', 'Seeded reminder for history.',
  'queued', $1::text || ':calendar.seed.reminder:variance', 'seed-reminder',
  '{"calendar_event_id":"calendar:86000000-0000-4000-8000-000000001009"}'::jsonb
)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING;
`
