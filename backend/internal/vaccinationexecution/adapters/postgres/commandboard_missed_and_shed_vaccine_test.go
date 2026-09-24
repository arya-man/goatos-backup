package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestVaccinationCommandBoardMissedDoseIsNotSwallowedByAnAcceptedDoseOnTheSameAnimal is the
// regression for the board reporting a herd with missed doses as fully green.
//
// The KPI chain folds every one of an animal's obligations to booleans with bool_or and then
// picks ONE bucket by priority. While verified led that chain, a single accepted dose anywhere
// in the animal's history set any_verified for good and swallowed every missed dose the same
// animal also held. On live stg that placed all 137 animals carrying a missed ET+TT dose inside
// VERIFIED while OVERDUE read 0.
//
// The fixture is the exact live shape and is deliberately hostile on BOTH halves of the defect:
//
//	dose 1 — 'completed', accepted completion        (sets any_verified)
//	dose 2 — 'missed', with a RECORDED, UNVERIFIED completion
//
// Dose 2's completion row is the second half. A missed obligation routinely carries one (proof
// submitted after the window shut, or the sweeper closing it while proof sat in the verification
// queue), so no_completion is FALSE and the row fell out of any_overdue and any_scheduled too.
// A test that gave the missed obligation no completion at all would pass against the old code
// via the overdue bucket and certify nothing.
func TestVaccinationCommandBoardMissedWithRecordedProofIsVerificationPendingNotMissed(t *testing.T) {
	// Aggregate-projection adversarial coverage: MultipleDimensions, MultiPage, ExecutionDate,
	// CohortScope, and EveryStatus. One goat has both an accepted historical dose and a missed
	// proof-recorded dose, so the KPI projection must choose the verifier-pending bucket without
	// fanout from completion/proof joins or date/status precedence drift.
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000f1"
	parkID := uuidFromSuffix("01", "f1")
	shedID := uuidFromSuffix("02", "f1")
	goatID := uuidFromSuffix("03", "f1a")
	partyID := uuidFromSuffix("0a", "f1a")
	oblAccepted := uuidFromSuffix("08", "f1a")
	oblMissed := uuidFromSuffix("08", "f1b")

	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, "f1")
	seedCommandBoardPark(t, ctx, pool, tenantID, parkID, shedID, "Missed")

	asOf := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	execProjectionSQL(t, ctx, pool, "custodian party",
		`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1, 'org', 'Custodian F1', 'active')`, partyID)
	execProjectionSQL(t, ctx, pool, "goat",
		`INSERT INTO goats (goat_id, tenant_id, sex, lifecycle_status, management_stage, shed_id, custodian_party_id, dob)
		 VALUES ($1, $2, 'female', 'alive', 'Non-Pregnant', $3, $4, '2025-01-01')`, goatID, tenantID, shedID, partyID)

	// Dose 1: given and verifier-accepted. This is what used to hide dose 2.
	execProjectionSQL(t, ctx, pool, "obligation accepted",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, 'completed', $7::timestamptz, 'missed-f1-accepted')`,
		oblAccepted, tenantID, protocolVersionID, goatID, shedID, ruleID, asOf.Add(-30*24*time.Hour))
	execProjectionSQL(t, ctx, pool, "completion accepted",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, status, administered_at, verified_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'accepted', $5::timestamptz, $5::timestamptz, 'missed-f1-completion-accepted')`,
		uuidFromSuffix("09", "f1a"), tenantID, oblAccepted, goatID, asOf.Add(-30*24*time.Hour))

	// Dose 2: the SAME animal, window closed unvaccinated, proof recorded but never verified.
	execProjectionSQL(t, ctx, pool, "obligation missed",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, 'missed', $7::timestamptz, 'missed-f1-missed')`,
		oblMissed, tenantID, protocolVersionID, goatID, shedID, ruleID, asOf.Add(-3*24*time.Hour))
	execProjectionSQL(t, ctx, pool, "completion recorded unverified",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, status, administered_at, verified_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'recorded', $5::timestamptz, NULL, 'missed-f1-completion-recorded')`,
		uuidFromSuffix("09", "f1b"), tenantID, oblMissed, goatID, asOf.Add(-2*24*time.Hour))

	repo := NewRepository(pool, 5*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}

	if resp.KPIs.Targets != 1 {
		t.Fatalf("targets = %d, want 1 distinct animal", resp.KPIs.Targets)
	}
	// MISSED means NO DOSE REACHED THE ANIMAL. This obligation carries a recorded completion, so the
	// dose was given and the only thing outstanding is a verifier's attention. Counting it as missed
	// is what made the live board report 137 vaccinated goats as unvaccinated while simultaneously
	// showing awaiting_verification = 0 -- two tiles wrong in opposite directions from one predicate.
	if resp.KPIs.MissedNotGiven != 0 {
		t.Fatalf("missed_not_given = %d, want 0; an obligation with a recorded completion was DOSED -- it is a verification backlog, not a missed dose", resp.KPIs.MissedNotGiven)
	}
	if resp.KPIs.AwaitingVerification != 1 {
		t.Fatalf("awaiting_verification = %d, want 1; the unverified proof must stay visible even when an older dose was accepted", resp.KPIs.AwaitingVerification)
	}
	if resp.KPIs.DosesVerified != 0 {
		t.Fatalf("doses_verified = %d, want 0; an older accepted dose must not hide a current proof awaiting verification", resp.KPIs.DosesVerified)
	}
	assertKPIPartitionExhaustive(t, resp.KPIs)
}

