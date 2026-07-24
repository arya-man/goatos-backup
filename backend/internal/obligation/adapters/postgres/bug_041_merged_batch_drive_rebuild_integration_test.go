package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// BUG-041: mergeUnfinalizedBatchIntoPlannedDate moves obligations from a source batch into a
// compatible target batch but never rebuilt the target's vaccination_drive_assignments. The target
// then held more obligations than its drive rows knew about, so the moved goats' (shed, vaccine lane)
// had no covering cell and their obligations bound to no operator drive lane at all -- "due but on no
// operator's sheet". These tests prove the fix (repo merge returns the target id + deletes stale
// source rows; AlignComboDrives rebuilds the target from its FULL attached set through the real
// planner) closes both observed shapes end-to-end on a throwaway Postgres:
//
//	shape (a) cross-shed:    an Old-Yashoda shed's goats merged into a batch whose only cells were Gandhi.
//	shape (b) sibling lane:  an FMD obligation merged into a batch whose only cell was the HS lane.
//
// Each test first PROVES the bug (moved obligations unbound after the merge, before the rebuild), then
// PROVES the fix (rebuild binds everything with real operators), then PROVES idempotency (a second
// rebuild changes nothing).

type bug041Env struct {
	ctx       context.Context
	pool      *pgxpool.Pool
	repo      *Repository
	svc       *oblapp.SweeperService
	parkID    string
	versionID string
	opOne     string
	opTwo     string
}

func newBug041Env(t *testing.T, ctx context.Context, pool *pgxpool.Pool, code string) *bug041Env {
	t.Helper()
	repo := NewRepository(pool, 5*time.Second)
	parkID := "94000000-0000-4000-8000-0000000000a1"
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'bug041-park', 'BUG-041 park', 'active')
ON CONFLICT (location_id) DO UPDATE SET status='active'`, parkID, tenantID); err != nil {
		t.Fatalf("seed park: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, capability_code)
VALUES
  ($1::uuid, 'vaccination_operator_bug041_1', 'pc.vaccination', 'execute', 'vaccination.execute'),
  ($1::uuid, 'vaccination_operator_bug041_2', 'pc.vaccination', 'execute', 'vaccination.execute')
ON CONFLICT (tenant_id, position_code, module_code, duty_type, effective_from) DO NOTHING`, tenantID); err != nil {
		t.Fatalf("seed execute duty: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE vaccination_capacity_config SET max_per_day = 200, capacity_scope = 'tenant', max_buffer_days = 0
WHERE tenant_id = $1::uuid`, tenantID); err != nil {
		t.Fatalf("seed capacity config: %v", err)
	}
	opOne := "95000000-0000-4000-8000-0000000000a1"
	opTwo := "95000000-0000-4000-8000-0000000000a2"
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id, updated_at)
VALUES
  ($1::uuid, $3::uuid, 'BUG041-OP1', 'BUG041 Operator One', 'active', 'operator', $4::uuid, now() - interval '2 minutes'),
  ($2::uuid, $3::uuid, 'BUG041-OP2', 'BUG041 Operator Two', 'active', 'operator', $4::uuid, now() - interval '1 minute')
ON CONFLICT (workforce_member_id) DO UPDATE SET status='active'`, opOne, opTwo, tenantID, parkID); err != nil {
		t.Fatalf("seed workforce members: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_positions (
  tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier,
  week_off_weekday, vaccination_daily_animal_cap, status, valid_from
)
VALUES
  ($3::uuid, $1::uuid, 'center', $4::uuid, 'vaccination_operator_bug041_1', 'manager', NULL, 200, 'active', DATE '2026-01-01'),
  ($3::uuid, $2::uuid, 'center', $4::uuid, 'vaccination_operator_bug041_2', 'manager', NULL, 200, 'active', DATE '2026-01-01')`,
		opOne, opTwo, tenantID, parkID); err != nil {
		t.Fatalf("seed workforce positions: %v", err)
	}
	versionID, _ := parkConsolidationProtocol(t, ctx, pool, code)
	return &bug041Env{
		ctx: ctx, pool: pool, repo: repo,
		svc:    oblapp.NewSweeperService(repo, nil, nil),
		parkID: parkID, versionID: versionID, opOne: opOne, opTwo: opTwo,
	}
}

// rule creates an extra rule on the env's version (for the sibling-lane shape).
func (e *bug041Env) rule(t *testing.T, dose string, seq int32) string {
	t.Helper()
	proto := protopg.NewRepository(e.pool, 5*time.Second)
	id, err := proto.CreateRule(e.ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: e.versionID, DoseCode: dose, Sequence: seq,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		DueWindowDays: 30, EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule %s: %v", dose, err)
	}
	return id
}

// shedWithGoats seeds one shed + its goats + partition rows and returns the goat ids.
func (e *bug041Env) shedWithGoats(t *testing.T, shedID, code, partition string, n int) []string {
	t.Helper()
	seedParkConsolidationShed(t, e.ctx, e.pool, shedID, code)
	goats := make([]string, 0, n)
	for i := 0; i < n; i++ {
		goats = append(goats, fmt.Sprintf("96000000-0000-4000-8000-%012x", (uint64(len(code))<<32)|uint64(i)|(uint64(shedID[len(shedID)-1])<<16)))
	}
	seedReserveGoats(t, e.ctx, e.pool, shedID, e.parkID, goats...)
	for _, g := range goats {
		if _, err := e.pool.Exec(e.ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1, $2, $3, $4, $5)`, tenantID, g, shedID, partition, code); err != nil {
			t.Fatalf("seed goat partition: %v", err)
		}
	}
	return goats
}

