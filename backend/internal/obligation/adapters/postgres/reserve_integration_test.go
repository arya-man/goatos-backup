package postgres

import (
	"context"
	"testing"
	"time"

	invpg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	invapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

func TestSM4cReservesStockPerBatch(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const item = "d0000000-0000-4000-8000-000000000001"
	const lot = "d0000000-0000-4000-8000-000000000002"
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-R', 'Reserve test', 'vaccine', 'dose')`, item, tenantID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', DATE '2026-09-30')`, lot, tenantID, item, cbePark); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.reserve", Name: "Reserve", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "phc_approval", EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	for i, key := range []string{"r1", "r2"} { // 2 obligations, same scope cbe -> one batch
		if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "park", TargetID: cbePark, ScopeType: "park", ScopeID: cbePark,
			DueAt: time.Date(2026, 8, i+1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("insert %s: %v", key, err)
		}
	}

	reserver := invapp.NewService(invpg.NewRepository(pool, 5*time.Second))
	sweep := oblapp.NewSweeperService(repo, nil, reserver)
	cfg := oblapp.SweepConfig{VaccineItemID: item, DosesPerGoat: 1}
	dueBefore := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)

	res, err := sweep.SweepVersion(ctx, tenantID, versionID, cfg, dueBefore)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if res.Batches != 1 {
		t.Fatalf("batches: want 1, got %d", res.Batches)
	}

	var reserved string
	if err := pool.QueryRow(ctx, `SELECT quantity_reserved::text FROM inventory_stock WHERE stock_id=$1`, lot).Scan(&reserved); err != nil {
		t.Fatalf("read reserved: %v", err)
	}
	if reserved != "2" {
		t.Fatalf("reserved: want 2 (2 obligations x 1 dose), got %q", reserved)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND movement_type='reserve'`, tenantID); got != 1 {
		t.Fatalf("expected 1 reserve movement, got %d", got)
	}

	// Idempotent re-sweep: no new batch, no double reserve.
	if _, err := sweep.SweepVersion(ctx, tenantID, versionID, cfg, dueBefore); err != nil {
		t.Fatalf("re-sweep: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT quantity_reserved::text FROM inventory_stock WHERE stock_id=$1`, lot).Scan(&reserved); err != nil {
		t.Fatalf("read reserved 2: %v", err)
	}
	if reserved != "2" {
		t.Fatalf("re-sweep must not double-reserve, reserved=%q", reserved)
	}
}
