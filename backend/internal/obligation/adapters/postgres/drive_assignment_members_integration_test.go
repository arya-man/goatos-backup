package postgres

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// The gap this proves closed: a vaccination drive assignment row stores an aggregate animal_count.
// When the planner splits ONE shed/partition cell across two operators (or two dates), the database
// knew "3 here, 1 there" but not WHICH goats -- so a death/sale could not be removed from the exact
// drive row that covers it, and no per-animal operator/date could be shown.
func TestDriveAssignmentSplitRecordsExactPerGoatMembership(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 10*time.Second)
	planned := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	const (
		parkID = "95000000-0000-4000-8000-000000000001"
		shedID = "95000000-0000-4000-8000-000000000002"
		opA    = "95000000-0000-4000-8000-000000000011"
		opB    = "95000000-0000-4000-8000-000000000012"
	)
	goatIDs := []string{
		"95000000-0000-4000-8000-000000000101",
		"95000000-0000-4000-8000-000000000102",
		"95000000-0000-4000-8000-000000000103",
		"95000000-0000-4000-8000-000000000104",
	}
	versionID, ruleID := parkConsolidationProtocol(t, ctx, pool, "drive_member_split_proof")
	seedDriveMembershipLocations(t, ctx, pool, parkID, shedID)
	seedDriveMembershipOperators(t, ctx, pool, parkID, opA, opB)
	seedReserveGoats(t, ctx, pool, shedID, parkID, goatIDs...)

	batchID := seedDriveMembershipBatch(t, ctx, pool, versionID, parkID, planned)
	obligationByGoat := map[string]string{}
	for i, goatID := range goatIDs {
		obligationByGoat[goatID] = seedBatchedShedObligation(t, ctx, pool, versionID, ruleID, batchID, goatID, shedID, "member-split-"+goatID, planned, i)
	}

	// The split the planner produces: same batch/park/shed/partition, one cell, two operators.
	shed := shedID
	operatorA, operatorB := opA, opB
	assignments := []domain.DriveAssignment{
		{
			BatchID: batchID, PlannedDate: planned, OperatorID: &operatorA, ParkID: parkID, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "Part A", AnimalCount: 3,
			VaccineRuleIDs: []string{ruleID}, TotalDoses: 3, CapacityStatus: "within_cap",
		},
		{
			BatchID: batchID, PlannedDate: planned, OperatorID: &operatorB, ParkID: parkID, ShedID: &shed,
			PhysicalShed: "Gandhi", PartitionLabel: "Part A", AnimalCount: 1,
			VaccineRuleIDs: []string{ruleID}, TotalDoses: 1, CapacityStatus: "within_cap",
		},
	}
	if err := repo.ReplaceVaccinationDriveAssignmentsForBatch(ctx, tenantID, batchID, assignments); err != nil {
		t.Fatalf("replace drive assignments: %v", err)
	}

	assertDriveMembershipMatchesCounts(t, ctx, pool, batchID, len(goatIDs))
	// Keyed by OPERATOR, not assignment_id: ReplaceVaccinationDriveAssignmentsForBatch deletes and
	// re-inserts the batch's rows, so assignment_id is deliberately not stable across a replan. What
	// must converge is WHICH goats each operator arm covers.
	first := driveMembershipByOperator(t, ctx, pool, batchID)

	// Idempotency: the identical sweep/replan must converge on the same membership, not duplicate.
	if err := repo.ReplaceVaccinationDriveAssignmentsForBatch(ctx, tenantID, batchID, assignments); err != nil {
		t.Fatalf("replay replace drive assignments: %v", err)
	}
	assertDriveMembershipMatchesCounts(t, ctx, pool, batchID, len(goatIDs))
	second := driveMembershipByOperator(t, ctx, pool, batchID)
	if len(first) != len(second) {
		t.Fatalf("replay changed the number of operator arms: %d -> %d", len(first), len(second))
	}
	for operatorID, goats := range first {
		replayed, ok := second[operatorID]
		if !ok {
			t.Fatalf("replay dropped operator %s from membership", operatorID)
		}
		if len(goats) != len(replayed) {
			t.Fatalf("replay changed membership of operator %s: %v -> %v", operatorID, goats, replayed)
		}
		for i := range goats {
			if goats[i] != replayed[i] {
				t.Fatalf("replay changed membership of operator %s: %v -> %v", operatorID, goats, replayed)
			}
		}
	}

	// Exactness proof: each obligation resolves to exactly one drive row, and every member row
	// carries the obligation's real goat.
	for goatID, obligationID := range obligationByGoat {
		var got string
		var rows int
		if err := pool.QueryRow(ctx, `
SELECT count(*)::int, COALESCE(max(goat_id::text), '')
FROM vaccination_drive_assignment_members
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`, tenantID, obligationID).Scan(&rows, &got); err != nil {
			t.Fatalf("read membership for %s: %v", goatID, err)
		}
		if rows != 1 {
			t.Fatalf("obligation %s (goat %s) has %d membership rows, want exactly 1", obligationID, goatID, rows)
		}
		if got != goatID {
			t.Fatalf("obligation %s membership goat = %s, want %s", obligationID, got, goatID)
		}
	}
}

