package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	invpg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	invapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// TestCancelReleaseQtyAcrossReservedBatches exercises OBL-001: the stock-release math in the
// bulk cancellation repair (`cancel_repair.release_qty`) across MULTIPLE reserved batches with
// DIFFERENT reserved quantities and pre-existing repair context. Reserve movements are produced
// by the real sweeper+inventory reserver; the cancellation runs through the production repo.
//
// release_qty (per batch, when reserved > 0) =
//
//	existing_cancel_release + LEAST( reserved-pending, count*(reserved-pending)/estimated_targets )
//
// where pending = sum of prior defer/shift/cancel/missed repair release_qty that are still
// 'stock_reconcile_required', and count = obligations of this cancel that belonged to the batch.
func TestCancelReleaseQtyAcrossReservedBatches(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const item = "d0000000-0000-4000-8000-0000000000c1"
	const lot = "d0000000-0000-4000-8000-0000000000c2"
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-REL', 'Release test', 'vaccine', 'dose')`, item, tenantID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 100, 0, 'dose', DATE '2026-12-31')`, lot, tenantID, item, cbePark); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.release", Name: "Release", Category: "vaccination", Status: "draft",
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
	ruleA, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("ruleA: %v", err)
	}
	ruleB, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "booster", Sequence: 2,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("ruleB: %v", err)
	}

	const goatG = "10000000-0000-4000-8000-0000000000a1"
	const goatH = "10000000-0000-4000-8000-0000000000a2"
	const goatI = "10000000-0000-4000-8000-0000000000a3"
	seedReserveGoats(t, ctx, pool, cbePark, cbePark, goatG, goatH, goatI)

	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	insertObl := func(rule, goat, key string) {
		t.Helper()
		if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: rule,
			TargetType: "goat", TargetID: goat, ScopeType: "park", ScopeID: cbePark,
			DueAt: due, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("insert %s: applied=%v err=%v", key, applied, err)
		}
	}
	// Batch A (ruleA) covers G,H,I -> estimated_targets 3. Batch B (ruleB) covers G,H -> estimated_targets 2.
	insertObl(ruleA, goatG, "rel-a-g")
	insertObl(ruleA, goatH, "rel-a-h")
	insertObl(ruleA, goatI, "rel-a-i")
	insertObl(ruleB, goatG, "rel-b-g")
	insertObl(ruleB, goatH, "rel-b-h")

	// Real reserve movements: 2 doses per goat -> batch A reserves 6, batch B reserves 4.
	reserver := invapp.NewService(invpg.NewRepository(pool, 5*time.Second))
	sweep := oblapp.NewSweeperService(repo, nil, reserver)
	cfg := oblapp.SweepConfig{VaccineItemID: item, DosesPerGoat: 2}
	if _, err := sweep.SweepVersion(ctx, tenantID, versionID, cfg, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// Identify batches by estimated_targets (A=3, B=2) and capture their ids.
	targets := batchTargets(t, ctx, pool, versionID)
	var batchA, batchB string
	for id, n := range targets {
		switch n {
		case 3:
			batchA = id
		case 2:
			batchB = id
		}
	}
	if batchA == "" || batchB == "" {
		t.Fatalf("expected batches with estimated_targets 3 and 2, got %v", targets)
	}

	// Sanity: reserved.qty per batch = doses actually reserved (proves the setup, not the code under test).
	if got := reservedQty(t, ctx, pool, batchA); got != 6 {
		t.Fatalf("batch A reserved = %v, want 6", got)
	}
	if got := reservedQty(t, ctx, pool, batchB); got != 4 {
		t.Fatalf("batch B reserved = %v, want 4", got)
	}

	// Pre-existing repair on batch B: a prior defer reconcile still holding 2 doses. This must
	// reduce the release base (pending_release), so the cancel releases fewer doses.
	if _, err := pool.Exec(ctx, `
UPDATE obligation_batches
SET context = context || jsonb_build_object('defer_repair',
      jsonb_build_object('state','stock_reconcile_required','release_qty',2))
WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchB); err != nil {
		t.Fatalf("inject prior defer repair: %v", err)
	}

	// Cancel goat G: one obligation in each batch -> count=1 per batch.
	n, err := repo.CancelOpenForGoat(ctx, tenantID, goatG, "ineligible_after_exit")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if n != 2 {
		t.Fatalf("cancelled = %d, want 2", n)
	}

	// Batch A: reserved 6, pending 0, est 3, count 1 -> LEAST(6, 6/3)=2.
	if got := cancelReleaseQty(t, ctx, pool, batchA); got != 2 {
		t.Fatalf("batch A cancel_repair.release_qty = %v, want 2", got)
	}
	// Batch B: reserved 4, pending 2 (prior defer), est 2, count 1 -> base=2, LEAST(2, 2/2)=1.
	if got := cancelReleaseQty(t, ctx, pool, batchB); got != 1 {
		t.Fatalf("batch B cancel_repair.release_qty = %v, want 1 (pending defer must reduce it)", got)
	}
	// estimated_targets decremented on both.
	after := batchTargets(t, ctx, pool, versionID)
	if after[batchA] != 2 || after[batchB] != 1 {
		t.Fatalf("estimated_targets after cancel: A=%d(want 2) B=%d(want 1)", after[batchA], after[batchB])
	}
}

func reservedQty(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batchID string) float64 {
	t.Helper()
	var q float64
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(SUM(quantity),0)::float8
FROM inventory_stock_movements
WHERE tenant_id=$1 AND batch_id=$2 AND movement_type='reserve'`, tenantID, batchID).Scan(&q); err != nil {
		t.Fatalf("reserved qty: %v", err)
	}
	return q
}

func cancelReleaseQty(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batchID string) float64 {
	t.Helper()
	var q float64
	if err := pool.QueryRow(ctx, `
SELECT COALESCE((context #>> '{cancel_repair,release_qty}')::float8, 0)
FROM obligation_batches
WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID).Scan(&q); err != nil {
		t.Fatalf("cancel release qty: %v", err)
	}
	return q
}
