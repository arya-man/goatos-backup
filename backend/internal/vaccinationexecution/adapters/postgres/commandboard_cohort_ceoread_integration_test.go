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

// TestVaccinationCommandBoardCohortCEOReadHeadCountDaySplitAndDoseSequenceException pins the three
// facts the CEO cohort matrix answers with, each of which was wrong or absent before:
//
//	(1) HEAD COUNT. The cohort's Animals column counted obligation TARGETS, so a cohort of 324 live
//	    adults whose Dose 1 obligations had closed out of the window reported 229 -- every animal
//	    without an obligation for that dose vanished from its own head count. Head count is a herd
//	    fact and is now read from goats.
//	(2) DAY SPLIT. min..max administered collapses "84 animals on 30 Jun, 237 on 1 Jul" into
//	    "30 Jun-1 Jul", which is the one thing leadership asks about a two-day drive.
//	(3) DOSE-SEQUENCE EXCEPTION. An animal holding an accepted LATER dose of the same course while
//	    THIS dose has no accepted completion is a medical exception. "324 verified" and "321
//	    verified with 3 animals whose Dose 1 was never accepted" must not render identically.
//
// Fixture shape (one park, one shed, adult cohort, ET+TT two-dose course):
//
//	18 live adult animals in the cohort
//	15 of them: accepted Dose 1 -- 5 administered on day one, 10 on day two
//	 3 of them: NO accepted Dose 1, but an accepted Dose 2      <- the exception
//	all 18:     accepted Dose 2
//	 2 extra live animals in the same cohort carry NO obligation at all  <- head-count case (1)
//
// Expected: Dose 1 cell -> 15 verified, days {day1:5, day2:10}, missingPriorDose 3 (named);
// head count 20 (18 + the 2 obligation-less animals), never 15 or 18.
//
// StatusBuckets / OneToMany / ParkScope. Business dates are fixed IST days; no now()±N.
func TestVaccinationCommandBoardCohortCEOReadHeadCountDaySplitAndDoseSequenceException(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("LoadLocation(Asia/Kolkata) error = %v", err)
	}

	const (
		tenantID          = "00000000-0000-4000-8000-0000000000f1"
		parkID            = "70000000-0000-4000-8000-0000010000f1"
		shedID            = "70000000-0000-4000-8000-0000020000f1"
		protocolID        = "70000000-0000-4000-8000-0000060000f0"
		protocolVersionID = "70000000-0000-4000-8000-0000060000f1"
		ruleDose1ID       = "70000000-0000-4000-8000-0000070000f1"
		ruleDose2ID       = "70000000-0000-4000-8000-0000070000f2"
		batchID           = "70000000-0000-4000-8000-0000040000f1"
		custodianPartyID  = "70000000-0000-4000-8000-0000090000f1"

		dosedDayOne = 5  // accepted Dose 1 on the first business day
		dosedDayTwo = 10 // accepted Dose 1 on the second business day
		exceptions  = 3  // accepted Dose 2, NO accepted Dose 1
		unobligated = 2  // live cohort animals with no obligation at all
	)
	withDose1 := dosedDayOne + dosedDayTwo
	obligated := withDose1 + exceptions
	headCount := obligated + unobligated

	dayOne := time.Date(2026, 6, 30, 0, 0, 0, 0, ist)
	dayTwo := time.Date(2026, 7, 1, 0, 0, 0, 0, ist)
	dose2Day := time.Date(2026, 7, 24, 0, 0, 0, 0, ist)
	asOf := time.Date(2026, 7, 25, 11, 0, 0, 0, ist)
	windowEnd := time.Date(2026, 7, 31, 23, 59, 59, 0, ist)

	execProjectionSQL(t, ctx, pool, "tenant",
		`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Test Org F', 'active')`, tenantID)
	execProjectionSQL(t, ctx, pool, "custodian party",
		`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1, 'org', 'Custodian F', 'active')`,
		custodianPartyID)
	execProjectionSQL(t, ctx, pool, "park",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, 'CPT-QA-F', 'park', NULL, 'active')`, parkID, tenantID)
	execProjectionSQL(t, ctx, pool, "shed",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, 'Shed F', 'shed', $3, 'active')`, shedID, tenantID, parkID)

	execProjectionSQL(t, ctx, pool, "protocol definition",
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		 VALUES ($1, $2, 'vaccination_f', 'Vaccination F', 'vaccination', 'active')`, protocolID, tenantID)
	execProjectionSQL(t, ctx, pool, "protocol version",
		`INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl)
		 VALUES ($1, $2, $3, 'tenant', 1, 'draft', '2026-01-01', '{}')`, protocolVersionID, tenantID, protocolID)
	// Same course family (et_tt_adult), sequence 1 then 2 -- this is what makes Dose 2 count as a
	// LATER dose of Dose 1's course, and what stops a kid-course dose from claiming it.
	execProjectionSQL(t, ctx, pool, "rule dose 1",
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type)
		 VALUES ($1, $2, $3, 'et_tt_adult_w1', 1, 'birth_age')`, ruleDose1ID, tenantID, protocolVersionID)
	execProjectionSQL(t, ctx, pool, "rule dose 2",
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type)
		 VALUES ($1, $2, $3, 'et_tt_adult_w2', 2, 'birth_age')`, ruleDose2ID, tenantID, protocolVersionID)
	execProjectionSQL(t, ctx, pool, "publish protocol version",
		`UPDATE protocol_versions SET status = 'published', published_at = now() WHERE protocol_version_id = $1`,
		protocolVersionID)
	execProjectionSQL(t, ctx, pool, "drive batch",
		`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id,
		   planned_date, window_start, window_end, status)
		 VALUES ($1, $2, $3, 'park', $4, $5::date, $5::timestamptz, $6::timestamptz, 'in_progress')`,
		batchID, tenantID, protocolVersionID, parkID, dayOne, windowEnd)

	goatIDOf := func(i int) string { return fmt.Sprintf("70000000-0000-4000-8000-0000310%05d", 400+i) }

	seedGoat := func(i int) {
		execProjectionSQL(t, ctx, pool, "goat",
			`INSERT INTO goats (goat_id, tenant_id, sex, lifecycle_status, management_stage, shed_id, custodian_party_id, dob)
			 VALUES ($1, $2, 'female', 'alive', 'Non-Pregnant', $3, $4, '2023-01-01')`,
			goatIDOf(i), tenantID, shedID, custodianPartyID)
	}
	// accepted=false records the obligation with NO completion at all, which is how the three
	// exception animals end up with a Dose 2 they hold and a Dose 1 they never had accepted.
	seedDose := func(i, slot int, ruleID string, accepted bool, administeredAt time.Time) {
		oblID := fmt.Sprintf("70000000-0000-4000-8000-0000810%01d%04d", slot, 400+i)
		execProjectionSQL(t, ctx, pool, "obligation",
			`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type,
			   scope_type, scope_id, rule_id, status, due_at, batch_id, idempotency_key)
			 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, $7, $8::timestamptz, $9, $10)`,
			oblID, tenantID, protocolVersionID, goatIDOf(i), shedID, ruleID,
			map[bool]string{true: "completed", false: "due"}[accepted],
			administeredAt, batchID, fmt.Sprintf("obl-f-%d-%d", slot, i))
		if !accepted {
			return
		}
		execProjectionSQL(t, ctx, pool, "completion",
			`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, status, administered_at, verified_at, idempotency_key)
			 VALUES ($1, $2, $3, $4, 'accepted', $5::timestamptz, $5::timestamptz, $6)`,
			fmt.Sprintf("70000000-0000-4000-8000-0000910%01d%04d", slot, 400+i), tenantID, oblID, goatIDOf(i),
			administeredAt, fmt.Sprintf("comp-f-%d-%d", slot, i))
	}

	for i := 0; i < obligated; i++ {
		seedGoat(i)
		switch {
		case i < dosedDayOne:
			seedDose(i, 1, ruleDose1ID, true, dayOne)
		case i < withDose1:
			seedDose(i, 1, ruleDose1ID, true, dayTwo)
		default:
			// The exception animals: a Dose 1 obligation exists and is NOT accepted.
			seedDose(i, 1, ruleDose1ID, false, dayTwo)
		}
		seedDose(i, 2, ruleDose2ID, true, dose2Day)
	}
	// Live cohort animals with no obligation of any dose. They belong to the herd and therefore to
	// the head count; the old obligation-derived count could not see them.
	for i := obligated; i < headCount; i++ {
		seedGoat(i)
	}

	// The board is still read so this test keeps exercising the endpoint end to end, but the cohort
	// cells now come from their own section.
	repo := NewRepository(pool, 5*time.Second)
	if _, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf}); err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}

	var dose1, dose2 *domain.CommandBoardCohortCell
	cohortMatrix := cohortMatrixCells(t, ctx, pool, tenantID, asOf)
	for i := range cohortMatrix {
		cell := &cohortMatrix[i]
		t.Logf("cohort cell vaccine=%q animals=%d pending=%d submitted=%d verified=%d exceptions=%d days=%v",
			cell.VaccineLabel, cell.Cohort.AnimalCount, cell.PendingCount, cell.SubmittedCount,
			cell.VerifiedCount, cell.MissingPriorDoseCount, cell.DoseCodes)
		switch cell.VaccineLabel {
		case "ET+TT · Dose 1":
			dose1 = cell
		case "ET+TT · Dose 2":
			dose2 = cell
		}
	}
	if dose1 == nil || dose2 == nil {
		t.Fatalf("cohort matrix missing a dose-qualified ET+TT cell: %+v", cohortMatrix)
	}

	// (1) HEAD COUNT is the live herd, not the obligation targets. This is the 229-vs-324 defect.
	if dose1.Cohort.AnimalCount != headCount {
		t.Errorf("Dose 1 cohort head count = %d, want %d -- the Animals column must report every live "+
			"animal in the cohort, including the %d that carry no obligation for this dose. Counting "+
			"obligation targets is what reported 229 for a 324-animal adult cohort",
			dose1.Cohort.AnimalCount, headCount, unobligated)
	}
	if dose2.Cohort.AnimalCount != headCount {
		t.Errorf("Dose 2 cohort head count = %d, want %d -- head count is a cohort fact and cannot differ "+
			"between two doses of the same cohort", dose2.Cohort.AnimalCount, headCount)
	}

	// (2) DAY SPLIT: the two administration days, each with its own animal count.
	if dose1.VerifiedCount != withDose1 {
		t.Fatalf("Dose 1 verified = %d, want %d (fixture did not seed the accepted doses it intended)",
			dose1.VerifiedCount, withDose1)
	}
	dose1Days := cohortAdministeredDays(t, ctx, pool, tenantID, dose1)
	if len(dose1Days) != 2 {
		t.Fatalf("Dose 1 administeredDays = %v, want 2 days -- a two-day drive rendered as the range "+
			"'30 Jun-1 Jul' hides that most of the work happened on one of them", dose1Days)
	}
	wantDays := []domain.CommandBoardCohortDay{
		{Date: dayOne.Format("2006-01-02"), AnimalCount: dosedDayOne},
		{Date: dayTwo.Format("2006-01-02"), AnimalCount: dosedDayTwo},
	}
	for i, want := range wantDays {
		got := dose1Days[i]
		if got.Date != want.Date || got.AnimalCount != want.AnimalCount {
			t.Errorf("Dose 1 administeredDays[%d] = %+v, want %+v -- days must be ascending IST business "+
				"dates carrying DISTINCT animals dosed on that date", i, got, want)
		}
	}
	total := 0
	for _, day := range dose1Days {
		total += day.AnimalCount
	}
	if total != dose1.VerifiedCount {
		t.Errorf("Dose 1 day counts sum to %d but verifiedCount = %d -- the split must partition the "+
			"cell's verified bucket, not a different set of animals", total, dose1.VerifiedCount)
	}

	// (3) The CEO board no longer presents accepted Dose 2 history as a Dose 1 "exception".
	if dose1.MissingPriorDoseCount != 0 {
		t.Errorf("Dose 1 missingPriorDoseCount = %d, want 0 -- accepted Dose 2 history must render as "+
			"accepted anchor history, not as a CEO dashboard exception", dose1.MissingPriorDoseCount)
	}
	dose1Exceptions := cohortExceptionAnimals(t, ctx, pool, tenantID, dose1)
	if got := len(dose1Exceptions); got != 0 {
		t.Errorf("Dose 1 compatibility exception list length = %d, want 0", got)
	}
	for _, animal := range dose1Exceptions {
		if animal.GoatID == "" || animal.DisplayID == "" {
			t.Errorf("exception animal %+v is missing identity -- an unnamed exception is not actionable", animal)
		}
	}
	if dose2.MissingPriorDoseCount != 0 {
		t.Errorf("Dose 2 missingPriorDoseCount = %d, want 0 -- Dose 2 is the LATER dose here and is "+
			"accepted for every animal; the exception belongs to the dose that is missing, not to the one "+
			"that proves it is missing", dose2.MissingPriorDoseCount)
	}

	// A clean cohort must report zero rather than omit the field, so a renderer can tell
	// "no exceptions" from "the server did not answer".
	if dose2.VerifiedCount != obligated {
		t.Errorf("Dose 2 verified = %d, want %d", dose2.VerifiedCount, obligated)
	}
}

// TestVaccinationCommandBoardCohortExceptionCompatibilityEndpointStaysEmpty pins the current CEO
// contract: accepted later-dose history is not exposed as a command-board "exceptions" lane.
func TestVaccinationCommandBoardCohortExceptionCompatibilityEndpointStaysEmpty(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("LoadLocation(Asia/Kolkata) error = %v", err)
	}

	const (
		tenantID          = "00000000-0000-4000-8000-0000000000f2"
		parkID            = "70000000-0000-4000-8000-0000010000f2"
		shedID            = "70000000-0000-4000-8000-0000020000f2"
		protocolID        = "70000000-0000-4000-8000-000006000ac0"
		protocolVersionID = "70000000-0000-4000-8000-000006000ac1"
		ruleDose1ID       = "70000000-0000-4000-8000-000007000ac1"
		ruleDose2ID       = "70000000-0000-4000-8000-000007000ac2"
		batchID           = "70000000-0000-4000-8000-000004000ac1"
		custodianPartyID  = "70000000-0000-4000-8000-000009000ac1"
	)
	overCap := domain.CommandBoardCohortExceptionListCap + 5

	dose1Day := time.Date(2026, 6, 30, 0, 0, 0, 0, ist)
	dose2Day := time.Date(2026, 7, 24, 0, 0, 0, 0, ist)
	asOf := time.Date(2026, 7, 25, 11, 0, 0, 0, ist)
	windowEnd := time.Date(2026, 7, 31, 23, 59, 59, 0, ist)

	seedCohortFixtureShell(t, ctx, pool, cohortFixtureIDs{
		tenantID: tenantID, parkID: parkID, parkName: "CPT-QA-G", shedID: shedID, shedName: "Shed G",
		protocolID: protocolID, protocolCode: "vaccination_g", protocolVersionID: protocolVersionID,
		ruleDose1ID: ruleDose1ID, ruleDose2ID: ruleDose2ID, batchID: batchID,
		custodianPartyID: custodianPartyID, plannedDate: dose1Day, windowEnd: windowEnd,
	})

	for i := 0; i < overCap; i++ {
		goatID := fmt.Sprintf("70000000-0000-4000-8000-0000320%05d", 500+i)
		execProjectionSQL(t, ctx, pool, "goat",
			`INSERT INTO goats (goat_id, tenant_id, sex, lifecycle_status, management_stage, shed_id, custodian_party_id, dob)
			 VALUES ($1, $2, 'female', 'alive', 'Non-Pregnant', $3, $4, '2023-01-01')`,
			goatID, tenantID, shedID, custodianPartyID)
		// Dose 1 obligation with NO accepted completion.
		oblOne := fmt.Sprintf("70000000-0000-4000-8000-0000821%05d", 500+i)
		execProjectionSQL(t, ctx, pool, "dose 1 obligation",
			`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type,
			   scope_type, scope_id, rule_id, status, due_at, batch_id, idempotency_key)
			 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, 'due', $7::timestamptz, $8, $9)`,
			oblOne, tenantID, protocolVersionID, goatID, shedID, ruleDose1ID, dose1Day, batchID,
			fmt.Sprintf("obl-g1-%d", i))
		// Dose 2 accepted -- the later dose that makes the missing Dose 1 an exception.
		oblTwo := fmt.Sprintf("70000000-0000-4000-8000-0000822%05d", 500+i)
		execProjectionSQL(t, ctx, pool, "dose 2 obligation",
			`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type,
			   scope_type, scope_id, rule_id, status, due_at, batch_id, idempotency_key)
			 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, 'completed', $7::timestamptz, $8, $9)`,
			oblTwo, tenantID, protocolVersionID, goatID, shedID, ruleDose2ID, dose2Day, batchID,
			fmt.Sprintf("obl-g2-%d", i))
		execProjectionSQL(t, ctx, pool, "dose 2 completion",
			`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, status, administered_at, verified_at, idempotency_key)
			 VALUES ($1, $2, $3, $4, 'accepted', $5::timestamptz, $5::timestamptz, $6)`,
			fmt.Sprintf("70000000-0000-4000-8000-0000922%05d", 500+i), tenantID, oblTwo, goatID, dose2Day,
			fmt.Sprintf("comp-g2-%d", i))
	}

	repo := NewRepository(pool, 5*time.Second)
	if _, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf}); err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}

	var dose1 *domain.CommandBoardCohortCell
	cohortMatrix := cohortMatrixCells(t, ctx, pool, tenantID, asOf)
	for i := range cohortMatrix {
		if cohortMatrix[i].VaccineLabel == "ET+TT · Dose 1" {
			dose1 = &cohortMatrix[i]
		}
	}
	if dose1 == nil {
		t.Fatalf("cohort matrix missing the ET+TT Dose 1 cell: %+v", cohortMatrix)
	}

	if dose1.MissingPriorDoseCount != 0 {
		t.Errorf("missingPriorDoseCount = %d, want 0 -- the CEO board no longer renders this as an exception",
			dose1.MissingPriorDoseCount)
	}
	dose1Exceptions := cohortExceptionAnimals(t, ctx, pool, tenantID, dose1)
	if got := len(dose1Exceptions); got != 0 {
		t.Errorf("compatibility exception list length = %d, want 0", got)
	}
}

// TestVaccinationCommandBoardCohortAdministeredDaySplitDateShiftUsesISTBusinessDate pins the day
// split to the IST business DATE, never the UTC instant.
//
// Two accepted doses 60 minutes apart across the IST midnight boundary (23:30 IST on 30 Jun and
// 00:30 IST on 1 Jul) are TWO farm days. Read in UTC they are 18:00 and 19:00 on the SAME day, so a
// UTC-grouped split would report one day with both animals and lose the day the farm actually
// worked. Vaccination time grain is the Asia/Kolkata business day.
//
// DateShift / ExecutionDate / OneToMany.
func TestVaccinationCommandBoardCohortAdministeredDaySplitDateShiftUsesISTBusinessDate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("LoadLocation(Asia/Kolkata) error = %v", err)
	}

	const (
		tenantID          = "00000000-0000-4000-8000-0000000000f3"
		parkID            = "70000000-0000-4000-8000-0000010000f3"
		shedID            = "70000000-0000-4000-8000-0000020000f3"
		protocolID        = "70000000-0000-4000-8000-000006000bd0"
		protocolVersionID = "70000000-0000-4000-8000-000006000bd1"
		ruleDose1ID       = "70000000-0000-4000-8000-000007000bd1"
		ruleDose2ID       = "70000000-0000-4000-8000-000007000bd2"
		batchID           = "70000000-0000-4000-8000-000004000bd1"
		custodianPartyID  = "70000000-0000-4000-8000-000009000bd1"
	)

	// 60 minutes apart, two IST business days, ONE UTC day (18:00Z and 19:00Z on 30 Jun).
	lateOnDayOne := time.Date(2026, 6, 30, 23, 30, 0, 0, ist)
	earlyOnDayTwo := time.Date(2026, 7, 1, 0, 30, 0, 0, ist)
	// Both instants are 2026-06-30 in UTC (18:00Z and 19:00Z) and two different days in IST. That
	// collision is what gives this fixture its teeth, and it is a property of the two fixed dates
	// above — asserting it in code would mean deriving a day boundary in UTC, which is the exact
	// mistake this test exists to catch.
	//
	// The IST business dates the split must report, derived in Asia/Kolkata, never from UTC.
	wantDayOne := lateOnDayOne.In(ist).Format("2006-01-02")
	wantDayTwo := earlyOnDayTwo.In(ist).Format("2006-01-02")
	asOf := time.Date(2026, 7, 2, 11, 0, 0, 0, ist)
	windowEnd := time.Date(2026, 7, 31, 23, 59, 59, 0, ist)

	seedCohortFixtureShell(t, ctx, pool, cohortFixtureIDs{
		tenantID: tenantID, parkID: parkID, parkName: "CPT-QA-H", shedID: shedID, shedName: "Shed H",
		protocolID: protocolID, protocolCode: "vaccination_h", protocolVersionID: protocolVersionID,
		ruleDose1ID: ruleDose1ID, ruleDose2ID: ruleDose2ID, batchID: batchID,
		custodianPartyID: custodianPartyID, plannedDate: lateOnDayOne, windowEnd: windowEnd,
	})

	for i, administeredAt := range []time.Time{lateOnDayOne, earlyOnDayTwo} {
		goatID := fmt.Sprintf("70000000-0000-4000-8000-0000330%05d", 600+i)
		oblID := fmt.Sprintf("70000000-0000-4000-8000-0000830%05d", 600+i)
		execProjectionSQL(t, ctx, pool, "goat",
			`INSERT INTO goats (goat_id, tenant_id, sex, lifecycle_status, management_stage, shed_id, custodian_party_id, dob)
			 VALUES ($1, $2, 'female', 'alive', 'Non-Pregnant', $3, $4, '2023-01-01')`,
			goatID, tenantID, shedID, custodianPartyID)
		execProjectionSQL(t, ctx, pool, "obligation",
			`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type,
			   scope_type, scope_id, rule_id, status, due_at, batch_id, idempotency_key)
			 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, 'completed', $7::timestamptz, $8, $9)`,
			oblID, tenantID, protocolVersionID, goatID, shedID, ruleDose1ID, administeredAt, batchID,
			fmt.Sprintf("obl-h-%d", i))
		execProjectionSQL(t, ctx, pool, "completion",
			`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, status, administered_at, verified_at, idempotency_key)
			 VALUES ($1, $2, $3, $4, 'accepted', $5::timestamptz, $5::timestamptz, $6)`,
			fmt.Sprintf("70000000-0000-4000-8000-0000930%05d", 600+i), tenantID, oblID, goatID, administeredAt,
			fmt.Sprintf("comp-h-%d", i))
	}

	repo := NewRepository(pool, 5*time.Second)
	if _, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf}); err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}

	var dose1 *domain.CommandBoardCohortCell
	cohortMatrix := cohortMatrixCells(t, ctx, pool, tenantID, asOf)
	for i := range cohortMatrix {
		if cohortMatrix[i].VaccineLabel == "ET+TT · Dose 1" {
			dose1 = &cohortMatrix[i]
		}
	}
	if dose1 == nil {
		t.Fatalf("cohort matrix missing the ET+TT Dose 1 cell: %+v", cohortMatrix)
	}

	dose1Days := cohortAdministeredDays(t, ctx, pool, tenantID, dose1)
	if len(dose1Days) != 2 {
		t.Fatalf("administeredDays = %v, want 2 IST business days -- 23:30 IST and 00:30 IST are two farm "+
			"days even though they share one UTC date; grouping by the UTC instant loses a day the operator "+
			"actually worked", dose1Days)
	}
	want := []domain.CommandBoardCohortDay{
		{Date: wantDayOne, AnimalCount: 1},
		{Date: wantDayTwo, AnimalCount: 1},
	}
	for i, expected := range want {
		if dose1Days[i] != expected {
			t.Errorf("administeredDays[%d] = %+v, want %+v", i, dose1Days[i], expected)
		}
	}
}

// cohortFixtureIDs / seedCohortFixtureShell build the tenant, park, shed, published two-dose
// protocol and drive batch every cohort-read test needs, so each test body carries only the
// animal/dose shape it is actually pinning.
type cohortFixtureIDs struct {
	tenantID          string
	parkID            string
	parkName          string
	shedID            string
	shedName          string
	protocolID        string
	protocolCode      string
	protocolVersionID string
	ruleDose1ID       string
	ruleDose2ID       string
	batchID           string
	custodianPartyID  string
	plannedDate       time.Time
	windowEnd         time.Time
}

func seedCohortFixtureShell(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ids cohortFixtureIDs) {
	t.Helper()
	execProjectionSQL(t, ctx, pool, "tenant",
		`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, $2, 'active')`,
		ids.tenantID, "Test Org "+ids.protocolCode)
	execProjectionSQL(t, ctx, pool, "custodian party",
		`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1, 'org', $2, 'active')`,
		ids.custodianPartyID, "Custodian "+ids.protocolCode)
	execProjectionSQL(t, ctx, pool, "park",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, $3, 'park', NULL, 'active')`, ids.parkID, ids.tenantID, ids.parkName)
	execProjectionSQL(t, ctx, pool, "shed",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, $3, 'shed', $4, 'active')`, ids.shedID, ids.tenantID, ids.shedName, ids.parkID)
	execProjectionSQL(t, ctx, pool, "protocol definition",
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		 VALUES ($1, $2, $3, $4, 'vaccination', 'active')`,
		ids.protocolID, ids.tenantID, ids.protocolCode, "Vaccination "+ids.protocolCode)
	execProjectionSQL(t, ctx, pool, "protocol version",
		`INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl)
		 VALUES ($1, $2, $3, 'tenant', 1, 'draft', '2026-01-01', '{}')`,
		ids.protocolVersionID, ids.tenantID, ids.protocolID)
	execProjectionSQL(t, ctx, pool, "rule dose 1",
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type)
		 VALUES ($1, $2, $3, 'et_tt_adult_w1', 1, 'birth_age')`, ids.ruleDose1ID, ids.tenantID, ids.protocolVersionID)
	execProjectionSQL(t, ctx, pool, "rule dose 2",
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type)
		 VALUES ($1, $2, $3, 'et_tt_adult_w2', 2, 'birth_age')`, ids.ruleDose2ID, ids.tenantID, ids.protocolVersionID)
	execProjectionSQL(t, ctx, pool, "publish protocol version",
		`UPDATE protocol_versions SET status = 'published', published_at = now() WHERE protocol_version_id = $1`,
		ids.protocolVersionID)
	execProjectionSQL(t, ctx, pool, "drive batch",
		`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id,
		   planned_date, window_start, window_end, status)
		 VALUES ($1, $2, $3, 'park', $4, $5::date, $5::timestamptz, $6::timestamptz, 'in_progress')`,
		ids.batchID, ids.tenantID, ids.protocolVersionID, ids.parkID, ids.plannedDate, ids.windowEnd)
}