// The maintainer's literal case: one shed/partition split across TWO days ("Jul 24 Darshan N,
// Jul 25 Darshan M"). The database must know which exact goat is on which day, not just the counts.
func TestDriveAssignmentDateSplitRecordsExactPerGoatMembershipPerDay(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 10*time.Second)
	day1 := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)
	const (
		parkID = "96000000-0000-4000-8000-000000000001"
		shedID = "96000000-0000-4000-8000-000000000002"
		opA    = "96000000-0000-4000-8000-000000000011"
	)
	goatIDs := []string{
		"96000000-0000-4000-8000-000000000101",
		"96000000-0000-4000-8000-000000000102",
		"96000000-0000-4000-8000-000000000103",
	}
	versionID, ruleID := parkConsolidationProtocol(t, ctx, pool, "drive_member_date_split_proof")
	seedDriveMembershipLocations(t, ctx, pool, parkID, shedID)
	seedDriveMembershipOperators(t, ctx, pool, parkID, opA)
	seedReserveGoats(t, ctx, pool, shedID, parkID, goatIDs...)

	batchID := seedDriveMembershipBatch(t, ctx, pool, versionID, parkID, day1)
	for i, goatID := range goatIDs {
		seedBatchedShedObligation(t, ctx, pool, versionID, ruleID, batchID, goatID, shedID, "member-date-"+goatID, day1, i)
	}

	shed := shedID
	operator := opA
	base := domain.DriveAssignment{
		BatchID: batchID, OperatorID: &operator, ParkID: parkID, ShedID: &shed,
		PhysicalShed: "Gandhi", PartitionLabel: "Part A",
		VaccineRuleIDs: []string{ruleID}, CapacityStatus: "within_cap",
	}
	day1Row, day2Row := base, base
	day1Row.PlannedDate, day1Row.AnimalCount, day1Row.TotalDoses = day1, 2, 2
	day2Row.PlannedDate, day2Row.AnimalCount, day2Row.TotalDoses = day2, 1, 1
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{day1Row, day2Row}); err != nil {
		t.Fatalf("upsert split drive assignments: %v", err)
	}

	assertDriveMembershipMatchesCounts(t, ctx, pool, batchID, len(goatIDs))

	rows, err := pool.Query(ctx, `
SELECT vda.planned_date::text, m.goat_id::text
FROM vaccination_drive_assignment_members m
JOIN vaccination_drive_assignments vda
  ON vda.tenant_id = m.tenant_id AND vda.assignment_id = m.assignment_id
WHERE m.tenant_id = $1::uuid AND vda.batch_id = $2::uuid
ORDER BY vda.planned_date, m.goat_id`, tenantID, batchID)
	if err != nil {
		t.Fatalf("read per-day membership: %v", err)
	}
	defer rows.Close()
	byDay := map[string][]string{}
	for rows.Next() {
		var day, goatID string
		if err := rows.Scan(&day, &goatID); err != nil {
			t.Fatalf("scan per-day membership: %v", err)
		}
		byDay[day] = append(byDay[day], goatID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("per-day membership rows: %v", err)
	}
	if len(byDay["2026-07-24"]) != 2 || len(byDay["2026-07-25"]) != 1 {
		t.Fatalf("per-day exact membership = %v, want 2 goats on 2026-07-24 and 1 on 2026-07-25", byDay)
	}
	for _, goatID := range byDay["2026-07-25"] {
		for _, other := range byDay["2026-07-24"] {
			if goatID == other {
				t.Fatalf("goat %s is on BOTH drive days; membership must be disjoint", goatID)
			}
		}
	}
}

