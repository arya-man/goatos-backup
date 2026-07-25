package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// Adversarial dimension coverage for the SM-3 planned-drive removal aggregate
// (removeGoatFromDriveAssignmentsTx): cardinality, page boundary, date, scope and status. Each test
// exercises the REAL production path -- a goat.exited envelope through the registered handler.

// exitDimensionRules creates a protocol version with n rules and returns (versionID, ruleIDs).
func exitDimensionRules(t *testing.T, ctx context.Context, proto *protopg.Repository, code string, n int) (string, []string) {
	t.Helper()
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: code, Name: code, Category: "vaccination", Status: "draft",
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
	ruleIDs := make([]string, 0, n)
	for i := 0; i < n; i++ {
		ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
			TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: fmt.Sprintf("dose-%d", i+1), Sequence: int32(i + 1),
			TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
			EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("rule %d: %v", i, err)
		}
		ruleIDs = append(ruleIDs, ruleID)
	}
	return versionID, ruleIDs
}

func exitDimensionOperators(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ids map[string]string) {
	t.Helper()
	for id, code := range ids {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
VALUES ($1, $2, $3, $3, 'active', 'operator')`, id, tenantID, code); err != nil {
			t.Fatalf("seed operator %s: %v", code, err)
		}
	}
}

func publishExit(t *testing.T, ctx context.Context, repo *Repository, eventID, goatID string) {
	t.Helper()
	bus := eventbus.NewInProcessBus()
	oblapp.NewGoatExitedHandler(repo).Register(bus)
	if err := bus.Publish(ctx, eventbus.Event{
		ID: eventID, Type: oblapp.EventGoatExited, TenantID: tenantID, Key: goatID,
		OccurredAt: time.Date(2026, 6, 15, 6, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("publish goat.exited: %v", err)
	}
}

func assignmentIDForPartition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batchID, partition string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
SELECT assignment_id::text FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2 AND partition_label=$3`, tenantID, batchID, partition).Scan(&id); err != nil {
		t.Fatalf("read assignment id for partition %s: %v", partition, err)
	}
	return id
}