// TestVaccinationCommandBoardKPIPartitionStaysExhaustiveWithMissedPresent proves the SIX
// buckets still add up once missed leads the chain.
//
// Disjointness and exhaustiveness are what make the row readable: tiles summing to less than
// targets read as a bug, and tiles summing to more cannot be reconciled at all. Adding a
// sixth bucket ahead of the other five is exactly the kind of edit that reopens that gap, so
// the invariant is pinned over a herd holding EVERY bucket at once rather than over the single
// animal above.
func TestVaccinationCommandBoardKPIPartitionStaysExhaustiveWithMissedPresent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000f2"
	parkID := uuidFromSuffix("01", "f2")
	shedID := uuidFromSuffix("02", "f2")
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, "f2")
	seedCommandBoardPark(t, ctx, pool, tenantID, parkID, shedID, "Partition")

	asOf := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	// One animal per bucket, seeded independently so no animal can satisfy two predicates by
	// accident and mask a disjointness break.
	seedMissedAnimal(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, "f2m", asOf.Add(-3*24*time.Hour))
	seedVerifiedAnimal(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, "f2v", asOf.Add(-10*24*time.Hour))
	seedAwaitingAnimal(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, "f2a", asOf.Add(-5*24*time.Hour))
	seedCommandBoardScheduledAnimal(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, "f2o", asOf.Add(-4*24*time.Hour)) // overdue: open, past due, no completion
	seedCommandBoardScheduledAnimal(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, "f2s", asOf.Add(4*24*time.Hour))  // scheduled ahead
	seedClosedWithoutDoseAnimal(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, "f2c", asOf.Add(-8*24*time.Hour))

	repo := NewRepository(pool, 5*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}

	if resp.KPIs.Targets != 6 {
		t.Fatalf("targets = %d, want 6 distinct animals", resp.KPIs.Targets)
	}
	for label, got := range map[string]int{
		"missed":     resp.KPIs.MissedNotGiven,
		"verified":   resp.KPIs.DosesVerified,
		"awaiting":   resp.KPIs.AwaitingVerification,
		"overdue":    resp.KPIs.OverdueNotGiven,
		"scheduled":  resp.KPIs.ScheduledAhead,
		"closed_nod": resp.KPIs.ClosedWithoutDose,
	} {
		if got != 1 {
			t.Errorf("%s = %d, want exactly 1; each animal was seeded into exactly one bucket", label, got)
		}
	}
	assertKPIPartitionExhaustive(t, resp.KPIs)
}

