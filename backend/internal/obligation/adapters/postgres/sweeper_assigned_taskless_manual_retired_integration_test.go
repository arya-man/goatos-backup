package postgres

import (
	"context"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vxpg "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/postgres"
	vxdomain "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// A maintainer-created manual batch (session "manual:...") can be stamped with a RETIRED protocol
// version while its member obligations sit on the published version. When that batch is on a live
// drive assignment (here reassigned to a second operator) the sweeper's published-version pass must
// still give it exactly one SOP task, otherwise the phone roster serves the animals with an empty
// task id and refuses every scan ("This animal's batch has no task yet").
func TestSweeperTasksAssignedManualBatchOnRetiredVersion(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const (
		goatA      = "10000000-0000-4000-8000-00000000fc01"
		goatB      = "10000000-0000-4000-8000-00000000fc02"
		shedID     = "00000000-0000-4000-8000-00000000dc01"
		operatorA  = "20000000-0000-4000-8000-000000000c01"
		operatorB  = "20000000-0000-4000-8000-000000000c02"
		sopVersion = "b0000000-0000-4000-8000-000000000002"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "manual-retired-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatA, goatB)
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1, $3, 'OP-MANUAL-A', 'Manual Operator A', 'active', 'operator', $4),
       ($2, $3, 'OP-MANUAL-B', 'Manual Operator B', 'active', 'operator', $4)`, operatorA, operatorB, tenantID, cbePark); err != nil {
		t.Fatalf("seed operators: %v", err)
	}
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.manual.retired", Name: "Manual retired", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	sop := sopVersion
	mkVersion := func(n int32) string {
		t.Helper()
		id, err := proto.CreateVersion(ctx, protodomain.NewVersion{
			TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: n, Status: "draft",
			EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
			SopVersionID: &sop,
		})
		if err != nil {
			t.Fatalf("version %d: %v", n, err)
		}
		return id
	}
	retiredVersion := mkVersion(2)
	if _, err := pool.Exec(ctx, `UPDATE protocol_versions SET status='retired' WHERE tenant_id=$1 AND protocol_version_id=$2`, tenantID, retiredVersion); err != nil {
		t.Fatalf("retire version: %v", err)
	}
	publishedVersion := mkVersion(9)
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: publishedVersion, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	planned := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	obligations := make([]string, 0, 2)
	for _, goat := range []string{goatA, goatB} {
		obl, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: publishedVersion, RuleID: ruleID,
			TargetType: "goat", TargetID: goat, ScopeType: "shed", ScopeID: shedID,
			DueAt: planned, Status: "scheduled", IdempotencyKey: "manual-retired-" + goat, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("insert obligation %s: applied=%v err=%v", goat, applied, err)
		}
		obligations = append(obligations, obl)
	}
	opA := operatorA
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: retiredVersion, ScopeType: "shed", ScopeID: shedID,
		Session: "manual:yashoda-3", PlannedDate: &planned, Status: "planned",
		EstimatedTargets: 2, PlannedQuantity: "2", QuantityUnit: "dose", ConductedBy: &opA,
	}, obligations)
	if err != nil || attached != 2 {
		t.Fatalf("create manual batch: attached=%d err=%v", attached, err)
	}
	shed := shedID
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: batchID, PlannedDate: planned, OperatorID: &opA, ParkID: cbePark, ShedID: &shed,
		PhysicalShed: "Yashoda", PartitionLabel: "3", AnimalCount: 2,
		VaccineRuleIDs: []string{ruleID}, TotalDoses: 2, CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed drive assignment: %v", err)
	}
	var assignmentID string
	if err := pool.QueryRow(ctx, `SELECT assignment_id::text FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID).Scan(&assignmentID); err != nil {
		t.Fatalf("load assignment: %v", err)
	}
	for i, goat := range []string{goatA, goatB} {
		if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, tenantID, assignmentID, obligations[i], goat); err != nil {
			t.Fatalf("seed assignment member: %v", err)
		}
	}
	// Reassigned drive: first operator -> second operator.
	if _, err := pool.Exec(ctx, `UPDATE vaccination_drive_assignments SET operator_id=$3 WHERE tenant_id=$1 AND assignment_id=$2`, tenantID, assignmentID, operatorB); err != nil {
		t.Fatalf("reassign operator: %v", err)
	}

	creator := &rawTaskCreator{pool: pool}
	sweep := oblapp.NewSweeperService(repo, creator, nil)
	cfg := oblapp.SweepConfig{SOPVersionID: sopVersion}
	// The sweeper runs the PUBLISHED version; the batch's own (retired) version never gets a pass.
	for pass := 0; pass < 2; pass++ {
		if err := sweep.FinalizePlannedBatches(ctx, tenantID, publishedVersion, cfg); err != nil {
			t.Fatalf("finalize pass %d: %v", pass, err)
		}
	}
	if creator.n != 1 {
		t.Fatalf("expected exactly one task for the manual retired-version batch, got %d", creator.n)
	}
	var taskID string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(sop_task_id::text,'') FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID).Scan(&taskID); err != nil || taskID == "" {
		t.Fatalf("manual batch still has no sop task: task=%q err=%v", taskID, err)
	}

	roster, err := vxpg.NewRepository(pool, 5*time.Second).ScanRoster(ctx, vxdomain.ScanRosterQuery{
		TenantID: tenantID, ShedID: shedID, PartitionLabel: "3", AssignmentID: assignmentID,
		OperatorScopeActorID: operatorB, Limit: 20,
	})
	if err != nil {
		t.Fatalf("ScanRoster(reassigned operator): %v", err)
	}
	if len(roster.Rows) != 2 {
		t.Fatalf("roster rows=%d want 2: %#v", len(roster.Rows), roster.Rows)
	}
	for _, row := range roster.Rows {
		if row.TaskID == "" {
			t.Fatalf("roster row for goat %s has empty task id: %#v", row.GoatID, row)
		}
	}
}

