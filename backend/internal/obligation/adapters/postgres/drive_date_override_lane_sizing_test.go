package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
)

// laneRow is one persisted drive-assignment row reduced to the two counters this test is about,
// plus the counters recomputed from the row's OWN per-goat membership ledger.
type laneRow struct {
	assignmentID  string
	operatorID    string
	ruleIDs       []string
	animalCount   int
	totalDoses    int
	memberAnimals int
	memberDoses   int
}

func laneRowsOnDate(t *testing.T, ctx context.Context, pool *pgxpool.Pool, date time.Time) []laneRow {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT vda.assignment_id::text,
       COALESCE(vda.operator_id::text, ''),
       (SELECT COALESCE(array_agg(r::text ORDER BY r), '{}'::text[]) FROM unnest(vda.vaccine_rule_ids) AS r),
       vda.animal_count,
       vda.total_doses,
       (SELECT count(DISTINCT m.goat_id) FROM vaccination_drive_assignment_members m
         WHERE m.tenant_id = vda.tenant_id AND m.assignment_id = vda.assignment_id),
       (SELECT count(*) FROM vaccination_drive_assignment_members m
         WHERE m.tenant_id = vda.tenant_id AND m.assignment_id = vda.assignment_id)
FROM vaccination_drive_assignments vda
WHERE vda.tenant_id = $1 AND vda.planned_date = $2
ORDER BY vda.assignment_id`, tenantID, date)
	if err != nil {
		t.Fatalf("query lane rows: %v", err)
	}
	defer rows.Close()
	out := make([]laneRow, 0)
	for rows.Next() {
		var row laneRow
		if err := rows.Scan(&row.assignmentID, &row.operatorID, &row.ruleIDs, &row.animalCount,
			&row.totalDoses, &row.memberAnimals, &row.memberDoses); err != nil {
			t.Fatalf("scan lane row: %v", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("lane rows: %v", err)
	}
	return out
}

// TestDriveDateOverrideMoveSizesCountsPerVaccineLaneNotPerCell is the cap-safety guard for the
// vaccine-lane SPLIT that a drive-date override performs.
//
// A drive-assignment row's grain is one (batch, park, shed, physical shed, partition, operator)
// CELL whose vaccine_rule_ids is the UNION of every vaccine planned in that cell. Its two counters
// have DIFFERENT grains, and that difference is the whole point:
//
//	animal_count = COUNT(DISTINCT animal) in the cell -- the operator cap unit ("unique animals per
//	               assigned operator per day"). An animal needing two vaccines counts ONCE.
//	total_doses  = COUNT(DISTINCT (animal, rule)) in the cell -- the dose/stock unit. That same
//	               animal counts TWICE.
//
// A date move re-splits one such cell along the vaccine axis: the moved rules leave for the target
// date, the remaining rules stay. After that split each side is a NARROWER lane, so BOTH counters
// must be re-derived over that lane's OWN animals. Copying the cell-wide animal_count onto the
// moved lane books capacity for animals that are not in it (over-filling the target operator's day
// and starving real work of slots), and multiplying animal_count by cardinality(rule_ids) turns
// total_doses into a CARTESIAN product that only accidentally agrees when every animal in the cell
// happens to need every vaccine in it.
//
// Fixture: one cell, three goats, two vaccines. All three need PPR; only ONE also needs FMD.
//
//	cell before the move: animal_count=3, total_doses=4, rules={PPR,FMD}
//	move FMD -> target:   source lane {PPR} = 3 animals / 3 doses
//	                      target lane {FMD} = 1 animal  / 1 dose
//
// The per-goat membership ledger (vaccination_drive_assignment_members, now written per vaccine
// lane) is the independent witness: a row whose animal_count disagrees with COUNT(DISTINCT goat_id)
// of its OWN members, or whose total_doses disagrees with COUNT(*) of them, contradicts its own
// ledger and is detectable in production. This test asserts that parity on every row of both dates.
func TestDriveDateOverrideMoveSizesCountsPerVaccineLaneNotPerCell(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.lanesize", 2)
	pprVersion, fmdVersion := versions[0], versions[1]

	const (
		goatA     = "10000000-0000-4000-8000-00000000fb01"
		goatB     = "10000000-0000-4000-8000-00000000fb02"
		goatC     = "10000000-0000-4000-8000-00000000fb03"
		shedID    = "00000000-0000-4000-8000-00000000db01"
		operatorA = "20000000-0000-4000-8000-000000000b01"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "lane-size-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatA, goatB, goatC)

	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rule_dimensions (
  tenant_id, protocol_version_id, rule_id, category, selector_key, dose_code, vaccine_code
) VALUES
  ($1, $2, $3, 'vaccination', 'lane-size-ppr', 'ppr_adult_w1', 'PPR'),
  ($1, $4, $5, 'vaccination', 'lane-size-fmd', 'fmd_adult_w1', 'FMD')
ON CONFLICT (tenant_id, protocol_version_id, rule_id, selector_key) DO UPDATE SET
  vaccine_code = EXCLUDED.vaccine_code`,
		tenantID, pprVersion.versionID, pprVersion.ruleID, fmdVersion.versionID, fmdVersion.ruleID); err != nil {
		t.Fatalf("seed rule dimensions: %v", err)
	}

	source := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	target := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	otherWeekday := strings.ToLower(source.AddDate(0, 0, 1).Weekday().String())

	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1, $2, 'LSZ-A', 'Lane Size Operator A', 'active', 'operator', $3)`,
		operatorA, tenantID, cbePark); err != nil {
		t.Fatalf("seed operator: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, capability_code, effective_from, status)
VALUES ($1, 'lane_size_operator_a', 'vaccination', 'execute', 'vaccination.drive.execute', '2026-01-01', 'active')`,
		tenantID); err != nil {
		t.Fatalf("seed duties: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, week_off_weekday, vaccination_daily_animal_cap, status, valid_from)
VALUES ($1, $2, 'center', $3, 'lane_size_operator_a', 'manager', $4, 50, 'active', '2026-01-01')`,
		tenantID, operatorA, cbePark, otherWeekday); err != nil {
		t.Fatalf("seed position: %v", err)
	}

	// ONE batch holding all four obligations, so the cell is one drive-assignment row with two
	// vaccine lanes: PPR for all three goats, FMD for goatA only.
	oblIDs := make([]string, 0, 4)
	seed := []struct {
		v    struct{ versionID, ruleID string }
		goat string
		key  string
	}{
		{pprVersion, goatA, "lane-size-ppr-a"},
		{pprVersion, goatB, "lane-size-ppr-b"},
		{pprVersion, goatC, "lane-size-ppr-c"},
		{fmdVersion, goatA, "lane-size-fmd-a"},
	}
	for _, s := range seed {
		oblID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: s.v.versionID, RuleID: s.v.ruleID,
			TargetType: "goat", TargetID: s.goat, ScopeType: "shed", ScopeID: shedID,
			DueAt: source, Status: "scheduled", IdempotencyKey: s.key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("seed obligation %s: applied=%v err=%v", s.key, applied, err)
		}
		oblIDs = append(oblIDs, oblID)
	}
	plannedDate := source
	batchID, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: pprVersion.versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "lane-size-cell", PlannedDate: &plannedDate, Status: "planned",
		EstimatedTargets: 3, PlannedQuantity: "4", QuantityUnit: "dose",
	}, oblIDs)
	if err != nil {
		t.Fatalf("create cell batch: %v", err)
	}
	if int(attached) != len(oblIDs) {
		t.Fatalf("cell batch attached=%d want %d", attached, len(oblIDs))
	}

	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: batchID, PlannedDate: source, OperatorID: testStringPtr(operatorA), ParkID: cbePark,
		ShedID: testStringPtr(shedID), PhysicalShed: "Gandhi", PartitionLabel: "1", AnimalCount: 3,
		VaccineRuleIDs: []string{pprVersion.ruleID, fmdVersion.ruleID}, TotalDoses: 4,
		CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed source-date assignment: %v", err)
	}

	// Fixture sanity: before the move the single cell row carries 3 animals / 4 doses and its
	// ledger agrees.
	before := laneRowsOnDate(t, ctx, pool, source)
	if len(before) != 1 {
		t.Fatalf("fixture invalid: source date has %d assignment rows, want exactly 1 cell: %+v", len(before), before)
	}
	if before[0].animalCount != 3 || before[0].totalDoses != 4 {
		t.Fatalf("fixture invalid: cell = %d animals / %d doses, want 3/4: %+v", before[0].animalCount, before[0].totalDoses, before[0])
	}
	if before[0].memberAnimals != 3 || before[0].memberDoses != 4 {
		t.Fatalf("fixture invalid: cell ledger = %d goats / %d member rows, want 3/4: %+v", before[0].memberAnimals, before[0].memberDoses, before[0])
	}

	if _, err := repo.UpsertVaccinationDriveDateOverride(ctx, domain.VaccineDriveDateOverride{
		TenantID: tenantID, ParkID: cbePark, VaccineCode: "FMD", OriginalDriveDate: source,
		OverrideDate: target, Reason: "admin moves FMD off the PPR day", CreatedBy: operatorA,
	}); err != nil {
		t.Fatalf("UpsertVaccinationDriveDateOverride: %v", err)
	}

	assertLane := func(label string, rows []laneRow, wantAnimals, wantDoses int) {
		t.Helper()
		if len(rows) == 0 {
			t.Fatalf("%s lane: no assignment rows persisted, want %d animals / %d doses", label, wantAnimals, wantDoses)
		}
		animals, doses := 0, 0
		for _, row := range rows {
			animals += row.animalCount
			doses += row.totalDoses
			if row.animalCount != row.memberAnimals {
				t.Errorf("%s lane row %s: animal_count = %d but its own membership ledger holds %d distinct goats -- the row contradicts its own ledger",
					label, row.assignmentID, row.animalCount, row.memberAnimals)
			}
			if row.totalDoses != row.memberDoses {
				t.Errorf("%s lane row %s: total_doses = %d but its own membership ledger holds %d member (dose) rows -- the row contradicts its own ledger",
					label, row.assignmentID, row.totalDoses, row.memberDoses)
			}
		}
		if animals != wantAnimals {
			t.Errorf("%s lane sum(animal_count) = %d, want %d (the operator cap unit is UNIQUE ANIMALS IN THIS LANE, not the whole cell's animals)",
				label, animals, wantAnimals)
		}
		if doses != wantDoses {
			t.Errorf("%s lane sum(total_doses) = %d, want %d (doses are DISTINCT (animal, rule) pairs in this lane, not animal_count x cardinality(rule_ids))",
				label, doses, wantDoses)
		}
	}

	// The PPR work never moved: all three goats stay on the source date, one dose each.
	assertLane("source PPR", laneRowsOnDate(t, ctx, pool, source), 3, 3)
	// Only goatA needs FMD, so the moved lane is ONE animal and ONE dose -- not the cell's three.
	assertLane("target FMD", laneRowsOnDate(t, ctx, pool, target), 1, 1)
}