// TestVaccinationCommandBoardClosedWithoutDoseAnimalsMatchTheTileAndCarryPartitionLocation pins the
// list behind the Closed, No Dose tile.
//
// Two failures it blocks:
//
//	(1) LIST/COUNT DIVERGENCE. The tile and the list must be selected by the same per-animal
//	    residual predicate. A list built from a looser filter would name animals the tile never
//	    counted, and the CEO would chase work that is not in the bucket.
//	(2) PARENT-SHED LOCATION. An animal standing in "Godel 1 - Part 3" must not be reported as
//	    "Godel 1". shed_id alone is not a ground location when the shed is partitioned, and a park
//	    head sent to the wrong side of a partition cannot find the animal.
//
// Fixture: 3 animals whose only obligation is cancelled with no completion (the residual), one of
// them in a partitioned shed; plus 1 animal with an accepted dose that must NOT appear.
//
// StatusBuckets / ParkScope / OneToMany.
func TestVaccinationCommandBoardClosedWithoutDoseAnimalsMatchTheTileAndCarryPartitionLocation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("LoadLocation(Asia/Kolkata) error = %v", err)
	}

	const (
		tenantID          = "00000000-0000-4000-8000-0000000000f4"
		parkID            = "70000000-0000-4000-8000-0000010000f4"
		shedID            = "70000000-0000-4000-8000-0000020000f4"
		protocolID        = "70000000-0000-4000-8000-000006000ce0"
		protocolVersionID = "70000000-0000-4000-8000-000006000ce1"
		ruleDose1ID       = "70000000-0000-4000-8000-000007000ce1"
		ruleDose2ID       = "70000000-0000-4000-8000-000007000ce2"
		batchID           = "70000000-0000-4000-8000-000004000ce1"
		custodianPartyID  = "70000000-0000-4000-8000-000009000ce1"
		closedCount       = 3
		partitionLabel    = "Part 3"
	)

	doseDay := time.Date(2026, 6, 30, 0, 0, 0, 0, ist)
	asOf := time.Date(2026, 7, 25, 11, 0, 0, 0, ist)
	windowEnd := time.Date(2026, 7, 31, 23, 59, 59, 0, ist)

	seedCohortFixtureShell(t, ctx, pool, cohortFixtureIDs{
		tenantID: tenantID, parkID: parkID, parkName: "CPT-QA-CE", shedID: shedID, shedName: "Godel 1",
		protocolID: protocolID, protocolCode: "vaccination_ce", protocolVersionID: protocolVersionID,
		ruleDose1ID: ruleDose1ID, ruleDose2ID: ruleDose2ID, batchID: batchID,
		custodianPartyID: custodianPartyID, plannedDate: doseDay, windowEnd: windowEnd,
	})

	seedAnimal := func(i int, status string, accepted bool, partitioned bool) string {
		goatID := fmt.Sprintf("70000000-0000-4000-8000-0000340%05d", 700+i)
		execProjectionSQL(t, ctx, pool, "goat",
			`INSERT INTO goats (goat_id, tenant_id, sex, lifecycle_status, management_stage, shed_id, custodian_party_id, dob)
			 VALUES ($1, $2, 'female', 'alive', 'Non-Pregnant', $3, $4, '2023-01-01')`,
			goatID, tenantID, shedID, custodianPartyID)
		if partitioned {
			// source_shed_name is the partition-bearing name the row was normalized FROM
			// ("Godel 1 - Part 3"), retained for traceability and NOT NULL in schema.
			execProjectionSQL(t, ctx, pool, "goat partition",
				`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
				 VALUES ($1, $2, $3, $4, $5)`, tenantID, goatID, shedID, partitionLabel,
				"Godel 1 - "+partitionLabel)
		}
		oblID := fmt.Sprintf("70000000-0000-4000-8000-0000840%05d", 700+i)
		execProjectionSQL(t, ctx, pool, "obligation",
			`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type,
			   scope_type, scope_id, rule_id, status, due_at, batch_id, idempotency_key)
			 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, $7, $8::timestamptz, $9, $10)`,
			oblID, tenantID, protocolVersionID, goatID, shedID, ruleDose1ID, status, doseDay, batchID,
			fmt.Sprintf("obl-ce-%d", i))
		if accepted {
			execProjectionSQL(t, ctx, pool, "completion",
				`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, status, administered_at, verified_at, idempotency_key)
				 VALUES ($1, $2, $3, $4, 'accepted', $5::timestamptz, $5::timestamptz, $6)`,
				fmt.Sprintf("70000000-0000-4000-8000-0000940%05d", 700+i), tenantID, oblID, goatID, doseDay,
				fmt.Sprintf("comp-ce-%d", i))
		}
		return goatID
	}

	// The residual: closed with no completion. One of them stands in a partition.
	partitionedGoat := seedAnimal(0, "canceled", false, true)
	// FAN-OUT TRAP: goat_identifiers is unique per (goat_id, identifier_type) only for the PRIMARY
	// active row, so a goat may hold several active NON-primary rows of one type -- real data does.
	// A plain join would emit this animal twice, so the drawer would repeat it and stop matching the
	// tile. The primary row must win and the animal must appear exactly once.
	execProjectionSQL(t, ctx, pool, "primary tag",
		`INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
		   scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
		 VALUES ($1, $2, 'animal_identifier_2', 'TAG-PRIMARY', 'tag-primary', 'tenant', true, 'active', now(), 'v1')`,
		tenantID, partitionedGoat)
	execProjectionSQL(t, ctx, pool, "second active non-primary tag",
		`INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
		   scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
		 VALUES ($1, $2, 'animal_identifier_2', 'TAG-SECONDARY', 'tag-secondary', 'tenant', false, 'active', now(), 'v1')`,
		tenantID, partitionedGoat)
	seedAnimal(1, "canceled", false, false)
	seedAnimal(2, "canceled", false, false)
	// Must NOT appear: this animal's dose is accepted, so it belongs to the verified tile.
	verifiedGoat := seedAnimal(3, "completed", true, false)

	repo := NewRepository(pool, 5*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}

	if resp.KPIs.ClosedWithoutDose != closedCount {
		t.Fatalf("closedWithoutDose tile = %d, want %d (fixture did not reproduce the residual bucket)",
			resp.KPIs.ClosedWithoutDose, closedCount)
	}
	// (1) The list is exactly the tile's animals -- same predicate, same key set.
	closedAnimals := closedWithoutDoseAnimals(t, ctx, pool, tenantID, asOf)
	if got := len(closedAnimals); got != closedCount {
		t.Fatalf("closedWithoutDoseAnimals = %d animals, want %d -- the list and the tile must be "+
			"selected by the same per-animal residual predicate, or the CEO chases animals the tile "+
			"never counted", got, closedCount)
	}
	for _, animal := range closedAnimals {
		if animal.GoatID == verifiedGoat {
			t.Errorf("verified animal %s appears in the closed-without-dose list -- an accepted dose "+
				"belongs to the verified tile", verifiedGoat)
		}
		if animal.DisplayID == "" || animal.Reason == "" || animal.VaccineLabel == "" {
			t.Errorf("animal %+v is missing identity/reason/vaccine -- the drawer exists to answer "+
				"'which animals and why', so a blank row answers nothing", animal)
		}
		if animal.Reason != "Cancelled" {
			t.Errorf("reason = %q, want %q -- raw obligation status tokens must never reach a CEO screen",
				animal.Reason, "Cancelled")
		}
	}

	// (2) The partitioned animal reports its ground location, not the parent shed.
	var partitioned *domain.CommandBoardClosedWithoutDoseAnimal
	for i := range closedAnimals {
		if closedAnimals[i].GoatID == partitionedGoat {
			partitioned = &closedAnimals[i]
		}
	}
	if partitioned == nil {
		t.Fatalf("the partitioned animal is missing from the list entirely")
	}
	// The animal with two active identifiers of one type appears exactly once, carrying its PRIMARY
	// tag. Duplication here would also mean the list no longer matches the tile.
	occurrences := 0
	for _, animal := range closedAnimals {
		if animal.GoatID == partitionedGoat {
			occurrences++
		}
	}
	if occurrences != 1 {
		t.Errorf("animal with two active animal_identifier_2 rows appears %d times, want 1 -- a plain "+
			"join on goat_identifiers fans the animal out, repeats it in the drawer, pushes distinct "+
			"animals past the list cap, and breaks parity with the closedWithoutDose tile", occurrences)
	}
	if partitioned.Tag2 != "TAG-PRIMARY" {
		t.Errorf("tag2 = %q, want the PRIMARY active identifier %q -- the non-primary row must not win",
			partitioned.Tag2, "TAG-PRIMARY")
	}
	if partitioned.LocationDisplay != "Godel 1 - Part 3" {
		t.Errorf("locationDisplay = %q, want %q -- an animal standing in a partition reported as its "+
			"parent shed sends a park head to the wrong side of the shed", partitioned.LocationDisplay,
			"Godel 1 - Part 3")
	}
	for _, animal := range closedAnimals {
		if animal.LocationDisplay == "whole" || animal.PartitionLabel == "whole" {
			t.Errorf("animal %s leaked the 'whole' matching sentinel to a display field: %+v",
				animal.DisplayID, animal)
		}
		if animal.GoatID != partitionedGoat && animal.LocationDisplay != "Godel 1" {
			t.Errorf("non-partitioned animal locationDisplay = %q, want %q -- a shed with no partition "+
				"renders as its own name", animal.LocationDisplay, "Godel 1")
		}
	}
}

