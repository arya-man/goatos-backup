package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Health Analytics, end to end on the production path.
//
// NOTHING derived is seeded. The observation is submitted through the real
// diagnosis service, the Director confirms through the real service, the course
// opens through the real repository, the treatment session is completed through
// the real completion path, and the death closes the case through the SAME
// consumer the approved-death event calls. Only three things are inserted
// directly, and each is an EXTERNAL input this module does not own: the tenant /
// park / pen scope, the animals, and the identity module's exit columns on a
// goat that died.
//
// What it proves, in one pass:
//
//	the disease board keys on the REGISTER RULE the engine named, not the card;
//	an animal that died UNDER TREATMENT is attributed to that disease;
//	an animal that died with no case is unattributed and never given one;
//	the two mortality buckets are disjoint and sum to the death total;
//	the adherence buckets are disjoint and sum to the sessions due;
//	the medicine table counts the dose the operator actually recorded;
//	the engine table counts a proposal once per run and matches it to its case;
//	the pen name comes back composed, not as a raw uuid.

const (
	analyticsDeadGoat = "70000000-0000-4000-8000-0000000000d1"
	analyticsUndx     = "70000000-0000-4000-8000-0000000000d2"
	// The DISCRIMINATING animal: it was treated, RECOVERED, and died later. It is
	// unattributed (no case was open at death) but NOT never-diagnosed. Without it
	// "had an open case at death" and "had any case ever" produce identical
	// numbers, and the attribution predicate could be widened to the second
	// without a single test going red.
	analyticsRecovered     = "70000000-0000-4000-8000-0000000000d3"
	analyticsDeadGoatTagID = "70000000-0000-4000-8000-0000000000a1"
)

func seedAnalyticsAnimals(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO goats (goat_id,tenant_id,display_id,species,sex,lifecycle_status,age_band,custodian_party_id,park_id,shed_id,current_location_id,origin_type,dob,entry_date)
		  VALUES ($1::uuid,$2::uuid,'G-710002','goat','female','alive','adult',$3::uuid,$4::uuid,$5::uuid,$5::uuid,'procured',DATE '2024-01-01',DATE '2024-01-01')`,
			[]any{analyticsDeadGoat, healthTenant, healthParty, healthPark, healthShed}},
		{`INSERT INTO goats (goat_id,tenant_id,display_id,species,sex,lifecycle_status,age_band,custodian_party_id,park_id,shed_id,current_location_id,origin_type,dob,entry_date)
		  VALUES ($1::uuid,$2::uuid,'G-710003','goat','male','alive','adult',$3::uuid,$4::uuid,$5::uuid,$5::uuid,'procured',DATE '2024-01-01',DATE '2024-01-01')`,
			[]any{analyticsUndx, healthTenant, healthParty, healthPark, healthShed}},
		{`INSERT INTO goats (goat_id,tenant_id,display_id,species,sex,lifecycle_status,age_band,custodian_party_id,park_id,shed_id,current_location_id,origin_type,dob,entry_date)
		  VALUES ($1::uuid,$2::uuid,'G-710004','goat','female','alive','adult',$3::uuid,$4::uuid,$5::uuid,$5::uuid,'procured',DATE '2024-01-01',DATE '2024-01-01')`,
			[]any{analyticsRecovered, healthTenant, healthParty, healthPark, healthShed}},
		// The farm reads an animal by its RFID, so the death list must render one.
		{`INSERT INTO goat_identifiers (identifier_id,tenant_id,goat_id,identifier_type,identifier_value,normalized_value,scope_key,is_primary_for_goat,status,valid_from,normalizer_version)
		  VALUES ($1::uuid,$2::uuid,$3::uuid,'animal_identifier_1','982000123456789','982000123456789','tenant',true,'active',now(),'v1')`,
			[]any{analyticsDeadGoatTagID, healthTenant, analyticsDeadGoat}},
	} {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed analytics animals: %v", err)
		}
	}
}

// exitAsDied writes the identity module's own exit columns. Health does not own
// this table; for this read it is an external input fact, exactly as the E2E
// contract allows.
func exitAsDied(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
UPDATE goats
SET lifecycle_status = 'dead', exit_reason = 'died', exited_at = now(), updated_at = now()
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`, healthTenant, goatID); err != nil {
		t.Fatalf("exit goat %s: %v", goatID, err)
	}
}

