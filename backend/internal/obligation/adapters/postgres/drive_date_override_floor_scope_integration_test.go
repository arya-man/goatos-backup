package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestDriveDateOverrideFloorScopeHierarchy: a CBE park PPR override covers every shed under CBE
// and nothing outside it. Two CBE sheds are rewritten together; a CPT-park batch on the same
// source date whose goat is BELOW its floor must neither veto the CBE move (the floor proof is
// scoped to the re-planned park) nor be rewritten by it.
func TestDriveDateOverrideFloorScopeHierarchy(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		shedA   = "00000000-0000-4000-8000-00000000eb01"
		shedB   = "00000000-0000-4000-8000-00000000eb02"
		cptShed = "6e769414-4dde-43a5-b30f-fb857392b638" // seeded CPT shed under cptPark
	)
	var seeds []floorSeed
	for i, shed := range []string{shedA, shedB} {
		for g := 1; g <= 2; g++ {
			seeds = append(seeds, floorSeed{lane: "ppr", goat: floorGoat(0xb1+i, g), shed: shed, due: floorSource, key: fmt.Sprintf("floor-scope-%d-%d", i, g)})
		}
	}
	f := newFloorFixture(t, ctx, pool, "floor-scope", "20000000-0000-4000-8000-00000000eb01",
		map[string]string{shedA: "Floor Scope A", shedB: "Floor Scope B"}, nil, seeds)

	// A goat in another park (made young below), same vaccine lane, same source date.
	cptGoat := floorGoat(0xbf, 1)
	seedReserveGoats(t, ctx, pool, cptShed, cptPark, cptGoat)
	cptObl, applied, err := f.repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: f.ppr.versionID, RuleID: f.ppr.ruleID,
		TargetType: "goat", TargetID: cptGoat, ScopeType: "shed", ScopeID: cptShed,
		DueAt: floorSource, Status: "scheduled", IdempotencyKey: "floor-scope-cpt", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("CPT obligation: applied=%v err=%v", applied, err)
	}
	planned := floorSource
	cptBatch, attached, err := f.repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: f.ppr.versionID, ScopeType: "park", ScopeID: cptPark,
		Session: "floor-scope-cpt", PlannedDate: &planned, Status: "planned",
		EstimatedTargets: 1, PlannedQuantity: "1", QuantityUnit: "dose",
	}, []string{cptObl})
	if err != nil || attached != 1 {
		t.Fatalf("CPT batch: attached=%d err=%v", attached, err)
	}
	const cptOp = "20000000-0000-4000-8000-00000000eb02"
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1, $2, 'floor-scope-cpt', 'floor-scope-cpt', 'active', 'operator', $3)`, cptOp, tenantID, cptPark); err != nil {
		t.Fatalf("seed CPT operator: %v", err)
	}
	if err := f.repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: cptBatch, PlannedDate: floorSource, OperatorID: testStringPtr(cptOp), ParkID: cptPark,
		ShedID: testStringPtr(cptShed), PhysicalShed: "Mandela 1", PartitionLabel: "5", AnimalCount: 1,
		VaccineRuleIDs: []string{f.ppr.ruleID}, TotalDoses: 1, CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("CPT assignment: %v", err)
	}

	// The DOB correction lands after planning, so the CPT row now sits below its floor.
	if _, err := pool.Exec(ctx, `UPDATE goats SET dob = $2 WHERE tenant_id = $1 AND goat_id = $3::uuid`, tenantID, youngDOB, cptGoat); err != nil {
		t.Fatalf("young CPT goat: %v", err)
	}

	if err := f.movePPR(ctx); err != nil {
		t.Fatalf("CBE PPR move vetoed by an out-of-park goat: %v", err)
	}
	got := f.dueDates(t, ctx)
	if len(got) != len(seeds) {
		t.Fatalf("CBE batch holds %d rows, want %d: %v", len(got), len(seeds), got)
	}
	for _, s := range seeds {
		if got[s.key] != floorTarget.Format(time.DateOnly) {
			t.Errorf("CBE %s due %s, want override date %s (every shed under the park moves)", s.key, got[s.key], floorTarget.Format(time.DateOnly))
		}
	}
	var cptDay string
	if err := pool.QueryRow(ctx, `
SELECT to_char((due_at AT TIME ZONE 'Asia/Kolkata')::date, 'YYYY-MM-DD')
FROM obligation_instances WHERE tenant_id = $1 AND obligation_id = $2::uuid`, tenantID, cptObl).Scan(&cptDay); err != nil {
		t.Fatalf("read CPT row: %v", err)
	}
	if cptDay != floorSource.Format(time.DateOnly) {
		t.Errorf("CPT row due %s, want untouched %s -- a CBE override must not rewrite another park", cptDay, floorSource.Format(time.DateOnly))
	}
	if n := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2 AND planned_date=$3::date`, tenantID, cptBatch, floorSource.Format(time.DateOnly)); n != 1 {
		t.Errorf("CPT assignment moved off its date (rows on source date = %d)", n)
	}
}
