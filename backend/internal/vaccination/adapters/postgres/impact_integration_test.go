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
	impTenant = "00000000-0000-4000-8000-000000000001"
	impParty  = "00000000-0000-4000-8000-000000001001"
	impCbe    = "00000000-0000-4000-8000-000000003001" // park/location
	impItem   = "c0000000-0000-4000-8000-000000000001"
	impLot    = "c0000000-0000-4000-8000-000000000002"
)

func seedGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id, lifecycle, stage string, shed bool) {
	t.Helper()
	shedExpr := "NULL"
	args := []any{id, impTenant, lifecycle, impParty, impCbe, stage}
	if shed {
		shedExpr = "$5"
	}
	_, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, identity_state, custodian_party_id,
		   current_location_id, park_id, shed_id, management_stage)
		 VALUES ($1, $2, $3, 'clean', $4, $5, $5, `+shedExpr+`, $6)`, args...)
	if err != nil {
		t.Fatalf("seed goat %s: %v", id, err)
	}
}

func TestImpactPreviewLiveCounts(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// 4 eligible (3 alive + 1 defer-state sick goat, stage K1, in shed cbe); 1 wrong-stage; 1 dead.
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000a1", "alive", "K1", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000a2", "alive", "K1", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000a3", "alive", "K1", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000a4", "sick", "K1", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000b1", "alive", "K2", true)
	seedGoat(t, ctx, pool, "20000000-0000-4000-8000-0000000000c1", "dead", "K1", true)

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
		Filter:        domain.ImpactFilter{TenantID: impTenant, Stage: "K1", ParkID: &park},
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
	if out.EligibleGoats != 4 {
		t.Fatalf("eligible: want 4, got %d", out.EligibleGoats)
	}
	if out.CatchupGoats != 0 {
		t.Fatalf("catchup: want 0, got %d", out.CatchupGoats)
	}
	if out.Obligations != 8 { // 4 eligible × 2 dose rows
		t.Fatalf("obligations: want 8, got %d", out.Obligations)
	}
	if out.Batches != 1 { // all eligible share shed cbe
		t.Fatalf("batches: want 1, got %d", out.Batches)
	}
	if out.DosesRequired != 4 {
		t.Fatalf("doses required: want 4, got %d", out.DosesRequired)
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
}