func diagnoseFever(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, key string) string {
	t.Helper()
	svc, _ := diagnosisStack(t, ctx, pool)
	observation := feverObservation(key)
	observation.GoatID = goatID
	submitted, err := svc.SubmitObservation(ctx, observation)
	if err != nil {
		t.Fatalf("submit observation for %s: %v", goatID, err)
	}
	confirmed, err := svc.ConfirmDiagnosis(ctx, domain.ConfirmDiagnosisInput{
		TenantID: healthTenant, ActorID: healthActor, DiagnosisRunID: submitted.DiagnosisRunID,
		ConfirmedProblems: []string{"FEVER"},
		IdempotencyKey:    "confirm-" + key, RequestFingerprint: "fp-confirm-" + key,
	})
	if err != nil {
		t.Fatalf("confirm diagnosis for %s: %v", goatID, err)
	}
	if len(confirmed.OpenedCases) != 1 {
		t.Fatalf("want one opened course for %s, got %+v", goatID, confirmed.OpenedCases)
	}
	return confirmed.OpenedCases[0].CaseID
}

func TestHealthAnalyticsEndToEndOnTheProductionPath(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	seedAnalyticsAnimals(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())

	repo := NewRepository(pool, 30*time.Second)

	// 1. A live animal is observed, diagnosed and treated. Day 1 is worked.
	treatedCase := diagnoseFever(t, ctx, pool, healthGoat, "obs-treated")
	var day1Session string
	if err := pool.QueryRow(ctx, `
SELECT health_session_id::text FROM health_treatment_sessions
WHERE tenant_id=$1::uuid AND health_case_id=$2::uuid AND day_no=1`,
		healthTenant, treatedCase).Scan(&day1Session); err != nil {
		t.Fatalf("find day 1 session: %v", err)
	}
	if _, err := repo.CompleteWorkItem(ctx, domain.CompleteInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: day1Session,
		IdempotencyKey: "analytics-complete-1", RequestFingerprint: "analytics-complete-fp",
	}); err != nil {
		t.Fatalf("complete day 1: %v", err)
	}

	// 2. A second animal is diagnosed and then DIES under treatment. The case is
	//    closed through the same consumer the approved-death event calls.
	diagnoseFever(t, ctx, pool, analyticsDeadGoat, "obs-died")
	if err := repo.CloseForApprovedDeath(ctx, healthTenant, analyticsDeadGoat, domain.DeathCause{}); err != nil {
		t.Fatalf("close for approved death: %v", err)
	}
	exitAsDied(t, ctx, pool, analyticsDeadGoat)

	// 3. A third animal dies having never been seen by the health module at all.
	exitAsDied(t, ctx, pool, analyticsUndx)

	// 4. A fourth animal was treated, RECOVERED, and died later with nothing open.
	//    This is the case that separates "a case was open when it died" from "it
	//    had a case at some point": it must be UNATTRIBUTED and must NOT be
	//    counted as never-diagnosed.
	recoveredCase := diagnoseFever(t, ctx, pool, analyticsRecovered, "obs-recovered")
	if _, err := repo.CloseCase(ctx, domain.CloseCaseInput{
		TenantID: healthTenant, ActorID: healthActor, CaseID: recoveredCase,
		Outcome: domain.CaseOutcomeRecovered, Note: "eating normally again",
		IdempotencyKey: "analytics-close-recovered", RequestFingerprint: "analytics-close-fp",
	}); err != nil {
		t.Fatalf("close recovered case: %v", err)
	}
	exitAsDied(t, ctx, pool, analyticsRecovered)

	ist := biztime.DefaultLocation()
	today := time.Now().In(ist)
	from := today.AddDate(0, 0, -30).Format(domain.HealthAnalyticsDateLayout)
	to := today.Format(domain.HealthAnalyticsDateLayout)

	got, err := repo.GetHealthAnalytics(ctx, domain.HealthAnalyticsQuery{
		TenantID: healthTenant, FromDate: from, ToDate: to,
	})
	if err != nil {
		t.Fatalf("read analytics: %v", err)
	}

	// ---- Totals -------------------------------------------------------------
	if got.Totals.NewCases != 3 {
		t.Errorf("new cases = %d, want 3", got.Totals.NewCases)
	}
	if got.Totals.OpenCases != 1 {
		t.Errorf("open cases = %d, want 1 -- the dead and recovered courses are closed", got.Totals.OpenCases)
	}
	if got.Totals.Recovered != 1 {
		t.Errorf("recovered = %d, want 1", got.Totals.Recovered)
	}
	if got.Totals.Deaths != 3 {
		t.Fatalf("deaths = %d, want 3", got.Totals.Deaths)
	}
	// ONLY the animal that had a case OPEN when it died. The recovered animal
	// also died and also had a case, and must not be attributed to a disease it
	// had already got over.
	if got.Totals.DeathsAttributed != 1 {
		t.Errorf("attributed deaths = %d, want 1 -- a recovered animal that later died is not a disease death", got.Totals.DeathsAttributed)
	}
	if got.Totals.DeathsUnattributed != 2 {
		t.Errorf("unattributed deaths = %d, want 2", got.Totals.DeathsUnattributed)
	}
	// THE INVARIANT. The two buckets are disjoint and cover every death; a chart
	// whose halves do not add to its own total is a chart nobody can read.
	if got.Totals.DeathsAttributed+got.Totals.DeathsUnattributed != got.Totals.Deaths {
		t.Errorf("attributed %d + unattributed %d != deaths %d",
			got.Totals.DeathsAttributed, got.Totals.DeathsUnattributed, got.Totals.Deaths)
	}
	// Never-diagnosed is a SUBSET of unattributed, never a third bucket.
	if got.Totals.DeathsNeverDiagnosed != 1 {
		t.Errorf("never-diagnosed deaths = %d, want 1", got.Totals.DeathsNeverDiagnosed)
	}
	if got.Totals.DeathsNeverDiagnosed > got.Totals.DeathsUnattributed {
		t.Errorf("never-diagnosed %d exceeds unattributed %d; it must be a subset",
			got.Totals.DeathsNeverDiagnosed, got.Totals.DeathsUnattributed)
	}

	// ---- Months -------------------------------------------------------------
	var monthDeaths int64
	for _, month := range got.Months {
		if month.DeathsAttributed+month.DeathsUnattributed != month.Deaths {
			t.Errorf("%s: attributed %d + unattributed %d != deaths %d",
				month.Month, month.DeathsAttributed, month.DeathsUnattributed, month.Deaths)
		}
		monthDeaths += month.Deaths
	}
	if monthDeaths != got.Totals.Deaths {
		t.Errorf("months sum to %d deaths, totals say %d", monthDeaths, got.Totals.Deaths)
	}

	// ---- Disease board ------------------------------------------------------
	if len(got.Diseases) != 1 {
		t.Fatalf("disease rows = %d, want 1: %+v", len(got.Diseases), got.Diseases)
	}
	fever := got.Diseases[0]
	// Keyed on the RULE the engine named, not on the treatment card. The card key
	// is 'fever' and is many-to-one across diseases, so it cannot answer this.
	if fever.Key != "FEVER" || fever.KeyKind != "register_rule" {
		t.Errorf("disease key = %q (%s), want FEVER as a register_rule", fever.Key, fever.KeyKind)
	}
	if fever.Label != "Fever" {
		t.Errorf("disease label = %q, want the farm-readable name", fever.Label)
	}
	if fever.NewCases != 3 || fever.Died != 1 || fever.OpenCases != 1 || fever.Recovered != 1 {
		t.Errorf("fever row = new %d / open %d / recovered %d / died %d, want 3 / 1 / 1 / 1",
			fever.NewCases, fever.OpenCases, fever.Recovered, fever.Died)
	}
	// One death among three cases, to one decimal place.
	if fever.CaseFatalityPct != 33.3 {
		t.Errorf("case fatality = %v, want 33.3", fever.CaseFatalityPct)
	}
	if fever.AgeBands != "adult" {
		t.Errorf("age bands = %q, want adult", fever.AgeBands)
	}

	// ---- Adherence ----------------------------------------------------------
	if got.Adherence.OnTime != 1 {
		t.Errorf("on time = %d, want the one session actually worked", got.Adherence.OnTime)
	}
	sum := got.Adherence.OnTime + got.Adherence.Late + got.Adherence.Rework + got.Adherence.NotDone
	if sum != got.Adherence.SessionsDue {
		t.Errorf("adherence buckets sum to %d, sessions due = %d", sum, got.Adherence.SessionsDue)
	}
	// Verification is a SEPARATE axis: awaiting-verification overlaps on-time and
	// late and must never be added to them.
	if got.Adherence.AwaitingVerification > got.Adherence.OnTime+got.Adherence.Late {
		t.Errorf("awaiting verification %d exceeds the completed sessions it is a subset of",
			got.Adherence.AwaitingVerification)
	}

	// ---- Medicines ----------------------------------------------------------
	if len(got.Medicines) != 1 {
		t.Fatalf("medicine rows = %d, want 1: %+v", len(got.Medicines), got.Medicines)
	}
	if got.Medicines[0].Name != "Tylosin" || got.Medicines[0].Route != "IM" {
		t.Errorf("medicine = %q by %q, want Tylosin by IM", got.Medicines[0].Name, got.Medicines[0].Route)
	}
	if got.Medicines[0].Doses != 1 || got.Medicines[0].Animals != 1 {
		t.Errorf("medicine = %d doses to %d animals, want 1 and 1",
			got.Medicines[0].Doses, got.Medicines[0].Animals)
	}

	// ---- Diagnosis engine ---------------------------------------------------
	if got.Engine.Observations != 3 || got.Engine.Confirmed != 3 {
		t.Errorf("engine = %d observations / %d confirmed, want 3 and 3",
			got.Engine.Observations, got.Engine.Confirmed)
	}
	if got.Engine.ConfirmedPct != 100 {
		t.Errorf("confirmed share = %v, want 100", got.Engine.ConfirmedPct)
	}
	var feverRule *domain.HealthAnalyticsEngineRule
	for i := range got.Engine.Rules {
		if got.Engine.Rules[i].Key == "FEVER" {
			feverRule = &got.Engine.Rules[i]
		}
	}
	if feverRule == nil {
		t.Fatalf("no FEVER rule row: %+v", got.Engine.Rules)
	}
	// Once per RUN, never once per entry in the proposal array.
	if feverRule.Proposed != 3 || feverRule.Opened != 3 {
		t.Errorf("FEVER = %d proposed / %d opened, want 3 and 3", feverRule.Proposed, feverRule.Opened)
	}
	if feverRule.NotTakenUpPct != 0 {
		t.Errorf("not taken up = %v, want 0", feverRule.NotTakenUpPct)
	}

	// ---- Death list ---------------------------------------------------------
	if len(got.Deaths) != 3 {
		t.Fatalf("death rows = %d, want 3: %+v", len(got.Deaths), got.Deaths)
	}
	byGoat := map[string]domain.HealthAnalyticsDeath{}
	for _, row := range got.Deaths {
		byGoat[row.GoatID] = row
	}
	treated, ok := byGoat[analyticsDeadGoat]
	if !ok {
		t.Fatalf("the animal that died under treatment is missing from the list")
	}
	if treated.Attribution != "attributed" || treated.DiseaseLabel != "Fever" {
		t.Errorf("treated death = %q / %q, want attributed to Fever", treated.Attribution, treated.DiseaseLabel)
	}
	if treated.NeverDiagnosed {
		t.Error("an animal that died under treatment is not never-diagnosed")
	}
	if treated.DaysUnderTreatment == nil {
		t.Error("a death under treatment carries its days under treatment")
	}
	if treated.Tag != "982000123456789" {
		t.Errorf("tag = %q, want the animal's RFID", treated.Tag)
	}
	// The pen name is COMPOSED through the canonical resolver. A raw uuid here is
	// the defect this round trip exists to prevent.
	if treated.OperationalLocationDisplay != "Health Shed" {
		t.Errorf("pen = %q, want the composed pen name", treated.OperationalLocationDisplay)
	}
	if treated.ParkLabel != "CPT" {
		t.Errorf("park = %q, want CPT", treated.ParkLabel)
	}

	undiagnosed, ok := byGoat[analyticsUndx]
	if !ok {
		t.Fatalf("the animal that died with no case is missing from the list")
	}
	if undiagnosed.Attribution != "unattributed" {
		t.Errorf("undiagnosed death = %q, want unattributed", undiagnosed.Attribution)
	}
	// THE RULE THIS WHOLE PAGE TURNS ON: an unattributed death is never given a
	// disease, because Goat OS does not record one.
	if undiagnosed.DiseaseLabel != "" {
		t.Errorf("an unattributed death carries the disease %q; it must carry none", undiagnosed.DiseaseLabel)
	}
	if !undiagnosed.NeverDiagnosed {
		t.Error("an animal with no case on record is never-diagnosed")
	}
	if undiagnosed.DaysUnderTreatment != nil {
		t.Error("an unattributed death has no days under treatment")
	}

	// THE DISCRIMINATING ROW. Treated, recovered, died later: unattributed like
	// the one above, but NOT never-diagnosed. Widening the attribution predicate
	// from "a case was open at death" to "it ever had a case" turns this row
	// attributed and this assertion red -- which is the point of it existing.
	recovered, ok := byGoat[analyticsRecovered]
	if !ok {
		t.Fatalf("the animal that recovered and died later is missing from the list")
	}
	if recovered.Attribution != "unattributed" {
		t.Errorf("recovered-then-died = %q, want unattributed -- it had got over the disease", recovered.Attribution)
	}
	if recovered.DiseaseLabel != "" {
		t.Errorf("recovered-then-died carries the disease %q; the case was closed before it died", recovered.DiseaseLabel)
	}
	if recovered.NeverDiagnosed {
		t.Error("an animal with a closed case on record has been diagnosed; only the count of open cases at death is zero")
	}
}

