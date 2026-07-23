package postgres

import (
	"context"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// TestGoatExitedRemovesAnimalFromPlannedDriveAssignments is the SM-3 cascade proof for the
// PLANNED-WORK read model. goat.exited already cancels the animal's open obligation_instances,
// but a dead/sold/culled animal must ALSO stop occupying the planned vaccination drive it was
// batched into: vaccination_drive_assignments is what the vaccination execution screens, the
// shed/operator day view, and Calendar render as "planned work" and what the operator sees as
// their day's animal load. Leaving the exited animal inside animal_count/total_doses keeps a dead
// animal on an operator's route and inflates the drive's planned load forever, because nothing
// else ever re-derives that row (the planner only rewrites assignments when it re-plans the batch).
//
// Fixture: one park, one shed, three live goats attached to ONE planned batch on one date. The
// exiting goat carries TWO vaccine rules (two doses), the other two carry one each, so the
// persisted assignment row is animal_count=3 / total_doses=4 -- exactly what the sweeper's
// driveAssignmentsForUnbatched bucket math (distinct targets, distinct target+rule dose keys)
// would have produced. The exit is fired through the REAL production path: an eventbus
// goat.exited envelope delivered to the registered obligation app GoatExitedHandler, the same
// handler cmd/domain-event-consumer and kernelstages wire.
func TestGoatExitedRemovesAnimalFromPlannedDriveAssignments(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const (
		exitingGoat = "10000000-0000-4000-8000-00000000fa01"
		stayingGoat = "10000000-0000-4000-8000-00000000fa02"
		thirdGoat   = "10000000-0000-4000-8000-00000000fa03"
		shedID      = "00000000-0000-4000-8000-00000000da01"
		operatorID  = "20000000-0000-4000-8000-000000000a01"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "goat-exit-assignment-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, exitingGoat, stayingGoat, thirdGoat)
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
VALUES ($1, $2, 'OP-EXIT', 'Exit Cascade Operator', 'active', 'operator')`, operatorID, tenantID); err != nil {
		t.Fatalf("seed operator: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.exitassign", Name: "Exit assign", Category: "vaccination", Status: "draft",
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
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule A: %v", err)
	}
	ruleB, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "booster", Sequence: 2,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule B: %v", err)
	}

	planned := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	insert := func(goatID, ruleID, key string) string {
		t.Helper()
		id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
			DueAt: planned, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("insert obligation %s: applied=%v err=%v", key, applied, err)
		}
		return id
	}
	exitingDoseA := insert(exitingGoat, ruleA, "exit-goat-dose-a")
	exitingDoseB := insert(exitingGoat, ruleB, "exit-goat-dose-b")
	stayingDose := insert(stayingGoat, ruleA, "staying-goat-dose-a")
	thirdDose := insert(thirdGoat, ruleA, "third-goat-dose-a")

	operator := operatorID
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "exit-assign-drive", PlannedDate: &planned, Status: "planned",
		EstimatedTargets: 3, PlannedQuantity: "4", QuantityUnit: "dose", ConductedBy: &operator,
	}, []string{exitingDoseA, exitingDoseB, stayingDose, thirdDose})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if attached != 4 {
		t.Fatalf("attached = %d, want 4", attached)
	}

	shed := shedID
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: batchID, PlannedDate: planned, OperatorID: &operator, ParkID: cbePark, ShedID: &shed,
		PhysicalShed: "Gandhi", PartitionLabel: "1", AnimalCount: 3,
		VaccineRuleIDs: []string{ruleA, ruleB}, TotalDoses: 4, CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed drive assignment: %v", err)
	}
	if got := countRows(t, ctx, pool,
		`SELECT animal_count FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != 3 {
		t.Fatalf("fixture invalid: planned animal_count = %d, want 3", got)
	}

	// Production path: goat.exited envelope -> registered SM-3 handler -> repository.
	bus := eventbus.NewInProcessBus()
	oblapp.NewGoatExitedHandler(repo).Register(bus)
	if err := bus.Publish(ctx, eventbus.Event{
		ID:         "event-exit-drive-assignment",
		Type:       oblapp.EventGoatExited,
		TenantID:   tenantID,
		Key:        exitingGoat,
		OccurredAt: time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("publish goat.exited: %v", err)
	}

	// (a) the exited animal's open obligations are cancelled, the survivors are untouched.
	for _, id := range []string{exitingDoseA, exitingDoseB} {
		if got := scanStatus(t, ctx, pool, id); got != "canceled" {
			t.Fatalf("exited goat obligation %s status = %s, want canceled", id, got)
		}
	}
	for _, id := range []string{stayingDose, thirdDose} {
		if got := scanStatus(t, ctx, pool, id); got != "scheduled" {
			t.Fatalf("surviving obligation %s status = %s, want scheduled", id, got)
		}
	}

	// (b) the exited animal no longer counts toward the planned drive assignment for that date.
	var animals, doses int
	if err := pool.QueryRow(ctx, `
SELECT animal_count, total_doses
FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID).Scan(&animals, &doses); err != nil {
		t.Fatalf("read drive assignment after exit: %v", err)
	}
	if animals != 2 {
		t.Fatalf("planned drive assignment animal_count = %d after goat.exited, want 2 (the dead animal must not occupy drive capacity)", animals)
	}
	if doses != 2 {
		t.Fatalf("planned drive assignment total_doses = %d after goat.exited, want 2 (both of the dead animal's doses must be removed)", doses)
	}

	// Park/date operator capacity ledger must agree: two live animals planned, not three.
	cells, err := repo.CountDriveCellsForParkDate(ctx, tenantID, cbePark, planned)
	if err != nil {
		t.Fatalf("CountDriveCellsForParkDate: %v", err)
	}
	if cells != 2 {
		t.Fatalf("park/date drive animal cells = %d, want 2", cells)
	}

	// Replay-safe: a duplicate goat.exited delivery must not decrement the drive again.
	if err := bus.Publish(ctx, eventbus.Event{
		ID:         "event-exit-drive-assignment",
		Type:       oblapp.EventGoatExited,
		TenantID:   tenantID,
		Key:        exitingGoat,
		OccurredAt: time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("republish goat.exited: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT animal_count, total_doses
FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID).Scan(&animals, &doses); err != nil {
		t.Fatalf("read drive assignment after replay: %v", err)
	}
	if animals != 2 || doses != 2 {
		t.Fatalf("after replay animal_count=%d total_doses=%d, want 2/2 (SM-3 re-delivery must be idempotent)", animals, doses)
	}
}

// TestGoatExitedDeletesEmptiedPlannedDriveAssignment proves the boundary case: when the exited
// animal was the ONLY animal on a shed's planned drive assignment row, that row must not survive
// as a zero-animal phantom drive on the operator's day. A 0-animal assignment row still renders as
// planned work on the vaccination execution/shed screens and still names an operator for a date.
func TestGoatExitedDeletesEmptiedPlannedDriveAssignment(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.exitassignsolo", 1)

	const (
		soloGoat   = "10000000-0000-4000-8000-00000000fb01"
		shedID     = "00000000-0000-4000-8000-00000000db01"
		operatorID = "20000000-0000-4000-8000-000000000b01"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "goat-exit-solo-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, soloGoat)
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
VALUES ($1, $2, 'OP-SOLO', 'Solo Drive Operator', 'active', 'operator')`, operatorID, tenantID); err != nil {
		t.Fatalf("seed operator: %v", err)
	}

	planned := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	_, batchID := seedShotOnDate(t, ctx, repo, versions[0], soloGoat, "shed", shedID, planned, planned, "exit-solo-dose")

	operator := operatorID
	shed := shedID
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: batchID, PlannedDate: planned, OperatorID: &operator, ParkID: cbePark, ShedID: &shed,
		PhysicalShed: "Godel 1", PartitionLabel: "Part 3", AnimalCount: 1,
		VaccineRuleIDs: []string{versions[0].ruleID}, TotalDoses: 1, CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed drive assignment: %v", err)
	}

	bus := eventbus.NewInProcessBus()
	oblapp.NewGoatExitedHandler(repo).Register(bus)
	if err := bus.Publish(ctx, eventbus.Event{
		ID: "event-exit-solo", Type: oblapp.EventGoatExited, TenantID: tenantID, Key: soloGoat,
		OccurredAt: time.Date(2026, 8, 2, 6, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("publish goat.exited: %v", err)
	}

	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != 0 {
		t.Fatalf("emptied drive assignment rows = %d, want 0 (a zero-animal drive row is phantom planned work)", got)
	}
}

// TestGoatExitedDecrementsOnlyTheAnimalsOwnDriveAssignmentRow is the grain proof for the SM-3
// planned-drive removal. vaccination_drive_assignments is keyed by
// (batch, planned_date, park, shed, physical_shed, partition_label, operator) -- ONE batch/shed can
// therefore hold SEVERAL assignment rows that differ by partition, operator, date, and vaccine
// rules. An exiting animal lives in exactly ONE of them. Decrementing by (batch, shed) alone
// subtracts one animal from every sibling row, silently deleting live animals from other
// operators' routes and other dates.
//
// Fixture: one batch, one shed, three assignment rows --
//
//	R1 partition "A", operator 1, 2026-09-10, rule A  (the exiting animal's row)
//	R2 partition "B", operator 2, 2026-09-11, rule A  (different partition + operator + date)
//	R3 partition "A", operator 2, 2026-09-10, rule B  (same partition, different vaccine)
//
// The exiting goat is registered in partition "A" and holds one open rule-A obligation, so ONLY
// R1 may lose an animal.
func TestGoatExitedDecrementsOnlyTheAnimalsOwnDriveAssignmentRow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const (
		exitingGoat = "10000000-0000-4000-8000-00000000fc01"
		shedID      = "00000000-0000-4000-8000-00000000dc01"
		operatorOne = "20000000-0000-4000-8000-000000000c01"
		operatorTwo = "20000000-0000-4000-8000-000000000c02"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "goat-exit-grain-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, exitingGoat)
	for id, code := range map[string]string{operatorOne: "OP-GRAIN-1", operatorTwo: "OP-GRAIN-2"} {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
VALUES ($1, $2, $3, $3, 'active', 'operator')`, id, tenantID, code); err != nil {
			t.Fatalf("seed operator %s: %v", code, err)
		}
	}
	// The exiting animal physically sits in partition "A" of the shed.
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1, $2, $3, 'A', 'Gandhi A')`, tenantID, exitingGoat, shedID); err != nil {
		t.Fatalf("seed goat partition: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.exitgrain", Name: "Exit grain", Category: "vaccination", Status: "draft",
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
	newRule := func(code string, seq int32) string {
		t.Helper()
		id, err := proto.CreateRule(ctx, protodomain.NewRule{
			TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: code, Sequence: seq,
			TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
			EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("rule %s: %v", code, err)
		}
		return id
	}
	ruleA := newRule("primary", 1)
	ruleB := newRule("booster", 2)

	planned := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	obligationID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleA,
		TargetType: "goat", TargetID: exitingGoat, ScopeType: "shed", ScopeID: shedID,
		DueAt: planned, Status: "scheduled", IdempotencyKey: "exit-grain-dose-a", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert obligation: applied=%v err=%v", applied, err)
	}
	opOne := operatorOne
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "exit-grain-drive", PlannedDate: &planned, Status: "planned",
		EstimatedTargets: 12, PlannedQuantity: "12", QuantityUnit: "dose", ConductedBy: &opOne,
	}, []string{obligationID})
	if err != nil || attached != 1 {
		t.Fatalf("create batch: attached=%d err=%v", attached, err)
	}

	shed := shedID
	opTwo := operatorTwo
	nextDay := planned.AddDate(0, 0, 1)
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{
		{BatchID: batchID, PlannedDate: planned, OperatorID: &opOne, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "A", AnimalCount: 5,
			VaccineRuleIDs: []string{ruleA}, TotalDoses: 5, CapacityStatus: "within_cap"},
		{BatchID: batchID, PlannedDate: nextDay, OperatorID: &opTwo, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "B", AnimalCount: 4,
			VaccineRuleIDs: []string{ruleA}, TotalDoses: 4, CapacityStatus: "within_cap"},
		{BatchID: batchID, PlannedDate: planned, OperatorID: &opTwo, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "A", AnimalCount: 3,
			VaccineRuleIDs: []string{ruleB}, TotalDoses: 3, CapacityStatus: "within_cap"},
	}); err != nil {
		t.Fatalf("seed drive assignments: %v", err)
	}

	bus := eventbus.NewInProcessBus()
	oblapp.NewGoatExitedHandler(repo).Register(bus)
	if err := bus.Publish(ctx, eventbus.Event{
		ID: "event-exit-grain", Type: oblapp.EventGoatExited, TenantID: tenantID, Key: exitingGoat,
		OccurredAt: time.Date(2026, 9, 1, 6, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("publish goat.exited: %v", err)
	}

	read := func(partition string, date time.Time, operator string) (int, int) {
		t.Helper()
		var animals, doses int
		if err := pool.QueryRow(ctx, `
SELECT animal_count, total_doses
FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2 AND partition_label=$3 AND planned_date=$4 AND operator_id=$5`,
			tenantID, batchID, partition, date, operator).Scan(&animals, &doses); err != nil {
			t.Fatalf("read assignment %s/%s/%s: %v", partition, date.Format("2006-01-02"), operator, err)
		}
		return animals, doses
	}
	if animals, doses := read("A", planned, operatorOne); animals != 4 || doses != 4 {
		t.Fatalf("own row animal_count/total_doses = %d/%d, want 4/4", animals, doses)
	}
	if animals, doses := read("B", nextDay, operatorTwo); animals != 4 || doses != 4 {
		t.Fatalf("sibling row (other partition/operator/date) = %d/%d, want 4/4 untouched", animals, doses)
	}
	if animals, doses := read("A", planned, operatorTwo); animals != 3 || doses != 3 {
		t.Fatalf("sibling row (other vaccine) = %d/%d, want 3/3 untouched", animals, doses)
	}
}

// TestGoatExitedDecrementsTheExactMemberDriveAssignmentRow is the EXACTNESS proof for the SM-3
// planned-drive removal. vaccination_drive_assignments is an aggregate row ("Gandhi A, 200 animals,
// Jul 24, Darshan") and, before vaccination_drive_assignment_members existed, it did not store WHICH
// animals it covered. When the planner splits one shed/partition across two dates/operators, the
// (batch, shed) bucket holds several rows that are IDENTICAL on every goat-bindable dimension the
// old heuristic could see (same partition, same vaccine rules) and differ only by planned_date and
// operator. The heuristic then picks the earliest planned_date -- deterministic, but not provable,
// and wrong whenever the exiting animal was actually planned into the LATER arm.
//
// Fixture: one batch, one shed, partition "A", rule A, split across two arms --
//
//	R_EARLY 2026-10-05, operator 1, 5 animals  (what the earliest-date heuristic would pick)
//	R_LATE  2026-10-06, operator 2, 4 animals  (where the exiting animal is ACTUALLY a member)
//
// The exiting goat's membership row points at R_LATE. Only R_LATE may lose an animal, and the
// membership row must be gone afterwards so the exact ledger never keeps a dead animal.
func TestGoatExitedDecrementsTheExactMemberDriveAssignmentRow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const (
		exitingGoat = "10000000-0000-4000-8000-00000000fd01"
		shedID      = "00000000-0000-4000-8000-00000000dd01"
		operatorOne = "20000000-0000-4000-8000-000000000d01"
		operatorTwo = "20000000-0000-4000-8000-000000000d02"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "goat-exit-member-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, exitingGoat)
	for id, code := range map[string]string{operatorOne: "OP-MEM-1", operatorTwo: "OP-MEM-2"} {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
VALUES ($1, $2, $3, $3, 'active', 'operator')`, id, tenantID, code); err != nil {
			t.Fatalf("seed operator %s: %v", code, err)
		}
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1, $2, $3, 'A', 'Gandhi A')`, tenantID, exitingGoat, shedID); err != nil {
		t.Fatalf("seed goat partition: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.exitmember", Name: "Exit member", Category: "vaccination", Status: "draft",
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
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule A: %v", err)
	}

	early := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	late := early.AddDate(0, 0, 1)
	obligationID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleA,
		TargetType: "goat", TargetID: exitingGoat, ScopeType: "shed", ScopeID: shedID,
		DueAt: late, Status: "scheduled", IdempotencyKey: "exit-member-dose-a", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert obligation: applied=%v err=%v", applied, err)
	}
	opOne := operatorOne
	opTwo := operatorTwo
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "exit-member-drive", PlannedDate: &early, Status: "planned",
		EstimatedTargets: 9, PlannedQuantity: "9", QuantityUnit: "dose", ConductedBy: &opOne,
	}, []string{obligationID})
	if err != nil || attached != 1 {
		t.Fatalf("create batch: attached=%d err=%v", attached, err)
	}

	shed := shedID
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{
		{BatchID: batchID, PlannedDate: early, OperatorID: &opOne, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "A", AnimalCount: 5,
			VaccineRuleIDs: []string{ruleA}, TotalDoses: 5, CapacityStatus: "within_cap"},
		{BatchID: batchID, PlannedDate: late, OperatorID: &opTwo, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "A", AnimalCount: 4,
			VaccineRuleIDs: []string{ruleA}, TotalDoses: 4, CapacityStatus: "within_cap"},
	}); err != nil {
		t.Fatalf("seed drive assignments: %v", err)
	}

	// EXACT membership: the exiting animal is planned into the LATE arm, not the early one the
	// (matched_rules, planned_date, assignment_id) heuristic would otherwise pick.
	var lateAssignmentID string
	if err := pool.QueryRow(ctx, `
SELECT assignment_id::text FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2 AND planned_date=$3`, tenantID, batchID, late).Scan(&lateAssignmentID); err != nil {
		t.Fatalf("read late assignment id: %v", err)
	}
	// The producer already recorded deterministic membership when the assignments were written
	// (it hands goats to split arms in goat_id order). This test deliberately RE-POINTS that
	// membership at the LATE arm so the exiting animal's true assignment disagrees with what the
	// legacy (matched_rules, planned_date, assignment_id) heuristic would pick -- that disagreement
	// is exactly what proves the exit path now follows membership instead of the heuristic.
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (tenant_id, obligation_id) DO UPDATE SET assignment_id = EXCLUDED.assignment_id`,
		tenantID, lateAssignmentID, obligationID, exitingGoat); err != nil {
		t.Fatalf("seed drive assignment membership: %v", err)
	}

	bus := eventbus.NewInProcessBus()
	oblapp.NewGoatExitedHandler(repo).Register(bus)
	if err := bus.Publish(ctx, eventbus.Event{
		ID: "event-exit-member", Type: oblapp.EventGoatExited, TenantID: tenantID, Key: exitingGoat,
		OccurredAt: time.Date(2026, 10, 1, 6, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("publish goat.exited: %v", err)
	}

	read := func(date time.Time) (int, int) {
		t.Helper()
		var animals, doses int
		if err := pool.QueryRow(ctx, `
SELECT animal_count, total_doses FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2 AND planned_date=$3`, tenantID, batchID, date).Scan(&animals, &doses); err != nil {
			t.Fatalf("read assignment %s: %v", date.Format("2006-01-02"), err)
		}
		return animals, doses
	}
	if animals, doses := read(late); animals != 3 || doses != 3 {
		t.Fatalf("EXACT member arm (%s) animal_count/total_doses = %d/%d, want 3/3 -- the arm the animal is actually a member of must be the one decremented",
			late.Format("2006-01-02"), animals, doses)
	}
	if animals, doses := read(early); animals != 5 || doses != 5 {
		t.Fatalf("non-member arm (%s) = %d/%d, want 5/5 untouched -- the earliest-date heuristic must not win over exact membership",
			early.Format("2006-01-02"), animals, doses)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM vaccination_drive_assignment_members WHERE tenant_id=$1 AND goat_id=$2`, tenantID, exitingGoat); got != 0 {
		t.Fatalf("membership rows for exited animal = %d, want 0 (the exact ledger must not keep a dead animal)", got)
	}
}