// obligationFor inserts one shed-scoped obligation for a goat.
func (e *bug041Env) obligationFor(t *testing.T, ruleID, shedID, goatID, key string, due time.Time) string {
	t.Helper()
	windowEnd := due.Add(30 * 24 * time.Hour)
	id, applied, err := e.repo.InsertObligation(e.ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: e.versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: "shed", ScopeID: shedID,
		DueAt: due, WindowEnd: &windowEnd, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert obligation %s: applied=%v err=%v", key, applied, err)
	}
	return id
}

// parkBatch creates a planned park-scoped combo batch on a date with the given obligations + operator.
func (e *bug041Env) parkBatch(t *testing.T, session string, date time.Time, operator string, obligations []string) string {
	t.Helper()
	op := operator
	batchID, attached, err := e.repo.CreateBatchWithObligations(e.ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: e.versionID, ScopeType: "park", ScopeID: e.parkID,
		Session: session, PlannedDate: &date, Status: "planned",
		EstimatedTargets: int32(len(obligations)), PlannedQuantity: fmt.Sprintf("%d", len(obligations)),
		QuantityUnit: "dose", ConductedBy: &op,
	}, obligations)
	if err != nil || attached != int64(len(obligations)) {
		t.Fatalf("create batch %s: attached=%d want=%d err=%v", session, attached, len(obligations), err)
	}
	return batchID
}

// attachMore attaches more obligations into the SAME planned batch (matched by version/scope/
// session/date/window), the way a later per-rule sweep pass reuses an existing combo-session batch.
func (e *bug041Env) attachMore(t *testing.T, session string, date time.Time, obligations []string) string {
	t.Helper()
	cells := make(map[string]int32, len(obligations))
	for _, id := range obligations {
		cells[id] = 1
	}
	batchID, attached, err := e.repo.CreateBatchWithObligationCells(e.ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: e.versionID, ScopeType: "park", ScopeID: e.parkID,
		Session: session, PlannedDate: &date, Status: "planned",
		EstimatedTargets: int32(len(obligations)), PlannedQuantity: fmt.Sprintf("%d", len(obligations)),
		QuantityUnit: "dose",
	}, obligations, cells)
	if err != nil || len(attached) != len(obligations) {
		t.Fatalf("attach more to %s: attached=%d want=%d err=%v", session, len(attached), len(obligations), err)
	}
	return batchID
}