func bindMember(t *testing.T, ctx context.Context, pool *pgxpool.Pool, assignmentID, obligationID, goatID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (tenant_id, obligation_id) DO UPDATE SET assignment_id = EXCLUDED.assignment_id`,
		tenantID, assignmentID, obligationID, goatID); err != nil {
		t.Fatalf("bind membership: %v", err)
	}
}

func readAssignmentByPartition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batchID, partition string) (int, int) {
	t.Helper()
	var animals, doses int
	if err := pool.QueryRow(ctx, `
SELECT animal_count, total_doses FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2 AND partition_label=$3`, tenantID, batchID, partition).Scan(&animals, &doses); err != nil {
		t.Fatalf("read assignment %s: %v", partition, err)
	}
	return animals, doses
}

// TestGoatExitDriveRemovalOneToManyDosesPerAnimal is the CARDINALITY proof. The member ledger's
// grain is one row per OBLIGATION, while animal_count's grain is one per ANIMAL: a goat due three
// vaccines in one drive contributes THREE member rows but only ONE animal. The decrement must
// subtract one animal and count(DISTINCT rule_id) doses -- never one animal per obligation.
func TestGoatExitDriveRemovalOneToManyDosesPerAnimal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	const (
		exitingGoat = "10000000-0000-4000-8000-0000000aa101"
		shedID      = "00000000-0000-4000-8000-0000000ba101"
		operatorID  = "20000000-0000-4000-8000-0000000ca101"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "exit-dim-cardinality-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, exitingGoat)
	exitDimensionOperators(t, ctx, pool, map[string]string{operatorID: "OP-DIM-CARD"})
	versionID, rules := exitDimensionRules(t, ctx, proto, "vaccination.exitdimcard", 3)

	planned := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	obligationIDs := make([]string, 0, 3)
	for i, ruleID := range rules {
		id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: exitingGoat, ScopeType: "shed", ScopeID: shedID,
			DueAt: planned, Status: "scheduled", IdempotencyKey: fmt.Sprintf("exit-dim-card-%d", i), Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("insert obligation %d: applied=%v err=%v", i, applied, err)
		}
		obligationIDs = append(obligationIDs, id)
	}
	operator := operatorID
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "exit-dim-card", PlannedDate: &planned, Status: "planned",
		EstimatedTargets: 4, PlannedQuantity: "9", QuantityUnit: "dose", ConductedBy: &operator,
	}, obligationIDs)
	if err != nil || attached != 3 {
		t.Fatalf("create batch: attached=%d err=%v", attached, err)
	}

	shed := shedID
	// Four animals, each due all three vaccines: 4 animals / 12 doses.
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: batchID, PlannedDate: planned, OperatorID: &operator, ParkID: cbePark, ShedID: &shed,
		PhysicalShed: "Gandhi", PartitionLabel: "A", AnimalCount: 4,
		VaccineRuleIDs: rules, TotalDoses: 12, CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed drive assignment: %v", err)
	}
	assignmentID := assignmentIDForPartition(t, ctx, pool, batchID, "A")
	for _, obligationID := range obligationIDs {
		bindMember(t, ctx, pool, assignmentID, obligationID, exitingGoat)
	}

	publishExit(t, ctx, repo, "event-exit-dim-card", exitingGoat)

	animals, doses := readAssignmentByPartition(t, ctx, pool, batchID, "A")
	if animals != 3 {
		t.Fatalf("animal_count = %d, want 3 -- three member rows for ONE animal must subtract one animal, not three", animals)
	}
	if doses != 9 {
		t.Fatalf("total_doses = %d, want 9 (three distinct rules removed)", doses)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM vaccination_drive_assignment_members WHERE tenant_id=$1 AND goat_id=$2`, tenantID, exitingGoat); got != 0 {
		t.Fatalf("member rows for exited animal = %d, want 0", got)
	}
}

