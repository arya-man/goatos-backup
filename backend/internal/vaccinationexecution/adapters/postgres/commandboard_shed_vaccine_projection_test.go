package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// The five adversarial lenses the aggregate/projection contract requires of any new read model:
// cardinality, pagination, date, scope, status. The shed x vaccine matrix is a new aggregate over
// obligation_instances, so each lens gets a test that can actually FAIL rather than a rename of an
// existing one.

// TestVaccinationCommandBoardShedVaccineOneToManyMultipleDimensions — CARDINALITY.
//
// protocol_rule_dimensions is 1..N per rule_id, not 0..1: publish compiles a single rule into one
// dimension row per selector combination, up to thousands. The matrix joins it to read vaccine_code,
// so the join multiplies every obligation row by that fan-out before aggregation. The counts survive
// only because vaccine_code is invariant across a rule's dimensions and both counts are
// COUNT(DISTINCT target_id).
//
// This fixture gives ONE rule THREE dimension rows and one animal TWO obligations of that rule. A
// naive COUNT(*) would report 6; anything that drops the DISTINCT reports 2 or 3. The truth is 1.
func TestVaccinationCommandBoardShedVaccineOneToManyMultipleDimensions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000a1"
	parkID := uuidFromSuffix("01", "g1")
	shedID := uuidFromSuffix("02", "g1")
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, "g1")
	seedCommandBoardPark(t, ctx, pool, tenantID, parkID, shedID, "Fanout")

	// THREE dimension rows for ONE rule — the real publish shape.
	for i, selector := range []string{"sel-a", "sel-b", "sel-c"} {
		seedVaccineDimension(t, ctx, pool, tenantID, protocolVersionID, ruleID,
			uuidFromSuffix("0b", fmt.Sprintf("g1d%d", i)), selector, "ET_TT")
	}

	asOf := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	goatID := uuidFromSuffix("03", "g1a")
	seedBareGoat(t, ctx, pool, tenantID, shedID, goatID, uuidFromSuffix("0a", "g1a"))
	// TWO behind obligations of the same vaccine on the SAME animal.
	seedObligation(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, goatID,
		uuidFromSuffix("08", "g1a"), "missed", asOf.Add(-3*24*time.Hour), "fanout-a")
	seedObligation(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, goatID,
		uuidFromSuffix("08", "g1b"), "missed", asOf.Add(-6*24*time.Hour), "fanout-b")

	cells := shedVaccineCellsByCode(t, ctx, pool, tenantID, asOf)
	et := cells["ET_TT"]
	if et.BehindAnimals != 1 {
		t.Fatalf("behindAnimals = %d, want 1; 3 dimension rows x 2 obligations must not multiply ONE animal", et.BehindAnimals)
	}
	if et.TotalAnimals != 1 {
		t.Fatalf("totalAnimals = %d, want 1", et.TotalAnimals)
	}
	// The drill-down folds the same fan-out via DISTINCT ON and must agree with the count.
	if got := len(et.FlaggedAnimals); got != 1 {
		t.Fatalf("flaggedAnimals has %d rows, want 1; the list must fold the dimension fan-out the same way the count does", got)
	}
}

// TestVaccinationCommandBoardShedVaccinePaginationPageBoundaryMultiPage — PAGINATION.
//
// The drill-down is capped across ALL behind cells in one board read, so the cap is a global budget
// and the ORDER decides who survives it. Order is (shed, vaccine, animal) for the DISTINCT ON, then
// oldest due first, so truncation keeps the animals that have been waiting longest rather than an
// arbitrary page. This pins the ordering the cap depends on, and pins that an untruncated list
// reconciles exactly with the cell's count.
func TestVaccinationCommandBoardShedVaccinePaginationPageBoundaryMultiPage(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000a2"
	parkID := uuidFromSuffix("01", "g2")
	shedID := uuidFromSuffix("02", "g2")
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, "g2")
	seedCommandBoardPark(t, ctx, pool, tenantID, parkID, shedID, "Cap")
	seedVaccineDimension(t, ctx, pool, tenantID, protocolVersionID, ruleID,
		uuidFromSuffix("0b", "g2d"), "sel-a", "ET_TT")

	asOf := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	// Seeded newest-first so a list that simply preserved insertion order would fail the ordering
	// assertion below.
	for i, ageDays := range []int{2, 30, 9} {
		suffix := fmt.Sprintf("g2%d", i)
		goatID := uuidFromSuffix("03", suffix)
		seedBareGoat(t, ctx, pool, tenantID, shedID, goatID, uuidFromSuffix("0a", suffix))
		seedObligation(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, goatID,
			uuidFromSuffix("08", suffix), "missed", asOf.Add(-time.Duration(ageDays)*24*time.Hour), "cap-"+suffix)
	}

	cells := shedVaccineCellsByCode(t, ctx, pool, tenantID, asOf)
	et := cells["ET_TT"]
	if et.BehindAnimals != 3 {
		t.Fatalf("behindAnimals = %d, want 3", et.BehindAnimals)
	}
	if len(et.FlaggedAnimals) != et.BehindAnimals {
		t.Fatalf("list has %d rows but the cell counts %d; under the cap the list must reconcile exactly with the count", len(et.FlaggedAnimals), et.BehindAnimals)
	}
	// Oldest due first: the property that makes truncation meaningful rather than arbitrary.
	for i := 1; i < len(et.FlaggedAnimals); i++ {
		prev, cur := et.FlaggedAnimals[i-1], et.FlaggedAnimals[i]
		if prev.DueAt == nil || cur.DueAt == nil {
			t.Fatalf("behind animal missing dueAt; the cap's ordering key must always be present")
		}
		if cur.DueAt.Before(*prev.DueAt) {
			t.Fatalf("list is not oldest-due-first at index %d (%s before %s); truncation would drop the longest-waiting animals", i, cur.DueAt, prev.DueAt)
		}
	}
	if domain.CommandBoardShedVaccineAnimalListCap <= 0 {
		t.Fatalf("cap = %d; an unbounded drill-down turns a bounded board read into a whole-herd scan", domain.CommandBoardShedVaccineAnimalListCap)
	}
}

