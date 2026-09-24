package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestListAssignedBatchesMissingSOPTaskParkScope pins the scope of the assigned-taskless lister.
// It is keyed by tenant + protocol version and reports each batch's own stored scope; a batch is
// only picked when one of ITS open obligations sits on a live drive-assignment member row. So:
//   - a CBE-shed batch with a live member is returned, carrying its own shed scope;
//   - a CPT-shed batch with a live member is returned with CPT scope (never re-scoped to CBE);
//   - a CPT-shed batch whose only member row is canceled is NOT returned;
//   - a CPT-shed batch on nobody's roster is NOT returned, even though a sibling park batch is;
//   - a live-assigned batch of another protocol version is NOT returned for this version;
//   - the same sweep for another tenant returns nothing.
func TestListAssignedBatchesMissingSOPTaskParkScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	const (
		cbeShed  = "00000000-0000-4000-8000-00000000dc01"
		cptShed  = "6e769414-4dde-43a5-b30f-fb857392b638" // seeded CPT shed under cptPark
		operator = "20000000-0000-4000-8000-000000000c01"
		cptOp    = "20000000-0000-4000-8000-000000000c02"
		goatCBE  = "10000000-0000-4000-8000-00000000fc01"
		goatCPT  = "10000000-0000-4000-8000-00000000fc02"
		goatCanc = "10000000-0000-4000-8000-00000000fc03"
		goatNone = "10000000-0000-4000-8000-00000000fc04"
		goatVer  = "10000000-0000-4000-8000-00000000fc05"
	)
	seedParkConsolidationShed(t, ctx, pool, cbeShed, "taskless-scope-cbe")
	seedReserveGoats(t, ctx, pool, cbeShed, cbePark, goatCBE, goatVer)
	seedReserveGoats(t, ctx, pool, cptShed, cptPark, goatCPT, goatCanc, goatNone)
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1, $2, 'OP-TASKLESS-SCOPE', 'Taskless Scope Operator', 'active', 'operator', $3)`, operator, tenantID, cbePark); err != nil {
		t.Fatalf("seed operator: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1, $2, 'OP-TASKLESS-SCOPE-CPT', 'Taskless Scope CPT Operator', 'active', 'operator', $3)`, cptOp, tenantID, cptPark); err != nil {
		t.Fatalf("seed CPT operator: %v", err)
	}
	versionID, ruleID := parkConsolidationProtocol(t, ctx, pool, "vaccination.taskless.scope")
	otherVersionID, otherRuleID := parkConsolidationProtocol(t, ctx, pool, "vaccination.taskless.scope.other")

	planned := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	type made struct{ batch, obl string }
	mk := func(version, rule, goat, park, shed, key string, assign, cancelMember bool) made {
		t.Helper()
		op := operator
		if park == cptPark {
			op = cptOp
		}
		obl, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: version, RuleID: rule,
			TargetType: "goat", TargetID: goat, ScopeType: "shed", ScopeID: shed,
			DueAt: planned, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("insert obligation %s: applied=%v err=%v", key, applied, err)
		}
		batch, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
			TenantID: tenantID, ProtocolVersionID: version, ScopeType: "shed", ScopeID: shed,
			Session: key, PlannedDate: &planned, Status: "planned",
			EstimatedTargets: 1, PlannedQuantity: "1", QuantityUnit: "dose", ConductedBy: &op,
		}, []string{obl})
		if err != nil || attached != 1 {
			t.Fatalf("create batch %s: attached=%d err=%v", key, attached, err)
		}
		if _, err := pool.Exec(ctx, `UPDATE obligation_batches SET status='in_progress' WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batch); err != nil {
			t.Fatalf("mark %s in_progress: %v", key, err)
		}
		if !assign {
			return made{batch, obl}
		}
		shedCopy := shed
		if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
			BatchID: batch, PlannedDate: planned, OperatorID: &op, ParkID: park, ShedID: &shedCopy,
			PhysicalShed: key, PartitionLabel: "1", AnimalCount: 1,
			VaccineRuleIDs: []string{rule}, TotalDoses: 1, CapacityStatus: "within_cap",
		}}); err != nil {
			t.Fatalf("seed assignment %s: %v", key, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
SELECT tenant_id, assignment_id, $3::uuid, $4::uuid
FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2
ON CONFLICT DO NOTHING`, tenantID, batch, obl, goat); err != nil {
			t.Fatalf("seed member %s: %v", key, err)
		}
		if cancelMember {
			if _, err := pool.Exec(ctx, `UPDATE vaccination_drive_assignment_members SET canceled_at = now() WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, obl); err != nil {
				t.Fatalf("cancel member %s: %v", key, err)
			}
		}
		return made{batch, obl}
	}
	cbe := mk(versionID, ruleID, goatCBE, cbePark, cbeShed, "taskless-scope-cbe", true, false)
	cpt := mk(versionID, ruleID, goatCPT, cptPark, cptShed, "taskless-scope-cpt", true, false)
	canceled := mk(versionID, ruleID, goatCanc, cptPark, cptShed, "taskless-scope-canceled", true, true)
	unassigned := mk(versionID, ruleID, goatNone, cptPark, cptShed, "taskless-scope-none", false, false)
	otherVersion := mk(otherVersionID, otherRuleID, goatVer, cbePark, cbeShed, "taskless-scope-version", true, false)

	got, err := repo.ListAssignedBatchesMissingSOPTask(ctx, tenantID, versionID, nil, 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	byID := map[string]domain.PlannedBatchFinalization{}
	for _, b := range got {
		if _, dup := byID[b.BatchID]; dup {
			t.Fatalf("batch %s returned twice", b.BatchID)
		}
		byID[b.BatchID] = b
	}
	if len(byID) != 2 {
		t.Fatalf("lister returned %d batches, want exactly the 2 live-assigned batches of this version: %+v", len(byID), got)
	}
	for _, want := range []struct {
		batch, shed string
	}{{cbe.batch, cbeShed}, {cpt.batch, cptShed}} {
		b, ok := byID[want.batch]
		if !ok {
			t.Fatalf("live-assigned batch %s missing from lister", want.batch)
		}
		if b.ScopeType != "shed" || b.ScopeID != want.shed {
			t.Errorf("batch %s scope = %s/%s, want its own shed %s", want.batch, b.ScopeType, b.ScopeID, want.shed)
		}
		if b.AttachedObligations != 1 || b.RuleID != ruleID {
			t.Errorf("batch %s attached=%d rule=%s, want 1/%s", want.batch, b.AttachedObligations, b.RuleID, ruleID)
		}
	}
	for name, id := range map[string]string{"canceled-member": canceled.batch, "unassigned": unassigned.batch, "other-version": otherVersion.batch} {
		if _, ok := byID[id]; ok {
			t.Errorf("%s batch %s leaked into the lister", name, id)
		}
	}
	other, err := repo.ListAssignedBatchesMissingSOPTask(ctx, "00000000-0000-4000-8000-000000000002", versionID, nil, 100)
	if err != nil {
		t.Fatalf("list other tenant: %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("another tenant's sweep saw %d of this tenant's batches", len(other))
	}
}