// TestGoatExitDriveRemovalPageBoundaryAcrossManyAssignments is the PAGINATION proof: the removal is
// one set-based statement over the animal's own canceled obligations, so it must never be truncated
// by a page size or LIMIT. An animal that is a member of MANY assignment rows must lose its animal
// from EVERY one of them, not just the first page.
func TestGoatExitDriveRemovalPageBoundaryAcrossManyAssignments(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	const (
		exitingGoat = "10000000-0000-4000-8000-0000000aa201"
		shedID      = "00000000-0000-4000-8000-0000000ba201"
		operatorID  = "20000000-0000-4000-8000-0000000ca201"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "exit-dim-page-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, exitingGoat)
	exitDimensionOperators(t, ctx, pool, map[string]string{operatorID: "OP-DIM-PAGE"})

	const arms = 25
	versionID, rules := exitDimensionRules(t, ctx, proto, "vaccination.exitdimpage", arms)

	planned := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	obligationIDs := make([]string, 0, arms)
	for i, ruleID := range rules {
		id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: exitingGoat, ScopeType: "shed", ScopeID: shedID,
			DueAt: planned, Status: "scheduled", IdempotencyKey: fmt.Sprintf("exit-dim-page-%d", i), Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("insert obligation %d: applied=%v err=%v", i, applied, err)
		}
		obligationIDs = append(obligationIDs, id)
	}
	operator := operatorID
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "exit-dim-page", PlannedDate: &planned, Status: "planned",
		EstimatedTargets: 50, PlannedQuantity: "50", QuantityUnit: "dose", ConductedBy: &operator,
	}, obligationIDs)
	if err != nil || attached != arms {
		t.Fatalf("create batch: attached=%d err=%v", attached, err)
	}

	shed := shedID
	// One assignment arm per vaccine (distinct partition labels keep them distinct rows).
	rowsToSeed := make([]domain.DriveAssignment, 0, arms)
	for i, ruleID := range rules {
		rowsToSeed = append(rowsToSeed, domain.DriveAssignment{
			BatchID: batchID, PlannedDate: planned, OperatorID: &operator, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: fmt.Sprintf("P%02d", i), AnimalCount: 3,
			VaccineRuleIDs: []string{ruleID}, TotalDoses: 3, CapacityStatus: "within_cap",
		})
	}
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, rowsToSeed); err != nil {
		t.Fatalf("seed drive assignments: %v", err)
	}
	for i := range rules {
		bindMember(t, ctx, pool,
			assignmentIDForPartition(t, ctx, pool, batchID, fmt.Sprintf("P%02d", i)), obligationIDs[i], exitingGoat)
	}

	publishExit(t, ctx, repo, "event-exit-dim-page", exitingGoat)

	for i := range rules {
		partition := fmt.Sprintf("P%02d", i)
		animals, doses := readAssignmentByPartition(t, ctx, pool, batchID, partition)
		if animals != 2 || doses != 2 {
			t.Fatalf("arm %s = %d animals/%d doses, want 2/2 -- every arm the animal belongs to must be decremented, not just the first page",
				partition, animals, doses)
		}
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM vaccination_drive_assignment_members WHERE tenant_id=$1 AND goat_id=$2`, tenantID, exitingGoat); got != 0 {
		t.Fatalf("member rows for exited animal = %d, want 0 across all arms", got)
	}
}

// TestGoatExitDriveRemovalDateShiftAcrossSplitDates is the DATE proof. When one shed/partition is
// split across two planned_dates, the arms are identical on every other goat-bindable dimension.
// Only the date the animal is actually a member of may lose an animal -- the removal must follow
// membership, never "the earliest planned_date".
func TestGoatExitDriveRemovalDateShiftAcrossSplitDates(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	const (
		exitingGoat = "10000000-0000-4000-8000-0000000aa301"
		shedID      = "00000000-0000-4000-8000-0000000ba301"
		operatorID  = "20000000-0000-4000-8000-0000000ca301"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "exit-dim-date-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, exitingGoat)
	exitDimensionOperators(t, ctx, pool, map[string]string{operatorID: "OP-DIM-DATE"})
	versionID, rules := exitDimensionRules(t, ctx, proto, "vaccination.exitdimdate", 1)

	early := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	late := early.AddDate(0, 0, 1)
	obligationID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: rules[0],
		TargetType: "goat", TargetID: exitingGoat, ScopeType: "shed", ScopeID: shedID,
		DueAt: late, Status: "scheduled", IdempotencyKey: "exit-dim-date", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert obligation: applied=%v err=%v", applied, err)
	}
	operator := operatorID
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "exit-dim-date", PlannedDate: &early, Status: "planned",
		EstimatedTargets: 9, PlannedQuantity: "9", QuantityUnit: "dose", ConductedBy: &operator,
	}, []string{obligationID})
	if err != nil || attached != 1 {
		t.Fatalf("create batch: attached=%d err=%v", attached, err)
	}

	shed := shedID
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{
		{BatchID: batchID, PlannedDate: early, OperatorID: &operator, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "A", AnimalCount: 5,
			VaccineRuleIDs: rules, TotalDoses: 5, CapacityStatus: "within_cap"},
		{BatchID: batchID, PlannedDate: late, OperatorID: &operator, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "B", AnimalCount: 4,
			VaccineRuleIDs: rules, TotalDoses: 4, CapacityStatus: "within_cap"},
	}); err != nil {
		t.Fatalf("seed drive assignments: %v", err)
	}
	bindMember(t, ctx, pool, assignmentIDForPartition(t, ctx, pool, batchID, "B"), obligationID, exitingGoat)

	publishExit(t, ctx, repo, "event-exit-dim-date", exitingGoat)

	if animals, doses := readAssignmentByPartition(t, ctx, pool, batchID, "B"); animals != 3 || doses != 3 {
		t.Fatalf("member arm on the LATE date = %d/%d, want 3/3", animals, doses)
	}
	if animals, doses := readAssignmentByPartition(t, ctx, pool, batchID, "A"); animals != 5 || doses != 5 {
		t.Fatalf("non-member arm on the EARLY date = %d/%d, want 5/5 untouched", animals, doses)
	}
}

// TestGoatExitDriveRemovalParkScopeVersusShedScope is the SCOPE proof. A park-grain obligation
// (scope_type <> 'shed') belongs to an assignment row with shed_id IS NULL, matched through the
// COALESCE nil-uuid sentinel; a shed-grain obligation belongs to a shed_id row. Removing the animal
// from one scope must not touch the other.
func TestGoatExitDriveRemovalParkScopeVersusShedScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	const (
		exitingGoat = "10000000-0000-4000-8000-0000000aa401"
		shedID      = "00000000-0000-4000-8000-0000000ba401"
		operatorID  = "20000000-0000-4000-8000-0000000ca401"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "exit-dim-scope-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, exitingGoat)
	exitDimensionOperators(t, ctx, pool, map[string]string{operatorID: "OP-DIM-SCOPE"})
	versionID, rules := exitDimensionRules(t, ctx, proto, "vaccination.exitdimscope", 2)

	planned := time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC)
	shedObligation, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: rules[0],
		TargetType: "goat", TargetID: exitingGoat, ScopeType: "shed", ScopeID: shedID,
		DueAt: planned, Status: "scheduled", IdempotencyKey: "exit-dim-scope-shed", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert shed obligation: applied=%v err=%v", applied, err)
	}
	parkObligation, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: rules[1],
		TargetType: "goat", TargetID: exitingGoat, ScopeType: "park", ScopeID: cbePark,
		DueAt: planned, Status: "scheduled", IdempotencyKey: "exit-dim-scope-park", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert park obligation: applied=%v err=%v", applied, err)
	}
	operator := operatorID
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "exit-dim-scope", PlannedDate: &planned, Status: "planned",
		EstimatedTargets: 8, PlannedQuantity: "8", QuantityUnit: "dose", ConductedBy: &operator,
	}, []string{shedObligation, parkObligation})
	if err != nil || attached != 2 {
		t.Fatalf("create batch: attached=%d err=%v", attached, err)
	}

	shed := shedID
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{
		{BatchID: batchID, PlannedDate: planned, OperatorID: &operator, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "SHED", AnimalCount: 4,
			VaccineRuleIDs: []string{rules[0]}, TotalDoses: 4, CapacityStatus: "within_cap"},
		{BatchID: batchID, PlannedDate: planned, OperatorID: &operator, ParkID: cbePark, ShedID: nil,
			PhysicalShed: "park", PartitionLabel: "PARK", AnimalCount: 6,
			VaccineRuleIDs: []string{rules[1]}, TotalDoses: 6, CapacityStatus: "within_cap"},
	}); err != nil {
		t.Fatalf("seed drive assignments: %v", err)
	}
	bindMember(t, ctx, pool, assignmentIDForPartition(t, ctx, pool, batchID, "SHED"), shedObligation, exitingGoat)
	bindMember(t, ctx, pool, assignmentIDForPartition(t, ctx, pool, batchID, "PARK"), parkObligation, exitingGoat)

	publishExit(t, ctx, repo, "event-exit-dim-scope", exitingGoat)

	// The animal is one animal in EACH scope's own row, so each loses exactly one.
	if animals, doses := readAssignmentByPartition(t, ctx, pool, batchID, "SHED"); animals != 3 || doses != 3 {
		t.Fatalf("shed-scope arm = %d/%d, want 3/3", animals, doses)
	}
	if animals, doses := readAssignmentByPartition(t, ctx, pool, batchID, "PARK"); animals != 5 || doses != 5 {
		t.Fatalf("park-scope arm (shed_id IS NULL) = %d/%d, want 5/5", animals, doses)
	}
}

// TestGoatExitDriveRemovalStatusMatrixOnlyOpenObligations is the STATUS proof. Only OPEN statuses
// (scheduled/due/in_progress/deferred/missed) are canceled by an exit, so only those may drive the
// planned-drive subtraction. Completed/accepted history must not be canceled, must not subtract an
// animal a second time, and must keep its membership row -- the ledger is the record of who was
// actually on that drive.
func TestGoatExitDriveRemovalStatusMatrixOnlyOpenObligations(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	const (
		exitingGoat = "10000000-0000-4000-8000-0000000aa501"
		shedID      = "00000000-0000-4000-8000-0000000ba501"
		operatorID  = "20000000-0000-4000-8000-0000000ca501"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "exit-dim-status-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, exitingGoat)
	exitDimensionOperators(t, ctx, pool, map[string]string{operatorID: "OP-DIM-STATUS"})
	versionID, rules := exitDimensionRules(t, ctx, proto, "vaccination.exitdimstatus", 2)

	planned := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	openObligation, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: rules[0],
		TargetType: "goat", TargetID: exitingGoat, ScopeType: "shed", ScopeID: shedID,
		DueAt: planned, Status: "scheduled", IdempotencyKey: "exit-dim-status-open", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert open obligation: applied=%v err=%v", applied, err)
	}
	doneObligation, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: rules[1],
		TargetType: "goat", TargetID: exitingGoat, ScopeType: "shed", ScopeID: shedID,
		DueAt: planned, Status: "scheduled", IdempotencyKey: "exit-dim-status-done", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert done obligation: applied=%v err=%v", applied, err)
	}
	operator := operatorID
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "exit-dim-status", PlannedDate: &planned, Status: "planned",
		EstimatedTargets: 5, PlannedQuantity: "5", QuantityUnit: "dose", ConductedBy: &operator,
	}, []string{openObligation, doneObligation})
	if err != nil || attached != 2 {
		t.Fatalf("create batch: attached=%d err=%v", attached, err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances SET status='completed' WHERE tenant_id=$1 AND obligation_id=$2`,
		tenantID, doneObligation); err != nil {
		t.Fatalf("complete obligation: %v", err)
	}

	shed := shedID
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: batchID, PlannedDate: planned, OperatorID: &operator, ParkID: cbePark, ShedID: &shed,
		PhysicalShed: "Gandhi", PartitionLabel: "A", AnimalCount: 4,
		VaccineRuleIDs: rules, TotalDoses: 8, CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed drive assignment: %v", err)
	}
	assignmentID := assignmentIDForPartition(t, ctx, pool, batchID, "A")
	bindMember(t, ctx, pool, assignmentID, openObligation, exitingGoat)
	bindMember(t, ctx, pool, assignmentID, doneObligation, exitingGoat)

	publishExit(t, ctx, repo, "event-exit-dim-status", exitingGoat)

	if got := scanStatus(t, ctx, pool, openObligation); got != "canceled" {
		t.Fatalf("open obligation status = %s, want canceled", got)
	}
	if got := scanStatus(t, ctx, pool, doneObligation); got != "completed" {
		t.Fatalf("completed obligation status = %s, want completed (history is never touched)", got)
	}
	animals, doses := readAssignmentByPartition(t, ctx, pool, batchID, "A")
	if animals != 3 {
		t.Fatalf("animal_count = %d, want 3 -- the animal is subtracted once, not once per obligation status", animals)
	}
	if doses != 7 {
		t.Fatalf("total_doses = %d, want 7 (only the ONE canceled open dose is removed)", doses)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM vaccination_drive_assignment_members WHERE tenant_id=$1 AND obligation_id=$2`,
		tenantID, doneObligation); got != 1 {
		t.Fatalf("membership row for the COMPLETED obligation = %d, want 1 (the ledger records who was actually on that drive)", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM vaccination_drive_assignment_members WHERE tenant_id=$1 AND obligation_id=$2`,
		tenantID, openObligation); got != 0 {
		t.Fatalf("membership row for the canceled obligation = %d, want 0", got)
	}
}
