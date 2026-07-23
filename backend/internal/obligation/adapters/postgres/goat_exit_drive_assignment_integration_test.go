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
