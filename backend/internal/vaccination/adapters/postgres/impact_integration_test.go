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
	shedExpr := "NULL"
	args := []any{id, impTenant, lifecycle, species, impParty, impCbe, stage}
	if shed {
		shedExpr = "$6"
	}
	_, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
			   current_location_id, park_id, shed_id, management_stage)
			 VALUES ($1, $2, $3, $4, $5, 'female', $6, $6, `+shedExpr+`, $7)`, args...)
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

func TestImpactPreviewLiveCounts(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// 3 dose-eligible goats (alive/healthy, stage K1, in shed cbe); 1 clinical hold; 1 wrong-stage.
	// A dead goat with stale sick health must not be resurrected into impact counts.
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000a1", "alive", "K1", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000a2", "alive", "K1", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000a3", "alive", "K1", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000a4", "sick", "K1", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000b1", "alive", "K2", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000c1", "dead", "K1", true)
	seedAnimal(t, ctx, pool, "20000000-0000-4000-8000-0000000000d1", "sheep", "alive", "K1", true)
	if _, err := pool.Exec(ctx,
		`UPDATE goats SET health_status='sick' WHERE tenant_id=$1 AND goat_id='20000000-0000-4000-8000-0000000000c1'`,
		impTenant); err != nil {
		t.Fatalf("mark dead goat stale sick: %v", err)
	}

	// Vaccine item + a lot with only 2 available doses at cbe (shortage vs 4 required).
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-ENT', 'Enterotox', 'vaccine', 'dose')`, impItem, impTenant); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 2, 0, 'dose', DATE '2026-07-15')`, impLot, impTenant, impItem, impCbe); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	svc := vaccapp.NewService(NewRepository(pool, 5*time.Second))
	park := impCbe
	item := impItem
	loc := impCbe
	out, err := svc.ImpactPreview(ctx, domain.ImpactRequest{
		Filter:        domain.ImpactFilter{TenantID: impTenant, Species: "goat", Stage: "K1", ParkID: &park},
		VaccineItemID: &item,
		LocationID:    &loc,
		DosesPerGoat:  1,
		DoseRows:      2,
		HorizonDays:   30,
		AsOf:          time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if out.EligibleGoats != 3 {
		t.Fatalf("eligible: want 3, got %d", out.EligibleGoats)
	}
	if out.CatchupGoats != 0 {
		t.Fatalf("catchup: want 0, got %d", out.CatchupGoats)
	}
	if out.Obligations != 6 { // 3 eligible × 2 dose rows
		t.Fatalf("obligations: want 6, got %d", out.Obligations)
	}
	if out.Batches != 1 { // all eligible share shed cbe
		t.Fatalf("batches: want 1, got %d", out.Batches)
	}
	if out.DosesRequired != 3 {
		t.Fatalf("doses required: want 3, got %d", out.DosesRequired)
	}
	if out.DosesAvailable != "2" {
		t.Fatalf("doses available: want 2, got %q", out.DosesAvailable)
	}
	foundShortage := false
	for _, w := range out.Warnings {
		if strings.Contains(w, "shortage") {
			foundShortage = true
		}
	}
	if !foundShortage {
		t.Fatalf("expected stock shortage warning, got %v", out.Warnings)
	}

	allOut, err := svc.ImpactPreview(ctx, domain.ImpactRequest{
		Filter:       domain.ImpactFilter{TenantID: impTenant, Species: "all", Stage: "all", Sex: "all", Breed: "all", Health: "any", ParkID: &park},
		DosesPerGoat: 1,
		DoseRows:     2,
		HorizonDays:  30,
		AsOf:         time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("impact all/any wildcard: %v", err)
	}
	if allOut.EligibleGoats != 5 {
		t.Fatalf("all/any wildcard eligible: want 5 dose-eligible live animals in park, got %d", allOut.EligibleGoats)
	}
	if allOut.Obligations != 10 {
		t.Fatalf("all/any wildcard obligations: want 10, got %d", allOut.Obligations)
	}
	if allOut.Batches != 1 {
		t.Fatalf("all/any wildcard batches: want 1, got %d", allOut.Batches)
	}
}

func TestImpactPreviewExcludesClinicalLocationAndWarmupHolds(t *testing.T) {
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
		 END,
		 entry_date = CASE goat_id
		   WHEN '20000000-0000-4000-8000-000000000103' THEN DATE '2026-06-22'
		   ELSE entry_date
		 END
		 WHERE tenant_id = $1
		   AND goat_id IN (
		     '20000000-0000-4000-8000-000000000102',
		     '20000000-0000-4000-8000-000000000103'
		   )`, impTenant); err != nil {
		t.Fatalf("mark held goats: %v", err)
	}

	svc := vaccapp.NewService(NewRepository(pool, 5*time.Second))
	park := impCbe
	out, err := svc.ImpactPreview(ctx, domain.ImpactRequest{
		Filter: domain.ImpactFilter{
			TenantID:                impTenant,
			Species:                 "goat",
			Stage:                   "K1",
			ParkID:                  &park,
			WarmupNoVaccinationDays: 7,
		},
		DosesPerGoat: 1,
		DoseRows:     1,
		AsOf:         time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if out.EligibleGoats != 1 || out.Obligations != 1 || out.DosesRequired != 1 || out.Batches != 1 {
		t.Fatalf("impact=%+v, want only the one healthy/non-held/non-warmup goat counted", out)
	}
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