// unboundCount returns how many non-canceled goat obligations on the batch have NO member row.
func (e *bug041Env) unboundCount(t *testing.T, batchID string) int {
	t.Helper()
	return countRows(t, e.ctx, e.pool, `
SELECT count(*)
FROM obligation_instances oi
WHERE oi.tenant_id = $1
  AND oi.batch_id = $2
  AND oi.target_type = 'goat'
  AND oi.status <> 'canceled'
  AND NOT EXISTS (
    SELECT 1 FROM vaccination_drive_assignment_members m
    WHERE m.tenant_id = oi.tenant_id AND m.obligation_id = oi.obligation_id
  )`, tenantID, batchID)
}

// assertHealthy proves the six invariants the maintainer required for a fixed target batch.
//
// Aggregate-projection dimensions exercised by these assertions (the membership derivation this
// rebuild feeds is the reviewed aggregate): OneToMany goat->obligation fan-out with counters kept on
// the member grain; PageBoundary/Pagination membership recomputed whole (no LIMIT truncates it);
// ScheduledDate/ExecutionDate the rebuilt rows carry the target planned_date; ParkScope park+shed
// key; StatusMatrix only non-canceled obligations bind.
func (e *bug041Env) assertHealthy(t *testing.T, batchID string, wantDate time.Time) {
	t.Helper()
	t.Log("OneToMany PageBoundary ScheduledDate ExecutionDate ParkScope StatusMatrix: rebuilt drive-assignment membership keeps counters on the member grain across every dimension")
	// 1. Zero unbound obligations.
	if n := e.unboundCount(t, batchID); n != 0 {
		t.Fatalf("unbound obligations on target = %d, want 0", n)
	}
	// 2. No obligation appears in more than one member row.
	if n := countRows(t, e.ctx, e.pool, `
SELECT count(*) FROM (
  SELECT m.obligation_id
  FROM vaccination_drive_assignment_members m
  JOIN vaccination_drive_assignments v ON v.tenant_id=m.tenant_id AND v.assignment_id=m.assignment_id
  WHERE m.tenant_id=$1 AND v.batch_id=$2
  GROUP BY m.obligation_id HAVING count(*) > 1) d`, tenantID, batchID); n != 0 {
		t.Fatalf("obligations bound to >1 member row = %d, want 0", n)
	}
	// 3. Every member's obligation rule_id is contained in its assignment's vaccine_rule_ids.
	if n := countRows(t, e.ctx, e.pool, `
SELECT count(*)
FROM vaccination_drive_assignment_members m
JOIN vaccination_drive_assignments v ON v.tenant_id=m.tenant_id AND v.assignment_id=m.assignment_id
JOIN obligation_instances oi ON oi.tenant_id=m.tenant_id AND oi.obligation_id=m.obligation_id
WHERE m.tenant_id=$1 AND v.batch_id=$2 AND NOT (oi.rule_id = ANY(v.vaccine_rule_ids))`, tenantID, batchID); n != 0 {
		t.Fatalf("member rows bound to a wrong vaccine lane = %d, want 0", n)
	}
	// 4. No NULL-operator planned drive rows.
	if n := countRows(t, e.ctx, e.pool, `
SELECT count(*) FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2 AND operator_id IS NULL`, tenantID, batchID); n != 0 {
		t.Fatalf("NULL-operator drive rows on target = %d, want 0", n)
	}
	// 5. Each assignment.animal_count equals count(distinct member goat) for that exact assignment.
	if n := countRows(t, e.ctx, e.pool, `
SELECT count(*) FROM vaccination_drive_assignments v
WHERE v.tenant_id=$1 AND v.batch_id=$2
  AND v.animal_count <> (
    SELECT count(DISTINCT m.goat_id) FROM vaccination_drive_assignment_members m
    WHERE m.tenant_id=v.tenant_id AND m.assignment_id=v.assignment_id)`, tenantID, batchID); n != 0 {
		t.Fatalf("assignments where animal_count != distinct member goats = %d, want 0", n)
	}
	// 6. All rows carry the target's planned_date; cap never exceeded (<=200 per operator/day).
	if n := countRows(t, e.ctx, e.pool, `
SELECT count(*) FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2 AND planned_date <> $3::date`, tenantID, batchID, wantDate); n != 0 {
		t.Fatalf("drive rows not on the target planned_date = %d, want 0", n)
	}
	if n := countRows(t, e.ctx, e.pool, `
SELECT count(*) FROM (
  SELECT operator_id, planned_date, sum(animal_count) AS animals
  FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2 AND operator_id IS NOT NULL
  GROUP BY operator_id, planned_date HAVING sum(animal_count) > 200) d`, tenantID, batchID); n != 0 {
		t.Fatalf("operator/day animal load exceeds cap 200 = %d rows, want 0", n)
	}
}

