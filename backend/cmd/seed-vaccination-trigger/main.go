package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

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
  '` + localItemID + `', $1::uuid, 'VAC-TRIGGER-PHC', 'Trigger Gate PHC Vaccine', 'vaccine', 'dose', 'active',
  '{"seed":"vaccination-trigger"}'::jsonb
)
ON CONFLICT (tenant_id, item_code) DO UPDATE
SET name = EXCLUDED.name,
    status = 'active',
    updated_at = now();

INSERT INTO vaccines (tenant_id, item_id, disease, manufacturer, doses_per_vial, withdrawal_days, context)
VALUES ($1::uuid, '` + localItemID + `', 'PHC trigger readiness', 'Mesha local seed', 10, 0, '{"seed":"vaccination-trigger"}'::jsonb)
ON CONFLICT (tenant_id, item_id) DO UPDATE
SET disease = EXCLUDED.disease,
    updated_at = now();

INSERT INTO inventory_stock (
  stock_id, tenant_id, item_id, location_id, lot_code, expiry_date,
  quantity_in_stock, quantity_reserved, quantity_unit, status
) VALUES (
  '` + localStockID + `', $1::uuid, '` + localItemID + `', '` + localParkID + `', 'TRIG-LOT-001',
  DATE '2027-12-31', 1000, 0, 'dose', 'active'
)
ON CONFLICT (stock_id) DO UPDATE
SET quantity_in_stock = GREATEST(inventory_stock.quantity_in_stock, 1000),
    status = 'active',
    updated_at = now();

INSERT INTO protocol_definitions (
  protocol_id, tenant_id, code, name, category, status
) VALUES (
  '` + localProtocolID + `', $1::uuid, 'vaccination.trigger_gate_phc', 'Trigger Gate PHC Vaccination', 'vaccination', 'active'
)
ON CONFLICT (tenant_id, code) DO UPDATE
SET name = EXCLUDED.name,
    status = 'active',
    updated_at = now();

INSERT INTO protocol_versions (
  protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
  version_label, status, effective_from, effective_to, rule_dsl, proof_policy,
  sop_version_id, published_at
) VALUES (
  '` + localVersionID + `', $1::uuid, '` + localProtocolID + `', 'park', '` + localParkID + `', 1,
  'Local trigger gate', 'published', DATE '2026-01-01', DATE '2028-01-01',
  '{"eligibility":{"stage":"all","sex":"all","breed":"all","defer_states":["sick","quarantine","icu"]}}'::jsonb,
  '{"proof_required":true,"seed":"vaccination-trigger"}'::jsonb,
  '` + localSOPVersionID + `', now()
)
ON CONFLICT (protocol_version_id) DO UPDATE
SET status = 'published',
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
  '` + localRuleID + `', $1::uuid, '` + localVersionID + `', 'PHC-INTAKE-0', 1, 'post_arrival',
  0, 14, 0, 'none', 'immediate', '{}'::jsonb,
  '` + localSOPVersionID + `', '{"proof_required":true}'::jsonb, 10
)
ON CONFLICT (tenant_id, protocol_version_id, dose_code) DO UPDATE
SET trigger_type = EXCLUDED.trigger_type,
    offset_days = EXCLUDED.offset_days,
    due_window_days = EXCLUDED.due_window_days,
    catch_up = EXCLUDED.catch_up,
    sop_version_id = EXCLUDED.sop_version_id,
    proof_policy = EXCLUDED.proof_policy;
`
