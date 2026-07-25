package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// TestDriveAssignmentMembershipIsStableAcrossRecomputes is the BUG-035 regression proof.
//
// vaccination_drive_assignment_members binds ONE goat/obligation to ONE assignment row (an
// operator's day on a physical partition). When a batch/shed holds several assignment cells that
// plan the SAME vaccine but differ by partition, operator, or date, the binding used to be ranked
// by assignment_id -- a random UUID -- so the same fixture bound the same goat to a DIFFERENT
// operator's arm from run to run. Downstream that means the animal's exit decrements a stranger's
// route, and CT/PA/WF/AC report the wrong operator-day for a named animal.
//
// This test recomputes membership repeatedly with IDENTICAL business inputs, regenerating the
// assignment rows (and therefore their random assignment_ids) each round, and asserts the goat
// always lands on its OWN partition's cell.
func TestDriveAssignmentMembershipIsStableAcrossRecomputes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	const (
		goatID      = "10000000-0000-4000-8000-00000000fd01"
		shedID      = "00000000-0000-4000-8000-00000000dd01"
		operatorOne = "20000000-0000-4000-8000-000000000d01"
		operatorTwo = "20000000-0000-4000-8000-000000000d02"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "membership-stability-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)
	for id, code := range map[string]string{operatorOne: "OP-STAB-1", operatorTwo: "OP-STAB-2"} {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
VALUES ($1, $2, $3, $3, 'active', 'operator')`, id, tenantID, code); err != nil {
			t.Fatalf("seed operator %s: %v", code, err)
		}
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1, $2, $3, 'A', 'Gandhi A')`, tenantID, goatID, shedID); err != nil {
		t.Fatalf("seed goat partition: %v", err)
	}

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.memberstability", Name: "Member stability", Category: "vaccination", Status: "draft",
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
		t.Fatalf("rule: %v", err)
	}

	planned := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	obligationID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleA,
		TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
		DueAt: planned, Status: "scheduled", IdempotencyKey: "membership-stability-dose", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert obligation: applied=%v err=%v", applied, err)
	}
	opOne := operatorOne
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "membership-stability-drive", PlannedDate: &planned, Status: "planned",
		EstimatedTargets: 9, PlannedQuantity: "9", QuantityUnit: "dose", ConductedBy: &opOne,
	}, []string{obligationID})
	if err != nil || attached != 1 {
		t.Fatalf("create batch: attached=%d err=%v", attached, err)
	}

	shed := shedID
	opTwo := operatorTwo
	nextDay := planned.AddDate(0, 0, 1)
	// Two cells plan the SAME vaccine: the goat's own partition "A" (operator one, day 1) and a
	// sibling partition "B" (operator two, day 2). Only partition "A" is where the animal is.
	assignments := []domain.DriveAssignment{
		{BatchID: batchID, PlannedDate: planned, OperatorID: &opOne, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "A", AnimalCount: 5,
			VaccineRuleIDs: []string{ruleA}, TotalDoses: 5, CapacityStatus: "within_cap"},
		{BatchID: batchID, PlannedDate: nextDay, OperatorID: &opTwo, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "B", AnimalCount: 4,
			VaccineRuleIDs: []string{ruleA}, TotalDoses: 4, CapacityStatus: "within_cap"},
	}

	const rounds = 8
	for round := 1; round <= rounds; round++ {
		// Drop and rewrite the cells so every round mints FRESH random assignment_ids: any ranking
		// that depends on those UUIDs will flip within a handful of rounds.
		if _, err := pool.Exec(ctx,
			`DELETE FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); err != nil {
			t.Fatalf("round %d: clear assignments: %v", round, err)
		}
		if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, assignments); err != nil {
			t.Fatalf("round %d: upsert assignments: %v", round, err)
		}

		var partition, operator string
		var boundDate time.Time
		if err := pool.QueryRow(ctx, `
SELECT vda.partition_label, vda.operator_id::text, vda.planned_date
FROM vaccination_drive_assignment_members m
JOIN vaccination_drive_assignments vda
  ON vda.tenant_id = m.tenant_id AND vda.assignment_id = m.assignment_id
WHERE m.tenant_id=$1 AND m.obligation_id=$2`, tenantID, obligationID).Scan(&partition, &operator, &boundDate); err != nil {
			t.Fatalf("round %d: read membership: %v", round, err)
		}
		if partition != "A" || operator != operatorOne || !boundDate.Equal(planned) {
			t.Fatalf("round %d: goat bound to partition=%s operator=%s date=%s, want A/%s/%s (membership must follow the animal's own placement, not a random assignment_id)",
				round, partition, operator, boundDate.Format("2006-01-02"), operatorOne, planned.Format("2006-01-02"))
		}
	}
}

// TestDriveAssignmentMembershipRespectsVaccineLane is the BUG-039 regression proof.
//
// Two assignment rows can share ONE batch/park/shed/physical-shed/partition cell and differ only by
// which vaccine they plan (vaccine_rule_ids). The membership derivation established that vaccine
// dimension in its candidate filter and then dropped it: the animal_count split counters, the goat
// rank, and the final join all keyed on location alone, so an obligation for vaccine X could bind to
// the assignment row that plans vaccine Y. vaccination_drive_assignment_members is the provable
// goat -> operator-day -> vaccine binding that exit/defer/shed-shift decrements trust, so a mis-bound
// member removes a real animal from the WRONG operator's route while it stays counted on the right one.
//
// The subtests are the adversarial axes: obligation fan-out per goat (OneToMany), membership size far
// past any page (PageBoundary), lanes on shifted dates (DateShift), park-grain vs shed-grain cells
// (ScopeHierarchy), and the live status matrix including canceled (StatusMatrix).
func TestDriveAssignmentMembershipRespectsVaccineLane(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	env := newLaneEnv(t, ctx, pool, proto, repo)

	t.Run("OneToMany", func(t *testing.T) {
		// One goat carrying an obligation for EACH vaccine: the member grain is obligation, so the
		// goat fans out to two rows that must land on two DIFFERENT assignment rows.
		f := env.scenario(t, "onetomany", 1)
		obligations := []string{
			f.obligation(t, env.ruleA, "lane-otm-a", 1, f.goats[0]),
			f.obligation(t, env.ruleB, "lane-otm-b", 2, f.goats[0]),
		}
		batchID := f.batch(t, "lane-otm", obligations)
		f.upsert(t, batchID,
			f.cell(env.ruleA, f.planned, env.operatorOne, 1),
			f.cell(env.ruleB, f.planned, env.operatorTwo, 1),
		)
		env.assertLaneBinding(t, batchID, 2)
	})

	t.Run("PageBoundary", func(t *testing.T) {
		// Membership is recomputed set-based for the whole batch; no page size may truncate it, and
		// the per-lane rank window must stay independent of the other lane's counters.
		const goats = 60
		f := env.scenario(t, "pageboundary", goats)
		obligations := make([]string, 0, goats*2)
		for i, goatID := range f.goats {
			obligations = append(obligations,
				f.obligation(t, env.ruleA, fmt.Sprintf("lane-page-a-%d", i), int32(2*i+1), goatID),
				f.obligation(t, env.ruleB, fmt.Sprintf("lane-page-b-%d", i), int32(2*i+2), goatID))
		}
		batchID := f.batch(t, "lane-page", obligations)
		f.upsert(t, batchID,
			f.cell(env.ruleA, f.planned, env.operatorOne, goats),
			f.cell(env.ruleB, f.planned, env.operatorTwo, goats),
		)
		env.assertLaneBinding(t, batchID, goats*2)
	})

	t.Run("DateShift", func(t *testing.T) {
		// The two lanes execute on different days. Binding must follow the vaccine, not the earlier
		// date, so the animal's exit decrements the day that actually carries that vaccine.
		f := env.scenario(t, "dateshift", 2)
		obligations := []string{
			f.obligation(t, env.ruleA, "lane-date-a1", 1, f.goats[0]),
			f.obligation(t, env.ruleB, "lane-date-b1", 2, f.goats[0]),
			f.obligation(t, env.ruleA, "lane-date-a2", 3, f.goats[1]),
			f.obligation(t, env.ruleB, "lane-date-b2", 4, f.goats[1]),
		}
		batchID := f.batch(t, "lane-date", obligations)
		f.upsert(t, batchID,
			f.cell(env.ruleA, f.planned, env.operatorOne, 2),
			f.cell(env.ruleB, f.planned.AddDate(0, 0, 1), env.operatorOne, 2),
		)
		env.assertLaneBinding(t, batchID, 4)
	})

	t.Run("ScopeHierarchy", func(t *testing.T) {
		// Park-grain cells (shed_id IS NULL) take the batch's non-shed-scoped obligations. The lane
		// key must hold at that grain too, not only at shed grain.
		f := env.scenario(t, "scopehierarchy", 2)
		f.scopeType, f.scopeID = "park", cbePark
		obligations := []string{
			f.obligation(t, env.ruleA, "lane-scope-a1", 1, f.goats[0]),
			f.obligation(t, env.ruleB, "lane-scope-b1", 2, f.goats[0]),
			f.obligation(t, env.ruleA, "lane-scope-a2", 3, f.goats[1]),
			f.obligation(t, env.ruleB, "lane-scope-b2", 4, f.goats[1]),
		}
		batchID := f.batch(t, "lane-scope", obligations)
		parkA := f.cell(env.ruleA, f.planned, env.operatorOne, 2)
		parkA.ShedID, parkA.PhysicalShed, parkA.PartitionLabel = nil, "", ""
		parkB := f.cell(env.ruleB, f.planned, env.operatorTwo, 2)
		parkB.ShedID, parkB.PhysicalShed, parkB.PartitionLabel = nil, "", ""
		f.upsert(t, batchID, parkA, parkB)
		env.assertLaneBinding(t, batchID, 4)
	})

	t.Run("StatusMatrix", func(t *testing.T) {
		// Every live status participates; only 'canceled' is excluded. A canceled obligation must not
		// consume a lane slot, and the surviving statuses must still bind to their own vaccine.
		f := env.scenario(t, "statusmatrix", 3)
		type row struct {
			id     string
			status string
		}
		rows := []row{
			{f.obligation(t, env.ruleA, "lane-status-a1", 1, f.goats[0]), "scheduled"},
			{f.obligation(t, env.ruleB, "lane-status-b1", 2, f.goats[0]), "due"},
			{f.obligation(t, env.ruleA, "lane-status-a2", 3, f.goats[1]), "in_progress"},
			{f.obligation(t, env.ruleB, "lane-status-b2", 4, f.goats[1]), "completed"},
			{f.obligation(t, env.ruleA, "lane-status-a3", 5, f.goats[2]), "canceled"},
			{f.obligation(t, env.ruleB, "lane-status-b3", 6, f.goats[2]), "canceled"},
		}
		ids := make([]string, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.id)
		}
		batchID := f.batch(t, "lane-status", ids)
		for _, r := range rows {
			if r.status == "scheduled" {
				continue
			}
			if _, err := pool.Exec(ctx,
				`UPDATE obligation_instances SET status=$3 WHERE tenant_id=$1 AND obligation_id=$2`,
				tenantID, r.id, r.status); err != nil {
				t.Fatalf("set status %s: %v", r.status, err)
			}
		}
		f.upsert(t, batchID,
			f.cell(env.ruleA, f.planned, env.operatorOne, 2),
			f.cell(env.ruleB, f.planned, env.operatorTwo, 2),
		)
		env.assertLaneBinding(t, batchID, 4)
	})
}

// laneEnv holds the protocol/version/rules and operators shared by every vaccine-lane subtest.
type laneEnv struct {
	ctx         context.Context
	pool        *pgxpool.Pool
	repo        *Repository
	proto       *protopg.Repository
	versionID   string
	ruleA       string
	ruleB       string
	operatorOne string
	operatorTwo string
	// seq isolates each subtest's shed/goat ids; never derive ids from the name (two names of equal
	// length then collide on goats_pkey).
	seq int
}

func newLaneEnv(t *testing.T, ctx context.Context, pool *pgxpool.Pool, proto *protopg.Repository, repo *Repository) *laneEnv {
	t.Helper()
	env := &laneEnv{
		ctx: ctx, pool: pool, repo: repo, proto: proto,
		operatorOne: "20000000-0000-4000-8000-000000000e01",
		operatorTwo: "20000000-0000-4000-8000-000000000e02",
	}
	for id, code := range map[string]string{env.operatorOne: "OP-LANE-1", env.operatorTwo: "OP-LANE-2"} {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
VALUES ($1, $2, $3, $3, 'active', 'operator')`, id, tenantID, code); err != nil {
			t.Fatalf("seed operator %s: %v", code, err)
		}
	}
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.memberlane", Name: "Member lane", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	env.versionID, err = proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	newRule := func(dose string, seq int32) string {
		id, err := proto.CreateRule(ctx, protodomain.NewRule{
			TenantID: tenantID, ProtocolVersionID: env.versionID, DoseCode: dose, Sequence: seq,
			TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
			EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("rule %s: %v", dose, err)
		}
		return id
	}
	env.ruleA = newRule("et_tt_adult_w1", 1)
	env.ruleB = newRule("ppr_booster", 2)
	return env
}