// TestVaccinationCommandBoardShedVaccineDateShiftScheduledDateExecutionDate — DATE.
//
// Behind is a BUSINESS-DAY comparison in Asia/Kolkata, never an instant. A dose due TODAY must read
// on-track at every clock time of that day and only roll to behind on the next business day. An
// instant comparison makes a whole shed go red at 00:01 IST on the morning of its own drive.
func TestVaccinationCommandBoardShedVaccineDateShiftScheduledDateExecutionDate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000a3"
	parkID := uuidFromSuffix("01", "g3")
	shedID := uuidFromSuffix("02", "g3")
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, "g3")
	seedCommandBoardPark(t, ctx, pool, tenantID, parkID, shedID, "Clock")
	seedVaccineDimension(t, ctx, pool, tenantID, protocolVersionID, ruleID,
		uuidFromSuffix("0b", "g3d"), "sel-a", "ET_TT")

	// 2026-08-07 18:00 UTC is 2026-08-07 23:30 IST — late on the SAME IST day the dose is due.
	asOf := time.Date(2026, 8, 7, 18, 0, 0, 0, time.UTC)
	// Due 2026-08-07 05:00 IST: earlier that same IST day, so already past as an INSTANT.
	dueSameISTDay := time.Date(2026, 8, 6, 23, 30, 0, 0, time.UTC)

	goatID := uuidFromSuffix("03", "g3a")
	seedBareGoat(t, ctx, pool, tenantID, shedID, goatID, uuidFromSuffix("0a", "g3a"))
	seedObligation(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, goatID,
		uuidFromSuffix("08", "g3a"), "scheduled", dueSameISTDay, "clock-a")

	cells := shedVaccineCellsByCode(t, ctx, pool, tenantID, asOf)
	if got := cells["ET_TT"].State; got != "ok" {
		t.Fatalf("state = %q, want \"ok\"; a dose due TODAY in IST must not read behind merely because as_of is later the same day", got)
	}

	// Next IST business day: the same obligation must now be behind.
	nextDay := asOf.Add(24 * time.Hour)
	cells = shedVaccineCellsByCode(t, ctx, pool, tenantID, nextDay)
	if got := cells["ET_TT"].State; got != "behind" {
		t.Fatalf("state = %q the next day, want \"behind\"; the date comparison must actually roll over", got)
	}
}