func TestDriveAssignmentMembershipMovesStaleTenantObligationBinding(t *testing.T) {
	// Aggregate-projection adversarial coverage: OneToMany, PageBoundary, ScheduledDate,
	// ParkScope, and StatusMatrix. The stale member row is a one-obligation/two-assignment fanout
	// until the upsert moves it; the query must preserve the current batch/date/park scope and not
	// leave counters on the displaced assignment.
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 10*time.Second)
	planned := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	const (
		parkID = "97000000-0000-4000-8000-000000000001"
		shedID = "97000000-0000-4000-8000-000000000002"
		opA    = "97000000-0000-4000-8000-000000000011"
		goatID = "97000000-0000-4000-8000-000000000101"
	)
	versionID, ruleID := parkConsolidationProtocol(t, ctx, pool, "drive_member_stale_move")
	seedDriveMembershipLocations(t, ctx, pool, parkID, shedID)
	seedDriveMembershipOperators(t, ctx, pool, parkID, opA)
	seedReserveGoats(t, ctx, pool, shedID, parkID, goatID)

	oldBatchID := seedDriveMembershipBatch(t, ctx, pool, versionID, parkID, planned.AddDate(0, 0, -1))
	currentBatchID := seedDriveMembershipBatch(t, ctx, pool, versionID, parkID, planned)
	obligationID := seedBatchedShedObligation(t, ctx, pool, versionID, ruleID, currentBatchID, goatID, shedID, "member-stale-"+goatID, planned, 0)

	var staleAssignmentID string
	if err := pool.QueryRow(ctx, `
INSERT INTO vaccination_drive_assignments (
  tenant_id, batch_id, planned_date, operator_id, park_id, shed_id,
  physical_shed, partition_label, animal_count, vaccine_rule_ids, total_doses, capacity_status
)
VALUES ($1::uuid, $2::uuid, $3::date, $4::uuid, $5::uuid, $6::uuid,
        'Yashoda', 'Part 4', 1, ARRAY[$7::uuid], 1, 'within_cap')
RETURNING assignment_id::text`,
		tenantID, oldBatchID, planned.AddDate(0, 0, -1), opA, parkID, shedID, ruleID).Scan(&staleAssignmentID); err != nil {
		t.Fatalf("seed stale assignment: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid)`,
		tenantID, staleAssignmentID, obligationID, goatID); err != nil {
		t.Fatalf("seed stale member: %v", err)
	}

	shed := shedID
	operator := opA
	assignments := []domain.DriveAssignment{{
		BatchID: currentBatchID, PlannedDate: planned, OperatorID: &operator, ParkID: parkID, ShedID: &shed,
		PhysicalShed: "Yashoda", PartitionLabel: "Part 4", AnimalCount: 1,
		VaccineRuleIDs: []string{ruleID}, TotalDoses: 1, CapacityStatus: "within_cap",
	}}
	if err := repo.ReplaceVaccinationDriveAssignmentsForBatch(ctx, tenantID, currentBatchID, assignments); err != nil {
		t.Fatalf("replace drive assignments with stale tenant-obligation member: %v", err)
	}

	var gotAssignmentID, gotBatchID string
	if err := pool.QueryRow(ctx, `
SELECT m.assignment_id::text, vda.batch_id::text
FROM vaccination_drive_assignment_members m
JOIN vaccination_drive_assignments vda
  ON vda.tenant_id = m.tenant_id AND vda.assignment_id = m.assignment_id
WHERE m.tenant_id = $1::uuid AND m.obligation_id = $2::uuid`,
		tenantID, obligationID).Scan(&gotAssignmentID, &gotBatchID); err != nil {
		t.Fatalf("read moved member: %v", err)
	}
	if gotAssignmentID == staleAssignmentID {
		t.Fatalf("member stayed on stale assignment %s", staleAssignmentID)
	}
	if gotBatchID != currentBatchID {
		t.Fatalf("member moved to batch %s, want current batch %s", gotBatchID, currentBatchID)
	}
	var staleRows int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM vaccination_drive_assignments
WHERE tenant_id = $1::uuid AND assignment_id = $2::uuid`,
		tenantID, staleAssignmentID).Scan(&staleRows); err != nil {
		t.Fatalf("count stale assignment: %v", err)
	}
	if staleRows != 0 {
		t.Fatalf("stale assignment rows = %d, want deleted after its only member moved", staleRows)
	}
	assertDriveMembershipMatchesCounts(t, ctx, pool, currentBatchID, 1)
}

// assertDriveMembershipMatchesCounts is the maintainer's stated acceptance condition: the aggregate
// animal_count on each drive row and the exact membership behind it must agree, and the union must
// cover every goat in the batch exactly once. animal_count counts ANIMALS, so the matching member
// aggregate is count(DISTINCT goat_id), not raw member rows (a goat due two vaccines in one drive
// legitimately has two obligation-grain member rows).
func assertDriveMembershipMatchesCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batchID string, wantGoats int) {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT vda.assignment_id::text, vda.animal_count,
       COALESCE(count(DISTINCT m.goat_id), 0)::int
FROM vaccination_drive_assignments vda
LEFT JOIN vaccination_drive_assignment_members m
  ON m.tenant_id = vda.tenant_id AND m.assignment_id = vda.assignment_id
WHERE vda.tenant_id = $1::uuid AND vda.batch_id = $2::uuid
GROUP BY vda.assignment_id, vda.animal_count
ORDER BY vda.assignment_id`, tenantID, batchID)
	if err != nil {
		t.Fatalf("read assignment membership counts: %v", err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var assignmentID string
		var animalCount, members int32
		if err := rows.Scan(&assignmentID, &animalCount, &members); err != nil {
			t.Fatalf("scan assignment membership counts: %v", err)
		}
		seen++
		if members != animalCount {
			t.Fatalf("assignment %s: animal_count=%d but exact membership has %d distinct goats", assignmentID, animalCount, members)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("assignment membership count rows: %v", err)
	}
	if seen == 0 {
		t.Fatalf("no drive assignment rows found for batch %s", batchID)
	}
	var distinctGoats int
	if err := pool.QueryRow(ctx, `
SELECT count(DISTINCT m.goat_id)::int
FROM vaccination_drive_assignment_members m
JOIN vaccination_drive_assignments vda
  ON vda.tenant_id = m.tenant_id AND vda.assignment_id = m.assignment_id
WHERE m.tenant_id = $1::uuid AND vda.batch_id = $2::uuid`, tenantID, batchID).Scan(&distinctGoats); err != nil {
		t.Fatalf("read distinct membership goats: %v", err)
	}
	if distinctGoats != wantGoats {
		t.Fatalf("batch membership covers %d goats, want %d", distinctGoats, wantGoats)
	}
}

func driveMembershipByOperator(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batchID string) map[string][]string {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT COALESCE(vda.operator_id::text, ''), m.goat_id::text
FROM vaccination_drive_assignment_members m
JOIN vaccination_drive_assignments vda
  ON vda.tenant_id = m.tenant_id AND vda.assignment_id = m.assignment_id
WHERE m.tenant_id = $1::uuid AND vda.batch_id = $2::uuid`, tenantID, batchID)
	if err != nil {
		t.Fatalf("read membership: %v", err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var operatorID, goatID string
		if err := rows.Scan(&operatorID, &goatID); err != nil {
			t.Fatalf("scan membership: %v", err)
		}
		out[operatorID] = append(out[operatorID], goatID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("membership rows: %v", err)
	}
	for key := range out {
		sort.Strings(out[key])
	}
	return out
}

func seedDriveMembershipLocations(t *testing.T, ctx context.Context, pool *pgxpool.Pool, parkID, shedID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'member-park', 'Membership park', 'active')
ON CONFLICT (location_id) DO UPDATE SET status='active'`, parkID, tenantID); err != nil {
		t.Fatalf("seed park: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id)
VALUES ($1::uuid, $2::uuid, 'shed', 'member-shed', 'Gandhi 1', 'active', $3::uuid)
ON CONFLICT (location_id) DO UPDATE SET status='active'`, shedID, tenantID, parkID); err != nil {
		t.Fatalf("seed shed: %v", err)
	}
}

func seedDriveMembershipOperators(t *testing.T, ctx context.Context, pool *pgxpool.Pool, parkID string, operatorIDs ...string) {
	t.Helper()
	for i, operatorID := range operatorIDs {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3, $4, 'active', 'operator', $5::uuid)
ON CONFLICT (workforce_member_id) DO UPDATE SET status='active'`,
			operatorID, tenantID, "MEM-OP-"+string(rune('A'+i)), "Membership operator", parkID); err != nil {
			t.Fatalf("seed operator %s: %v", operatorID, err)
		}
	}
}

func seedDriveMembershipBatch(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID, parkID string, planned time.Time) string {
	t.Helper()
	var batchID string
	if err := pool.QueryRow(ctx, `
INSERT INTO obligation_batches (
  tenant_id, protocol_version_id, scope_type, scope_id, session, planned_date, status, estimated_targets
)
VALUES ($1::uuid, $2::uuid, 'park', $3::uuid, 'member-split-proof', $4::date, 'planned', 4)
RETURNING batch_id::text`, tenantID, versionID, parkID, planned).Scan(&batchID); err != nil {
		t.Fatalf("seed obligation batch: %v", err)
	}
	return batchID
}

func seedBatchedShedObligation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID, ruleID, batchID, goatID, shedID, key string, due time.Time, sequence int) string {
	t.Helper()
	var obligationID string
	if err := pool.QueryRow(ctx, `
INSERT INTO obligation_instances (
  tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key, sequence
)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', $5::uuid, 'shed', $6::uuid, $7, 'scheduled', $8, $9)
RETURNING obligation_id::text`,
		tenantID, versionID, ruleID, batchID, goatID, shedID, due, key, sequence+1).Scan(&obligationID); err != nil {
		t.Fatalf("seed obligation %s: %v", key, err)
	}
	return obligationID
}