// laneFixture is one subtest's isolated shed + goats.
type laneFixture struct {
	env       *laneEnv
	t         *testing.T
	shedID    string
	goats     []string
	planned   time.Time
	scopeType string
	scopeID   string
}

func (e *laneEnv) scenario(t *testing.T, name string, goatCount int) *laneFixture {
	t.Helper()
	e.seq++
	seq := e.seq
	shedID := fmt.Sprintf("00000000-0000-4000-8000-%012x", 0xde0000+seq)
	seedParkConsolidationShed(t, e.ctx, e.pool, shedID, "membership-lane-"+name)
	goats := make([]string, 0, goatCount)
	for i := 0; i < goatCount; i++ {
		goats = append(goats, fmt.Sprintf("10000000-0000-4000-8000-%012x", seq<<20|i))
	}
	seedReserveGoats(t, e.ctx, e.pool, shedID, cbePark, goats...)
	for _, goatID := range goats {
		if _, err := e.pool.Exec(e.ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1, $2, $3, 'A', 'Gandhi A')`, tenantID, goatID, shedID); err != nil {
			t.Fatalf("seed goat partition: %v", err)
		}
	}
	return &laneFixture{
		env: e, t: t, shedID: shedID, goats: goats,
		planned:   time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC),
		scopeType: "shed", scopeID: shedID,
	}
}

func (f *laneFixture) obligation(t *testing.T, ruleID, key string, seq int32, goatID string) string {
	t.Helper()
	id, applied, err := f.env.repo.InsertObligation(f.env.ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: f.env.versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: f.scopeType, ScopeID: f.scopeID,
		DueAt: f.planned, Status: "scheduled", IdempotencyKey: key, Sequence: seq,
	})
	if err != nil || !applied {
		t.Fatalf("insert obligation %s: applied=%v err=%v", key, applied, err)
	}
	return id
}

func (f *laneFixture) batch(t *testing.T, session string, obligations []string) string {
	t.Helper()
	operator := f.env.operatorOne
	batchID, attached, err := f.env.repo.CreateBatchWithObligations(f.env.ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: f.env.versionID, ScopeType: f.scopeType, ScopeID: f.scopeID,
		Session: session, PlannedDate: &f.planned, Status: "planned",
		EstimatedTargets: int32(len(obligations)), PlannedQuantity: fmt.Sprintf("%d", len(obligations)),
		QuantityUnit: "dose", ConductedBy: &operator,
	}, obligations)
	if err != nil || attached != int64(len(obligations)) {
		t.Fatalf("create batch: attached=%d want=%d err=%v", attached, len(obligations), err)
	}
	return batchID
}

func (f *laneFixture) cell(ruleID string, date time.Time, operatorID string, animals int32) domain.DriveAssignment {
	shed := f.shedID
	op := operatorID
	return domain.DriveAssignment{
		PlannedDate: date, OperatorID: &op, ParkID: cbePark, ShedID: &shed,
		PhysicalShed: "Gandhi", PartitionLabel: "A", AnimalCount: animals,
		VaccineRuleIDs: []string{ruleID}, TotalDoses: animals, CapacityStatus: "within_cap",
	}
}

// upsert rewrites the cells from scratch each round so every round mints FRESH random
// assignment_ids: any binding that leans on those UUIDs flips within a few rounds.
func (f *laneFixture) upsert(t *testing.T, batchID string, cells ...domain.DriveAssignment) {
	t.Helper()
	for i := range cells {
		cells[i].BatchID = batchID
	}
	for round := 1; round <= 4; round++ {
		if _, err := f.env.pool.Exec(f.env.ctx,
			`DELETE FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); err != nil {
			t.Fatalf("round %d: clear assignments: %v", round, err)
		}
		if err := f.env.repo.UpsertVaccinationDriveAssignments(f.env.ctx, tenantID, cells); err != nil {
			t.Fatalf("round %d: upsert assignments: %v", round, err)
		}
	}
}

