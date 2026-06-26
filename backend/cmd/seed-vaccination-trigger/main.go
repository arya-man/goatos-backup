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
	localSOPVersionID = "b0000000-0000-4000-8000-000000000002"
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
	fmt.Printf("seeded vaccination trigger fixtures tenant=%s farm=%s park=%s shed=%s vaccine_item=%s protocol_version=%s\n",
		*tenantID, localFarmID, localParkID, localShedID, localItemID, localVersionID)
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

	INSERT INTO inventory_items (
	  item_id, tenant_id, item_code, name, category, base_unit, status, context
	) VALUES (
	  '` + localItemID + `', $1::uuid, 'VAC-ET-PHC', 'Enterotoxaemia Vaccine', 'vaccine', 'dose', 'active',
	  '{"seed":"vaccination-source-derived-dev-baseline","source_ref":"context/source-findings/phc-vaccination-roster-stage-proposal.md"}'::jsonb
	)
	ON CONFLICT (item_id) DO UPDATE
	SET item_code = EXCLUDED.item_code,
	    name = EXCLUDED.name,
	    status = 'active',
	    context = EXCLUDED.context,
	    updated_at = now();

	INSERT INTO vaccines (tenant_id, item_id, disease, manufacturer, doses_per_vial, withdrawal_days, context)
	VALUES ($1::uuid, '` + localItemID + `', 'Enterotoxaemia', 'Mesha source-derived dev baseline', 10, 0, '{"seed":"vaccination-source-derived-dev-baseline","source_ref":"docs/phc-vaccination/PRD.md:60"}'::jsonb)
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
	  'Source-derived dev baseline', 'published', DATE '2026-01-01', DATE '2028-01-01',
	  '{"eligibility":{"stage":"K1","sex":"all","breed":"all","defer_states":["sick","quarantine","icu"]},"schedule":[{"dose_code":"ET-PRIMARY-1","trigger_type":"birth_age","offset_days":21,"due_window_days":7,"min_gap_days":14,"repeat":"none","catch_up":"immediate","sop_label":"Vaccination SOP","proof_policy":{"required_proofs":["shed","vial_lot","administration"]}}],"source":{"source_system":"phc","source_ref":"docs/phc-vaccination/PRD.md:60; context/source-findings/phc-vaccination-roster-stage-proposal.md","imported_at":"2026-06-26T00:00:00Z","reviewed_by":"source-findings","review_status":"approved","approved_by":"source-derived-dev-baseline","approved_at":"2026-06-26T00:00:00Z"}}'::jsonb,
	  '{"required_proofs":["shed","vial_lot","administration"],"seed":"vaccination-source-derived-dev-baseline"}'::jsonb,
	  '` + localSOPVersionID + `', now()
	)
ON CONFLICT (protocol_version_id) DO UPDATE
SET status = 'published',
    version_label = EXCLUDED.version_label,
    effective_from = EXCLUDED.effective_from,
    effective_to = EXCLUDED.effective_to,
    rule_dsl = EXCLUDED.rule_dsl,
    proof_policy = EXCLUDED.proof_policy,
    sop_version_id = EXCLUDED.sop_version_id,
    published_at = now(),
    updated_at = now();

INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
  offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json,
  sop_version_id, proof_policy, sort_order
	) VALUES (
	  '` + localRuleID + `', $1::uuid, '` + localVersionID + `', 'ET-PRIMARY-1', 1, 'birth_age',
	  21, 7, 14, 'none', 'immediate', '{"stage":"K1","source_ref":"docs/phc-vaccination/PRD.md:60"}'::jsonb,
	  '` + localSOPVersionID + `', '{"required_proofs":["shed","vial_lot","administration"]}'::jsonb, 10
	)
	ON CONFLICT (rule_id) DO UPDATE
	SET dose_code = EXCLUDED.dose_code,
	    "sequence" = EXCLUDED."sequence",
	    trigger_type = EXCLUDED.trigger_type,
	    offset_days = EXCLUDED.offset_days,
	    due_window_days = EXCLUDED.due_window_days,
	    min_gap_days = EXCLUDED.min_gap_days,
	    repeat = EXCLUDED.repeat,
	    catch_up = EXCLUDED.catch_up,
	    eligibility_json = EXCLUDED.eligibility_json,
	    sop_version_id = EXCLUDED.sop_version_id,
	    proof_policy = EXCLUDED.proof_policy,
	    sort_order = EXCLUDED.sort_order;
	`