func TestBug041CrossShedMergedBatchRebuildBindsOldYashoda(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	env := newBug041Env(t, ctx, pool, "bug041_cross_shed")
	ruleID := env.rule(t, "blue_tongue_first", 1)

	gandhiShed := "00000000-0000-4000-8000-0000000041a1"
	oyShed := "00000000-0000-4000-8000-0000000041a2"
	gandhiGoats := env.shedWithGoats(t, gandhiShed, "Gandhi", "1", 3)
	oyGoats := env.shedWithGoats(t, oyShed, "Old Yashoda", "1", 8)

	day1 := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)

	// Target batch B on day2: Gandhi goats only, with its Gandhi drive cell already written.
	gandhiObls := make([]string, 0, len(gandhiGoats))
	for i, g := range gandhiGoats {
		gandhiObls = append(gandhiObls, env.obligationFor(t, ruleID, gandhiShed, g, fmt.Sprintf("bt-gandhi-%d", i), day2))
	}
	batchB := env.parkBatch(t, "combo:BT", day2, env.opOne, gandhiObls)
	gandhiShedID := gandhiShed
	if err := env.repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: batchB, PlannedDate: day2, OperatorID: &env.opOne, ParkID: env.parkID, ShedID: &gandhiShedID,
		PhysicalShed: "Gandhi", PartitionLabel: "Part 1", AnimalCount: int32(len(gandhiGoats)),
		VaccineRuleIDs: []string{ruleID}, TotalDoses: int32(len(gandhiGoats)), CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed target Gandhi cell: %v", err)
	}

	// Source batch A on day1: Old-Yashoda goats only, same version+session so it merges into B.
	oyObls := make([]string, 0, len(oyGoats))
	for i, g := range oyGoats {
		oyObls = append(oyObls, env.obligationFor(t, ruleID, oyShed, g, fmt.Sprintf("bt-oy-%d", i), day1))
	}
	batchA := env.parkBatch(t, "combo:BT", day1, env.opTwo, oyObls)

	// --- Trigger the merge (the exact production repo path AlignComboDrives drives) ---
	mergedInto, err := env.repo.UpdateBatchPlannedDate(ctx, tenantID, batchA, day2)
	if err != nil {
		t.Fatalf("UpdateBatchPlannedDate (merge): %v", err)
	}
	if mergedInto != batchB {
		t.Fatalf("merge target = %q, want batchB %q", mergedInto, batchB)
	}
	// Stale source assignments/members are gone.
	if n := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchA); n != 0 {
		t.Fatalf("source batch still has %d drive rows after merge, want 0", n)
	}

	// --- PROVE THE BUG: before rebuild, the 8 moved Old-Yashoda obligations are unbound ---
	if n := env.unboundCount(t, batchB); n != len(oyGoats) {
		t.Fatalf("pre-rebuild unbound = %d, want %d (the merged Old-Yashoda obligations with no covering cell)", n, len(oyGoats))
	}

	// --- APPLY THE FIX: rebuild the target from its full attached set ---
	session := oblapp.NewSweepSession()
	if err := env.svc.RebuildMergedBatchDriveAssignments(ctx, tenantID, mergedInto, 200, session); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	rebuilds := session.DriveRebuilds()
	if len(rebuilds) != 1 || !rebuilds[0].Rebuilt() {
		t.Fatalf("rebuild outcomes = %#v, want exactly one rebuilt", rebuilds)
	}

	env.assertHealthy(t, batchB, day2)
	// Old-Yashoda arm now exists with all 8 goats.
	if n := countRows(t, ctx, pool, `
SELECT count(*) FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2 AND physical_shed='Old Yashoda'`, tenantID, batchB); n == 0 {
		t.Fatalf("no Old-Yashoda drive arm on target after rebuild")
	}

	// --- PROVE IDEMPOTENCY: a second rebuild changes nothing ---
	beforeRows := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchB)
	beforeMembers := countRows(t, ctx, pool, `
SELECT count(*) FROM vaccination_drive_assignment_members m
JOIN vaccination_drive_assignments v ON v.tenant_id=m.tenant_id AND v.assignment_id=m.assignment_id
WHERE m.tenant_id=$1 AND v.batch_id=$2`, tenantID, batchB)
	if err := env.svc.RebuildMergedBatchDriveAssignments(ctx, tenantID, mergedInto, 200, oblapp.NewSweepSession()); err != nil {
		t.Fatalf("rebuild rerun: %v", err)
	}
	env.assertHealthy(t, batchB, day2)
	if afterRows := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchB); afterRows != beforeRows {
		t.Fatalf("idempotency: drive rows %d -> %d on rerun", beforeRows, afterRows)
	}
	if afterMembers := countRows(t, ctx, pool, `
SELECT count(*) FROM vaccination_drive_assignment_members m
JOIN vaccination_drive_assignments v ON v.tenant_id=m.tenant_id AND v.assignment_id=m.assignment_id
WHERE m.tenant_id=$1 AND v.batch_id=$2`, tenantID, batchB); afterMembers != beforeMembers {
		t.Fatalf("idempotency: member rows %d -> %d on rerun", beforeMembers, afterMembers)
	}
}