// cohortExceptionAnimals and cohortAdministeredDays fetch the two cohort drawers for ONE cell.
//
// Both used to ride on the board response (cell.MissingPriorDoseGoats, cell.AdministeredDays),
// computed for every cell on every render. They are now per-cell reads addressed by the cell's own
// park/stage/sex/doseCodes, so these tests open them the way the UI does. The assertions are
// unchanged; only where the rows come from moved.
func cohortExceptionAnimals(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, cell *domain.CommandBoardCohortCell) []domain.CommandBoardCohortAnimal {
	t.Helper()
	repo := NewRepository(pool, 5*time.Second)
	page, err := repo.CommandBoardCohortExceptions(ctx, cohortCellQuery(tenantID, cell))
	if err != nil {
		t.Fatalf("CommandBoardCohortExceptions() error = %v", err)
	}
	return page.Animals
}

func cohortAdministeredDays(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, cell *domain.CommandBoardCohortCell) []domain.CommandBoardCohortDay {
	t.Helper()
	repo := NewRepository(pool, 5*time.Second)
	page, err := repo.CommandBoardCohortDays(ctx, cohortCellQuery(tenantID, cell))
	if err != nil {
		t.Fatalf("CommandBoardCohortDays() error = %v", err)
	}
	return page.Days
}