// TestVaccinationCommandBoardShedVaccineMatrixIsDenseAndFlagsBehindWithoutSummingDoses pins the
// shed x vaccine roll-up.
//
// Two properties matter and neither is obvious from reading a single cell.
//
// DENSITY: a vaccine the tenant's protocol defines but which generated NO obligations must still
// get a cell, as not_planned. The live tenant configures BLUE_TONGUE across eight rule dimensions
// and has produced zero obligations for it, so a column list derived from the cells omits it
// silently and the matrix reads complete while a whole vaccine is unaccounted for.
//
// NO SUMMING: the cell is a boolean OR over the shed's doses of that vaccine, never a sum. An
// earlier vaccine-collapse of the dose-qualified matrix was reverted for adding Dose 1 + Dose 2 +
// Revaccination into a figure larger than the cohort head count. The fixture gives one animal
// TWO doses of the same vaccine so a summing implementation would report behind=2 (or
// total=2) for a one-animal shed and fail here.
func TestVaccinationCommandBoardShedVaccineMatrixIsDenseAndFlagsBehindWithoutSummingDoses(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000f3"
	parkID := uuidFromSuffix("01", "f3")
	shedID := uuidFromSuffix("02", "f3")
	goatID := uuidFromSuffix("03", "f3a")
	partyID := uuidFromSuffix("0a", "f3a")
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, "f3")
	seedCommandBoardPark(t, ctx, pool, tenantID, parkID, shedID, "Matrix")

	asOf := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	// ET_TT carries the obligations through the legacy dimension fallback. BLUE_TONGUE is
	// configured from the rule JSON but has no dimension row -- the live V9 shape that made the
	// grid show only ET+TT even though the published rules contained all vaccine families.
	execProjectionSQL(t, ctx, pool, "dimension et_tt",
		`INSERT INTO protocol_rule_dimensions (protocol_rule_dimension_id, tenant_id, protocol_version_id, rule_id, category, selector_key, vaccine_code)
		 VALUES ($1, $2, $3, $4, 'vaccination', 'sel-et', 'ET_TT')`,
		uuidFromSuffix("0b", "f3a"), tenantID, protocolVersionID, ruleID)
	// The rest of the V9 catalogue lives on its OWN protocol version, PUBLISHED but generating no obligations.
	// seedCommandBoardProtocol publishes the version it creates and published config is immutable,
	// so extra JSON-only rules need a second version. This is the case the density assertion pins:
	// vaccines the CURRENT protocol requires, with nothing planned for them anywhere, must still get
	// columns -- that silence is the finding. A RETIRED vaccine must NOT appear, which the companion
	// test below pins separately.
	btProtocolID := uuidFromSuffix("06", "f3zp")
	btVersionID := uuidFromSuffix("06", "f3zv")
	execProjectionSQL(t, ctx, pool, "blue tongue protocol definition",
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
	 VALUES ($1, $2, 'vaccination_f3_bt', 'Vaccination F3 BT', 'vaccination', 'active')`,
		btProtocolID, tenantID)
	execProjectionSQL(t, ctx, pool, "blue tongue protocol version",
		`INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl)
		 VALUES ($1, $2, $3, 'tenant', 1, 'draft', '2026-01-01', '{}')`,
		btVersionID, tenantID, btProtocolID)
	v9NoWork := []struct {
		code string
		dose string
		id   string
	}{
		{"BLUE_TONGUE", "blue_tongue_adult", "f3z0"},
		{"FMD", "fmd_adult", "f3z1"},
		{"GOAT_POX", "goat_pox_adult", "f3z2"},
		{"HS", "hs_adult", "f3z3"},
		{"PPR", "ppr_adult", "f3z4"},
		{"SHEEP_POX", "sheep_pox_adult", "f3z5"},
		{"Z1_Z3", "z1_z3_adult", "f3z6"},
	}
	for _, vaccine := range v9NoWork {
		execProjectionSQL(t, ctx, pool, "unused rule "+vaccine.code,
			`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, trigger_type, eligibility_json)
			 VALUES ($1, $2, $3, $4, 'birth_age', jsonb_build_object('vaccine', jsonb_build_object('code', $5::text)))`,
			uuidFromSuffix("07", vaccine.id), tenantID, btVersionID, vaccine.dose, vaccine.code)
	}
	execProjectionSQL(t, ctx, pool, "publish blue tongue version",
		`UPDATE protocol_versions SET status = 'published', published_at = now() WHERE protocol_version_id = $1`,
		btVersionID)

	execProjectionSQL(t, ctx, pool, "custodian party",
		`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1, 'org', 'Custodian F3', 'active')`, partyID)
	execProjectionSQL(t, ctx, pool, "goat",
		`INSERT INTO goats (goat_id, tenant_id, sex, lifecycle_status, management_stage, shed_id, custodian_party_id, dob)
		 VALUES ($1, $2, 'female', 'alive', 'Non-Pregnant', $3, $4, '2025-01-01')`, goatID, tenantID, shedID, partyID)

	// TWO ET_TT doses on ONE animal: one missed, one still scheduled ahead. A summing cell
	// would double this single animal.
	execProjectionSQL(t, ctx, pool, "obligation missed",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, 'missed', $7::timestamptz, 'sv-f3-missed')`,
		uuidFromSuffix("08", "f3a"), tenantID, protocolVersionID, goatID, shedID, ruleID, asOf.Add(-3*24*time.Hour))
	execProjectionSQL(t, ctx, pool, "obligation scheduled",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, 'scheduled', $7::timestamptz, 'sv-f3-scheduled')`,
		uuidFromSuffix("08", "f3b"), tenantID, protocolVersionID, goatID, shedID, ruleID, asOf.Add(5*24*time.Hour))

	repo := NewRepository(pool, 5*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}

	wantCodes := []string{"BLUE_TONGUE", "ET_TT", "FMD", "GOAT_POX", "HS", "PPR", "SHEEP_POX", "Z1_Z3"}
	wantLabels := []string{"Blue Tongue", "ET+TT", "FMD", "Goat Pox", "HS", "PPR", "Sheep Pox", "Z1+Z3"}
	if len(resp.ShedVaccineColumns) != len(wantCodes) {
		t.Fatalf("shedVaccineColumns = %v, want %v; columns come from the protocol catalogue, not from the cells", resp.ShedVaccineColumns, wantCodes)
	}
	for i, want := range wantCodes {
		if resp.ShedVaccineColumns[i].Code != want {
			t.Fatalf("shedVaccineColumns[%d].Code = %q, want %q", i, resp.ShedVaccineColumns[i].Code, want)
		}
		// The header LABEL must be server copy. If this ever comes back empty the client falls back
		// to the raw code and the admin-UI contract guard's whole point is lost.
		if resp.ShedVaccineColumns[i].Label != wantLabels[i] {
			t.Fatalf("shedVaccineColumns[%d].Label = %q, want %q", i, resp.ShedVaccineColumns[i].Label, wantLabels[i])
		}
	}

	byCode := map[string]domain.CommandBoardShedVaccineCell{}
	for _, cell := range resp.ShedVaccineMatrix {
		byCode[cell.VaccineCode] = cell
	}
	if len(resp.ShedVaccineMatrix) != len(wantCodes) {
		t.Fatalf("shedVaccineMatrix has %d cells for 1 shed x %d vaccines, want %d; the matrix must be DENSE", len(resp.ShedVaccineMatrix), len(wantCodes), len(wantCodes))
	}

	et := byCode["ET_TT"]
	if et.State != "behind" {
		t.Errorf("ET_TT state = %q, want \"behind\"; the shed holds a missed dose with NO proof against it", et.State)
	}
	if et.BehindAnimals != 1 {
		t.Errorf("ET_TT behindAnimals = %d, want 1; two doses of one vaccine on ONE animal must not sum into two animals", et.BehindAnimals)
	}
	if et.VerifyingAnimals != 0 {
		t.Errorf("ET_TT verifyingAnimals = %d, want 0; no proof was recorded against this shed's doses", et.VerifyingAnimals)
	}
	if et.TotalAnimals != 1 {
		t.Errorf("ET_TT totalAnimals = %d, want 1; the shed holds exactly one animal", et.TotalAnimals)
	}
	if et.BehindAnimals > et.TotalAnimals {
		t.Errorf("ET_TT behindAnimals=%d > totalAnimals=%d; behind is a SUBSET of total by construction", et.BehindAnimals, et.TotalAnimals)
	}

	bt := byCode["BLUE_TONGUE"]
	if bt.State != "not_planned" {
		t.Errorf("BLUE_TONGUE state = %q, want \"not_planned\"; a configured vaccine with zero obligations must be NAMED, never an absent cell", bt.State)
	}
	if bt.ShedName != et.ShedName {
		t.Errorf("BLUE_TONGUE shedName = %q, want %q; the densified cell must carry its shed identity", bt.ShedName, et.ShedName)
	}
}

func TestVaccinationCommandBoardShedVaccineJsonOnlyRuleFeedsCellAndDrawer(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000f5"
	parkID := uuidFromSuffix("01", "f5")
	shedID := uuidFromSuffix("02", "f5")
	goatID := uuidFromSuffix("03", "f5a")
	partyID := uuidFromSuffix("0a", "f5a")
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, "f5")
	seedCommandBoardPark(t, ctx, pool, tenantID, parkID, shedID, "JsonOnly")
	execProjectionSQL(t, ctx, pool, "json vaccine identity without dimension",
		`UPDATE protocol_rules
		    SET dose_code = 'ppr_adult_w1',
		        eligibility_json = '{"vaccine":{"code":"PPR"}}'::jsonb
		  WHERE tenant_id = $1 AND rule_id = $2`,
		tenantID, ruleID)

	asOf := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	execProjectionSQL(t, ctx, pool, "custodian party",
		`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1, 'org', 'Custodian F5', 'active')`, partyID)
	execProjectionSQL(t, ctx, pool, "goat",
		`INSERT INTO goats (goat_id, tenant_id, sex, lifecycle_status, management_stage, shed_id, custodian_party_id, dob)
		 VALUES ($1, $2, 'female', 'alive', 'Non-Pregnant', $3, $4, '2025-01-01')`, goatID, tenantID, shedID, partyID)
	execProjectionSQL(t, ctx, pool, "json-only missed obligation",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, 'missed', $7::timestamptz, 'sv-f5-missed')`,
		uuidFromSuffix("08", "f5a"), tenantID, protocolVersionID, goatID, shedID, ruleID, asOf.Add(-3*24*time.Hour))

	repo := NewRepository(pool, 5*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}

	var ppr *domain.CommandBoardShedVaccineCell
	for i := range resp.ShedVaccineMatrix {
		if resp.ShedVaccineMatrix[i].VaccineCode == "PPR" {
			ppr = &resp.ShedVaccineMatrix[i]
			break
		}
	}
	if ppr == nil {
		t.Fatalf("missing PPR cell from JSON-only rule; columns=%v matrix=%v", resp.ShedVaccineColumns, resp.ShedVaccineMatrix)
	}
	if ppr.State != "behind" || ppr.BehindAnimals != 1 || ppr.TotalAnimals != 1 {
		t.Fatalf("PPR cell = %+v, want one behind animal from JSON-only rule", *ppr)
	}
	drill := shedVaccineDrilldown(t, ctx, pool, tenantID, asOf, *ppr)
	if len(drill.Animals) != 1 {
		t.Fatalf("json-only PPR drawer has %d animals, want 1; drilldown must use the same vaccine source as the cell", len(drill.Animals))
	}
	if got := drill.Animals[0].GoatID; got != goatID {
		t.Fatalf("drawer goat = %s, want %s", got, goatID)
	}
}

