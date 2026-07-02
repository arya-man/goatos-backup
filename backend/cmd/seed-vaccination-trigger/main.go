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
	defaultTenantID   = "00000000-0000-4000-8000-000000000001"
	localFarmID       = "00000000-0000-4000-8000-000000003100"
	localParkID       = "00000000-0000-4000-8000-000000003001"
	localShedID       = "00000000-0000-4000-8000-000000003101"
	localItemID       = "00000000-0000-4000-8000-00000000b001"
	localStockID      = "00000000-0000-4000-8000-00000000b002"
	localProtocolID   = "00000000-0000-4000-8000-00000000b010"
	localVersionID    = "00000000-0000-4000-8000-00000000b011"
	localRuleID       = "00000000-0000-4000-8000-00000000b012"
	localV1ProtocolID = "00000000-0000-4000-8000-00000000b050"
	localV1VersionID  = "00000000-0000-4000-8000-00000000b051"
	localV1PrimaryID  = "00000000-0000-4000-8000-00000000b052"
	localV1BoosterID  = "00000000-0000-4000-8000-00000000b053"
	localOperatorID   = "00000000-0000-4000-8000-00000000b071"
	localParkHeadID   = "00000000-0000-4000-8000-00000000b072"
	localVerifierID   = "00000000-0000-4000-8000-00000000b073"
	localSOPVersionID = "b0000000-0000-4000-8000-000000000002"
	localStageK0ID    = "00000000-0000-4000-8000-00000000b030"
	localStageK1ID    = "00000000-0000-4000-8000-00000000b031"
	localStageK2ID    = "00000000-0000-4000-8000-00000000b032"
	localStageK3ID    = "00000000-0000-4000-8000-00000000b033"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("seed-vaccination-trigger", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID", defaultTenantID), "tenant id")
	timeout := fs.Duration("timeout", 30*time.Second, "seed timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	pgCfg := platformpg.ConfigFromEnv()
	if err := validateTarget(os.Getenv("GOATOS_ENV"), pgCfg.DatabaseURL); err != nil {
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
	fmt.Printf("seeded vaccination trigger fixtures tenant=%s farm=%s park=%s shed=%s vaccine_item=%s legacy_protocol_version=%s legacy_status=retired v1_protocol_version=%s\n",
		*tenantID, localFarmID, localParkID, localShedID, localItemID, localVersionID, localV1VersionID)
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

func validateTarget(env, databaseURL string) error {
	return localtarget.ValidateLocalDatabaseTarget("seed-vaccination-trigger", env, databaseURL, "local", "dev", "test")
}

const seedSQL = `
INSERT INTO animal_stage_lookup (
  animal_stage_id, tenant_id, stage_code, name, min_age_days, max_age_days,
  sort_order, status
) VALUES
  ('` + localStageK0ID + `', $1::uuid, 'K0', 'Newborn', 0, 1, 0, 'active'),
  ('` + localStageK1ID + `', $1::uuid, 'K1', 'Milk training', 2, 7, 10, 'active'),
  ('` + localStageK2ID + `', $1::uuid, 'K2', 'Milk drinking', 8, 42, 20, 'active'),
  ('` + localStageK3ID + `', $1::uuid, 'K3', 'Weaned kids', 43, NULL, 30, 'active')
ON CONFLICT (tenant_id, stage_code) DO UPDATE
SET name = EXCLUDED.name,
    min_age_days = EXCLUDED.min_age_days,
    max_age_days = EXCLUDED.max_age_days,
    sort_order = EXCLUDED.sort_order,
    status = 'active',
    updated_at = now();

INSERT INTO locations (
  location_id, tenant_id, location_type, location_code, name, parent_location_id,
  country, state_region, timezone, status, updated_at
) VALUES
  ('` + localFarmID + `', $1::uuid, 'farm', 'TRIG_FARM', 'Trigger Gate Farm', NULL,
   'IN', 'Tamil Nadu', 'Asia/Kolkata', 'active', now()),
  ('` + localShedID + `', $1::uuid, 'shed', 'CBE-TRIG-SHED', 'CBE Trigger Shed', '` + localParkID + `',
   'IN', 'Tamil Nadu', 'Asia/Kolkata', 'active', now())
ON CONFLICT (location_id) DO UPDATE
SET name = EXCLUDED.name,
    parent_location_id = EXCLUDED.parent_location_id,
    status = 'active',
    updated_at = now();

INSERT INTO location_operational_attributes (
  tenant_id, location_id, usable_for_counts, usable_for_feed, usable_for_vaccination,
  usable_for_sop, is_holding, is_quarantine, is_icu, display_order, notes
) VALUES
  ($1::uuid, '` + localParkID + `', true, true, true, true, false, false, false, 10, 'Local vaccination trigger seed park'),
  ($1::uuid, '` + localShedID + `', true, true, true, true, false, false, false, 20, 'Local vaccination trigger seed shed')
ON CONFLICT (location_id) DO UPDATE
SET usable_for_vaccination = true,
    usable_for_sop = true,
    is_quarantine = false,
    is_icu = false,
    updated_at = now();

WITH seeded_workforce AS (
  INSERT INTO workforce_members (
    workforce_member_id, tenant_id, display_code, display_name, status,
    primary_role_hint, primary_location_id, metadata, updated_at
  ) VALUES
    ('` + localOperatorID + `', $1::uuid, 'CBE-VACC-OP-01', 'CBE Vaccination Operator', 'active',
     'operator', '` + localParkID + `', '{"seed":"vaccination-trigger","role":"vaccination_operator"}'::jsonb, now()),
    ('` + localParkHeadID + `', $1::uuid, 'CBE-PARK-HEAD-01', 'CBE Park Head', 'active',
     'park_head', '` + localParkID + `', '{"seed":"vaccination-trigger","role":"park_head"}'::jsonb, now()),
    ('` + localVerifierID + `', $1::uuid, 'CBE-VACC-VERIFY-01', 'Video Verification Team', 'active',
     'verifier', NULL, '{"seed":"vaccination-trigger","role":"vaccination_verifier"}'::jsonb, now())
  ON CONFLICT (tenant_id, display_code) DO UPDATE
  SET display_name = EXCLUDED.display_name,
      status = 'active',
      primary_role_hint = EXCLUDED.primary_role_hint,
      primary_location_id = EXCLUDED.primary_location_id,
      metadata = EXCLUDED.metadata,
      updated_at = now(),
      row_version = workforce_members.row_version + 1
  RETURNING workforce_member_id, display_code
),
capability_grants AS (
  SELECT
    sw.workforce_member_id,
    wc.capability_id,
    CASE sw.display_code
      WHEN 'CBE-VACC-OP-01' THEN 'vaccination.execute'
      WHEN 'CBE-VACC-VERIFY-01' THEN 'proof.verify'
      ELSE 'sop.execute'
    END AS capability_code,
    CASE sw.display_code
      WHEN 'CBE-VACC-VERIFY-01' THEN 'tenant'
      ELSE 'park'
    END AS scope_type,
    CASE sw.display_code
      WHEN 'CBE-VACC-VERIFY-01' THEN $1::uuid
      ELSE '` + localParkID + `'::uuid
    END AS scope_id
  FROM seeded_workforce sw
  JOIN workforce_capabilities wc
    ON wc.tenant_id = $1::uuid
   AND wc.capability_code = CASE sw.display_code
      WHEN 'CBE-VACC-OP-01' THEN 'vaccination.execute'
      WHEN 'CBE-VACC-VERIFY-01' THEN 'proof.verify'
      ELSE 'sop.execute'
    END
   AND wc.status = 'active'
)
INSERT INTO workforce_member_capabilities (
  tenant_id, workforce_member_id, capability_id, scope_type, scope_id, status
)
SELECT $1::uuid, workforce_member_id, capability_id, scope_type, scope_id, 'active'
FROM capability_grants
ON CONFLICT DO NOTHING;

	INSERT INTO inventory_items (
	  item_id, tenant_id, item_code, name, category, base_unit, status, context
	) VALUES (
	  '` + localItemID + `', $1::uuid, 'VAC-ET-PHC', 'Enterotoxaemia Vaccine', 'vaccine', 'dose', 'active',
	  '{"seed":"vaccination-source-derived-dev-baseline","source_ref":"context/source-findings/preventive-care-vaccination-roster-stage-proposal.md"}'::jsonb
	)
	ON CONFLICT (item_id) DO UPDATE
	SET item_code = EXCLUDED.item_code,
	    name = EXCLUDED.name,
	    status = 'active',
	    context = EXCLUDED.context,
	    updated_at = now();

	INSERT INTO vaccines (tenant_id, item_id, disease, manufacturer, doses_per_vial, withdrawal_days, context)
	VALUES ($1::uuid, '` + localItemID + `', 'Enterotoxaemia', 'Mesha source-derived dev baseline', 10, 0, '{"seed":"vaccination-source-derived-dev-baseline","source_ref":"docs/preventive-care-vaccination/PRD.md:60"}'::jsonb)
	ON CONFLICT (tenant_id, item_id) DO UPDATE
	SET disease = EXCLUDED.disease,
	    manufacturer = EXCLUDED.manufacturer,
	    context = EXCLUDED.context,
	    updated_at = now();

INSERT INTO inventory_stock (
  stock_id, tenant_id, item_id, location_id, lot_code, expiry_date,
  quantity_in_stock, quantity_reserved, quantity_unit, status
	) VALUES (
	  '` + localStockID + `', $1::uuid, '` + localItemID + `', '` + localParkID + `', 'ET-LOT-001',
	  DATE '2027-12-31', 1000, 0, 'dose', 'active'
	)
	ON CONFLICT (stock_id) DO UPDATE
	SET lot_code = EXCLUDED.lot_code,
	    quantity_in_stock = GREATEST(inventory_stock.quantity_in_stock, 1000),
	    status = 'active',
	    updated_at = now();

INSERT INTO protocol_definitions (
	  protocol_id, tenant_id, code, name, category, status
	) VALUES (
	  '` + localProtocolID + `', $1::uuid, 'vaccination.enterotoxaemia_k1_primary', 'Enterotoxaemia K1 Primary', 'vaccination', 'active'
	)
	ON CONFLICT (protocol_id) DO UPDATE
	SET code = EXCLUDED.code,
	    name = EXCLUDED.name,
	    status = 'active',
	    updated_at = now();

INSERT INTO protocol_versions (
  protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
  version_label, status, effective_from, effective_to, rule_dsl, proof_policy,
  sop_version_id, published_at
	) VALUES (
	  '` + localVersionID + `', $1::uuid, '` + localProtocolID + `', 'park', '` + localParkID + `', 1,
	  'Source-derived dev baseline', 'draft', DATE '2026-01-01', DATE '2028-01-01',
	  '{"vaccine":{"code":"ET","name":"Enterotoxaemia","type":"toxoid","inventory_item_id":"00000000-0000-4000-8000-00000000b001","manufacturer":"Mesha source-derived dev baseline","disease":"Enterotoxaemia","compatibility_group":"ET"},"eligibility":{"stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["sick","quarantine","icu"]},"schedule":[{"dose_code":"ET-PRIMARY-1","trigger_type":"birth_age","offset_days":21,"due_window_days":7,"dose_amount":0.5,"dose_unit":"ml","route_site":"subcutaneous","max_delay_days":7,"course_lapse_policy":"phc_review","min_gap_days":14,"repeat":"none","catch_up":"immediate","sop_label":"Vaccination SOP","proof_policy":{"required_proofs":["shed","vial_lot","administration"]}}],"source":{"source_system":"phc","source_ref":"docs/preventive-care-vaccination/PRD.md:60; context/source-findings/preventive-care-vaccination-roster-stage-proposal.md","imported_at":"2026-06-26T00:00:00Z","reviewed_by":"source-findings","review_status":"approved","approved_by":"source-derived-dev-baseline","approved_at":"2026-06-26T00:00:00Z"}}'::jsonb,
	  '{"required_proofs":["shed","vial_lot","administration"],"seed":"vaccination-source-derived-dev-baseline"}'::jsonb,
	  '` + localSOPVersionID + `', NULL
	)
ON CONFLICT (protocol_version_id) DO UPDATE
SET version_label = EXCLUDED.version_label,
    effective_from = EXCLUDED.effective_from,
    effective_to = EXCLUDED.effective_to,
    rule_dsl = EXCLUDED.rule_dsl,
    proof_policy = EXCLUDED.proof_policy,
    sop_version_id = EXCLUDED.sop_version_id,
    updated_at = now()
WHERE protocol_versions.status = 'draft';

INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
  offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json,
  sop_version_id, proof_policy, sort_order
	)
	SELECT
	  '` + localRuleID + `'::uuid, $1::uuid, '` + localVersionID + `'::uuid, 'ET-PRIMARY-1', 1, 'birth_age',
	  21, 7, 14, 'none', 'immediate', '{"stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["sick","quarantine","icu"],"source_ref":"docs/preventive-care-vaccination/PRD.md:60"}'::jsonb,
	  '` + localSOPVersionID + `'::uuid, '{"required_proofs":["shed","vial_lot","administration"]}'::jsonb, 10
	WHERE EXISTS (
	  SELECT 1
	  FROM protocol_versions pv
	  WHERE pv.tenant_id = $1::uuid
	    AND pv.protocol_version_id = '` + localVersionID + `'
	    AND pv.status = 'draft'
	)
	ON CONFLICT (rule_id) DO NOTHING;

UPDATE protocol_versions
SET status = 'published',
    published_at = COALESCE(published_at, now()),
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND protocol_version_id = '` + localVersionID + `'
  AND status = 'draft';

UPDATE protocol_versions
SET status = 'retired',
    retired_at = COALESCE(retired_at, now()),
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND protocol_version_id = '` + localVersionID + `'
  AND status = 'published';

INSERT INTO protocol_definitions (
  protocol_id, tenant_id, code, name, category, status
) VALUES (
  '` + localV1ProtocolID + `', $1::uuid, 'vaccination.enterotoxaemia_k1_v1_matrix_proof', 'Enterotoxaemia K1 V1 Matrix Proof', 'vaccination', 'active'
)
ON CONFLICT (protocol_id) DO UPDATE
SET code = EXCLUDED.code,
    name = EXCLUDED.name,
    status = 'active',
    updated_at = now();

INSERT INTO protocol_versions (
  protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
  version_label, status, effective_from, effective_to, rule_dsl, proof_policy,
  sop_version_id, published_at
) VALUES (
  '` + localV1VersionID + `', $1::uuid, '` + localV1ProtocolID + `', 'park', '` + localParkID + `', 1,
  'V1 matrix proof baseline', 'draft', DATE '2026-01-01', DATE '2028-01-01',
  '{"vaccine":{"code":"ET","name":"Enterotoxaemia","type":"toxoid","inventory_item_id":"00000000-0000-4000-8000-00000000b001","manufacturer":"Mesha source-derived dev baseline","disease":"Enterotoxaemia","compatibility_group":"ET"},"eligibility":{"stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["sick","quarantine","icu"]},"missed_dose_policy":"phc_approval","schedule":[{"dose_code":"ET-PRIMARY-1","sequence":1,"trigger_type":"birth_age","offset_days":21,"due_window_days":7,"dose_amount":0.5,"dose_unit":"ml","route_site":"subcutaneous","max_delay_days":7,"course_lapse_policy":"phc_review","min_gap_days":0,"repeat":"none","catch_up":"immediate","sop_label":"Vaccination SOP","proof_policy":{"required_proofs":["shed","vial_lot","administration"]}},{"dose_code":"ET-BOOSTER-1","sequence":2,"trigger_type":"after_previous_completion","offset_days":14,"due_window_days":7,"dose_amount":0.5,"dose_unit":"ml","route_site":"subcutaneous","max_delay_days":7,"course_lapse_policy":"phc_review","min_gap_days":14,"repeat":"none","catch_up":"phc_approval","sop_label":"Vaccination SOP","proof_policy":{"required_proofs":["shed","vial_lot","administration"]}}],"source":{"source_system":"phc","source_ref":"docs/preventive-care-vaccination/PRD.md:60; context/source-findings/preventive-care-vaccination-roster-stage-proposal.md","imported_at":"2026-06-30T00:00:00Z","reviewed_by":"source-findings","review_status":"approved","approved_by":"source-derived-dev-baseline","approved_at":"2026-06-30T00:00:00Z"}}'::jsonb,
  '{"required_proofs":["shed","vial_lot","administration"],"seed":"vaccination-v1-matrix-proof-baseline"}'::jsonb,
  '` + localSOPVersionID + `', NULL
)
ON CONFLICT (protocol_version_id) DO UPDATE
SET version_label = EXCLUDED.version_label,
    effective_from = EXCLUDED.effective_from,
    effective_to = EXCLUDED.effective_to,
    rule_dsl = EXCLUDED.rule_dsl,
    proof_policy = EXCLUDED.proof_policy,
    sop_version_id = EXCLUDED.sop_version_id,
    updated_at = now()
WHERE protocol_versions.status = 'draft';

INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
  offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json,
  sop_version_id, proof_policy, sort_order
)
SELECT
  '` + localV1PrimaryID + `'::uuid, $1::uuid, '` + localV1VersionID + `'::uuid, 'ET-PRIMARY-1', 1, 'birth_age',
  21, 7, 0, 'none', 'immediate',
  '{"stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["sick","quarantine","icu"],"source_ref":"docs/preventive-care-vaccination/PRD.md:60"}'::jsonb,
  '` + localSOPVersionID + `'::uuid, '{"required_proofs":["shed","vial_lot","administration"]}'::jsonb, 10
WHERE EXISTS (
  SELECT 1
  FROM protocol_versions pv
  WHERE pv.tenant_id = $1::uuid
    AND pv.protocol_version_id = '` + localV1VersionID + `'
    AND pv.status = 'draft'
)
ON CONFLICT (rule_id) DO NOTHING;

INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
  offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json,
  sop_version_id, proof_policy, sort_order
)
SELECT
  '` + localV1BoosterID + `'::uuid, $1::uuid, '` + localV1VersionID + `'::uuid, 'ET-BOOSTER-1', 2, 'after_previous_completion',
  14, 7, 14, 'none', 'phc_approval',
  '{"stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":["sick","quarantine","icu"],"source_ref":"docs/preventive-care-vaccination/PRD.md:60"}'::jsonb,
  '` + localSOPVersionID + `'::uuid, '{"required_proofs":["shed","vial_lot","administration"]}'::jsonb, 20
WHERE EXISTS (
  SELECT 1
  FROM protocol_versions pv
  WHERE pv.tenant_id = $1::uuid
    AND pv.protocol_version_id = '` + localV1VersionID + `'
    AND pv.status = 'draft'
)
ON CONFLICT (rule_id) DO NOTHING;

UPDATE protocol_versions
SET status = 'published',
    published_at = COALESCE(published_at, now()),
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND protocol_version_id = '` + localV1VersionID + `'
  AND status = 'draft';
	`