// TestVaccinationCommandBoardShedVaccineScopeHierarchyParkScope — SCOPE.
//
// Two halves, both of which produced wrong rows before they were guarded:
//
//	park filter — an obligation in ANOTHER park must not enter a park-scoped board. The filter
//	reaches the animal only through locations.parent_location_id.
//	scope_type  — obligation_instances.scope_type is not always 'shed'. A PARK-scoped obligation
//	joins locations on the park row and, ungated, renders as a phantom shed named after the park.
func TestVaccinationCommandBoardShedVaccineScopeHierarchyParkScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000a4"
	mineParkID := uuidFromSuffix("01", "g4a")
	otherParkID := uuidFromSuffix("01", "g4b")
	mineShedID := uuidFromSuffix("02", "g4a")
	otherShedID := uuidFromSuffix("02", "g4b")
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, "g4")
	seedCommandBoardPark(t, ctx, pool, tenantID, mineParkID, mineShedID, "Mine")
	seedCommandBoardPark(t, ctx, pool, tenantID, otherParkID, otherShedID, "Other")
	seedVaccineDimension(t, ctx, pool, tenantID, protocolVersionID, ruleID,
		uuidFromSuffix("0b", "g4d"), "sel-a", "ET_TT")

	asOf := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	for i, shed := range []string{mineShedID, otherShedID} {
		suffix := fmt.Sprintf("g4s%d", i)
		goatID := uuidFromSuffix("03", suffix)
		seedBareGoat(t, ctx, pool, tenantID, shed, goatID, uuidFromSuffix("0a", suffix))
		seedObligation(t, ctx, pool, tenantID, protocolVersionID, ruleID, shed, goatID,
			uuidFromSuffix("08", suffix), "missed", asOf.Add(-3*24*time.Hour), "scope-"+suffix)
	}

	// A PARK-scoped obligation. Without the scope_type='shed' guard this becomes a phantom row.
	parkGoatID := uuidFromSuffix("03", "g4p")
	seedBareGoat(t, ctx, pool, tenantID, mineShedID, parkGoatID, uuidFromSuffix("0a", "g4p"))
	execProjectionSQL(t, ctx, pool, "park-scoped obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'goat', 'park', $5, $6, 'missed', $7::timestamptz, 'scope-park')`,
		uuidFromSuffix("08", "g4p"), tenantID, protocolVersionID, parkGoatID, mineParkID, ruleID, asOf.Add(-3*24*time.Hour))

	repo := NewRepository(pool, 5*time.Second)
	scoped, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{
		TenantID: tenantID, ParkID: &mineParkID, AsOf: asOf,
	})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}
	sheds := map[string]bool{}
	for _, cell := range scoped.ShedVaccineMatrix {
		sheds[cell.ShedID] = true
		if cell.ShedID == mineParkID {
			t.Fatalf("the PARK appears as a shed row; scope_type='shed' must exclude park-scoped obligations")
		}
		if cell.ShedID == "" {
			t.Fatalf("a blank shed row exists; obligations whose scope is not a shed must be excluded, not COALESCEd into one phantom row")
		}
	}
	if len(sheds) != 1 || !sheds[mineShedID] {
		t.Fatalf("park-scoped board has sheds %v, want only %s; the other park must not enter", sheds, mineShedID)
	}

	all, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("unscoped VaccinationCommandBoard() error = %v", err)
	}
	allSheds := map[string]bool{}
	for _, cell := range all.ShedVaccineMatrix {
		allSheds[cell.ShedID] = true
	}
	if len(allSheds) != 2 {
		t.Fatalf("unscoped board has %d sheds, want 2; the park filter must NARROW, not be the only path that works", len(allSheds))
	}
}

// TestVaccinationCommandBoardShedVaccineStatusMatrixEveryStatusStatusBuckets — STATUS.
//
// Every obligation status an animal can hold, mapped to the cell state it must produce. The cell is
// a boolean OR, so the risk is not double counting but a status silently falling on the wrong side:
// a terminal 'canceled' dose must NOT make a shed red (nothing is owed), while an accepted
// completion must clear an otherwise past-due dose.
func TestVaccinationCommandBoardShedVaccineStatusMatrixEveryStatusStatusBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	asOf := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	past := asOf.Add(-5 * 24 * time.Hour)

	cases := []struct {
		name             string
		obligationStatus string
		completionStatus string
		wantState        string
		why              string
	}{
		{"missed", "missed", "", "behind", "the window closed unvaccinated"},
		// This expectation was WRONG when first written, and the wrongness shipped: "proof recorded
		// after the window shut is not a dose given in time" assumed the proof arrived late. On live
		// stg the opposite is true -- all 137 such doses were administered ON the due day and the
		// obligation was swept to 'missed' only because no verifier had looked at the proof yet.
		// Red here accuses an operator who did the work on time.
		{"missed with unverified proof", "missed", "recorded", "verifying", "dose given, proof queued for a verifier -- a desk backlog, not an unvaccinated animal"},
		{"past due still scheduled", "scheduled", "", "behind", "the sweeper has not run; the animal is still unvaccinated past its window"},
		{"past due in progress", "in_progress", "", "behind", "started is not given"},
		{"past due accepted", "scheduled", "accepted", "ok", "an accepted completion clears the dose regardless of the due date"},
		{"completed and accepted", "completed", "accepted", "ok", "done"},
		{"canceled with no dose", "canceled", "", "ok", "terminal with nothing owed must not paint the shed red"},
	}

	for i, tc := range cases {
		suffix := fmt.Sprintf("g5%d", i)
		tenantID := fmt.Sprintf("00000000-0000-4000-8000-0000000a5%03d", i)
		parkID := uuidFromSuffix("01", suffix)
		shedID := uuidFromSuffix("02", suffix)
		protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, suffix)
		seedCommandBoardPark(t, ctx, pool, tenantID, parkID, shedID, "Status")
		seedVaccineDimension(t, ctx, pool, tenantID, protocolVersionID, ruleID,
			uuidFromSuffix("0b", suffix), "sel-a", "ET_TT")

		goatID := uuidFromSuffix("03", suffix)
		oblID := uuidFromSuffix("08", suffix)
		seedBareGoat(t, ctx, pool, tenantID, shedID, goatID, uuidFromSuffix("0a", suffix))
		seedObligation(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, goatID, oblID,
			tc.obligationStatus, past, "status-"+suffix)
		if tc.completionStatus != "" {
			verified := "NULL"
			if tc.completionStatus == "accepted" {
				verified = "$5::timestamptz"
			}
			execProjectionSQL(t, ctx, pool, "completion "+tc.completionStatus,
				`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, status, administered_at, verified_at, idempotency_key)
				 VALUES ($1, $2, $3, $4, '`+tc.completionStatus+`', $5::timestamptz, `+verified+`, $6)`,
				uuidFromSuffix("09", suffix), tenantID, oblID, goatID, past, "status-completion-"+suffix)
		}

		cells := shedVaccineCellsByCode(t, ctx, pool, tenantID, asOf)
		if got := cells["ET_TT"].State; got != tc.wantState {
			t.Errorf("%s: state = %q, want %q — %s", tc.name, got, tc.wantState, tc.why)
		}
	}
}