// TestVaccinationCommandBoardShedVaccineMatrixTreatsOverduePastDueAsBehind pins the one place
// this matrix is deliberately BROADER than the KPI row's missed bucket.
//
// An obligation still 'scheduled' whose due business date has passed has not been swept to
// 'missed' yet. A park head standing in the shed cannot act on that difference -- both mean the
// animal is unvaccinated past its window -- so the cell must go red on either. Reading only
// status='missed' here would make the matrix green for a whole shed between the window closing
// and the sweeper running.
func TestVaccinationCommandBoardShedVaccineMatrixTreatsOverduePastDueAsBehind(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000f4"
	parkID := uuidFromSuffix("01", "f4")
	shedID := uuidFromSuffix("02", "f4")
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, tenantID, "f4")
	seedCommandBoardPark(t, ctx, pool, tenantID, parkID, shedID, "Sweeper")
	execProjectionSQL(t, ctx, pool, "dimension et_tt",
		`INSERT INTO protocol_rule_dimensions (protocol_rule_dimension_id, tenant_id, protocol_version_id, rule_id, category, selector_key, vaccine_code)
		 VALUES ($1, $2, $3, $4, 'vaccination', 'sel-et', 'ET_TT')`,
		uuidFromSuffix("0b", "f4a"), tenantID, protocolVersionID, ruleID)

	asOf := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	// Still 'scheduled', due four days ago: the sweeper has not run.
	seedCommandBoardScheduledAnimal(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, "f4a", asOf.Add(-4*24*time.Hour))

	repo := NewRepository(pool, 5*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}
	if resp.KPIs.MissedNotGiven != 0 {
		t.Fatalf("missed_not_given = %d, want 0; nothing has been swept to 'missed' yet", resp.KPIs.MissedNotGiven)
	}
	if len(resp.ShedVaccineMatrix) != 1 {
		t.Fatalf("shedVaccineMatrix has %d cells, want 1", len(resp.ShedVaccineMatrix))
	}
	if got := resp.ShedVaccineMatrix[0].State; got != "behind" {
		t.Fatalf("ET_TT state = %q, want \"behind\"; a past-due open dose is behind whether or not the sweeper has flipped it to missed", got)
	}
}

