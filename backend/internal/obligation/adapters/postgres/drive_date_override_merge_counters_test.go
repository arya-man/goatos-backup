package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
)

// TestDriveDateOverrideMergeOneToManyDateShiftLedgerParity is the guard for the MERGE half
// of a drive-date override: what happens when the moved vaccine lane lands on a target-date row
// that ALREADY EXISTS for the same cell and operator.
//
// The write is an upsert on the assignment table's cell key
// (tenant, batch, planned_date, park, shed, physical_shed, partition, operator). Its conflict branch
// correctly UNIONs vaccine_rule_ids -- the merged row plans both vaccines -- so the two counters
// that DESCRIBE that union must describe both lanes too:
//
//	animal_count = COUNT(DISTINCT animal) over the merged row's animals -- the operator cap unit.
//	total_doses  = COUNT(DISTINCT (animal, rule)) over them -- the dose/stock unit.
//
// Two ways to get this wrong, and this fixture is built to catch BOTH:
//
//	overwrite (animal_count = EXCLUDED.animal_count) -- the pre-existing row's animals vanish from
//	  the count while still being planned. The operator's day under-reports its real load, cap
//	  checks under-fill it, and the row contradicts its own membership ledger, which the exit /
//	  clinical-defer / shed-shift decrement paths all trust.
//	naive addition (vda.animal_count + EXCLUDED.animal_count) -- an animal present in BOTH lanes is
//	  counted twice, because the two numbers being added are counts of overlapping animal sets.
//
// Fixture: ONE batch, ONE cell, ONE operator, two vaccines, four goats.
//
//	target date already holds: Blue Tongue for goatA and goatD  -> 2 animals / 2 doses
//	source date holds:         PPR for goatA, goatB, goatC -> 3 animals / 3 doses
//	move PPR source -> target; both land on the same cell/operator row.
//
//	merged row MUST be: rules {Blue Tongue,PPR}
//	                    animal_count = |{A,B,C,D}|                        = 4  (goatA counts ONCE)
//	                    total_doses  = |{(A,PPR),(B,PPR),(C,PPR),(A,Blue Tongue),(D,Blue Tongue)}| = 5  (goatA gives TWO doses)
//
// Overwrite yields 3/3, naive addition yields 5/5; only counters derived from the merged row's own
// per-goat membership ledger yield 4/5. The ledger parity assertion below is the independent
// witness: animal_count == COUNT(DISTINCT goat_id) and total_doses == COUNT(*) of the row's members.
func TestDriveDateOverrideMergeOneToManyDateShiftLedgerParity(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.lanemerge", 2)
	pprVersion, blueTongueVersion := versions[0], versions[1]

	const (
		goatA     = "10000000-0000-4000-8000-00000000fc01"
		goatB     = "10000000-0000-4000-8000-00000000fc02"
		goatC     = "10000000-0000-4000-8000-00000000fc03"
		goatD     = "10000000-0000-4000-8000-00000000fc04"
		shedID    = "00000000-0000-4000-8000-00000000dc01"
		operatorA = "20000000-0000-4000-8000-000000000c01"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "lane-merge-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatA, goatB, goatC, goatD)

	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rule_dimensions (
  tenant_id, protocol_version_id, rule_id, category, selector_key, dose_code, vaccine_code
) VALUES
  ($1, $2, $3, 'vaccination', 'lane-merge-ppr', 'ppr_adult_w1', 'PPR'),
  ($1, $4, $5, 'vaccination', 'lane-merge-bt', 'blue_tongue_adult_w1', 'BLUE_TONGUE')
ON CONFLICT (tenant_id, protocol_version_id, rule_id, selector_key) DO UPDATE SET
  vaccine_code = EXCLUDED.vaccine_code`,
		tenantID, pprVersion.versionID, pprVersion.ruleID, blueTongueVersion.versionID, blueTongueVersion.ruleID); err != nil {
		t.Fatalf("seed rule dimensions: %v", err)
	}

	// Vaccination time grain is the BUSINESS DAY: both of these are fixed calendar dates at day
	// start, and every derivation below is whole-day arithmetic (AddDate). No hour/minute value and
	// no `time.Now()` appears in this fixture, so the test cannot pass or fail depending on the
	// clock time it is run at.
	source := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	target := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	// The operator must be executable on BOTH days, otherwise the moved lane lands operator-less and
	// never reaches the collision this test is about.
	weekOff := strings.ToLower(source.AddDate(0, 0, 2).Weekday().String())

	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1, $2, 'LMG-A', 'Lane Merge Operator A', 'active', 'operator', $3)`,
		operatorA, tenantID, cbePark); err != nil {
		t.Fatalf("seed operator: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, capability_code, effective_from, status)
VALUES ($1, 'lane_merge_operator_a', 'vaccination', 'execute', 'vaccination.drive.execute', '2026-01-01', 'active')`,
		tenantID); err != nil {
		t.Fatalf("seed duties: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, week_off_weekday, vaccination_daily_animal_cap, status, valid_from)
VALUES ($1, $2, 'center', $3, 'lane_merge_operator_a', 'manager', $4, 50, 'active', '2026-01-01')`,
		tenantID, operatorA, cbePark, weekOff); err != nil {
		t.Fatalf("seed position: %v", err)
	}

	// ONE batch holding both lanes, so both dates' rows are the SAME cell and collide on the
	// assignment table's conflict key when the moved lane lands.
	oblIDs := make([]string, 0, 5)
	seed := []struct {
		v    struct{ versionID, ruleID string }
		goat string
		due  time.Time
		key  string
	}{
		{pprVersion, goatA, source, "lane-merge-ppr-a"},
		{pprVersion, goatB, source, "lane-merge-ppr-b"},
		{pprVersion, goatC, source, "lane-merge-ppr-c"},
		// goatA is in BOTH lanes: one animal, two doses. Naive count addition double-counts it.
		{blueTongueVersion, goatA, target, "lane-merge-bt-a"},
		{blueTongueVersion, goatD, target, "lane-merge-bt-d"},
	}
	for _, s := range seed {
		oblID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: s.v.versionID, RuleID: s.v.ruleID,
			TargetType: "goat", TargetID: s.goat, ScopeType: "shed", ScopeID: shedID,
			DueAt: s.due, Status: "scheduled", IdempotencyKey: s.key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("seed obligation %s: applied=%v err=%v", s.key, applied, err)
		}
		oblIDs = append(oblIDs, oblID)
	}
	plannedDate := source
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: pprVersion.versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "lane-merge-cell", PlannedDate: &plannedDate, Status: "planned",
		EstimatedTargets: 4, PlannedQuantity: "5", QuantityUnit: "dose",
	}, oblIDs)
	if err != nil {
		t.Fatalf("create cell batch: %v", err)
	}
	if int(attached) != len(oblIDs) {
		t.Fatalf("cell batch attached=%d want %d", attached, len(oblIDs))
	}

	// The pre-existing TARGET-date row: same cell, same operator, the OTHER vaccine.
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: batchID, PlannedDate: target, OperatorID: testStringPtr(operatorA), ParkID: cbePark,
		ShedID: testStringPtr(shedID), PhysicalShed: "Gandhi", PartitionLabel: "1", AnimalCount: 2,
		VaccineRuleIDs: []string{blueTongueVersion.ruleID}, TotalDoses: 2, CapacityStatus: "within_cap",
	}, {
		BatchID: batchID, PlannedDate: source, OperatorID: testStringPtr(operatorA), ParkID: cbePark,
		ShedID: testStringPtr(shedID), PhysicalShed: "Gandhi", PartitionLabel: "1", AnimalCount: 3,
		VaccineRuleIDs: []string{pprVersion.ruleID}, TotalDoses: 3, CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed assignments: %v", err)
	}

	beforeTarget := laneRowsOnDate(t, ctx, pool, target)
	if len(beforeTarget) != 1 || beforeTarget[0].animalCount != 2 || beforeTarget[0].totalDoses != 2 {
		t.Fatalf("fixture invalid: target date must start with exactly one 2-animal/2-dose Blue Tongue row: %+v", beforeTarget)
	}
	beforeSource := laneRowsOnDate(t, ctx, pool, source)
	if len(beforeSource) != 1 || beforeSource[0].animalCount != 3 || beforeSource[0].totalDoses != 3 {
		t.Fatalf("fixture invalid: source date must start with exactly one 3-animal/3-dose PPR row: %+v", beforeSource)
	}

	if _, err := repo.UpsertVaccinationDriveDateOverride(ctx, domain.VaccineDriveDateOverride{
		TenantID: tenantID, ParkID: cbePark, VaccineCode: "PPR", OriginalDriveDate: source,
		OverrideDate: target, Reason: "admin moves PPR onto the day that already runs Blue Tongue", CreatedBy: operatorA,
	}); err != nil {
		t.Fatalf("UpsertVaccinationDriveDateOverride: %v", err)
	}

	after := laneRowsOnDate(t, ctx, pool, target)
	if len(after) != 1 {
		t.Fatalf("the moved lane did not collide with the pre-existing target row (%d rows on target, want exactly 1 merged row) -- fixture no longer reproduces the merge: %+v", len(after), after)
	}
	merged := after[0]
	if len(merged.ruleIDs) != 2 {
		t.Errorf("merged row carries %d vaccine rules, want 2 (the union of both lanes): %+v", len(merged.ruleIDs), merged)
	}
	if merged.animalCount != 4 {
		t.Errorf("merged row animal_count = %d, want 4 -- the union of both lanes is {A,B,C,D} and goatA (in BOTH lanes) counts ONCE; overwriting with the arriving lane's 3 hides the pre-existing row's animals, naive addition would report 5",
			merged.animalCount)
	}
	if merged.totalDoses != 5 {
		t.Errorf("merged row total_doses = %d, want 5 -- doses are DISTINCT (animal, rule) pairs: (A,PPR),(B,PPR),(C,PPR),(A,Blue Tongue),(D,Blue Tongue)",
			merged.totalDoses)
	}
	if merged.animalCount != merged.memberAnimals {
		t.Errorf("merged row %s: animal_count = %d but its own membership ledger holds %d distinct goats -- the row contradicts the ledger the exit / clinical-defer / shed-shift decrement paths trust",
			merged.assignmentID, merged.animalCount, merged.memberAnimals)
	}
	if merged.totalDoses != merged.memberDoses {
		t.Errorf("merged row %s: total_doses = %d but its own membership ledger holds %d member (dose) rows",
			merged.assignmentID, merged.totalDoses, merged.memberDoses)
	}
	if rows := laneRowsOnDate(t, ctx, pool, source); len(rows) != 0 {
		t.Errorf("source date still holds %d assignment row(s) after the whole PPR lane moved away: %+v", len(rows), rows)
	}
}