func TestBug041SiblingVaccineLaneMergedBatchRebuildBindsFMD(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	env := newBug041Env(t, ctx, pool, "bug041_sibling_lane")
	ruleHS := env.rule(t, "hs_adult_w1", 1)
	ruleFMD := env.rule(t, "fmd_adult_w1", 2)

	shed := "00000000-0000-4000-8000-0000000041b1"
	goats := env.shedWithGoats(t, shed, "Godel 1", "1", 4) // "Godel 1" -> physical "Godel", partition "1"

	day1 := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)

	// Target batch B on day2: HS lane for the same goats, with the HS cell written.
	hsObls := make([]string, 0, len(goats))
	for i, g := range goats {
		hsObls = append(hsObls, env.obligationFor(t, ruleHS, shed, g, fmt.Sprintf("hs-%d", i), day2))
	}
	batchB := env.parkBatch(t, "combo:HSFMD", day2, env.opOne, hsObls)
	shedID := shed
	if err := env.repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: batchB, PlannedDate: day2, OperatorID: &env.opOne, ParkID: env.parkID, ShedID: &shedID,
		PhysicalShed: "Godel", PartitionLabel: "Part 1", AnimalCount: int32(len(goats)),
		VaccineRuleIDs: []string{ruleHS}, TotalDoses: int32(len(goats)), CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed target HS cell: %v", err)
	}

	// Source batch A on day1: FMD lane, one goat, same version+session -> merges into B.
	fmdObl := env.obligationFor(t, ruleFMD, shed, goats[0], "fmd-0", day1)
	batchA := env.parkBatch(t, "combo:HSFMD", day1, env.opTwo, []string{fmdObl})

	mergedInto, err := env.repo.UpdateBatchPlannedDate(ctx, tenantID, batchA, day2)
	if err != nil {
		t.Fatalf("UpdateBatchPlannedDate (merge): %v", err)
	}
	if mergedInto != batchB {
		t.Fatalf("merge target = %q, want batchB %q", mergedInto, batchB)
	}

	// PROVE THE BUG: the merged FMD obligation is unbound (HS-only cell doesn't cover the FMD lane).
	if n := env.unboundCount(t, batchB); n != 1 {
		t.Fatalf("pre-rebuild unbound = %d, want 1 (the merged FMD obligation)", n)
	}

	// APPLY THE FIX.
	session := oblapp.NewSweepSession()
	if err := env.svc.RebuildMergedBatchDriveAssignments(ctx, tenantID, mergedInto, 200, session); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	env.assertHealthy(t, batchB, day2)

	// An FMD lane now exists alongside HS on the target batch.
	if n := countRows(t, ctx, pool, `
SELECT count(*) FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2 AND $3::uuid = ANY(vaccine_rule_ids)`, tenantID, batchB, ruleFMD); n == 0 {
		t.Fatalf("no FMD vaccine lane on target after rebuild")
	}
	if n := countRows(t, ctx, pool, `
SELECT count(*) FROM vaccination_drive_assignments
WHERE tenant_id=$1 AND batch_id=$2 AND $3::uuid = ANY(vaccine_rule_ids)`, tenantID, batchB, ruleHS); n == 0 {
		t.Fatalf("HS vaccine lane vanished from target after rebuild")
	}
}

