package postgres

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	salespg "github.com/vgoats/goatos/backend/internal/sales/adapters/postgres"
)

// ONE ANIMAL, ONE PRICE, ON BOTH PAGES (maintainer decision 2026-10-02). Farm value and Load wise
// both price an animal off Sales Config through the shared farmvaluation fragments. For a
// fattening animal -- priced at its measured weight on both pages -- the rupees must be identical,
// and a sheep must take the sheep rate on both. A copy of the pricing SQL drifting on one page is
// exactly what this pins (loadwise-stock-valuation-guard blocks the copy; this proves the result).
func TestLoadwiseAndFarmValuePriceOneSheepTheSameMultipleDimensions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	fx := seedLoadwiseFixture(t, ctx, pool)
	cbe := parkIDByCode(t, ctx, pool, "CBE")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	const operator = "11111111-0000-4000-8000-00000000c0de"
	exec(`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now()) ON CONFLICT DO NOTHING`, testTenant, operator, cbe)
	exec(`
INSERT INTO sales_valuation_assumptions (tenant_id, buckets, stages)
VALUES ($1::uuid,
  '[{"bucket":"fattening_goat_female","label":"Fattening · Goat · Female","price_per_kg":450,"display_order":1},
    {"bucket":"fattening_goat_male","label":"Fattening · Goat · Male","price_per_kg":450,"display_order":2},
    {"bucket":"fattening_sheep_female","label":"Fattening · Sheep · Female","price_per_kg":430,"display_order":3},
    {"bucket":"fattening_sheep_male","label":"Fattening · Sheep · Male","price_per_kg":430,"display_order":4}]'::jsonb,
  '[{"stage":"fattening","label":"Fattening","display_order":1,"matches":["F2"]}]'::jsonb)
ON CONFLICT (tenant_id) DO UPDATE SET buckets = EXCLUDED.buckets, stages = EXCLUDED.stages`, testTenant)

	var shed, campaign, bucket, proof, loadX, sheep string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO locations (tenant_id, location_type, name, parent_location_id, status, display_order)
VALUES ($1::uuid, 'shed', 'Parity Pen', $2::uuid, 'active', 910) RETURNING location_id::text`, testTenant, cbe).Scan(&shed))
	must(pool.QueryRow(ctx, `INSERT INTO weighing_campaigns (tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, '2026-09-14', '2026-09-20', '2026-09-14', 'completed', 100, $3::uuid, $3::uuid) RETURNING campaign_id::text`, testTenant, cbe, operator).Scan(&campaign))
	must(pool.QueryRow(ctx, `INSERT INTO weighing_campaign_sheds (campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, park_id, start_business_date)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', 'Parity Pen', 'individual_animal', $4::uuid, $5::uuid, '2026-09-14') RETURNING campaign_shed_id::text`, campaign, testTenant, shed, operator, cbe).Scan(&bucket))
	must(pool.QueryRow(ctx, `INSERT INTO proof_artifacts (tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, uploaded_at)
VALUES ($1::uuid, 'local', 'parity-proof', 'video/mp4', 'completed', 'shed', $2::uuid, 'shed', $2::uuid, 'video', $3::uuid, now()) RETURNING proof_id::text`, testTenant, shed, operator).Scan(&proof))
	must(pool.QueryRow(ctx, `
INSERT INTO procurement_loads (tenant_id, source_party_id, purchase_date, arrived_on, status, idempotency_key, expected_count, animal_cost)
SELECT tenant_id, source_party_id, '2026-09-01', '2026-09-02', status, 'lw-parity', 1, 5000
FROM procurement_loads WHERE tenant_id = $1 AND load_id = $2::uuid RETURNING load_id::text`, testTenant, fx.loadA).Scan(&loadX))
	must(pool.QueryRow(ctx, `
INSERT INTO goats (tenant_id, species, sex, management_stage, lifecycle_status, custodian_party_id, park_id, shed_id)
SELECT tenant_id, 'sheep', 'female', 'F2', 'alive', source_party_id, $3::uuid, $4::uuid
FROM procurement_loads WHERE tenant_id = $1 AND load_id = $2::uuid RETURNING goat_id::text`, testTenant, loadX, cbe, shed).Scan(&sheep))
	exec(`INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, selection_state, current_state, intake_accepted_at)
VALUES ($1, $2::uuid, $3::uuid, 'accepted_herd_intake', 'accepted_herd_intake', '2026-09-02T10:00:00Z')`, testTenant, loadX, sheep)
	exec(`INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', 'parity-sheep', 'PARITY-SHEEP', 'tenant', true, 'active', now(), 'test')`, testTenant, sheep)
	exec(`INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key, accepted_at, submitted_at, verification_status)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'parity-sheep', 30, $4::uuid, $5::uuid, 'parity:1', '2026-09-15T05:00:00Z', '2026-09-15T05:00:00Z', 'verified')`,
		testTenant, campaign, bucket, proof, operator)

	lw, err := NewRepository(pool, 10*time.Second).LoadwiseSales(ctx, testTenant, "", 60)
	must(err)
	var loadValue float64 = -1
	for _, l := range lw.Loads {
		if l.LoadID == loadX && l.AssumedValue != nil {
			loadValue = *l.AssumedValue
		}
	}
	overview, err := salespg.NewRepository(pool, 10*time.Second).GetOverview(ctx, testTenant, "")
	must(err)
	farmValue := -1.0
	for _, b := range overview.FarmValuation.Buckets {
		if b.Bucket == "fattening_sheep_female" {
			farmValue = b.ValueRupees
		}
	}
	// 30 kg x ₹430, the SHEEP rate, on both pages -- never the goat rate's ₹13,500.
	if math.Abs(loadValue-12900) > 0.01 || math.Abs(farmValue-12900) > 0.01 {
		t.Fatalf("one sheep, one price: load wise %v, farm value %v, want 12900 on both", loadValue, farmValue)
	}

	// A config stored BEFORE the species split (pre-000470 keys) still prices both species, at the
	// one figure it carried -- the deploy window, and any database the migration has not reached.
	exec(`UPDATE sales_valuation_assumptions SET buckets =
  '[{"bucket":"fattening_female","label":"Fattening · Female","price_per_kg":400,"display_order":1},
    {"bucket":"fattening_male","label":"Fattening · Male","price_per_kg":400,"display_order":2}]'::jsonb
WHERE tenant_id = $1::uuid`, testTenant)
	lw, err = NewRepository(pool, 10*time.Second).LoadwiseSales(ctx, testTenant, "", 60)
	must(err)
	loadValue = -1
	for _, l := range lw.Loads {
		if l.LoadID == loadX && l.AssumedValue != nil {
			loadValue = *l.AssumedValue
		}
	}
	overview, err = salespg.NewRepository(pool, 10*time.Second).GetOverview(ctx, testTenant, "")
	must(err)
	farmValue, label := -1.0, ""
	for _, b := range overview.FarmValuation.Buckets {
		if b.Bucket == "fattening_sheep_female" {
			farmValue, label = b.ValueRupees, b.Label
		}
	}
	if math.Abs(loadValue-12000) > 0.01 || math.Abs(farmValue-12000) > 0.01 || label != "Fattening · Sheep · Female" {
		t.Fatalf("a pre-split config must still price the sheep: load wise %v, farm value %v (%q), want 12000", loadValue, farmValue, label)
	}
}
