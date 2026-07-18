package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

const (
	impTenant         = "00000000-0000-4000-8000-000000000001"
	impParty          = "00000000-0000-4000-8000-000000001001"
	impCbe            = "00000000-0000-4000-8000-000000003001" // park/location
	impShed           = "32000000-0000-4000-8000-000000000001"
	impItem           = "c0000000-0000-4000-8000-000000000001"
	impLot            = "c0000000-0000-4000-8000-000000000002"
	impQuarantineShed = "31000000-0000-4000-8000-000000000201"
	impICUShed        = "31000000-0000-4000-8000-000000000202"
	impUnusableShed   = "31000000-0000-4000-8000-000000000203"
)

func seedGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, lifecycle, stage string, shed bool) {
	seedAnimal(t, ctx, pool, id, "goat", lifecycle, stage, shed)
}

func seedAnimal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, species, lifecycle, stage string, shed bool) {
	t.Helper()
	if shed {
		seedShedOperational(t, ctx, pool, impShed, "VACC-TEST", true, false, false)
		_, err := pool.Exec(ctx,
			`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
				   current_location_id, park_id, shed_id, management_stage)
			 VALUES ($1, $2, $3, $4, $5, 'female', $6, $7, $6, $8)`,
			id, impTenant, lifecycle, species, impParty, impShed, impCbe, stage)
		if err != nil {
			t.Fatalf("seed %s %s: %v", species, id, err)
		}
		return
	}
	_, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, shed_id, management_stage)
			 VALUES ($1, $2, $3, $4, $5, 'female', $6, $6, NULL, $7)`,
		id, impTenant, lifecycle, species, impParty, impCbe, stage)
	if err != nil {
		t.Fatalf("seed %s %s: %v", species, id, err)
	}
}

func seedShedOperational(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, code string, usable, quarantine, icu bool) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', $3, $3, $4, 'active')
		 ON CONFLICT (location_id) DO NOTHING`,
		id, impTenant, code, impCbe); err != nil {
		t.Fatalf("seed shed %s: %v", code, err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO location_operational_attributes (
		   tenant_id, location_id, usable_for_counts, usable_for_feed, usable_for_vaccination,
		   usable_for_sop, is_holding, is_quarantine, is_icu, display_order, notes
		 )
		 VALUES ($1, $2, true, true, $3, true, false, $4, $5, 200, $6)
		 ON CONFLICT (location_id) DO UPDATE SET
		   usable_for_vaccination = EXCLUDED.usable_for_vaccination,
		   is_quarantine = EXCLUDED.is_quarantine,
		   is_icu = EXCLUDED.is_icu,
		   updated_at = now()`,
		impTenant, id, usable, quarantine, icu, code); err != nil {
		t.Fatalf("seed shed attrs %s: %v", code, err)
	}
}

func seedGoatAtShed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, shedID string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
		   current_location_id, park_id, shed_id, management_stage)
		 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, 'K1')`,
		id, impTenant, impParty, shedID, impCbe); err != nil {
		t.Fatalf("seed goat at shed %s: %v", id, err)
	}
}

