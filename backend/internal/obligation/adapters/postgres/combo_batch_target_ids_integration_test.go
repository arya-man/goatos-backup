package postgres

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestListPlannedComboBatchesReturnsTargetIDs verifies the ListPlannedComboBatches query (used
// by AlignComboDrives to keep combo-batch alignment shot-cap aware, BUG2 requirement 6)
// aggregates the distinct animal target IDs attached to a planned combo batch via a single
// set-based join, not a per-batch follow-up query.
func TestListPlannedComboBatchesReturnsTargetIDs(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool) // obligation for testGoatID
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	secondGoat := "10000000-0000-4000-8000-0000000000bb"
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4)`,
		secondGoat, tenantID, meshaParty, cbePark); err != nil {
		t.Fatalf("seed second goat: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)
	obB, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: secondGoat, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled",
		IdempotencyKey: "obl-combo-target-ids", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("seed obB: applied=%v err=%v", applied, err)
	}

	plannedDate := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
		Session: "combo:FMD+HS", PlannedDate: &plannedDate, Status: "planned",
		EstimatedTargets: 2, PlannedQuantity: "2", QuantityUnit: "dose",
	}, []string{obA, obB})
	if err != nil {
		t.Fatalf("create combo batch: %v", err)
	}
	if attached != 2 {
		t.Fatalf("attached = %d, want 2", attached)
	}

	rows, err := repo.ListPlannedComboBatches(ctx, tenantID, plannedDate.AddDate(0, 0, 1), 100)
	if err != nil {
		t.Fatalf("list planned combo batches: %v", err)
	}
	var found *domain.ComboDriveBatch
	for i := range rows {
		if rows[i].BatchID == batchID {
			found = &rows[i]
		}
	}
	if found == nil {
		t.Fatalf("expected combo batch %s among results, got %#v", batchID, rows)
	}
	got := append([]string(nil), found.TargetIDs...)
	sort.Strings(got)
	want := []string{secondGoat, testGoatID}
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("target ids = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("target ids = %#v, want %#v", got, want)
		}
	}
}