// TestDriveDateOverrideMergeMultiPageParkScopeStatusMatrix covers the three axes the single-cell
// merge fixture above cannot reach, all on the same write path:
//
//	MultiPage   -- a date move can collide on MORE THAN ONE cell at once. Counter reconciliation is a
//	               single set-based statement over the whole touched batch list; if it were ever
//	               truncated (a page, a LIMIT, a "first row wins" join) the second merged cell would
//	               keep the arriving lane's counts. Two sheds merge here and BOTH are asserted.
//	ParkScope   -- the move is scoped to one park's batches. A drive row in ANOTHER park must not be
//	               rewritten, and this one is also deliberately ledger-less with counters no ledger
//	               can justify (7 animals / 9 doses, zero members), so it doubles as the
//	               ledgerCoversRow completeness-gate proof: a row the ledger cannot account for is
//	               left alone, never shrunk to what the ledger happens to see.
//	StatusMatrix-- a CANCELED obligation is not work. It must not appear in the merged row's ledger
//	               and must not inflate either counter.
func TestDriveDateOverrideMergeMultiPageParkScopeStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.lanemerge2", 2)
	pprVersion, blueTongueVersion := versions[0], versions[1]

	const (
		goatA1    = "10000000-0000-4000-8000-00000000fd01"
		goatB1    = "10000000-0000-4000-8000-00000000fd02"
		goatE1    = "10000000-0000-4000-8000-00000000fd03"
		goatA2    = "10000000-0000-4000-8000-00000000fd04"
		goatD2    = "10000000-0000-4000-8000-00000000fd05"
		goatOther = "10000000-0000-4000-8000-00000000fd06"
		shed1     = "00000000-0000-4000-8000-00000000dd01"
		shed2     = "00000000-0000-4000-8000-00000000dd02"
		otherShed = "00000000-0000-4000-8000-00000000dd03"
		operatorA = "20000000-0000-4000-8000-000000000d01"
	)
	seedParkConsolidationShed(t, ctx, pool, shed1, "lane-merge2-shed-1")
	seedParkConsolidationShed(t, ctx, pool, shed2, "lane-merge2-shed-2")
	seedReserveGoats(t, ctx, pool, shed1, cbePark, goatA1, goatB1, goatE1)
	seedReserveGoats(t, ctx, pool, shed2, cbePark, goatA2, goatD2)
	// A shed in a DIFFERENT park, holding the row that must survive the move untouched.
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id)
VALUES ($1, $2, 'shed', 'lane-merge2-other-park-shed', 'lane-merge2-other-park-shed', 'active', $3)
ON CONFLICT (location_id) DO NOTHING`, otherShed, tenantID, cptPark); err != nil {
		t.Fatalf("seed other-park shed: %v", err)
	}
	seedReserveGoats(t, ctx, pool, otherShed, cptPark, goatOther)

	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rule_dimensions (
  tenant_id, protocol_version_id, rule_id, category, selector_key, dose_code, vaccine_code
) VALUES
  ($1, $2, $3, 'vaccination', 'lane-merge2-ppr', 'ppr_adult_w1', 'PPR'),
  ($1, $4, $5, 'vaccination', 'lane-merge2-bt', 'blue_tongue_adult_w1', 'BLUE_TONGUE')
ON CONFLICT (tenant_id, protocol_version_id, rule_id, selector_key) DO UPDATE SET
  vaccine_code = EXCLUDED.vaccine_code`,
		tenantID, pprVersion.versionID, pprVersion.ruleID, blueTongueVersion.versionID, blueTongueVersion.ruleID); err != nil {
		t.Fatalf("seed rule dimensions: %v", err)
	}

	// Business-DAY grain only: fixed calendar dates, whole-day arithmetic, no clock reading.
	source := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	target := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	weekOff := strings.ToLower(source.AddDate(0, 0, 2).Weekday().String())

	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1, $2, 'LMG-B', 'Lane Merge Operator B', 'active', 'operator', $3)`,
		operatorA, tenantID, cbePark); err != nil {
		t.Fatalf("seed operator: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, capability_code, effective_from, status)
VALUES ($1, 'lane_merge2_operator_a', 'vaccination', 'execute', 'vaccination.drive.execute', '2026-01-01', 'active')`,
		tenantID); err != nil {
		t.Fatalf("seed duties: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, week_off_weekday, vaccination_daily_animal_cap, status, valid_from)
VALUES ($1, $2, 'center', $3, 'lane_merge2_operator_a', 'manager', $4, 50, 'active', '2026-01-01')`,
		tenantID, operatorA, cbePark, weekOff); err != nil {
		t.Fatalf("seed position: %v", err)
	}

	type oblSeed struct {
		v      struct{ versionID, ruleID string }
		goat   string
		shed   string
		due    time.Time
		key    string
		status string
	}
	seeds := []oblSeed{
		{pprVersion, goatA1, shed1, source, "lane-merge2-ppr-a1", "scheduled"},
		{pprVersion, goatB1, shed1, source, "lane-merge2-ppr-b1", "scheduled"},
		// CANCELED: not work. Must not reach the merged row's ledger or its counters.
		{pprVersion, goatE1, shed1, source, "lane-merge2-ppr-e1", "canceled"},
		{blueTongueVersion, goatA1, shed1, target, "lane-merge2-bt-a1", "scheduled"},
		{pprVersion, goatA2, shed2, source, "lane-merge2-ppr-a2", "scheduled"},
		{blueTongueVersion, goatD2, shed2, target, "lane-merge2-bt-d2", "scheduled"},
	}
	oblIDs := make([]string, 0, len(seeds))
	for _, s := range seeds {
		oblID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: s.v.versionID, RuleID: s.v.ruleID,
			TargetType: "goat", TargetID: s.goat, ScopeType: "shed", ScopeID: s.shed,
			DueAt: s.due, Status: "scheduled", IdempotencyKey: s.key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("seed obligation %s: applied=%v err=%v", s.key, applied, err)
		}
		oblIDs = append(oblIDs, oblID)
	}
	plannedDate := source
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: pprVersion.versionID, ScopeType: "park", ScopeID: cbePark,
		Session: "lane-merge2-cells", PlannedDate: &plannedDate, Status: "planned",
		EstimatedTargets: 4, PlannedQuantity: "6", QuantityUnit: "dose",
	}, oblIDs)
	if err != nil {
		t.Fatalf("create cell batch: %v", err)
	}
	if int(attached) != len(oblIDs) {
		t.Fatalf("cell batch attached=%d want %d", attached, len(oblIDs))
	}
	// Cancel goatE1's PPR only AFTER it is attached to the batch, so the cell carries a real
	// canceled obligation that the merged row's ledger must exclude from both counters.
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status = 'canceled' WHERE tenant_id = $1 AND idempotency_key = 'lane-merge2-ppr-e1'`,
		tenantID); err != nil {
		t.Fatalf("cancel goatE1 PPR: %v", err)
	}

	// The other park's own batch and drive row -- a different park, a different batch, and a row with
	// NO membership ledger at all.
	otherOblID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: pprVersion.versionID, RuleID: pprVersion.ruleID,
		TargetType: "goat", TargetID: goatOther, ScopeType: "shed", ScopeID: otherShed,
		DueAt: target, Status: "scheduled", IdempotencyKey: "lane-merge2-other-park", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("seed other-park obligation: applied=%v err=%v", applied, err)
	}
	otherPlanned := target
	otherBatchID, _, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: pprVersion.versionID, ScopeType: "park", ScopeID: cptPark,
		Session: "lane-merge2-other-park", PlannedDate: &otherPlanned, Status: "planned",
		EstimatedTargets: 7, PlannedQuantity: "9", QuantityUnit: "dose",
	}, []string{otherOblID})
	if err != nil {
		t.Fatalf("create other-park batch: %v", err)
	}

	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: batchID, PlannedDate: target, OperatorID: testStringPtr(operatorA), ParkID: cbePark,
		ShedID: testStringPtr(shed1), PhysicalShed: "Gandhi", PartitionLabel: "1", AnimalCount: 1,
		VaccineRuleIDs: []string{blueTongueVersion.ruleID}, TotalDoses: 1, CapacityStatus: "within_cap",
	}, {
		BatchID: batchID, PlannedDate: source, OperatorID: testStringPtr(operatorA), ParkID: cbePark,
		ShedID: testStringPtr(shed1), PhysicalShed: "Gandhi", PartitionLabel: "1", AnimalCount: 2,
		VaccineRuleIDs: []string{pprVersion.ruleID}, TotalDoses: 2, CapacityStatus: "within_cap",
	}, {
		BatchID: batchID, PlannedDate: target, OperatorID: testStringPtr(operatorA), ParkID: cbePark,
		ShedID: testStringPtr(shed2), PhysicalShed: "Nehru", PartitionLabel: "1", AnimalCount: 1,
		VaccineRuleIDs: []string{blueTongueVersion.ruleID}, TotalDoses: 1, CapacityStatus: "within_cap",
	}, {
		BatchID: batchID, PlannedDate: source, OperatorID: testStringPtr(operatorA), ParkID: cbePark,
		ShedID: testStringPtr(shed2), PhysicalShed: "Nehru", PartitionLabel: "1", AnimalCount: 1,
		VaccineRuleIDs: []string{pprVersion.ruleID}, TotalDoses: 1, CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed assignments: %v", err)
	}
	// Written OUTSIDE the upsert path on purpose: this row must keep counters no ledger can justify.
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_drive_assignments (
  tenant_id, batch_id, planned_date, operator_id, park_id, shed_id,
  physical_shed, partition_label, animal_count, vaccine_rule_ids, total_doses, capacity_status, warnings)