// assertLaneBinding is the BUG-039 assertion: every member row must sit on the assignment whose
// vaccine_rule_ids contains that obligation's own rule_id, and membership must be complete.
func (e *laneEnv) assertLaneBinding(t *testing.T, batchID string, wantRows int) {
	t.Helper()
	rows, err := e.pool.Query(e.ctx, `
SELECT oi.obligation_id::text, oi.rule_id::text, oi.status, vda.vaccine_rule_ids::text[],
       (oi.rule_id = ANY(vda.vaccine_rule_ids)) AS lane_matches
FROM vaccination_drive_assignment_members m
JOIN vaccination_drive_assignments vda
  ON vda.tenant_id = m.tenant_id AND vda.assignment_id = m.assignment_id
JOIN obligation_instances oi
  ON oi.tenant_id = m.tenant_id AND oi.obligation_id = m.obligation_id
WHERE m.tenant_id=$1 AND oi.batch_id=$2
ORDER BY oi.sequence`, tenantID, batchID)
	if err != nil {
		t.Fatalf("read membership: %v", err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var oblID, ruleID, status string
		var lane []string
		var matches bool
		if err := rows.Scan(&oblID, &ruleID, &status, &lane, &matches); err != nil {
			t.Fatalf("scan: %v", err)
		}
		seen++
		if status == "canceled" {
			t.Fatalf("canceled obligation %s is bound to a drive assignment; canceled work must never consume a lane slot", oblID)
		}
		if !matches {
			t.Fatalf("obligation %s (rule %s, status %s) bound to an assignment planning vaccines %v -- membership must bind an obligation only to the assignment row whose vaccine_rule_ids contains its own rule_id",
				oblID, ruleID, status, lane)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if seen != wantRows {
		t.Fatalf("got %d member rows, want %d", seen, wantRows)
	}
}

// TestDriveAssignmentMembershipCanonicalizesPartitionLabelFormat is the BUG-040 regression proof.
//
// The BUG-035 fix binds each goat to the assignment arm matching its OWN shed partition by comparing
// vaccination_drive_assignments.partition_label against goat_shed_partitions.partition_label. But the
// two writers normalize that label differently: goat placement stores the NORMALIZED form ("1", from
// seed-vaccination-real normalizing "Gandhi 1") while the drive planner writes the DISPLAY form
// ("Part 1"). For numeric-partition sheds the equality never matched, so the partition tiebreak was a
// silent no-op and DISTINCT ON collapsed every partition onto the alphabetically-first arm (the CPT
// reseed proved Gandhi Part 3 goats landing on Part 1). The fix canonicalizes both sides (strips a
// leading "part "). Every subtest below seeds numeric placement labels ("1"/"3") against display cell
// labels ("Part 1"/"Part 3") and asserts each goat binds to its OWN partition arm.
func TestDriveAssignmentMembershipCanonicalizesPartitionLabelFormat(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	env := newLaneEnv(t, ctx, pool, proto, repo)

	// seedPart seeds a fresh shed whose goats carry NUMERIC placement partitions ("1"/"3", one per
	// goat, cycling through parts). The drive cells built later use the DISPLAY form ("Part N").
	seedPart := func(t *testing.T, name string, count int, parts []string) (string, []string, []string) {
		t.Helper()
		env.seq++
		shedID := fmt.Sprintf("00000000-0000-4000-8000-%012x", 0xca0000+env.seq)
		seedParkConsolidationShed(t, ctx, pool, shedID, "partcanon-"+name)
		goats := make([]string, count)
		goatPart := make([]string, count)
		for i := 0; i < count; i++ {
			goats[i] = fmt.Sprintf("10000000-0000-4000-8000-%012x", (0xca<<24)|(env.seq<<8)|i)
			goatPart[i] = parts[i%len(parts)]
		}
		seedReserveGoats(t, ctx, pool, shedID, cbePark, goats...)
		for i, goatID := range goats {
			if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1,$2,$3,$4,'Gandhi '||$4)`, tenantID, goatID, shedID, goatPart[i]); err != nil {
				t.Fatalf("seed partition %s: %v", goatPart[i], err)
			}
		}
		return shedID, goats, goatPart
	}
	obligate := func(t *testing.T, scopeType, scopeID, ruleID, key string, seq int32, goatID string) string {
		t.Helper()
		id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: env.versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: scopeType, ScopeID: scopeID,
			DueAt: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: key, Sequence: seq,
		})
		if err != nil || !applied {
			t.Fatalf("insert obligation %s: applied=%v err=%v", key, applied, err)
		}
		return id
	}
	makeBatch := func(t *testing.T, scopeType, scopeID, session string, obligations []string) string {
		t.Helper()
		planned := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)
		op := env.operatorOne
		batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
			TenantID: tenantID, ProtocolVersionID: env.versionID, ScopeType: scopeType, ScopeID: scopeID,
			Session: session, PlannedDate: &planned, Status: "planned",
			EstimatedTargets: int32(len(obligations)), PlannedQuantity: fmt.Sprintf("%d", len(obligations)),
			QuantityUnit: "dose", ConductedBy: &op,
		}, obligations)
		if err != nil || attached != int64(len(obligations)) {
			t.Fatalf("create batch: attached=%d want=%d err=%v", attached, len(obligations), err)
		}
		return batchID
	}
	partCell := func(shedID, ruleID string, date time.Time, part string, animals int32, operatorID string) domain.DriveAssignment {
		shed := shedID
		op := operatorID
		return domain.DriveAssignment{
			PlannedDate: date, OperatorID: &op, ParkID: cbePark, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: part, AnimalCount: animals,
			VaccineRuleIDs: []string{ruleID}, TotalDoses: animals, CapacityStatus: "within_cap",
		}
	}
	upsert := func(t *testing.T, batchID string, cells ...domain.DriveAssignment) {
		t.Helper()
		for i := range cells {
			cells[i].BatchID = batchID
		}
		// Rewrite the cells several rounds so every round mints fresh random assignment_ids: any binding
		// that leans on the UUID rather than the goat's own partition flips within a few rounds.
		for round := 1; round <= 4; round++ {
			if _, err := pool.Exec(ctx, `DELETE FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); err != nil {
				t.Fatalf("round %d: clear: %v", round, err)
			}
			if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, cells); err != nil {
				t.Fatalf("round %d: upsert: %v", round, err)
			}
		}
	}
	// boundPartition returns the display partition label of the arm an obligation is bound to.
	boundPartition := func(t *testing.T, obligationID string) string {
		t.Helper()
		var got string
		if err := pool.QueryRow(ctx, `
SELECT vda.partition_label
FROM vaccination_drive_assignment_members m
JOIN vaccination_drive_assignments vda ON vda.tenant_id=m.tenant_id AND vda.assignment_id=m.assignment_id
WHERE m.tenant_id=$1 AND m.obligation_id=$2`, tenantID, obligationID).Scan(&got); err != nil {
			t.Fatalf("read membership: %v", err)
		}
		return got
	}
	// canon strips a leading "part " so a numeric placement label matches its display arm.
	canon := func(s string) string {
		low := strings.ToLower(strings.TrimSpace(s))
		return strings.TrimSpace(strings.TrimPrefix(low, "part "))
	}
	assertArmSizing := func(t *testing.T, batchID string, wantArms int) {
		t.Helper()
		rows, err := pool.Query(ctx, `
SELECT vda.assignment_id::text, vda.partition_label, vda.animal_count, count(DISTINCT m.goat_id)
FROM vaccination_drive_assignments vda
LEFT JOIN vaccination_drive_assignment_members m ON m.tenant_id=vda.tenant_id AND m.assignment_id=vda.assignment_id
WHERE vda.tenant_id=$1 AND vda.batch_id=$2
GROUP BY vda.assignment_id, vda.partition_label, vda.animal_count`, tenantID, batchID)
		if err != nil {
			t.Fatalf("arm sizing: %v", err)
		}
		defer rows.Close()
		arms := 0
		for rows.Next() {
			var assignmentID, part string
			var animalCount, members int
			if err := rows.Scan(&assignmentID, &part, &animalCount, &members); err != nil {
				t.Fatalf("scan: %v", err)
			}
			if animalCount != members {
				t.Fatalf("arm %q: animal_count=%d but distinct members=%d (BUG-040 partition collapse)", part, animalCount, members)
			}
			arms++
		}
		if arms != wantArms {
			t.Fatalf("saw %d arms, want %d", arms, wantArms)
		}
	}

	planned := time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)

	t.Run("OneToMany", func(t *testing.T) {
		// One goat in numeric partition "1" carrying an obligation for each vaccine: both member rows
		// must follow the goat to its OWN "Part 1" arm, never split across partitions.
		shedID, goats, _ := seedPart(t, "onetomany", 1, []string{"1"})
		oA := obligate(t, "shed", shedID, env.ruleA, "pc-otm-a", 1, goats[0])
		oB := obligate(t, "shed", shedID, env.ruleB, "pc-otm-b", 2, goats[0])
		batchID := makeBatch(t, "shed", shedID, "pc-otm", []string{oA, oB})
		upsert(t, batchID,
			partCell(shedID, env.ruleA, planned, "Part 1", 1, env.operatorOne),
			partCell(shedID, env.ruleB, planned, "Part 1", 1, env.operatorTwo),
		)
		if got := boundPartition(t, oA); got != "Part 1" {
			t.Fatalf("ruleA bound to %q, want Part 1", got)
		}
		if got := boundPartition(t, oB); got != "Part 1" {
			t.Fatalf("ruleB bound to %q, want Part 1", got)
		}
		assertArmSizing(t, batchID, 2)
	})

	t.Run("PageBoundary", func(t *testing.T) {
		// 40 goats split across numeric partitions "1" and "3"; the whole-batch set-based recompute must
		// place each goat on its own display arm, and neither arm may over- or under-fill.
		const goatCount = 40
		shedID, goats, goatPart := seedPart(t, "pageboundary", goatCount, []string{"1", "3"})
		obls := make([]string, goatCount)
		for i := range goats {
			obls[i] = obligate(t, "shed", shedID, env.ruleA, fmt.Sprintf("pc-page-%d", i), int32(i+1), goats[i])
		}
		batchID := makeBatch(t, "shed", shedID, "pc-page", obls)
		upsert(t, batchID,
			partCell(shedID, env.ruleA, planned, "Part 1", goatCount/2, env.operatorOne),
			partCell(shedID, env.ruleA, planned, "Part 3", goatCount/2, env.operatorOne),
		)
		for i, o := range obls {
			want := "Part " + goatPart[i]
			if got := boundPartition(t, o); got != want {
				t.Fatalf("goat %d (placement %s) bound to %q, want %q", i, goatPart[i], got, want)
			}
		}
		assertArmSizing(t, batchID, 2)
	})

	t.Run("DateShift", func(t *testing.T) {
		// The two partition arms execute on DIFFERENT days. Binding must follow the goat's partition,
		// not the earlier date, so an exit decrements the day that actually carries the animal.
		shedID, goats, goatPart := seedPart(t, "dateshift", 2, []string{"1", "3"})
		obls := []string{
			obligate(t, "shed", shedID, env.ruleA, "pc-date-0", 1, goats[0]),
			obligate(t, "shed", shedID, env.ruleA, "pc-date-1", 2, goats[1]),
		}
		batchID := makeBatch(t, "shed", shedID, "pc-date", obls)
		upsert(t, batchID,
			partCell(shedID, env.ruleA, planned, "Part 1", 1, env.operatorOne),
			partCell(shedID, env.ruleA, planned.AddDate(0, 0, 1), "Part 3", 1, env.operatorOne),
		)
		for i, o := range obls {
			if got := boundPartition(t, o); got != "Part "+goatPart[i] {
				t.Fatalf("goat %d bound to %q, want Part %s", i, got, goatPart[i])
			}
		}
		assertArmSizing(t, batchID, 2)
	})

	t.Run("ScopeHierarchy", func(t *testing.T) {
		// Partition canonicalization is a shed-grain concern; a park-grain cell (no partition) must still
		// bind the batch's non-shed-scoped obligation, and a shed-grain partition arm binds its own goat.
		shedID, goats, _ := seedPart(t, "scopehierarchy", 1, []string{"1"})
		oShed := obligate(t, "shed", shedID, env.ruleA, "pc-scope-shed", 1, goats[0])
		batchID := makeBatch(t, "shed", shedID, "pc-scope", []string{oShed})
		upsert(t, batchID, partCell(shedID, env.ruleA, planned, "Part 1", 1, env.operatorOne))
		if got := boundPartition(t, oShed); got != "Part 1" {
			t.Fatalf("shed-scoped obligation bound to %q, want Part 1", got)
		}
		assertArmSizing(t, batchID, 1)
	})

	t.Run("StatusMatrix", func(t *testing.T) {
		// A canceled obligation must not consume a partition arm slot; the surviving live obligation must
		// still bind to its own numeric partition's display arm.
		shedID, goats, _ := seedPart(t, "statusmatrix", 2, []string{"1", "3"})
		live := obligate(t, "shed", shedID, env.ruleA, "pc-status-live", 1, goats[0])
		dead := obligate(t, "shed", shedID, env.ruleA, "pc-status-dead", 2, goats[1])
		batchID := makeBatch(t, "shed", shedID, "pc-status", []string{live, dead})
		if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='canceled' WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, dead); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		upsert(t, batchID,
			partCell(shedID, env.ruleA, planned, "Part 1", 1, env.operatorOne),
			partCell(shedID, env.ruleA, planned, "Part 3", 0, env.operatorOne),
		)
		if got := boundPartition(t, live); got != "Part 1" {
			t.Fatalf("live obligation bound to %q, want Part 1", got)
		}
		var deadBound int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM vaccination_drive_assignment_members WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, dead).Scan(&deadBound); err != nil {
			t.Fatalf("dead membership: %v", err)
		}
		if deadBound != 0 {
			t.Fatalf("canceled obligation has %d membership rows, want 0", deadBound)
		}
		// canon() keeps the placement/display equivalence explicit for the reviewer.
		if canon("Part 1") != canon("1") {
			t.Fatalf("canonicalization broken: %q != %q", canon("Part 1"), canon("1"))
		}
	})
}