// A park scope narrows every figure on the page, and an unrelated park must
// return the honest empty rather than the tenant's whole clinical picture.
func TestHealthAnalyticsParkScopeNarrowsEveryFigure(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	seedAnalyticsAnimals(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())

	repo := NewRepository(pool, 30*time.Second)
	diagnoseFever(t, ctx, pool, healthGoat, "obs-scope")
	exitAsDied(t, ctx, pool, analyticsUndx)

	otherPark := "70000000-0000-4000-8000-0000000000f9"
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id,tenant_id,location_type,location_code,name,status)
VALUES ($2::uuid,$1::uuid,'park','CBE','CBE','active') ON CONFLICT DO NOTHING`,
		healthTenant, otherPark); err != nil {
		t.Fatalf("seed other park: %v", err)
	}

	ist := biztime.DefaultLocation()
	today := time.Now().In(ist)
	from := today.AddDate(0, 0, -30).Format(domain.HealthAnalyticsDateLayout)
	to := today.Format(domain.HealthAnalyticsDateLayout)

	scoped, err := repo.GetHealthAnalytics(ctx, domain.HealthAnalyticsQuery{
		TenantID: healthTenant, ParkID: &otherPark, FromDate: from, ToDate: to,
	})
	if err != nil {
		t.Fatalf("read scoped analytics: %v", err)
	}
	if scoped.Totals.NewCases != 0 || scoped.Totals.OpenCases != 0 || scoped.Totals.Deaths != 0 {
		t.Errorf("a park with no health work reports new=%d open=%d deaths=%d, want zeroes",
			scoped.Totals.NewCases, scoped.Totals.OpenCases, scoped.Totals.Deaths)
	}
	if len(scoped.Diseases) != 0 || len(scoped.Deaths) != 0 || len(scoped.Medicines) != 0 {
		t.Errorf("a park with no health work returned rows: %+v", scoped)
	}
	if scoped.Engine.Observations != 0 {
		t.Errorf("engine observations = %d in a park with none", scoped.Engine.Observations)
	}
	// The month spine still spans the window: an empty park has quiet months, not
	// missing ones, and a chart with no columns reads as a broken page.
	if len(scoped.Months) == 0 {
		t.Error("the month spine must still cover the window")
	}
}