func cohortCellQuery(tenantID string, cell *domain.CommandBoardCohortCell) domain.CommandBoardCohortCellQuery {
	return domain.CommandBoardCohortCellQuery{
		CommandBoardDrilldownQuery: domain.CommandBoardDrilldownQuery{TenantID: tenantID},
		CohortParkID:               cell.Cohort.ParkID,
		ManagementStage:            cell.Cohort.ManagementStage,
		Sex:                        cell.Cohort.Sex,
		DoseCodes:                  cell.DoseCodes,
	}
}

// closedWithoutDoseAnimals fetches the ClosedWithoutDose tile's animal list.
//
// The list used to ship eagerly on the board; on the staging-scale tenant that statement exhausted
// the 15s pool timeout and returned a 500. It is now keyset-paginated behind its own endpoint. The
// page is asked for larger than the fixture so the assertions below still see the whole list.
func closedWithoutDoseAnimals(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, asOf time.Time) []domain.CommandBoardClosedWithoutDoseAnimal {
	t.Helper()
	repo := NewRepository(pool, 5*time.Second)
	page, err := repo.CommandBoardClosedWithoutDoseAnimals(ctx, domain.CommandBoardDrilldownQuery{
		TenantID: tenantID,
		AsOf:     asOf,
		Limit:    domain.CommandBoardDrilldownMaxLimit,
	})
	if err != nil {
		t.Fatalf("CommandBoardClosedWithoutDoseAnimals() error = %v", err)
	}
	return page.Animals
}

