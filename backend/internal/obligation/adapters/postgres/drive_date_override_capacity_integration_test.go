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
)

// TestDriveDateOverrideMoveReplansAgainstTargetDateCapacity is the cap-aware-move guard for
// POST /vaccination/schedule/drive-date-overrides.
//
// Shape: the vaccine's drive is already PLANNED on the source date and persisted with the source
// date's operator (A). The admin then moves that vaccine onto a target date where
//   - operator A is on week-off (so A cannot execute anything that day), and
//   - operator B (the only executable operator that day) has a daily animal cap of 3 with 2 animals
//     already committed on that date, i.e. only ONE animal slot of real remaining capacity.
//
// Moving the drive must RE-PLAN the moved rows against the TARGET date's real operator resolution
// and remaining capacity. Copying the source date's operator_id / animal_count / capacity_status
// verbatim silently books an operator who is off that day and reports the day as within_cap while
// it is 3x over the real remaining capacity.
//
// Clearing the override must symmetrically re-plan back onto the original date (where B is off and
// A is available), not carry the target date's operator back.
func TestDriveDateOverrideMoveReplansAgainstTargetDateCapacity(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.overridecap", 3)

	const (
		movedGoat  = "10000000-0000-4000-8000-00000000fa01"
		movedGoat2 = "10000000-0000-4000-8000-00000000fa04"
		movedGoat3 = "10000000-0000-4000-8000-00000000fa05"
		loadGoatA  = "10000000-0000-4000-8000-00000000fa02"
		loadGoatB  = "10000000-0000-4000-8000-00000000fa03"
		shedID     = "00000000-0000-4000-8000-00000000da01"
		operatorA  = "20000000-0000-4000-8000-000000000a01"
		operatorB  = "20000000-0000-4000-8000-000000000a02"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "override-cap-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, movedGoat, movedGoat2, movedGoat3, loadGoatA, loadGoatB)

	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rule_dimensions (
  tenant_id, protocol_version_id, rule_id, category, selector_key, dose_code, vaccine_code
) VALUES ($1, $2, $3, 'vaccination', 'override-cap-ppr', 'ppr_adult_w1', 'PPR')
ON CONFLICT (tenant_id, protocol_version_id, rule_id, selector_key) DO UPDATE SET
  vaccine_code = EXCLUDED.vaccine_code`,
		tenantID, versions[0].versionID, versions[0].ruleID); err != nil {
		t.Fatalf("seed rule dimensions: %v", err)
	}

	source := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	target := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	sourceWeekday := strings.ToLower(source.Weekday().String())
	targetWeekday := strings.ToLower(target.Weekday().String())

	// Operator A: available on the SOURCE date, week-off on the TARGET date, big cap.
	// Operator B: week-off on the SOURCE date, available on the TARGET date, cap 3.
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES
  ($1, $3, 'OVC-A', 'Override Cap Operator A', 'active', 'operator', $4),
  ($2, $3, 'OVC-B', 'Override Cap Operator B', 'active', 'operator', $4)`,
		operatorA, operatorB, tenantID, cbePark); err != nil {
		t.Fatalf("seed operators: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, capability_code, effective_from, status)
VALUES
  ($1, 'override_cap_operator_a', 'vaccination', 'execute', 'vaccination.drive.execute', '2026-01-01', 'active'),
  ($1, 'override_cap_operator_b', 'vaccination', 'execute', 'vaccination.drive.execute', '2026-01-01', 'active')`,
		tenantID); err != nil {
		t.Fatalf("seed duties: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, week_off_weekday, vaccination_daily_animal_cap, status, valid_from)
VALUES
  ($1, $2, 'center', $4, 'override_cap_operator_a', 'manager', $6, 10, 'active', '2026-01-01'),
  ($1, $3, 'center', $4, 'override_cap_operator_b', 'manager', $5, 3, 'active', '2026-01-01')`,
		tenantID, operatorA, operatorB, cbePark, sourceWeekday, targetWeekday); err != nil {
		t.Fatalf("seed positions: %v", err)
	}

	// Sanity: the fixture really does flip operator availability between the two dates.
	assertOperatorAvailability(t, ctx, repo, source, []string{operatorA})
	assertOperatorAvailability(t, ctx, repo, target, []string{operatorB})

	// The PPR drive is already planned on the source date with operator A and 3 animals. The three
	// animals are REAL obligations in one batch: animal_count is the count of the animals actually
	// in the cell, so a fixture that declared 3 while seeding 1 would be asserting against a number
	// the database cannot back.
	movedOblIDs := make([]string, 0, 3)
	for i, goat := range []string{movedGoat, movedGoat2, movedGoat3} {
		oblID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versions[0].versionID, RuleID: versions[0].ruleID,
			TargetType: "goat", TargetID: goat, ScopeType: "shed", ScopeID: shedID,
			DueAt: source, Status: "scheduled", IdempotencyKey: fmt.Sprintf("override-cap-moved-%d", i), Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("seed moved obligation %d: applied=%v err=%v", i, applied, err)
		}
		movedOblIDs = append(movedOblIDs, oblID)
	}
	movedPlannedDate := source
	movedBatch, movedAttached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versions[0].versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "override-cap-moved", PlannedDate: &movedPlannedDate, Status: "planned",
		EstimatedTargets: 3, PlannedQuantity: "3", QuantityUnit: "dose",
	}, movedOblIDs)
	if err != nil {
		t.Fatalf("create moved batch: %v", err)
	}
	if int(movedAttached) != len(movedOblIDs) {
		t.Fatalf("moved batch attached=%d want %d", movedAttached, len(movedOblIDs))
	}
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: movedBatch, PlannedDate: source, OperatorID: testStringPtr(operatorA), ParkID: cbePark,
		ShedID: testStringPtr(shedID), PhysicalShed: "Gandhi", PartitionLabel: "1", AnimalCount: 3,
		VaccineRuleIDs: []string{versions[0].ruleID}, TotalDoses: 3, CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed source-date assignment: %v", err)
	}

	// The TARGET date already carries 2 committed animals for operator B (cap 3) from other drives.
	_, loadBatchA := seedShotOnDate(t, ctx, repo, versions[1], loadGoatA, "park", cbePark, target, target, "override-cap-load-a")
	_, loadBatchB := seedShotOnDate(t, ctx, repo, versions[2], loadGoatB, "park", cbePark, target, target, "override-cap-load-b")
	if _, err := pool.Exec(ctx, `
UPDATE obligation_batches SET conducted_by = $2 WHERE tenant_id = $1 AND batch_id = ANY($3::uuid[])`,
		tenantID, operatorB, []string{loadBatchA, loadBatchB}); err != nil {
		t.Fatalf("seed target-date operator load: %v", err)
	}

	if _, err := repo.UpsertVaccinationDriveDateOverride(ctx, domain.VaccineDriveDateOverride{
		TenantID: tenantID, ParkID: cbePark, VaccineCode: "PPR", OriginalDriveDate: source,
		OverrideDate: target, Reason: "admin moves PPR onto a loaded day", CreatedBy: operatorA,
	}); err != nil {
		t.Fatalf("UpsertVaccinationDriveDateOverride: %v", err)
	}

	movedRows := driveAssignmentRowsForRule(t, ctx, pool, target, versions[0].ruleID)
	totalMoved := 0
	for _, row := range movedRows {
		totalMoved += row.animalCount
		if row.operatorID == operatorA {
			t.Fatalf("moved assignment row is still booked on operator A, who is on week-off (%s) on the target date %s: %+v",
				targetWeekday, target.Format("2006-01-02"), row)
		}
		if row.operatorID != "" && row.operatorID != operatorB {
			t.Fatalf("moved assignment row booked on an operator not resolvable on the target date: %+v", row)
		}
	}
	if totalMoved != 3 {
		t.Fatalf("moved animal total on target date = %d, want 3 (no work may be lost by the move)", totalMoved)
	}
	overCap := false
	for _, row := range movedRows {
		if row.capacityStatus != "within_cap" {
			overCap = true
		}
	}
	if !overCap {
		t.Fatalf("every moved row on the target date reports within_cap, but only 1 animal slot of operator capacity remains for 3 animals: %+v", movedRows)
	}
	if left := driveAssignmentRowsForRule(t, ctx, pool, source, versions[0].ruleID); len(left) != 0 {
		t.Fatalf("moved vaccine still has rows on the source date: %+v", left)
	}

	// Clearing the override must re-plan back onto the original date, where B is off and A is free.
	if _, err := repo.UpsertVaccinationDriveDateOverride(ctx, domain.VaccineDriveDateOverride{
		TenantID: tenantID, ParkID: cbePark, VaccineCode: "PPR", OriginalDriveDate: source,
		OverrideDate: source, Reason: "admin clears the PPR override", CreatedBy: operatorA,
	}); err != nil {
		t.Fatalf("clear UpsertVaccinationDriveDateOverride: %v", err)
	}
	restored := driveAssignmentRowsForRule(t, ctx, pool, source, versions[0].ruleID)
	totalRestored := 0
	for _, row := range restored {
		totalRestored += row.animalCount
		if row.operatorID == operatorB {
			t.Fatalf("restored assignment row is booked on operator B, who is on week-off (%s) on the original date: %+v", sourceWeekday, row)
		}
	}
	if totalRestored != 3 {
		t.Fatalf("restored animal total on original date = %d, want 3", totalRestored)
	}
	if leftover := driveAssignmentRowsForRule(t, ctx, pool, target, versions[0].ruleID); len(leftover) != 0 {
		t.Fatalf("cleared override left rows on the target date: %+v", leftover)
	}
}