// assertKPIPartitionExhaustive is the invariant every KPI test shares: the seven buckets are a
// disjoint, exhaustive partition of targets.
func assertKPIPartitionExhaustive(t *testing.T, k domain.CommandBoardKPI) {
	t.Helper()
	sum := k.MissedNotGiven + k.DosesVerified + k.AwaitingVerification + k.ReworkNeeded + k.OverdueNotGiven + k.ScheduledAhead + k.ClosedWithoutDose
	if sum != k.Targets {
		t.Fatalf("buckets sum to %d but targets = %d (missed=%d verified=%d awaiting=%d rework=%d overdue=%d scheduled=%d closed=%d); the tiles must partition targets",
			sum, k.Targets, k.MissedNotGiven, k.DosesVerified, k.AwaitingVerification, k.ReworkNeeded, k.OverdueNotGiven, k.ScheduledAhead, k.ClosedWithoutDose)
	}
}

func seedMissedAnimal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, protocolVersionID, ruleID, shedID, suffix string, dueAt time.Time) {
	t.Helper()
	seedBucketAnimal(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, suffix, dueAt, "missed", "")
}

func seedVerifiedAnimal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, protocolVersionID, ruleID, shedID, suffix string, dueAt time.Time) {
	t.Helper()
	seedBucketAnimal(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, suffix, dueAt, "completed", "accepted")
}