// cohortMatrixCells fetches the cohort matrix from its own section endpoint.
//
// It used to ride on the board response. Its three statements -- the cell aggregate, the true herd
// head count and the dose-sequence exception count -- were ~420ms of the board's ~850ms of SQL and
// were what held GET /vaccination/command at p90 416ms against a 300ms budget that
// tools/perf/api-latency-policy.mjs will not allow anyone to raise. The numbers below are unchanged;
// only where they are read from moved.
func cohortMatrixCells(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, asOf time.Time) []domain.CommandBoardCohortCell {
	t.Helper()
	return cohortMatrixCellsScoped(t, ctx, pool, domain.CommandBoardDrilldownQuery{TenantID: tenantID, AsOf: asOf})
}

// cohortMatrixCellsScoped is the same read under an explicit drive/park scope, so scope-leak
// assertions are made against the section that actually serves the cells.
func cohortMatrixCellsScoped(t *testing.T, ctx context.Context, pool *pgxpool.Pool, q domain.CommandBoardDrilldownQuery) []domain.CommandBoardCohortCell {
	t.Helper()
	repo := NewRepository(pool, 30*time.Second)
	page, err := repo.CommandBoardCohortMatrix(ctx, q)
	if err != nil {
		t.Fatalf("CommandBoardCohortMatrix() error = %v", err)
	}
	return page.Cells
}