func TestVaccinationCommandBoardShedVaccineAnimalListIgnoresStalePartitionFromOtherShed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000aa"
	parkID := uuidFromSuffix("01", "g10a")
	shedID := uuidFromSuffix("02", "g10a")
	staleParkID := uuidFromSuffix("01", "g10b")
	staleShedID := uuidFromSuffix("02", "g10b")
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, "g10")
	seedCommandBoardPark(t, ctx, pool, tenantID, parkID, shedID, "Mandela 2")
	seedCommandBoardPark(t, ctx, pool, tenantID, staleParkID, staleShedID, "Yashoda")
	seedVaccineDimension(t, ctx, pool, tenantID, protocolVersionID, ruleID,
		uuidFromSuffix("0b", "g10d"), "sel-a", "GOAT_POX")

	asOf := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	goatID := uuidFromSuffix("03", "g10a")
	seedBareGoat(t, ctx, pool, tenantID, shedID, goatID, uuidFromSuffix("0a", "g10a"))
	seedObligation(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, goatID,
		uuidFromSuffix("08", "g10a"), "missed", asOf.Add(-3*24*time.Hour), "stale-partition")

	// A stale per-goat partition row from a previous shed must not re-key the animal drawer list.
	// The aggregate query already joins gsp.shed_id = goats.shed_id; the drilldown query must use
	// the same scope or the red cell opens with a title from one shed and animal evidence from
	// another partition.
	execProjectionSQL(t, ctx, pool, "stale partition row",
		`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
		 VALUES ($1, $2, $3, 'Part 8', 'Yashoda - Part 8')`,
		tenantID, goatID, staleShedID)

	repo := NewRepository(pool, 5*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}
	var cell *domain.CommandBoardShedVaccineCell
	for i := range resp.ShedVaccineMatrix {
		candidate := &resp.ShedVaccineMatrix[i]
		if candidate.ShedID == shedID && candidate.VaccineCode == "GOAT_POX" {
			cell = candidate
			break
		}
	}
	if cell == nil {
		t.Fatalf("missing current-shed GOAT_POX cell")
	}
	if cell.OperationalLocationDisplay != "Mandela 2" {
		t.Fatalf("cell display = %q, want current shed without stale partition", cell.OperationalLocationDisplay)
	}
	if cell.BehindAnimals != 1 {
		t.Fatalf("behindAnimals = %d, want 1", cell.BehindAnimals)
	}
	if len(cell.FlaggedAnimals) != 1 {
		t.Fatalf("flaggedAnimals = %d, want 1; stale partition re-keyed the drawer list away from the clicked cell", len(cell.FlaggedAnimals))
	}
	if got := cell.FlaggedAnimals[0].PartitionLabel; got != "" {
		t.Fatalf("flagged partitionLabel = %q, want empty because the only partition row belongs to another shed", got)
	}
}

// shedVaccineCellsByCode reads the board and indexes its shed x vaccine cells by vaccine code.
// Every test here seeds exactly one shed, so code alone identifies a cell.
func shedVaccineCellsByCode(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, asOf time.Time) map[string]domain.CommandBoardShedVaccineCell {
	t.Helper()
	repo := NewRepository(pool, 5*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}
	byCode := map[string]domain.CommandBoardShedVaccineCell{}
	for _, cell := range resp.ShedVaccineMatrix {
		byCode[cell.VaccineCode] = cell
	}
	return byCode
}

func seedVaccineDimension(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, protocolVersionID, ruleID, dimensionID, selectorKey, vaccineCode string) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "vaccine dimension "+selectorKey,
		`INSERT INTO protocol_rule_dimensions (protocol_rule_dimension_id, tenant_id, protocol_version_id, rule_id, category, selector_key, vaccine_code)
		 VALUES ($1, $2, $3, $4, 'vaccination', $5, $6)`,
		dimensionID, tenantID, protocolVersionID, ruleID, selectorKey, vaccineCode)
}

func seedBareGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, shedID, goatID, partyID string) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "custodian party",
		`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1, 'org', 'Custodian', 'active')`, partyID)
	execProjectionSQL(t, ctx, pool, "goat",
		`INSERT INTO goats (goat_id, tenant_id, sex, lifecycle_status, management_stage, shed_id, custodian_party_id, dob)
		 VALUES ($1, $2, 'female', 'alive', 'Non-Pregnant', $3, $4, '2025-01-01')`, goatID, tenantID, shedID, partyID)
}