// The lister's membership is keyed on the MEMBER obligations' protocol version, never the batch's.
// Adversarial matrix on one published-version pass:
//   - one-to-many: a batch with two assigned member obligations is ONE row carrying both;
//   - page boundary: limit=1 keyset paging returns every qualifying batch exactly once;
//   - date shift: a batch whose planned_date moved off its obligations' due date still qualifies;
//   - park scope: a park-scoped batch over shed-scoped obligations still qualifies;
//   - status matrix: a completed batch, a batch whose obligations are all canceled, a batch whose
//     only assignment member is canceled, and a batch whose obligations sit on the RETIRED version
//     are all excluded.
func TestAssignedTasklessListerOneToManyPageBoundaryDateShiftParkScopeStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const (
		shedID     = "00000000-0000-4000-8000-00000000dd01"
		operatorID = "20000000-0000-4000-8000-000000000d01"
		sopVersion = "b0000000-0000-4000-8000-000000000002"
	)
	goats := []string{
		"10000000-0000-4000-8000-00000000fd01", "10000000-0000-4000-8000-00000000fd02",
		"10000000-0000-4000-8000-00000000fd03", "10000000-0000-4000-8000-00000000fd04",
		"10000000-0000-4000-8000-00000000fd05", "10000000-0000-4000-8000-00000000fd06",
		"10000000-0000-4000-8000-00000000fd07", "10000000-0000-4000-8000-00000000fd08",
	}
	seedParkConsolidationShed(t, ctx, pool, shedID, "lister-matrix-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goats...)
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1, $2, 'OP-LISTER', 'Lister Operator', 'active', 'operator', $3)`, operatorID, tenantID, cbePark); err != nil {
		t.Fatalf("seed operator: %v", err)
	}
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.lister.matrix", Name: "Lister matrix", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	sop := sopVersion
	mkVersion := func(n int32) (string, string) {
		t.Helper()
		id, err := proto.CreateVersion(ctx, protodomain.NewVersion{
			TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: n, Status: "draft",
			EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
			SopVersionID: &sop,
		})
		if err != nil {
			t.Fatalf("version %d: %v", n, err)
		}
		rule, err := proto.CreateRule(ctx, protodomain.NewRule{
			TenantID: tenantID, ProtocolVersionID: id, DoseCode: "primary", Sequence: 1,
			TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
			EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("rule %d: %v", n, err)
		}
		return id, rule
	}
	retiredVersion, retiredRule := mkVersion(2)
	if _, err := pool.Exec(ctx, `UPDATE protocol_versions SET status='retired' WHERE tenant_id=$1 AND protocol_version_id=$2`, tenantID, retiredVersion); err != nil {
		t.Fatalf("retire version: %v", err)
	}
	publishedVersion, publishedRule := mkVersion(9)

	due := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	operator := operatorID
	shed := shedID
	type spec struct {
		name         string
		goats        []string
		oblVersion   string
		oblRule      string
		batchScope   string
		batchScopeID string
		planned      time.Time
		batchStatus  string
		after        string // extra SQL applied with $1=tenant, $2=batch
	}
	mk := func(s spec) string {
		t.Helper()
		obls := make([]string, 0, len(s.goats))
		for _, g := range s.goats {
			obl, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
				TenantID: tenantID, ProtocolVersionID: s.oblVersion, RuleID: s.oblRule,
				TargetType: "goat", TargetID: g, ScopeType: "shed", ScopeID: shedID,
				DueAt: due, Status: "scheduled", IdempotencyKey: "lister-" + s.name + "-" + g, Sequence: 1,
			})
			if err != nil || !applied {
				t.Fatalf("%s obligation: applied=%v err=%v", s.name, applied, err)
			}
			obls = append(obls, obl)
		}
		planned := s.planned
		batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
			TenantID: tenantID, ProtocolVersionID: retiredVersion, ScopeType: s.batchScope, ScopeID: s.batchScopeID,
			Session: "manual:" + s.name, PlannedDate: &planned, Status: "planned",
			EstimatedTargets: int32(len(obls)), PlannedQuantity: "1", QuantityUnit: "dose", ConductedBy: &operator,
		}, obls)
		if err != nil || int(attached) != len(obls) {
			t.Fatalf("%s batch: attached=%d err=%v", s.name, attached, err)
		}
		if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
			BatchID: batchID, PlannedDate: planned, OperatorID: &operator, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Lister", PartitionLabel: s.name, AnimalCount: int32(len(obls)),
			VaccineRuleIDs: []string{s.oblRule}, TotalDoses: int32(len(obls)), CapacityStatus: "within_cap",
		}}); err != nil {
			t.Fatalf("%s assignment: %v", s.name, err)
		}
		for i, g := range s.goats {
			if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
SELECT tenant_id, assignment_id, $3::uuid, $4::uuid FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2
ON CONFLICT DO NOTHING`, tenantID, batchID, obls[i], g); err != nil {
				t.Fatalf("%s member: %v", s.name, err)
			}
		}
		if s.batchStatus != "" && s.batchStatus != "planned" {
			if _, err := pool.Exec(ctx, `UPDATE obligation_batches SET status=$3 WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID, s.batchStatus); err != nil {
				t.Fatalf("%s batch status: %v", s.name, err)
			}
		}
		if s.after != "" {
			if _, err := pool.Exec(ctx, s.after, tenantID, batchID); err != nil {
				t.Fatalf("%s after: %v", s.name, err)
			}
		}
		return batchID
	}
	oneToMany := mk(spec{name: "one-to-many", goats: goats[0:2], oblVersion: publishedVersion, oblRule: publishedRule,
		batchScope: "shed", batchScopeID: shedID, planned: due})
	dateShift := mk(spec{name: "date-shift", goats: goats[2:3], oblVersion: publishedVersion, oblRule: publishedRule,
		batchScope: "shed", batchScopeID: shedID, planned: due.AddDate(0, 0, 3), batchStatus: "in_progress"})
	parkScope := mk(spec{name: "park-scope", goats: goats[3:4], oblVersion: publishedVersion, oblRule: publishedRule,
		batchScope: "park", batchScopeID: cbePark, planned: due})
	completed := mk(spec{name: "completed", goats: goats[4:5], oblVersion: publishedVersion, oblRule: publishedRule,
		batchScope: "shed", batchScopeID: shedID, planned: due, batchStatus: "completed"})
	canceledObl := mk(spec{name: "canceled-obligations", goats: goats[5:6], oblVersion: publishedVersion, oblRule: publishedRule,
		batchScope: "shed", batchScopeID: shedID, planned: due,
		after: `UPDATE obligation_instances SET status='canceled' WHERE tenant_id=$1 AND batch_id=$2`})
	canceledMember := mk(spec{name: "canceled-member", goats: goats[6:7], oblVersion: publishedVersion, oblRule: publishedRule,
		batchScope: "shed", batchScopeID: shedID, planned: due,
		after: `UPDATE vaccination_drive_assignment_members m SET canceled_at=now() FROM vaccination_drive_assignments a
WHERE a.tenant_id=$1 AND a.batch_id=$2 AND m.tenant_id=a.tenant_id AND m.assignment_id=a.assignment_id`})
	retiredObl := mk(spec{name: "retired-obligations", goats: goats[7:8], oblVersion: retiredVersion, oblRule: retiredRule,
		batchScope: "shed", batchScopeID: shedID, planned: due})

	seen := map[string]domain.PlannedBatchFinalization{}
	var after *domain.PlannedBatchFinalizationCursor
	for page := 0; page < 10; page++ {
		rows, err := repo.ListAssignedBatchesMissingSOPTask(ctx, tenantID, publishedVersion, after, 1)
		if err != nil {
			t.Fatalf("list page %d: %v", page, err)
		}
		if len(rows) == 0 {
			break
		}
		if len(rows) != 1 {
			t.Fatalf("page %d returned %d rows at limit 1", page, len(rows))
		}
		if _, dup := seen[rows[0].BatchID]; dup {
			t.Fatalf("batch %s returned on two pages", rows[0].BatchID)
		}
		seen[rows[0].BatchID] = rows[0]
		after = &domain.PlannedBatchFinalizationCursor{CreatedAt: rows[0].CreatedAt, BatchID: rows[0].BatchID}
	}
	for name, id := range map[string]string{"one-to-many": oneToMany, "date-shift": dateShift, "park-scope": parkScope} {
		row, ok := seen[id]
		if !ok {
			t.Fatalf("%s batch on a retired batch version with published-version assigned obligations was not listed", name)
		}
		if row.RuleID != publishedRule {
			t.Fatalf("%s batch listed with rule %s, want the published rule %s", name, row.RuleID, publishedRule)
		}
	}
	if got := seen[oneToMany].AttachedObligations; got != 2 {
		t.Fatalf("one-to-many batch attached_obligations=%d want 2 (one row per batch, both members counted)", got)
	}
	for name, id := range map[string]string{"completed": completed, "canceled-obligations": canceledObl, "canceled-member": canceledMember, "retired-obligations": retiredObl} {
		if _, ok := seen[id]; ok {
			t.Fatalf("%s batch must not be listed on the published-version pass", name)
		}
	}
	if len(seen) != 3 {
		t.Fatalf("listed %d batches, want exactly 3", len(seen))
	}
}
