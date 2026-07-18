package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// VAXCAP-002/003/004 Postgres guards.
//
// These tests prove, against a real Postgres instance:
//   - VAXCAP-002: the drive-cell counter and both combo-list projections execute valid SQL over
//     the NUMERIC planned_quantity column, for both NULL and non-null quantities.
//   - VAXCAP-003: the batch writer persists EXACT administration-cell totals -- per-obligation
//     cells summed over the actually-attached rows -- across mixed-rule create, partial attach,
//     merge-attach, and same-batch retry. Never an average.
//   - VAXCAP-004: the park/date capacity counter includes planned, in_progress, AND completed
//     batches, and excludes only canceled and superseded.
//
// Run with GOATOS_RUN_POSTGRES_TESTS=1 (explicit opt-in per repo policy).

// seedCapacityGoat inserts one alive goat in the CBE park.
func seedCapacityGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4)
			 ON CONFLICT (goat_id) DO NOTHING`,
		goatID, tenantID, meshaParty, cbePark); err != nil {
		t.Fatalf("seed capacity goat %s: %v", goatID, err)
	}
}

// seedCapacityObligations seeds n goats with one scheduled park-scoped obligation each and
// returns the obligation IDs in order.
func seedCapacityObligations(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *Repository, versionID, ruleID, keyPrefix string, n int, due time.Time) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		goatID := fmt.Sprintf("20000000-0000-4000-8000-0000000000%02x", 0x10+i)
		seedCapacityGoat(t, ctx, pool, goatID)
		id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "park", ScopeID: cbePark,
			DueAt: due, Status: "scheduled",
			IdempotencyKey: fmt.Sprintf("%s-%d", keyPrefix, i), Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("seed capacity obligation %d: applied=%v err=%v", i, applied, err)
		}
		ids = append(ids, id)
	}
	return ids
}

func batchPlannedQuantity(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batchID string) float64 {
	t.Helper()
	var qty *float64
	if err := pool.QueryRow(ctx,
		`SELECT planned_quantity::float8 FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`,
		tenantID, batchID).Scan(&qty); err != nil {
		t.Fatalf("read planned_quantity: %v", err)
	}
	if qty == nil {
		return -1
	}
	return *qty
}

// TestCreateBatchWithObligationCellsExactTotals is the VAXCAP-003 guard: mixed-rule [2,1,1]
// creation, partial attach, merge-attach, and same-batch retry each persist EXACT cell totals.
func TestCreateBatchWithObligationCellsExactTotals(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	due := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	planned := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	ids := seedCapacityObligations(t, ctx, pool, repo, versionID, ruleID, "cells", 5, due)

	newBatch := func(session string) domain.NewBatch {
		return domain.NewBatch{
			TenantID: tenantID, ProtocolVersionID: versionID,
			ScopeType: "park", ScopeID: cbePark, Session: session,
			PlannedDate: &planned, Status: "planned",
			EstimatedTargets: 3, PlannedQuantity: "4", QuantityUnit: "dose",
		}
	}

	// (1) Mixed-rule [2,1,1] create: exact total 4, never a rounded-average 6 or 3.
	cells := map[string]int32{ids[0]: 2, ids[1]: 1, ids[2]: 1}
	batchID, attached, err := repo.CreateBatchWithObligationCells(ctx, newBatch("s1"), ids[:3], cells)
	if err != nil {
		t.Fatalf("create mixed batch: %v", err)
	}
	if len(attached) != 3 {
		t.Fatalf("attached = %d, want 3", len(attached))
	}
	if got := batchPlannedQuantity(t, ctx, pool, batchID); got != 4 {
		t.Fatalf("mixed [2,1,1] planned_quantity = %v, want exactly 4", got)
	}

	// (2) Partial attach: ids[3] (2 cells) + ids[0] (already attached, cannot re-attach) into a
	// NEW batch -> only ids[3] attaches, and the persisted total is ITS 2 cells, not the
	// selected-set total 4.
	partialCells := map[string]int32{ids[3]: 2, ids[0]: 2}
	batch2, attached2, err := repo.CreateBatchWithObligationCells(ctx, newBatch("s2"), []string{ids[3], ids[0]}, partialCells)
	if err != nil {
		t.Fatalf("partial attach: %v", err)
	}
	if len(attached2) != 1 || attached2[0] != ids[3] {
		t.Fatalf("partial attach ids = %v, want only %s", attached2, ids[3])
	}
	if got := batchPlannedQuantity(t, ctx, pool, batch2); got != 2 {
		t.Fatalf("partial attach planned_quantity = %v, want 2 (attached cells only, not selected-set 4)", got)
	}

	// (3) Merge-attach: same batch identity (s2) with a NEW 1-cell obligation -> found existing
	// planned batch, planned_quantity ADDS exactly the newly attached cell: 2 + 1 = 3.
	mergeCells := map[string]int32{ids[4]: 1}
	batch3, attached3, err := repo.CreateBatchWithObligationCells(ctx, newBatch("s2"), []string{ids[4]}, mergeCells)
	if err != nil {
		t.Fatalf("merge attach: %v", err)
	}
	if batch3 != batch2 {
		t.Fatalf("merge attach batch = %s, want existing %s", batch3, batch2)
	}
	if len(attached3) != 1 {
		t.Fatalf("merge attached = %d, want 1", len(attached3))
	}
	if got := batchPlannedQuantity(t, ctx, pool, batch2); got != 3 {
		t.Fatalf("merge planned_quantity = %v, want 3 (2 + newly attached 1)", got)
	}

	// (4) Same-batch retry: replaying the merge attaches zero rows and must NOT change the total.
	_, attached4, err := repo.CreateBatchWithObligationCells(ctx, newBatch("s2"), []string{ids[4]}, mergeCells)
	if err != nil {
		t.Fatalf("retry attach: %v", err)
	}
	if len(attached4) != 0 {
		t.Fatalf("retry attached = %d, want 0", len(attached4))
	}
	if got := batchPlannedQuantity(t, ctx, pool, batch2); got != 3 {
		t.Fatalf("retry planned_quantity = %v, want unchanged 3", got)
	}
}

// TestCountDriveCellsForParkDateStatusMatrixAndNullQuantity is the VAXCAP-002 + VAXCAP-004
// guard: the counter executes over numeric planned_quantity (NULL and non-null), counts
// planned/in_progress/completed batches across two vaccines, and excludes canceled/superseded.
func TestCountDriveCellsForParkDateStatusMatrixAndNullQuantity(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	due := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	planned := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	ids := seedCapacityObligations(t, ctx, pool, repo, versionID, ruleID, "matrix", 5, due)

	mkBatch := func(session, quantity string, obligationIDs []string, cells map[string]int32) string {
		t.Helper()
		batchID, attached, err := repo.CreateBatchWithObligationCells(ctx, domain.NewBatch{
			TenantID: tenantID, ProtocolVersionID: versionID,
			ScopeType: "park", ScopeID: cbePark, Session: session,
			PlannedDate: &planned, Status: "planned",
			EstimatedTargets: int32(len(obligationIDs)), PlannedQuantity: quantity, QuantityUnit: "dose",
		}, obligationIDs, cells)
		if err != nil || len(attached) != len(obligationIDs) {
			t.Fatalf("create %s batch: attached=%d err=%v", session, len(attached), err)
		}
		return batchID
	}
	setStatus := func(batchID, status string) {
		t.Helper()
		if _, err := pool.Exec(ctx,
			`UPDATE obligation_batches SET status=$3 WHERE tenant_id=$1 AND batch_id=$2`,
			tenantID, batchID, status); err != nil {
			t.Fatalf("set batch %s status %s: %v", batchID, status, err)
		}
	}

	// Vaccine A ("fmd"): planned batch, 2 cells (one 2-dose obligation).
	bPlanned := mkBatch("fmd", "2", ids[0:1], map[string]int32{ids[0]: 2})
	// Vaccine A: in_progress batch, 1 cell.
	bInProgress := mkBatch("fmd:2", "1", ids[1:2], map[string]int32{ids[1]: 1})
	setStatus(bInProgress, "in_progress")
	// Vaccine B ("hs"): completed batch with NULL planned_quantity -> falls back to row count 1.
	bCompleted := mkBatch("hs", "1", ids[2:3], map[string]int32{ids[2]: 1})
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_batches SET planned_quantity=NULL, status='completed' WHERE tenant_id=$1 AND batch_id=$2`,
		tenantID, bCompleted); err != nil {
		t.Fatalf("null quantity + complete: %v", err)
	}
	// Vaccine B: canceled and superseded batches must NOT count.
	bCanceled := mkBatch("hs:2", "1", ids[3:4], map[string]int32{ids[3]: 1})
	setStatus(bCanceled, "canceled")
	bSuperseded := mkBatch("hs:3", "1", ids[4:5], map[string]int32{ids[4]: 1})
	setStatus(bSuperseded, "superseded")

	// planned(2) + in_progress(1) + completed(NULL->1) = 4; canceled/superseded excluded.
	count, err := repo.CountDriveCellsForParkDate(ctx, tenantID, cbePark, planned)
	if err != nil {
		t.Fatalf("CountDriveCellsForParkDate: %v", err)
	}
	if count != 4 {
		t.Fatalf("park/date cells = %d, want 4 (planned 2 + in_progress 1 + completed NULL-quantity 1; canceled+superseded excluded)", count)
	}
	_ = bPlanned
}