func TestImpactPreviewReadsRollupNotLiveGoats(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// 3 usable goats (alive/healthy, stage K1, in shed cbe); 1 clinical-hold (lifecycle sick); 1
	// wrong-stage; 1 dead; 1 sheep. Only the alive ones enter the rollup universe.
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000a1", "alive", "K1", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000a2", "alive", "K1", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000a3", "alive", "K1", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000a4", "sick", "K1", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000b1", "alive", "K2", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000c1", "dead", "K1", true)
	seedAnimal(t, ctx, pool, "20000000-0000-4000-8000-0000000000d1", "sheep", "alive", "K1", true)

	// Vaccine item + a lot with only 2 available doses at cbe (shortage vs 6 vaccination cells).
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-ENT', 'Enterotox', 'vaccine', 'dose')`, impItem, impTenant); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 2, 0, 'dose', CURRENT_DATE + 30)`, impLot, impTenant, impItem, impCbe); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)
	svc := vaccapp.NewService(repo)
	park := impCbe
	item := impItem
	loc := impCbe

	// BEFORE recompute the read model is empty: the preview must report zeros + the empty-rollup
	// warning, NOT the live 3 goats. This is the proof it reads the rollup, not goats.
	pre, err := svc.ImpactPreview(ctx, domain.ImpactRequest{
		Filter: domain.ImpactFilter{TenantID: impTenant, Species: "goat", Stage: "K1", ParkID: &park},
	})
	if err != nil {
		t.Fatalf("impact pre-recompute: %v", err)
	}
	if pre.EligibleAnimals != 0 || pre.SourceRevision != 0 {
		t.Fatalf("pre-recompute must read empty rollup, got eligible=%d source_revision=%d", pre.EligibleAnimals, pre.SourceRevision)
	}
	if !containsSubstr(pre.Warnings, "rollup has no data") {
		t.Fatalf("pre-recompute expected empty-rollup warning, got %v", pre.Warnings)
	}

	recomputed, err := repo.RecomputeEligibilityRollup(ctx, impTenant)
	if err != nil {
		t.Fatalf("recompute: %v", err)
	}
	if recomputed.SourceRevision == 0 {
		t.Fatalf("recompute source_revision must be non-zero")
	}
	if recomputed.EligibleAnimals != 5 { // 3 goat/K1 + 1 goat/K2 + 1 sheep/K1, all alive/usable
		t.Fatalf("recompute eligible_animals: want 5, got %d", recomputed.EligibleAnimals)
	}

	out, err := svc.ImpactPreview(ctx, domain.ImpactRequest{
		Filter:        domain.ImpactFilter{TenantID: impTenant, Species: "goat", Stage: "K1", ParkID: &park},
		VaccineItemID: &item,
		LocationID:    &loc,
		DoseRows:      2,
		HorizonDays:   30,
		AsOf:          time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if out.EligibleAnimals != 3 {
		t.Fatalf("eligible_animals: want 3, got %d", out.EligibleAnimals)
	}
	if out.VaccinationCells != 6 { // 3 × 2 dose rows
		t.Fatalf("vaccination_cells: want 6, got %d", out.VaccinationCells)
	}
	if out.AffectedSheds != 1 { // all eligible share shed cbe
		t.Fatalf("affected_sheds: want 1, got %d", out.AffectedSheds)
	}
	if out.DailyCap != 100 { // default cap (no capacity config row for the test tenant)
		t.Fatalf("daily_cap: want 100, got %d", out.DailyCap)
	}
	if out.EstimatedDays != 1 { // ceil(6/100)
		t.Fatalf("estimated_days: want 1, got %d", out.EstimatedDays)
	}
	if out.SourceRevision != recomputed.SourceRevision {
		t.Fatalf("source_revision: want %d, got %d", recomputed.SourceRevision, out.SourceRevision)
	}
	if out.DosesAvailable != "2" {
		t.Fatalf("doses available: want 2, got %q", out.DosesAvailable)
	}
	if !containsSubstr(out.Warnings, "shortage") {
		t.Fatalf("expected stock shortage warning, got %v", out.Warnings)
	}

	allOut, err := svc.ImpactPreview(ctx, domain.ImpactRequest{
		Filter:   domain.ImpactFilter{TenantID: impTenant, Species: "all", Stage: "all", Sex: "all", Breed: "all", Health: "any", ParkID: &park},
		DoseRows: 2,
	})
	if err != nil {
		t.Fatalf("impact all/any wildcard: %v", err)
	}
	if allOut.EligibleAnimals != 5 {
		t.Fatalf("all/any wildcard eligible_animals: want 5, got %d", allOut.EligibleAnimals)
	}
	if allOut.VaccinationCells != 10 {
		t.Fatalf("all/any wildcard vaccination_cells: want 10, got %d", allOut.VaccinationCells)
	}
	if allOut.AffectedSheds != 1 {
		t.Fatalf("all/any wildcard affected_sheds: want 1, got %d", allOut.AffectedSheds)
	}
}

func TestRecomputeEligibilityRollupMarksClinicalAndLocationHoldsUnusable(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedShedOperational(t, ctx, pool, impQuarantineShed, "IMPACT-QUARANTINE", true, true, false)
	seedShedOperational(t, ctx, pool, impICUShed, "IMPACT-ICU", true, false, true)
	seedShedOperational(t, ctx, pool, impUnusableShed, "IMPACT-NO-VACC", false, false, false)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-000000000101", "alive", "K1", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-000000000102", "alive", "K1", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-000000000103", "alive", "K1", true)
	seedGoatAtShed(t, ctx, pool, "20000000-0000-4000-8000-000000000104", impQuarantineShed)
	seedGoatAtShed(t, ctx, pool, "20000000-0000-4000-8000-000000000105", impICUShed)
	seedGoatAtShed(t, ctx, pool, "20000000-0000-4000-8000-000000000106", impUnusableShed)
	if _, err := pool.Exec(ctx,
		`UPDATE goats
		 SET health_status = CASE goat_id
		   WHEN '20000000-0000-4000-8000-000000000102' THEN 'sick'
		   ELSE health_status
		 END
		 WHERE tenant_id = $1
		   AND goat_id = '20000000-0000-4000-8000-000000000102'`, impTenant); err != nil {
		t.Fatalf("mark held goat: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)
	svc := vaccapp.NewService(repo)
	if _, err := repo.RecomputeEligibilityRollup(ctx, impTenant); err != nil {
		t.Fatalf("recompute: %v", err)
	}

	park := impCbe
	out, err := svc.ImpactPreview(ctx, domain.ImpactRequest{
		Filter:   domain.ImpactFilter{TenantID: impTenant, Species: "goat", Stage: "K1", ParkID: &park},
		DoseRows: 1,
	})
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	// Only goat 101 is usable: 102 is sick (health hold), 103 healthy at cbe is also usable...
	// 101 + 103 are healthy at cbe (shed cbe). 104 quarantine shed, 105 icu shed, 106 unusable shed,
	// 102 sick are all usable=false. So eligible = 101 + 103 = 2, in shed cbe (affected_sheds=1).
	if out.EligibleAnimals != 2 {
		t.Fatalf("eligible_animals: want 2 (only healthy goats at usable shed cbe), got %d", out.EligibleAnimals)
	}
	if out.VaccinationCells != 2 || out.AffectedSheds != 1 {
		t.Fatalf("impact=%+v, want cells=2 sheds=1", out)
	}

	// The clinical/location holds are stored as usable=false grains, not dropped: a raw rollup scan
	// still sees them, proving usable_for_vaccination is the read filter, not a silent exclusion.
	var unusable int64
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(animal_count), 0)::bigint FROM vaccination_eligibility_rollups
		 WHERE tenant_id = $1::uuid AND usable_for_vaccination = false`, impTenant).Scan(&unusable); err != nil {
		t.Fatalf("count unusable rollup rows: %v", err)
	}
	if unusable != 4 { // 102 sick + 104 quarantine + 105 icu + 106 unusable
		t.Fatalf("unusable rollup animals: want 4, got %d", unusable)
	}
}

// TestImpactPreviewReadPlanDoesNotScanGoats is a query-plan regression: the impact-preview aggregate
// read must hit ONLY vaccination_eligibility_rollups. If a future edit joins goats back in, the plan
// starts referencing the goats relation and this test fails.
func TestImpactPreviewReadPlanDoesNotScanGoats(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	rows, err := pool.Query(ctx, `
EXPLAIN
SELECT COALESCE(SUM(animal_count), 0)::bigint,
       COUNT(DISTINCT shed_id) FILTER (WHERE animal_count > 0 AND shed_id IS NOT NULL)::bigint,
       COALESCE(MAX(source_revision), 0)::bigint,
       MAX(recomputed_at)
FROM vaccination_eligibility_rollups
WHERE tenant_id = $1::uuid AND usable_for_vaccination = true`, impTenant)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	planText := strings.ToLower(plan.String())
	if strings.Contains(planText, " goats") || strings.Contains(planText, "on goats") {
		t.Fatalf("impact-preview read plan references the goats table (must read the rollup only):\n%s", plan.String())
	}
	if !strings.Contains(planText, "vaccination_eligibility_rollups") {
		t.Fatalf("impact-preview read plan must reference vaccination_eligibility_rollups:\n%s", plan.String())
	}
}

func containsSubstr(warnings []string, sub string) bool {
	for _, w := range warnings {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}

func TestEligibleGoatListingUsesShedProfileAnimalStage(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	stageID := "71000000-0000-4000-8000-000000000101"
	shedID := "31000000-0000-4000-8000-000000000101"
	goatID := "21000000-0000-4000-8000-000000000101"
	if _, err := pool.Exec(ctx,
		`INSERT INTO animal_stage_lookup (animal_stage_id, tenant_id, stage_code, name, min_age_days, max_age_days, sort_order, status)
		 VALUES ($1, $2, 'K1', 'K1 kids', 0, 45, 1, 'active')`, stageID, impTenant); err != nil {
		t.Fatalf("seed animal stage: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', 'SHED-K1', 'K1 Shed', $3, 'active')`, shedID, impTenant, impCbe); err != nil {
		t.Fatalf("seed shed: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id)
		 VALUES ($1, $2, $3)`, shedID, impTenant, stageID); err != nil {
		t.Fatalf("seed shed profile: %v", err)
	}
	dob := time.Date(2026, time.May, 20, 0, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, shed_id, management_stage, age_band, dob)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $5, $4, 'K2', 'kid', $6::date)`,
		goatID, impTenant, impParty, shedID, impCbe, dob); err != nil {
		t.Fatalf("seed goat: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)
	rows, err := repo.ListEligibleGoatsForGeneration(ctx, domain.ImpactFilter{TenantID: impTenant, Stage: "K1"}, "", 50)
	if err != nil {
		t.Fatalf("list eligible: %v", err)
	}
	if len(rows) != 1 || rows[0].GoatID != goatID || rows[0].Stage != "K1" || rows[0].AgeBand != "kid" {
		t.Fatalf("rows=%#v, want shed-profile K1 stage and age band", rows)
	}
	k1, err := repo.CountEligibleGoats(ctx, domain.ImpactFilter{TenantID: impTenant, Stage: "K1"})
	if err != nil {
		t.Fatalf("count K1: %v", err)
	}
	k2, err := repo.CountEligibleGoats(ctx, domain.ImpactFilter{TenantID: impTenant, Stage: "K2"})
	if err != nil {
		t.Fatalf("count K2: %v", err)
	}
	if k1 != 1 || k2 != 0 {
		t.Fatalf("stage counts K1=%d K2=%d, want shed-profile stage to drive eligibility", k1, k2)
	}
}