func seedObligation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, protocolVersionID, ruleID, shedID, goatID, oblID, status string, dueAt time.Time, idempotencyKey string) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "obligation "+status,
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, $7, $8::timestamptz, $9)`,
		oblID, tenantID, protocolVersionID, goatID, shedID, ruleID, status, dueAt, idempotencyKey)
}

// TestVaccinationCommandBoardShedVaccineRecordedProofReadsVerifyingNotBehind is the regression for
// the board accusing operators who had already done the work.
//
// A dose given on its due day, whose proof is recorded and waiting on a verifier, must read
// VERIFYING (amber) and never BEHIND (red). On live stg every one of the 137 obligations sitting at
// status='missed' was administered on 2026-08-05 IST -- the exact day it was due -- and carried a
// recorded, unverified completion. Flagging those red told a park head that 76 goats in Sumathi 1
// were unvaccinated when the operator had vaccinated all 76 on time, and it contradicted the
// dose-qualified matrix on the same screen, which correctly showed them amber.
//
// The two states must also stay SEPARATE counts: a herd nobody dosed and a queue nobody verified
// need different people to act, so summing them into one number destroys the only decision the cell
// supports.
func TestVaccinationCommandBoardShedVaccineRecordedProofReadsVerifyingNotBehind(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000a6"
	parkID := uuidFromSuffix("01", "g6")
	shedID := uuidFromSuffix("02", "g6")
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, "g6")
	seedCommandBoardPark(t, ctx, pool, tenantID, parkID, shedID, "Verify")
	seedVaccineDimension(t, ctx, pool, tenantID, protocolVersionID, ruleID,
		uuidFromSuffix("0b", "g6d"), "sel-a", "ET_TT")

	asOf := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	due := asOf.Add(-2 * 24 * time.Hour)

	// The live shape: obligation swept to 'missed', dose administered ON the due day, proof recorded
	// and never verified.
	goatID := uuidFromSuffix("03", "g6a")
	oblID := uuidFromSuffix("08", "g6a")
	seedBareGoat(t, ctx, pool, tenantID, shedID, goatID, uuidFromSuffix("0a", "g6a"))
	seedObligation(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, goatID, oblID, "missed", due, "verify-a")
	execProjectionSQL(t, ctx, pool, "recorded completion",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, status, administered_at, verified_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'recorded', $5::timestamptz, NULL, 'verify-completion-a')`,
		uuidFromSuffix("09", "g6a"), tenantID, oblID, goatID, due)

	// A second animal in the same shed genuinely missed with NO proof, so the two states are proven
	// to be counted separately rather than one label winning for the whole shed.
	goatB := uuidFromSuffix("03", "g6b")
	seedBareGoat(t, ctx, pool, tenantID, shedID, goatB, uuidFromSuffix("0a", "g6b"))
	seedObligation(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, goatB,
		uuidFromSuffix("08", "g6b"), "missed", due, "verify-b")

	cells := shedVaccineCellsByCode(t, ctx, pool, tenantID, asOf)
	et := cells["ET_TT"]
	if et.BehindAnimals != 1 {
		t.Fatalf("behindAnimals = %d, want 1; only the animal with NO proof is behind", et.BehindAnimals)
	}
	if et.VerifyingAnimals != 1 {
		t.Fatalf("verifyingAnimals = %d, want 1; the dosed animal awaiting a verifier must be counted, and counted separately", et.VerifyingAnimals)
	}
	// behind outranks verifying for the cell colour: an undosed animal is the bigger problem.
	if et.State != "behind" {
		t.Fatalf("state = %q, want \"behind\"; a shed holding BOTH must show the more serious one", et.State)
	}

	// And with the undosed animal removed, the shed must go amber, never red.
	execProjectionSQL(t, ctx, pool, "drop the undosed obligation",
		`DELETE FROM obligation_instances WHERE obligation_id = $1`, uuidFromSuffix("08", "g6b"))
	cells = shedVaccineCellsByCode(t, ctx, pool, tenantID, asOf)
	et = cells["ET_TT"]
	if et.State != "verifying" {
		t.Fatalf("state = %q, want \"verifying\"; every dose was given and the wait is on a verifier, not on the herd", et.State)
	}
	if et.BehindAnimals != 0 {
		t.Fatalf("behindAnimals = %d, want 0; no animal here is unvaccinated", et.BehindAnimals)
	}
	// The drill-down must say WHY, and must carry the ground location including partition.
	if len(et.FlaggedAnimals) != 1 {
		t.Fatalf("flaggedAnimals has %d rows, want 1", len(et.FlaggedAnimals))
	}
	row := et.FlaggedAnimals[0]
	if !row.AwaitingVerification {
		t.Fatalf("awaitingVerification = false; the drawer would print the raw 'missed' status and accuse an operator who dosed on time")
	}
	if row.LocationDisplay == "" {
		t.Fatalf("locationDisplay is empty; a shed name alone sends a person to the wrong pen on a partitioned shed")
	}
	// Proof is filmed per SHED per day, so it hangs off the CELL and not off each animal. Nothing was
	// filmed in this fixture, so there is nothing to show -- and an empty list is a finding, since a
	// verification queue with no footage cannot be drained by anyone.
	if len(et.ProofVideos) != 0 {
		t.Fatalf("proofVideos = %d, want 0; nothing was filmed in this fixture", len(et.ProofVideos))
	}
}