func seedAwaitingAnimal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, protocolVersionID, ruleID, shedID, suffix string, dueAt time.Time) {
	t.Helper()
	seedBucketAnimal(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, suffix, dueAt, "in_progress", "recorded")
}

// seedClosedWithoutDoseAnimal is the residual bucket: the obligation reached a terminal status
// with NO completion row against it at all.
func seedClosedWithoutDoseAnimal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, protocolVersionID, ruleID, shedID, suffix string, dueAt time.Time) {
	t.Helper()
	seedBucketAnimal(t, ctx, pool, tenantID, protocolVersionID, ruleID, shedID, suffix, dueAt, "canceled", "")
}

// seedBucketAnimal creates one goat with exactly ONE obligation in the given status, plus at
// most one completion. completionStatus "" means no completion row -- which is a different fact
// from a completion in any status and is what separates the overdue and closed buckets.
func seedBucketAnimal(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, protocolVersionID, ruleID, shedID, suffix string, dueAt time.Time, obligationStatus, completionStatus string) {
	t.Helper()
	partyID := uuidFromSuffix("0a", suffix)
	goatID := uuidFromSuffix("03", suffix)
	oblID := uuidFromSuffix("08", suffix)
	execProjectionSQL(t, ctx, pool, "custodian party",
		`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1, 'org', 'Custodian', 'active')`, partyID)
	execProjectionSQL(t, ctx, pool, "goat",
		`INSERT INTO goats (goat_id, tenant_id, sex, lifecycle_status, management_stage, shed_id, custodian_party_id, dob)
		 VALUES ($1, $2, 'female', 'alive', 'Non-Pregnant', $3, $4, '2025-01-01')`, goatID, tenantID, shedID, partyID)
	execProjectionSQL(t, ctx, pool, "obligation "+obligationStatus,
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'goat', 'shed', $5, $6, $7, $8::timestamptz, $9)`,
		oblID, tenantID, protocolVersionID, goatID, shedID, ruleID, obligationStatus, dueAt, "bucket-"+suffix)
	if completionStatus == "" {
		return
	}
	verifiedAt := "NULL"
	if completionStatus == "accepted" {
		verifiedAt = "$5::timestamptz"
	}
	execProjectionSQL(t, ctx, pool, "completion "+completionStatus,
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, status, administered_at, verified_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, '`+completionStatus+`', $5::timestamptz, `+verifiedAt+`, $6)`,
		uuidFromSuffix("09", suffix), tenantID, oblID, goatID, dueAt, "bucket-completion-"+suffix)
}
