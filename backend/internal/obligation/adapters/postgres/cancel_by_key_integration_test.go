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

// TestCancelByIdempotencyKeyRepairsBatchQuantities is the PEND-2 basic case: a 3-obligation batch
// where every obligation is cell-ledger tracked (1 cell each). Canceling ONE by idempotency key
// must repair estimated_targets, planned_quantity, and remove that obligation's cell_ledger key --
// the R50-022 fix this test guards (previously untested).
func TestCancelByIdempotencyKeyRepairsBatchQuantities(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.cancel_key_basic", Name: "CancelKeyBasic", Category: "vaccination", Status: "draft",
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
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	const goatA = "10000000-0000-4000-8000-0000000000e1"
	const goatB = "10000000-0000-4000-8000-0000000000e2"
	const goatC = "10000000-0000-4000-8000-0000000000e3"
	seedReserveGoats(t, ctx, pool, cbePark, cbePark, goatA, goatB, goatC)

	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	insertObl := func(goat, key string) string {
		t.Helper()
		id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goat, ScopeType: "park", ScopeID: cbePark,
			DueAt: due, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("insert %s: applied=%v err=%v", key, applied, err)
		}
		return id
	}
	obA := insertObl(goatA, "cancel-key-basic-a")
	obB := insertObl(goatB, "cancel-key-basic-b")
	obC := insertObl(goatC, "cancel-key-basic-c")

	batchID, err := repo.CreateBatch(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
		Status: "planned", EstimatedTargets: 3, PlannedQuantity: "3", QuantityUnit: "dose",
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if attached, err := repo.AttachObligationsToBatch(ctx, tenantID, batchID, []string{obA, obB, obC}); err != nil || attached != 3 {
		t.Fatalf("attach: attached=%d err=%v", attached, err)
	}
	// Fully-tracked cell ledger: 1 cell per obligation, no legacy component.
	if _, err := pool.Exec(ctx, `
UPDATE obligation_batches
SET context = jsonb_build_object('cell_ledger', jsonb_build_object($3::text, 1, $4::text, 1, $5::text, 1))
WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID, obA, obB, obC); err != nil {
		t.Fatalf("seed cell ledger: %v", err)
	}

	obligationID, changed, err := repo.CancelOpenObligationByIdempotencyKey(ctx, tenantID, "cancel-key-basic-a", "superseded", due)
	if err != nil {
		t.Fatalf("cancel by key: %v", err)
	}
	if !changed || obligationID != obA {
		t.Fatalf("cancel by key: changed=%v id=%q, want changed=true id=%q", changed, obligationID, obA)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "canceled" {
		t.Fatalf("obA status = %s, want canceled", got)
	}

	targets := batchTargets(t, ctx, pool, versionID)
	if got := targets[batchID]; got != 2 {
		t.Fatalf("estimated_targets = %d, want 2", got)
	}
	if got := scanTextObligation(t, ctx, pool,
		`SELECT planned_quantity::text FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != "2" {
		t.Fatalf("planned_quantity = %q, want 2", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2 AND context #>> ARRAY['cell_ledger', $3::text] IS NOT NULL`,
		tenantID, batchID, obA); got != 0 {
		t.Fatalf("canceled obligation's cell_ledger key = %d rows, want removed (0)", got)
	}
}

// TestCancelByIdempotencyKeyMixedBatch exercises a batch with TWO ledger-tracked obligations
// (different cell counts) plus a legacy (untracked) component. Canceling a TRACKED obligation must
// shrink planned_quantity by exactly its own tracked cells, leaving the other tracked entry and the
// legacy component intact.
func TestCancelByIdempotencyKeyMixedBatch(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.cancel_key_mixed", Name: "CancelKeyMixed", Category: "vaccination", Status: "draft",
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
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	const goatTracked1 = "10000000-0000-4000-8000-0000000000f1"
	const goatTracked2 = "10000000-0000-4000-8000-0000000000f2"
	const goatLegacy = "10000000-0000-4000-8000-0000000000f3"
	seedReserveGoats(t, ctx, pool, cbePark, cbePark, goatTracked1, goatTracked2, goatLegacy)

	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	insertObl := func(goat, key string) string {
		t.Helper()
		id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goat, ScopeType: "park", ScopeID: cbePark,
			DueAt: due, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("insert %s: applied=%v err=%v", key, applied, err)
		}
		return id
	}
	obTracked1 := insertObl(goatTracked1, "cancel-key-mixed-tracked-1")
	obTracked2 := insertObl(goatTracked2, "cancel-key-mixed-tracked-2")
	obLegacy := insertObl(goatLegacy, "cancel-key-mixed-legacy")

	batchID, err := repo.CreateBatch(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
		Status: "planned", EstimatedTargets: 3, PlannedQuantity: "9", QuantityUnit: "dose",
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if attached, err := repo.AttachObligationsToBatch(ctx, tenantID, batchID, []string{obTracked1, obTracked2, obLegacy}); err != nil || attached != 3 {
		t.Fatalf("attach: attached=%d err=%v", attached, err)
	}
	// obTracked1=2 cells, obTracked2=3 cells; obLegacy has NO ledger entry, covered by
	// legacy_cell_total=4. planned_quantity = 4 (legacy) + 2 + 3 = 9, matching the batch above.
	if _, err := pool.Exec(ctx, `
UPDATE obligation_batches
SET context = jsonb_build_object(
  'legacy_cell_total', 4,
  'cell_ledger', jsonb_build_object($3::text, 2, $4::text, 3)
)
WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID, obTracked1, obTracked2); err != nil {
		t.Fatalf("seed mixed cell ledger: %v", err)
	}

	obligationID, changed, err := repo.CancelOpenObligationByIdempotencyKey(ctx, tenantID, "cancel-key-mixed-tracked-2", "superseded", due)
	if err != nil {
		t.Fatalf("cancel by key: %v", err)
	}
	if !changed || obligationID != obTracked2 {
		t.Fatalf("cancel by key: changed=%v id=%q, want changed=true id=%q", changed, obligationID, obTracked2)
	}

	// planned_quantity shrinks by exactly the canceled obligation's tracked cells (3): 9 -> 6.
	if got := scanTextObligation(t, ctx, pool,
		`SELECT planned_quantity::text FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != "6" {
		t.Fatalf("planned_quantity = %q, want 6 (9 - 3 tracked cells)", got)
	}
	targets := batchTargets(t, ctx, pool, versionID)
	if got := targets[batchID]; got != 2 {
		t.Fatalf("estimated_targets = %d, want 2", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2 AND context #>> ARRAY['cell_ledger', $3::text] IS NOT NULL`,
		tenantID, batchID, obTracked2); got != 0 {
		t.Fatalf("canceled tracked obligation's cell_ledger key = %d rows, want removed (0)", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2 AND context #>> ARRAY['cell_ledger', $3::text] = '2'`,
		tenantID, batchID, obTracked1); got != 1 {
		t.Fatalf("surviving tracked obligation's cell_ledger entry disturbed: got %d rows", got)
	}
}

// TestCancelByIdempotencyKeyPartialLegacyCellLedgerConservative is R50-023: a batch whose
// context.cell_ledger covers only SOME obligations, with the rest covered by a flat
// legacy_cell_total fallback. Canceling a LEGACY (untracked) obligation must NOT shrink
// planned_quantity -- the recompute has no per-obligation cell count to attribute to it, so it
// conservatively leaves the flat legacy floor untouched rather than guessing a share to remove.
// Canceling the TRACKED obligation afterwards DOES shrink planned_quantity by its exact cells,
// proving the conservative behavior is specific to the untracked/legacy portion.
func TestCancelByIdempotencyKeyPartialLegacyCellLedgerConservative(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.cancel_key_r50023", Name: "CancelKeyR50023", Category: "vaccination", Status: "draft",
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
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	const goatLegacy1 = "10000000-0000-4000-8000-0000000000f4"
	const goatLegacy2 = "10000000-0000-4000-8000-0000000000f5"
	const goatTracked = "10000000-0000-4000-8000-0000000000f6"
	seedReserveGoats(t, ctx, pool, cbePark, cbePark, goatLegacy1, goatLegacy2, goatTracked)

	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	insertObl := func(goat, key string) string {
		t.Helper()
		id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goat, ScopeType: "park", ScopeID: cbePark,
			DueAt: due, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("insert %s: applied=%v err=%v", key, applied, err)
		}
		return id
	}
	obLegacy1 := insertObl(goatLegacy1, "cancel-key-r50023-legacy-1")
	obLegacy2 := insertObl(goatLegacy2, "cancel-key-r50023-legacy-2")
	obTracked := insertObl(goatTracked, "cancel-key-r50023-tracked")

	batchID, err := repo.CreateBatch(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
		Status: "planned", EstimatedTargets: 3, PlannedQuantity: "7", QuantityUnit: "dose",
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if attached, err := repo.AttachObligationsToBatch(ctx, tenantID, batchID, []string{obLegacy1, obLegacy2, obTracked}); err != nil || attached != 3 {
		t.Fatalf("attach: attached=%d err=%v", attached, err)
	}
	// Legacy pair (obLegacy1, obLegacy2) has NO ledger entry -- covered by legacy_cell_total=5.
	// obTracked has an explicit ledger entry of 2 cells. planned_quantity = 5 + 2 = 7.
	if _, err := pool.Exec(ctx, `
UPDATE obligation_batches
SET context = jsonb_build_object(
  'legacy_cell_total', 5,
  'cell_ledger', jsonb_build_object($3::text, 2)
)
WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID, obTracked); err != nil {
		t.Fatalf("seed partial legacy cell ledger: %v", err)
	}

	// Cancel a LEGACY (untracked) obligation: planned_quantity must NOT shrink (conservative --
	// there is no per-obligation cell count to remove for it).
	obligationID, changed, err := repo.CancelOpenObligationByIdempotencyKey(ctx, tenantID, "cancel-key-r50023-legacy-1", "superseded", due)
	if err != nil {
		t.Fatalf("cancel legacy by key: %v", err)
	}
	if !changed || obligationID != obLegacy1 {
		t.Fatalf("cancel legacy by key: changed=%v id=%q, want changed=true id=%q", changed, obligationID, obLegacy1)
	}
	if got := scanTextObligation(t, ctx, pool,
		`SELECT planned_quantity::text FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != "7" {
		t.Fatalf("planned_quantity after legacy cancel = %q, want 7 (conservative: unchanged)", got)
	}

	// Now cancel the TRACKED obligation: planned_quantity DOES shrink by its exact 2 cells.
	obligationID2, changed2, err := repo.CancelOpenObligationByIdempotencyKey(ctx, tenantID, "cancel-key-r50023-tracked", "superseded", due)
	if err != nil {
		t.Fatalf("cancel tracked by key: %v", err)
	}
	if !changed2 || obligationID2 != obTracked {
		t.Fatalf("cancel tracked by key: changed=%v id=%q, want changed=true id=%q", changed2, obligationID2, obTracked)
	}
	if got := scanTextObligation(t, ctx, pool,
		`SELECT planned_quantity::text FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != "5" {
		t.Fatalf("planned_quantity after tracked cancel = %q, want 5 (7 - 2 tracked cells)", got)
	}
}

// TestCancelByIdempotencyKeyReleasesReservedStock is the PEND-2 fix under test: the single-key
// cancel path must compute cancel_repair.release_qty exactly like the bulk CancelOpenForGoatAt
// path, using real reserve movements produced by the sweeper + inventory reserver (style of
// TestCancelReleaseQtyAcrossReservedBatches).
func TestCancelByIdempotencyKeyReleasesReservedStock(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const item = "d0000000-0000-4000-8000-0000000000d1"
	const lot = "d0000000-0000-4000-8000-0000000000d2"
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-KEY-REL', 'Key release test', 'vaccine', 'dose')`, item, tenantID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 100, 0, 'dose', DATE '2026-12-31')`, lot, tenantID, item, cbePark); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.cancel_key_release", Name: "CancelKeyRelease", Category: "vaccination", Status: "draft",
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
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	const goatG = "10000000-0000-4000-8000-0000000000d3"
	const goatH = "10000000-0000-4000-8000-0000000000d4"
	seedReserveGoats(t, ctx, pool, cbePark, cbePark, goatG, goatH)

	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	insertObl := func(goat, key string) {
		t.Helper()
		if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goat, ScopeType: "park", ScopeID: cbePark,
			DueAt: due, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("insert %s: applied=%v err=%v", key, applied, err)
		}
	}
	insertObl(goatG, "cancel-key-release-g")
	insertObl(goatH, "cancel-key-release-h")

	// Real reserve movements via the sweeper + inventory reserver: 2 doses per goat -> batch
	// reserves 4.
	reserver := invapp.NewService(invpg.NewRepository(pool, 5*time.Second))
	sweep := oblapp.NewSweeperService(repo, nil, reserver)
	cfg := oblapp.SweepConfig{VaccineItemID: item, DosesPerGoat: 2}
	if _, err := sweep.SweepVersion(ctx, tenantID, versionID, cfg, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	targets := batchTargets(t, ctx, pool, versionID)
	var batchID string
	for id, n := range targets {
		if n == 2 {
			batchID = id
		}
	}
	if batchID == "" {
		t.Fatalf("expected a batch with estimated_targets 2, got %v", targets)
	}
	if got := reservedQty(t, ctx, pool, batchID); got != 4 {
		t.Fatalf("reserved qty = %v, want 4", got)
	}

	// Cancel goat G by idempotency key (single-obligation cancel path -- the PEND-2 fix).
	obligationID, changed, err := repo.CancelOpenObligationByIdempotencyKey(ctx, tenantID, "cancel-key-release-g", "ineligible_after_exit", due)
	if err != nil {
		t.Fatalf("cancel by key: %v", err)
	}
	if !changed || obligationID == "" {
		t.Fatalf("cancel by key: changed=%v id=%q, want changed=true", changed, obligationID)
	}

	// reserved=4, pending=0, estimated_targets(pre-cancel)=2, count=1 -> LEAST(4, 1*4/2)=2.
	if got := cancelReleaseQty(t, ctx, pool, batchID); got != 2 {
		t.Fatalf("cancel_repair.release_qty = %v, want 2", got)
	}
	after := batchTargets(t, ctx, pool, versionID)
	if after[batchID] != 1 {
		t.Fatalf("estimated_targets after cancel = %d, want 1", after[batchID])
	}

	// Idempotent replay: same key, no-op -- release_qty must not double-count.
	_, changedReplay, err := repo.CancelOpenObligationByIdempotencyKey(ctx, tenantID, "cancel-key-release-g", "ineligible_after_exit", due)
	if err != nil {
		t.Fatalf("replay cancel by key: %v", err)
	}
	if changedReplay {
		t.Fatalf("replay cancel should be a no-op")
	}
	if got := cancelReleaseQty(t, ctx, pool, batchID); got != 2 {
		t.Fatalf("cancel_repair.release_qty after replay = %v, want 2 (unchanged)", got)
	}
}