// TestVaccinationCommandBoardShedVaccineLinksShedVideoAndNeverWeighingCapture pins WHICH footage a
// verifier is shown.
//
// Vaccination proof is filmed per shed for the operator day and stored under the generic
// `shed_video` field key; the weighing flow writes explicitly-named keys
// (weighing_individual_video, weighing_shed_partition_video) into the SAME shed-scoped table on the
// SAME days. Two ways to get this wrong, and this branch shipped both before it was caught:
//
//	an include-list keyed on "vaccination" matched NOTHING, so 137 doses whose footage was sitting
//	in GCS reported "no video uploaded";
//	a shed+date match with no key filter at all linked the WEIGHING clip, which would have put a
//	verifier in front of footage of a goat on a scale and asked them to accept a vaccination from
//	it -- worse than leaving the dose unverified.
//
// The fixture seeds one of each in the same shed on the same day and asserts exactly one is linked.
func TestVaccinationCommandBoardShedVaccineLinksShedVideoAndNeverWeighingCapture(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000a7"
	parkID := uuidFromSuffix("01", "g7")
	shedID := uuidFromSuffix("02", "g7")
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, "g7")
	seedCommandBoardPark(t, ctx, pool, tenantID, parkID, shedID, "Video")
	seedVaccineDimension(t, ctx, pool, tenantID, protocolVersionID, ruleID,
		uuidFromSuffix("0b", "g7d"), "sel-a", "ET_TT")

	asOf := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	due := asOf.Add(-2 * 24 * time.Hour)

	goatID := uuidFromSuffix("03", "g7a")
	oblID := uuidFromSuffix("08", "g7a")
	seedBareGoat(t, ctx, pool, tenantID, shedID, goatID, uuidFromSuffix("0a", "g7a"))
	seedObligation(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, goatID, oblID, "missed", due, "video-a")
	execProjectionSQL(t, ctx, pool, "recorded completion",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, status, administered_at, verified_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'recorded', $5::timestamptz, NULL, 'video-completion-a')`,
		uuidFromSuffix("09", "g7a"), tenantID, oblID, goatID, due)

	seedShedVideo(t, ctx, pool, tenantID, shedID, uuidFromSuffix("0c", "g7v"), "shed_video", due)
	seedShedVideo(t, ctx, pool, tenantID, shedID, uuidFromSuffix("0c", "g7w"), "weighing_individual_video", due)

	cells := shedVaccineCellsByCode(t, ctx, pool, tenantID, asOf)
	et := cells["ET_TT"]
	if len(et.ProofVideos) != 1 {
		t.Fatalf("proofVideos = %d, want exactly 1; the shed_video must be linked and the weighing capture must not", len(et.ProofVideos))
	}
	want := "/app/proofs/" + uuidFromSuffix("0c", "g7v") + "/download"
	if et.ProofVideos[0].Path != want {
		t.Fatalf("proofVideos[0].Path = %q, want %q; the WEIGHING clip was linked instead of the vaccination shed video", et.ProofVideos[0].Path, want)
	}
}

func seedShedVideo(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, shedID, proofID, fieldKey string, uploadedAt time.Time) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "proof "+fieldKey,
		`INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, upload_state,
		                              scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_at, metadata)
		 VALUES ($1::uuid, $2::uuid, 'gcs', $3::text, 'video/mp4', 'completed', 'shed', $4::uuid, 'shed', $4::uuid,
		         'video', $5::timestamptz, jsonb_build_object('field_key', $6::text))`,
		proofID, tenantID, proofID, shedID, uploadedAt, fieldKey)
}

// TestVaccinationCommandBoardShedVaccineColumnsExcludeRetiredProtocolVaccines pins the SOURCE OF
// TRUTH for the matrix's columns.
//
// protocol_rule_dimensions accumulates per protocol VERSION and never forgets: a retired version
// keeps every dimension row it ever had. Listing the whole table therefore resurrects withdrawn
// vaccines as all-grey columns that read as protocol gaps. On the live tenant BLUE_TONGUE exists
// ONLY on a retired version, and the board was showing it against all 18 sheds as though the herd
// were owed a vaccine nobody had planned -- an invented finding on every row.
//
// The distinction that matters is "what is the herd currently required to receive", which is the
// PUBLISHED vaccination protocol. A published vaccine with no obligations must still get its
// column, because that silence is a real finding; a retired one must disappear.
func TestVaccinationCommandBoardShedVaccineColumnsExcludeRetiredProtocolVaccines(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000a8"
	parkID := uuidFromSuffix("01", "g8")
	shedID := uuidFromSuffix("02", "g8")
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, "g8")
	seedCommandBoardPark(t, ctx, pool, tenantID, parkID, shedID, "Retired")
	// ET_TT on the PUBLISHED version.
	seedVaccineDimension(t, ctx, pool, tenantID, protocolVersionID, ruleID,
		uuidFromSuffix("0b", "g8a"), "sel-a", "ET_TT")

	// BLUE_TONGUE on a RETIRED version — the live shape.
	retiredProtocolID := uuidFromSuffix("06", "g8rp")
	retiredVersionID := uuidFromSuffix("06", "g8rv")
	retiredRuleID := uuidFromSuffix("07", "g8ret")
	execProjectionSQL(t, ctx, pool, "retired protocol definition",
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		 VALUES ($1, $2, 'vaccination_g8_retired', 'Vaccination G8 Retired', 'vaccination', 'active')`,
		retiredProtocolID, tenantID)
	execProjectionSQL(t, ctx, pool, "retired protocol version",
		`INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl)
		 VALUES ($1, $2, $3, 'tenant', 1, 'draft', '2026-01-01', '{}')`,
		retiredVersionID, tenantID, retiredProtocolID)
	execProjectionSQL(t, ctx, pool, "retired rule",
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, trigger_type)
		 VALUES ($1, $2, $3, 'blue_tongue_adult', 'birth_age')`, retiredRuleID, tenantID, retiredVersionID)
	seedVaccineDimension(t, ctx, pool, tenantID, retiredVersionID, retiredRuleID,
		uuidFromSuffix("0b", "g8ret"), "sel-bt", "BLUE_TONGUE")
	execProjectionSQL(t, ctx, pool, "retire the version",
		`UPDATE protocol_versions SET status = 'retired' WHERE protocol_version_id = $1`, retiredVersionID)

	asOf := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	goatID := uuidFromSuffix("03", "g8a")
	seedBareGoat(t, ctx, pool, tenantID, shedID, goatID, uuidFromSuffix("0a", "g8a"))
	seedObligation(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, goatID,
		uuidFromSuffix("08", "g8a"), "scheduled", asOf.Add(5*24*time.Hour), "retired-a")

	repo := NewRepository(pool, 5*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}
	codes := map[string]bool{}
	for _, column := range resp.ShedVaccineColumns {
		codes[column.Code] = true
	}
	if !codes["ET_TT"] {
		t.Fatalf("columns = %v, want ET_TT; the published protocol's vaccine must be a column", resp.ShedVaccineColumns)
	}
	if codes["BLUE_TONGUE"] {
		t.Fatalf("columns = %v; a RETIRED vaccine must not appear -- its all-grey column reads as a protocol gap for a vaccine the herd is no longer owed", resp.ShedVaccineColumns)
	}
	for _, cell := range resp.ShedVaccineMatrix {
		if cell.VaccineCode == "BLUE_TONGUE" {
			t.Fatalf("a retired vaccine reached the matrix cells as well as the columns")
		}
	}
}

// TestVaccinationCommandBoardExcludesExitedAndMergedGoatsFromEverySurface pins HERD MEMBERSHIP
// across the board's halves.
//
// The KPI row, the shed x vaccine cells and the drill-down read obligation_instances directly, while
// the cohort matrix reads the live herd through goats. Without the same membership rule on both
// sides a sold or merged animal that still holds an open obligation inflates targets and the shed
// cells while the cohort head count drops it -- two numbers on one screen describing different
// herds, and nothing on the screen hints that they disagree.
//
// The filter these queries carried was `lifecycle_status != 'terminated'`, which excluded NOTHING --
// 'terminated' is not a value the goats check constraint permits, so a dead, sold, culled,
// transferred or lost animal passed straight through a filter that looked correct. The predicate is
// now the positive IN-list the rest of the backend uses.
//
// MERGED matters as much and is the easier one to forget: a merge RETIRES the source
// goat into another record, so counting it is counting the same animal twice. This tenant currently
// has zero of either, which is exactly why the gap was invisible and why it is pinned here rather
// than left to be discovered the first time an animal is sold mid-drive.
func TestVaccinationCommandBoardExcludesExitedAndMergedGoatsFromEverySurface(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000a9"
	parkID := uuidFromSuffix("01", "g9")
	shedID := uuidFromSuffix("02", "g9")
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, "g9")
	seedCommandBoardPark(t, ctx, pool, tenantID, parkID, shedID, "Herd")
	seedVaccineDimension(t, ctx, pool, tenantID, protocolVersionID, ruleID,
		uuidFromSuffix("0b", "g9d"), "sel-a", "ET_TT")

	asOf := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	past := asOf.Add(-4 * 24 * time.Hour)

	// One LIVE animal, behind. This is the only one that may ever be counted.
	live := uuidFromSuffix("03", "g9live")
	seedBareGoat(t, ctx, pool, tenantID, shedID, live, uuidFromSuffix("0a", "g9live"))
	seedObligation(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, live,
		uuidFromSuffix("08", "g9live"), "missed", past, "herd-live")

	// An animal that has LEFT the herd, holding the same open obligation. 'sold' is used rather than
	// the word "terminated" because 'terminated' is not a value goats_lifecycle_status_check permits
	// -- the filter this test guards used to compare against it and therefore excluded nothing.
	gone := uuidFromSuffix("03", "g9gone")
	seedBareGoat(t, ctx, pool, tenantID, shedID, gone, uuidFromSuffix("0a", "g9gone"))
	seedObligation(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, gone,
		uuidFromSuffix("08", "g9gone"), "missed", past, "herd-gone")
	execProjectionSQL(t, ctx, pool, "sell the goat out of the herd",
		`UPDATE goats SET lifecycle_status = 'sold' WHERE goat_id = $1`, gone)

	// A MERGED animal holding the same open obligation, retired into the live one.
	merged := uuidFromSuffix("03", "g9merg")
	seedBareGoat(t, ctx, pool, tenantID, shedID, merged, uuidFromSuffix("0a", "g9merg"))
	seedObligation(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, merged,
		uuidFromSuffix("08", "g9merg"), "missed", past, "herd-merged")
	execProjectionSQL(t, ctx, pool, "merge the goat",
		`UPDATE goats SET merged_into_goat_id = $2 WHERE goat_id = $1`, merged, live)

	// Every animal also gets a SECOND obligation carrying a recorded-unverified completion. Without
	// it the shed-dose matrix, the weekly-given chart and the verification queue have nothing to
	// leak, and the test would pass on those surfaces for the wrong reason -- which is exactly how
	// the first version of this test earned a name it had not tested.
	for i, id := range []string{live, gone, merged} {
		suffix := fmt.Sprintf("g9c%d", i)
		obl := uuidFromSuffix("08", suffix)
		// A DIFFERENT due date from the behind obligation above: obligation_instances_dup_guard is
		// UNIQUE on (tenant, protocol_version, rule, target_type, target_id, due_at), so reusing the
		// date collides rather than creating the second dose this test needs.
		given := past.Add(-24 * time.Hour)
		seedObligation(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, id, obl, "completed", given, "herd-comp-"+suffix)
		execProjectionSQL(t, ctx, pool, "recorded completion "+suffix,
			`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, status, administered_at, verified_at, idempotency_key)
			 VALUES ($1, $2, $3, $4, 'recorded', $5::timestamptz, NULL, $6)`,
			uuidFromSuffix("09", suffix), tenantID, obl, id, given, "herd-completion-"+suffix)
	}

	repo := NewRepository(pool, 5*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}

	if resp.KPIs.Targets != 1 {
		t.Fatalf("targets = %d, want 1; a sold goat and a merged goat both still held an open obligation and must not be counted as animals in the programme", resp.KPIs.Targets)
	}
	if resp.KPIs.MissedNotGiven != 1 {
		t.Fatalf("missed_not_given = %d, want 1; only the live animal is genuinely un-dosed", resp.KPIs.MissedNotGiven)
	}
	assertKPIPartitionExhaustive(t, resp.KPIs)

	var et domain.CommandBoardShedVaccineCell
	for _, cell := range resp.ShedVaccineMatrix {
		if cell.VaccineCode == "ET_TT" {
			et = cell
		}
	}
	if et.BehindAnimals != 1 {
		t.Fatalf("behindAnimals = %d, want 1; the shed cell must count the same herd the KPI row does", et.BehindAnimals)
	}
	if et.TotalAnimals != 1 {
		t.Fatalf("totalAnimals = %d, want 1", et.TotalAnimals)
	}
	// The drill-down is what a person acts on, so a retired animal appearing there sends someone to
	// look for a goat that is not in the shed.
	if len(et.FlaggedAnimals) != 1 {
		t.Fatalf("flaggedAnimals = %d, want 1; the list must not name animals that have left the herd or been merged away", len(et.FlaggedAnimals))
	}
	if got := et.FlaggedAnimals[0].GoatID; got != live {
		t.Fatalf("flaggedAnimals[0] = %s, want the live goat %s", got, live)
	}

	// The three surfaces the first version of this test did not touch. They read
	// obligation_instances and vaccination_completions directly, so each needed its own goats join;
	// a name promising EVERY surface has to be paid for on every surface.
	//
	// Counts are per OBLIGATION here, not per animal: the shed-dose matrix and the verification
	// queue are dose-grain by design. One live animal holds two obligations (one behind, one with
	// recorded proof), so the honest expectation is "every row belongs to the live animal", checked
	// by summing and comparing against the live animal's own obligations rather than by asserting a
	// single hard-coded total that would silently absorb a leak of the same size.
	var shedDoseAnimals int
	for _, cell := range resp.ShedDoseMatrix {
		shedDoseAnimals += cell.AnimalCount
	}
	if shedDoseAnimals != 2 {
		t.Errorf("shed dose matrix counts %d, want 2 (the live animal's two obligations); a sold or merged animal is leaking into the dose matrix", shedDoseAnimals)
	}

	var weeklyGiven int
	for _, row := range resp.WeeklyGiven {
		weeklyGiven += row.Count
	}
	if weeklyGiven != 1 {
		t.Errorf("weekly given totals %d, want 1 (only the live animal's recorded dose); the chart is counting doses given to animals that have left the herd", weeklyGiven)
	}

	var queued int
	for _, row := range resp.VerificationQueue {
		queued += row.AwaitingCount
	}
	if queued != 1 {
		t.Errorf("verification queue holds %d, want 1; a verifier must never be asked to review proof for an animal that was sold or merged away", queued)
	}
}