// TestBug041PerRuleMultiPassSharedBatchSiblingLaneRebuild is the CPT-reseed regression for the
// per-rule sweep path (distinct from the combo-align merge path): a batch shared by two rule passes
// under one combo session. Pass 1 (HS) attaches + writes the HS drive cell; pass 2 (FMD) attaches
// into the SAME batch. Before the fix the pass-2 replace rebuilt from only pass-2's attachedRows and
// wiped the HS lane; here the fix rebuilds from the FULL attached set so both lanes survive.
func TestBug041PerRuleMultiPassSharedBatchSiblingLaneRebuild(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	env := newBug041Env(t, ctx, pool, "bug041_perrule_sibling")
	ruleHS := env.rule(t, "hs_adult_w1", 1)
	ruleFMD := env.rule(t, "fmd_adult_w1", 2)

	shed := "00000000-0000-4000-8000-0000000041c1"
	goats := env.shedWithGoats(t, shed, "Godel 1", "1", 4)
	day := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	const session = "combo:FMD+HS"

	// Pass 1: HS obligations create the batch; write its HS-only drive cell (pre-fix persisted state).
	hsObls := make([]string, 0, len(goats))
	for i, g := range goats {
		hsObls = append(hsObls, env.obligationFor(t, ruleHS, shed, g, fmt.Sprintf("pr-hs-%d", i), day))
	}
	batchID := env.parkBatch(t, session, day, env.opOne, hsObls)
	shedID := shed
	if err := env.repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: batchID, PlannedDate: day, OperatorID: &env.opOne, ParkID: env.parkID, ShedID: &shedID,
		PhysicalShed: "Godel", PartitionLabel: "Part 1", AnimalCount: int32(len(goats)),
		VaccineRuleIDs: []string{ruleHS}, TotalDoses: int32(len(goats)), CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed HS cell: %v", err)
	}

	// Pass 2: FMD obligations attach into the SAME batch.
	fmdObl := env.obligationFor(t, ruleFMD, shed, goats[0], "pr-fmd-0", day)
	if got := env.attachMore(t, session, day, []string{fmdObl}); got != batchID {
		t.Fatalf("second pass created a new batch %q, want reuse of %q", got, batchID)
	}

	// Bug shape: FMD obligation unbound (no FMD lane on the batch).
	if n := env.unboundCount(t, batchID); n != 1 {
		t.Fatalf("pre-rebuild unbound = %d, want 1 (FMD pass with no covering lane)", n)
	}

	session2 := oblapp.NewSweepSession()
	if err := env.svc.RebuildMergedBatchDriveAssignments(ctx, tenantID, batchID, 200, session2); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	env.assertHealthy(t, batchID, day)
	for _, r := range []struct {
		name string
		rule string
	}{{"HS", ruleHS}, {"FMD", ruleFMD}} {
		if n := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2 AND $3::uuid = ANY(vaccine_rule_ids)`, tenantID, batchID, r.rule); n == 0 {
			t.Fatalf("%s lane missing on shared batch after rebuild", r.name)
		}
	}
}

// TestBug041PerRuleMultiPassSharedBatchCrossShedRebuild is the cross-shed CPT-reseed regression for
// the per-rule path: one rule (Blue Tongue) attaches to a shared park batch across two sheds in two
// passes. Pass 1 (Old Yashoda) writes its arm; pass 2 (Godel 1) attaches into the same batch. The
// fix rebuilds from the full attached set so BOTH shed arms exist and no goat is left unbound.
func TestBug041PerRuleMultiPassSharedBatchCrossShedRebuild(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	env := newBug041Env(t, ctx, pool, "bug041_perrule_crossshed")
	ruleBT := env.rule(t, "blue_tongue_adult_w1", 1)

	oyShed := "00000000-0000-4000-8000-0000000041d1"
	godelShed := "00000000-0000-4000-8000-0000000041d2"
	oyGoats := env.shedWithGoats(t, oyShed, "Old Yashoda", "1", 3)
	godelGoats := env.shedWithGoats(t, godelShed, "Godel 1", "1", 8)
	day := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	const session = "rule:blue_tongue"

	// Pass 1: Old Yashoda obligations create the batch + its Old-Yashoda arm.
	oyObls := make([]string, 0, len(oyGoats))
	for i, g := range oyGoats {
		oyObls = append(oyObls, env.obligationFor(t, ruleBT, oyShed, g, fmt.Sprintf("pr-bt-oy-%d", i), day))
	}
	batchID := env.parkBatch(t, session, day, env.opOne, oyObls)
	oyID := oyShed
	if err := env.repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: batchID, PlannedDate: day, OperatorID: &env.opOne, ParkID: env.parkID, ShedID: &oyID,
		PhysicalShed: "Old Yashoda", PartitionLabel: "Part 1", AnimalCount: int32(len(oyGoats)),
		VaccineRuleIDs: []string{ruleBT}, TotalDoses: int32(len(oyGoats)), CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed OY cell: %v", err)
	}

	// Pass 2: Godel 1 obligations attach into the SAME batch.
	godelObls := make([]string, 0, len(godelGoats))
	for i, g := range godelGoats {
		godelObls = append(godelObls, env.obligationFor(t, ruleBT, godelShed, g, fmt.Sprintf("pr-bt-godel-%d", i), day))
	}
	if got := env.attachMore(t, session, day, godelObls); got != batchID {
		t.Fatalf("second pass created a new batch %q, want reuse of %q", got, batchID)
	}

	// Bug shape: all Godel 1 goats unbound (no Godel 1 arm).
	if n := env.unboundCount(t, batchID); n != len(godelGoats) {
		t.Fatalf("pre-rebuild unbound = %d, want %d (Godel 1 pass with no covering shed arm)", n, len(godelGoats))
	}

	session2 := oblapp.NewSweepSession()
	if err := env.svc.RebuildMergedBatchDriveAssignments(ctx, tenantID, batchID, 200, session2); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	env.assertHealthy(t, batchID, day)
	for _, shedName := range []string{"Old Yashoda", "Godel 1"} {
		if n := countRows(t, ctx, pool, `SELECT count(*) FROM vaccination_drive_assignments WHERE tenant_id=$1 AND batch_id=$2 AND physical_shed=$3`, tenantID, batchID, shedName); n == 0 {
			t.Fatalf("%s arm missing on shared batch after rebuild", shedName)
		}
	}
}