type driveAssignmentRow struct {
	operatorID     string
	animalCount    int
	capacityStatus string
}

func driveAssignmentRowsForRule(t *testing.T, ctx context.Context, pool *pgxpool.Pool, date time.Time, ruleID string) []driveAssignmentRow {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT COALESCE(operator_id::text, ''), animal_count, capacity_status
FROM vaccination_drive_assignments
WHERE tenant_id = $1 AND planned_date = $2 AND $3::uuid = ANY(vaccine_rule_ids)
ORDER BY COALESCE(operator_id::text, ''), animal_count`, tenantID, date, ruleID)
	if err != nil {
		t.Fatalf("query drive assignments: %v", err)
	}
	defer rows.Close()
	out := make([]driveAssignmentRow, 0)
	for rows.Next() {
		var row driveAssignmentRow
		if err := rows.Scan(&row.operatorID, &row.animalCount, &row.capacityStatus); err != nil {
			t.Fatalf("scan drive assignment: %v", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("drive assignment rows: %v", err)
	}
	return out
}

func assertOperatorAvailability(t *testing.T, ctx context.Context, repo *Repository, date time.Time, want []string) {
	t.Helper()
	operators, err := repo.AvailableVaccinationOperatorsForDrive(ctx, tenantID, cbePark, date, 0)
	if err != nil {
		t.Fatalf("available operators on %s: %v", date.Format("2006-01-02"), err)
	}
	got := make([]string, 0, len(operators))
	for _, operator := range operators {
		got = append(got, operator.OperatorID)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("available operators on %s = %v, want %v", date.Format("2006-01-02"), got, want)
	}
}