// TestComboListCellCountNumericQuantity is the VAXCAP-002 combo-list guard: both combo-list
// methods execute over numeric planned_quantity and return GREATEST(quantity, row count) for
// NULL and non-null quantities.
func TestComboListCellCountNumericQuantity(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	due := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	planned := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	ids := seedCapacityObligations(t, ctx, pool, repo, versionID, ruleID, "combo", 3, due)

	// Combo batch 1: planned_quantity 4 > 2 rows -> cell_count 4.
	b1, attached, err := repo.CreateBatchWithObligationCells(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID,
		ScopeType: "park", ScopeID: cbePark, Session: "combo:etv+ppr",
		PlannedDate: &planned, Status: "planned",
		EstimatedTargets: 2, PlannedQuantity: "4", QuantityUnit: "dose",
	}, ids[:2], map[string]int32{ids[0]: 2, ids[1]: 2})
	if err != nil || len(attached) != 2 {
		t.Fatalf("create combo batch 1: attached=%d err=%v", len(attached), err)
	}
	// Combo batch 2: NULL planned_quantity -> cell_count falls back to row count 1.
	b2, attached2, err := repo.CreateBatchWithObligationCells(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID,
		ScopeType: "park", ScopeID: cbePark, Session: "combo:fmd+hs",
		PlannedDate: &planned, Status: "planned",
		EstimatedTargets: 1, PlannedQuantity: "1", QuantityUnit: "dose",
	}, ids[2:3], map[string]int32{ids[2]: 1})
	if err != nil || len(attached2) != 1 {
		t.Fatalf("create combo batch 2: attached=%d err=%v", len(attached2), err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_batches SET planned_quantity=NULL WHERE tenant_id=$1 AND batch_id=$2`,
		tenantID, b2); err != nil {
		t.Fatalf("null combo quantity: %v", err)
	}

	wantCells := map[string]int32{b1: 4, b2: 1}
	assertCells := func(name string, rows []domain.ComboDriveBatch, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		seen := map[string]int32{}
		for _, row := range rows {
			seen[row.BatchID] = row.CellCount
		}
		for batchID, want := range wantCells {
			if got, ok := seen[batchID]; !ok || got != want {
				t.Fatalf("%s: batch %s cell_count = %d (found=%v), want %d", name, batchID, got, ok, want)
			}
		}
	}
	dueBefore := planned.AddDate(0, 0, 1)
	rows, err := repo.ListPlannedComboBatches(ctx, tenantID, dueBefore, 100)
	assertCells("ListPlannedComboBatches", rows, err)
	keysetRows, err := repo.ListPlannedComboBatchesKeyset(ctx, tenantID, dueBefore, nil, 100)
	assertCells("ListPlannedComboBatchesKeyset", keysetRows, err)
}