VALUES ($1, $2, $3, $4, $5, $6, 'Patel', '1', 7, ARRAY[$7::uuid], 9, 'within_cap', '[]'::jsonb)`,
		tenantID, otherBatchID, target, operatorA, cptPark, otherShed, pprVersion.ruleID); err != nil {
		t.Fatalf("seed other-park drive row: %v", err)
	}

	if _, err := repo.UpsertVaccinationDriveDateOverride(ctx, domain.VaccineDriveDateOverride{
		TenantID: tenantID, ParkID: cbePark, VaccineCode: "PPR", OriginalDriveDate: source,
		OverrideDate: target, Reason: "admin moves PPR onto the day that already runs Blue Tongue", CreatedBy: operatorA,
	}); err != nil {
		t.Fatalf("UpsertVaccinationDriveDateOverride: %v", err)
	}

	merged := map[string]laneRow{}
	for _, row := range laneRowsOnDate(t, ctx, pool, target) {
		var shed, park string
		if err := pool.QueryRow(ctx,
			`SELECT COALESCE(shed_id::text,''), park_id::text FROM vaccination_drive_assignments WHERE tenant_id=$1 AND assignment_id=$2`,
			tenantID, row.assignmentID).Scan(&shed, &park); err != nil {
			t.Fatalf("read row cell: %v", err)
		}
		if park != cbePark {
			if row.animalCount != 7 || row.totalDoses != 9 {
				t.Errorf("the OTHER park's drive row was rewritten to %d animals / %d doses -- a date move in one park must not touch another park's batch, and a row with no membership ledger must never be shrunk to what the ledger can see (completeness gate)",
					row.animalCount, row.totalDoses)
			}
			continue
		}
		merged[shed] = row
	}
	if len(merged) != 2 {
		t.Fatalf("expected BOTH cells to merge onto the target date, got %d: %+v", len(merged), merged)
	}
	// shed1: {A1,B1} animals; doses (A1,PPR),(B1,PPR),(A1,Blue Tongue). The canceled goatE1 PPR is absent.
	// shed2: {A2,D2} animals; doses (A2,PPR),(D2,Blue Tongue).
	for _, want := range []struct {
		shed    string
		label   string
		animals int
		doses   int
	}{
		{shed1, "shed1", 2, 3},
		{shed2, "shed2", 2, 2},
	} {
		row, ok := merged[want.shed]
		if !ok {
			t.Errorf("%s: no merged row on the target date", want.label)
			continue
		}
		if row.animalCount != want.animals {
			t.Errorf("%s merged row animal_count = %d, want %d -- both merged cells must be reconciled, and a CANCELED obligation is not work",
				want.label, row.animalCount, want.animals)
		}
		if row.totalDoses != want.doses {
			t.Errorf("%s merged row total_doses = %d, want %d", want.label, row.totalDoses, want.doses)
		}
		if row.animalCount != row.memberAnimals || row.totalDoses != row.memberDoses {
			t.Errorf("%s merged row %s: counters (%d animals / %d doses) contradict its own membership ledger (%d goats / %d member rows)",
				want.label, row.assignmentID, row.animalCount, row.totalDoses, row.memberAnimals, row.memberDoses)
		}
	}
}
