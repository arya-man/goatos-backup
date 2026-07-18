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

// TestSweeperDefersStockAndTaskPastComboAlignIncludingRetryBatch is the R2-04 guard. It exercises
// the REAL kernel-stage composition -- SweepVersionWithSessionNoFinalize for every plan, THEN
// AlignComboDrives, THEN FinalizePlannedBatches per plan -- with a REAL StockReserver (inventory
// service) and a REAL TaskCreator wired, against real Postgres, including a pre-existing "retry"
// batch (simulating one left over from an interrupted prior run, already planned but never
// finalized).
//
// Two vaccines ("fmd" and "hs", an approved combo bundle -> session "combo:FMD+HS") each get one
// due obligation in the SAME park scope:
//   - version A's obligation is already attached to a planned combo batch on Aug 1 (the "retry"
//     batch: no sop_task_id, no stock reservation -- exactly the state a crash between
//     CreateBatchWithObligations and finalization would leave behind).
//   - version B's obligation is fresh and due Aug 5; sweeping it creates a brand-new planned combo
//     batch on Aug 5.
//
// Before the R2-04 fix, the retry batch's stock/task would have been finalized by sweepVersion's
// own top-of-function retry-repair call BEFORE AlignComboDrives ran for this pass, permanently
// excluding it from ListPlannedComboBatchesKeyset's candidate set (sop_task_id IS NULL AND NOT
// context ? 'stock_reservation') -- it would stay stranded on Aug 1 forever, stock reserved against
// the wrong date. This test asserts BOTH batches converge onto the SAME aligned date (Aug 5) and
// BOTH end up with a SOP task and a stock reservation dated to that aligned batch.
func TestSweeperDefersStockAndTaskPastComboAlignIncludingRetryBatch(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const item = "d0000000-0000-4000-8000-0000000000f1"
	const lot = "d0000000-0000-4000-8000-0000000000f2"
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-COMBO-DEFER', 'Combo defer test', 'vaccine', 'dose')`, item, tenantID); err != nil {
		t.Fatalf("seed item: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', DATE '2026-09-30')`, lot, tenantID, item, cbePark); err != nil {
		t.Fatalf("seed stock: %v", err)
	}

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	skeletonVersion := "b0000000-0000-4000-8000-000000000002"

	mkVersion := func(code string) (versionID, ruleID string) {
		protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
			TenantID: tenantID, Code: code, Name: code, Category: "vaccination", Status: "draft",
		})
		if err != nil {
			t.Fatalf("definition %s: %v", code, err)
		}
		versionID, err = proto.CreateVersion(ctx, protodomain.NewVersion{
			TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
			EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
			SopVersionID: &skeletonVersion,
		})
		if err != nil {
			t.Fatalf("version %s: %v", code, err)
		}
		ruleID, err = proto.CreateRule(ctx, protodomain.NewRule{
			TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
			TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 30,
			EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("rule %s: %v", code, err)
		}
		return versionID, ruleID
	}
	versionA, ruleA := mkVersion("vaccination.combo.defer.fmd")
	versionB, ruleB := mkVersion("vaccination.combo.defer.hs")

	const retryGoat = "10000000-0000-4000-8000-0000000000f1"
	const freshGoat = "10000000-0000-4000-8000-0000000000f2"
	seedReserveGoats(t, ctx, pool, cbePark, cbePark, retryGoat, freshGoat)

	dayAug1 := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	dayAug5 := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	const comboSession = "combo:FMD+HS"

	// Retry fixture: version A's obligation is due Aug1 and ALREADY attached to a planned combo
	// batch on Aug1 -- no sop_task_id, no stock reservation -- simulating a crash between
	// CreateBatchWithObligations and finalization in an earlier run.
	retryOblID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionA, RuleID: ruleA,
		TargetType: "goat", TargetID: retryGoat, ScopeType: "park", ScopeID: cbePark,
		DueAt: dayAug1, Status: "scheduled", IdempotencyKey: "combo-defer-retry", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert retry obligation: applied=%v err=%v", applied, err)
	}
	retryBatchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionA, ScopeType: "park", ScopeID: cbePark,
		Session: comboSession, PlannedDate: &dayAug1, Status: "planned",
		EstimatedTargets: 1, PlannedQuantity: "1", QuantityUnit: "dose",
	}, []string{retryOblID})
	if err != nil {
		t.Fatalf("create retry batch: %v", err)
	}
	if attached != 1 {
		t.Fatalf("retry batch attached = %d, want 1", attached)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2 AND sop_task_id IS NULL AND NOT (context ? 'stock_reservation')`, tenantID, retryBatchID); got != 1 {
		t.Fatalf("retry batch fixture invalid: expected unfinalized (no task, no stock), got %d matching rows", got)
	}

	// Fresh obligation: version B, due Aug5, not yet batched at all.
	if _, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionB, RuleID: ruleB,
		TargetType: "goat", TargetID: freshGoat, ScopeType: "park", ScopeID: cbePark,
		DueAt: dayAug5, Status: "scheduled", IdempotencyKey: "combo-defer-fresh", Sequence: 1,
	}); err != nil || !applied {
		t.Fatalf("insert fresh obligation: applied=%v err=%v", applied, err)
	}

	creator := &rawTaskCreator{pool: pool}
	reserver := invapp.NewService(invpg.NewRepository(pool, 5*time.Second))
	sweeper := oblapp.NewSweeperService(repo, creator, reserver)

	cfgFor := func(vaccineCode string) oblapp.SweepConfig {
		return oblapp.SweepConfig{
			SOPVersionID:  skeletonVersion,
			VaccineItemID: item,
			VaccineCode:   vaccineCode,
			DosesPerGoat:  1,
		}
	}
	plans := []struct {
		versionID, vaccineCode string
	}{
		{versionA, "fmd"},
		{versionB, "hs"},
	}

	dueBefore := dayAug5
	session := oblapp.NewSweepSession()
	for _, p := range plans {
		if _, err := sweeper.SweepVersionWithSessionNoFinalize(ctx, tenantID, p.versionID, cfgFor(p.vaccineCode), dueBefore, session); err != nil {
			t.Fatalf("sweep version %s: %v", p.versionID, err)
		}
	}

	// Nothing may be finalized yet: neither the fresh batch NOR the retry batch.
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND session=$2 AND (sop_task_id IS NOT NULL OR context ? 'stock_reservation')`, tenantID, comboSession); got != 0 {
		t.Fatalf("finalized combo batches before alignment = %d, want 0 (finalization must be fully deferred)", got)
	}

	defaultPlanner := domain.DefaultDrivePlannerSettings()
	if _, err := sweeper.AlignComboDrives(ctx, tenantID, defaultPlanner.ComboAlignWindowDays, dueBefore, defaultPlanner.MaxShotsPerAnimalPerDrive, defaultPlanner.MaxGoatsPerDrive, session); err != nil {
		t.Fatalf("align combo drives: %v", err)
	}
	for _, p := range plans {
		if err := sweeper.FinalizePlannedBatches(ctx, tenantID, p.versionID, cfgFor(p.vaccineCode)); err != nil {
			t.Fatalf("finalize batches version %s: %v", p.versionID, err)
		}
	}

	// Both batches must now share the SAME aligned planned_date.
	rows, err := pool.Query(ctx, `SELECT DISTINCT planned_date FROM obligation_batches WHERE tenant_id=$1 AND session=$2`, tenantID, comboSession)
	if err != nil {
		t.Fatalf("query planned dates: %v", err)
	}
	var dates []time.Time
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			t.Fatalf("scan planned date: %v", err)
		}
		dates = append(dates, d)
	}
	rows.Close()
	if len(dates) != 1 {
		t.Fatalf("distinct planned dates across the combo group = %d (%v), want exactly 1 (retry batch must have been aligned, not stranded)", len(dates), dates)
	}
	if !dates[0].Equal(dayAug5) {
		t.Fatalf("aligned planned date = %s, want %s", dates[0].Format("2006-01-02"), dayAug5.Format("2006-01-02"))
	}

	// Both batches must now have a SOP task AND a stock reservation, finalized against the aligned
	// date -- proving finalization ran AFTER alignment, not before.
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND session=$2 AND sop_task_id IS NOT NULL`, tenantID, comboSession); got != 2 {
		t.Fatalf("batches with sop_task_id = %d, want 2 (both retry and fresh batch finalized)", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND session=$2 AND context ? 'stock_reservation'`, tenantID, comboSession); got != 2 {
		t.Fatalf("batches with stock_reservation context = %d, want 2 (both retry and fresh batch reserved)", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND item_id=$2 AND movement_type='reserve'`, tenantID, item); got != 2 {
		t.Fatalf("reserve movements = %d, want 2 (one per batch)", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2 AND planned_date=$3::date`, tenantID, retryBatchID, dayAug5); got != 1 {
		t.Fatalf("retry batch %s not moved onto the aligned date %s", retryBatchID, dayAug5.Format("2006-01-02"))
	}
}
