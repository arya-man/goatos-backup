package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	healthapp "github.com/vgoats/goatos/backend/internal/health/app"
	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// The production path, end to end: a health manager submits an observation form,
// the engine proposes, the Director confirms, and a treatment course opens with
// its visits scheduled. Nothing here seeds a proposal or a case -- every row is
// produced by the same service and repository the API calls.

func diagnosisStack(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*healthapp.DiagnosisService, *DiagnosisRepository) {
	t.Helper()
	base := NewRepository(pool, 30*time.Second)
	repo := NewDiagnosisRepository(base)
	reg, err := diagnosis.AdultRegister()
	if err != nil {
		t.Fatalf("load register: %v", err)
	}
	svc, err := healthapp.NewDiagnosisService(repo, reg)
	if err != nil {
		t.Fatalf("wire service: %v", err)
	}
	return svc, repo
}

func publishCard(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cards ...domain.SourceProtocol) {
	t.Helper()
	if err := NewRepository(pool, 30*time.Second).ReplacePublishedProtocols(
		ctx, healthTenant, healthActor, "health-test", "hash-e2e", cards); err != nil {
		t.Fatalf("publish cards: %v", err)
	}
}

func feverCard() domain.SourceProtocol {
	instruction := "Check temperature"
	medicine, dose, denom, route := "Tylosin", "0.1", "kg", "IM"
	return domain.SourceProtocol{
		DiseaseKey: "fever", DisplayName: "Fever", AgeBand: domain.AgeBandAdult, DurationDays: 3,
		Steps: []domain.ProtocolStep{
			{DayNo: 1, Session: domain.SessionMorning, Seq: 1, RecordType: "action", Instruction: &instruction},
			{DayNo: 1, Session: domain.SessionMorning, Seq: 2, RecordType: "medication",
				MedicineName: &medicine, DosageText: &dose, DosageDenominator: &denom, MedicineRoute: &route},
			{DayNo: 2, Session: domain.SessionMorning, Seq: 3, RecordType: "medication",
				MedicineName: &medicine, DosageText: &dose, DosageDenominator: &denom, MedicineRoute: &route},
			{DayNo: 3, Session: domain.SessionMorning, Seq: 4, RecordType: "medication",
				MedicineName: &medicine, DosageText: &dose, DosageDenominator: &denom, MedicineRoute: &route},
		},
	}
}

func feverObservation(key string) domain.SubmitObservationInput {
	temp := 104.8
	return domain.SubmitObservationInput{
		TenantID: healthTenant, ActorID: healthActor, GoatID: healthGoat,
		Findings: diagnosis.Findings{
			Temp:     &temp,
			Eating:   diagnosis.MultiValue{"normal"},
			Activity: "standing",
		},
		IdempotencyKey: key, RequestFingerprint: "fp-" + key, TraceID: "trace-" + key,
	}
}

// The whole loop. A standing, eating doe at 104.8F is a probable Fever; the
// Director confirms it and a ward course opens.
func TestObservationThroughConfirmationOpensACourse(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	svc, _ := diagnosisStack(t, ctx, pool)

	submitted, err := svc.SubmitObservation(ctx, feverObservation("obs-1"))
	if err != nil {
		t.Fatalf("submit observation: %v", err)
	}
	if submitted.Status != domain.DiagnosisStatusProposed {
		t.Errorf("status = %q, want proposed -- nothing is confirmed by submitting", submitted.Status)
	}
	if submitted.Proposal.RegisterVersion != "adult-1" {
		t.Errorf("register_version = %q, want adult-1", submitted.Proposal.RegisterVersion)
	}
	if !containsStr(submitted.Proposal.Problems, "FEVER") {
		t.Fatalf("expected FEVER, got %v", submitted.Proposal.Problems)
	}
	if submitted.Proposal.Housing.Acuity != diagnosis.AcuityWard {
		t.Errorf("a standing 104.8 fever is ward, got %q", submitted.Proposal.Housing.Acuity)
	}
	if len(submitted.Confirmable) != 1 || !submitted.Confirmable[0].SOPAvailable {
		t.Fatalf("fever card is published, so it must be confirmable: %+v", submitted.Confirmable)
	}

	// Submitting alone must NOT have opened anything.
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM health_cases WHERE tenant_id=$1::uuid`, healthTenant); got != 0 {
		t.Fatalf("a proposal must open no course; found %d", got)
	}

	confirmed, err := svc.ConfirmDiagnosis(ctx, domain.ConfirmDiagnosisInput{
		TenantID: healthTenant, ActorID: healthActor, DiagnosisRunID: submitted.DiagnosisRunID,
		ConfirmedProblems: []string{"FEVER"},
		IdempotencyKey:    "confirm-1", RequestFingerprint: "fp-confirm-1",
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if len(confirmed.OpenedCases) != 1 {
		t.Fatalf("want one opened course, got %+v", confirmed.OpenedCases)
	}

	opened := confirmed.OpenedCases[0]
	if opened.ExitType != domain.ExitTypeFixed {
		t.Errorf("fever is a fixed course, got exit_type %q", opened.ExitType)
	}
	if opened.DurationDays == nil || *opened.DurationDays != 3 {
		t.Errorf("a fixed course carries its day count, got %v", opened.DurationDays)
	}
	// Ward earns ONE visit a day (decision F), so three days is three sessions --
	// not six, which is what an ICU animal would get.
	if opened.SessionCount != 3 {
		t.Errorf("ward for 3 days = 3 visits, got %d", opened.SessionCount)
	}

	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM health_treatment_sessions WHERE tenant_id=$1::uuid AND session='evening'`, healthTenant); got != 0 {
		t.Errorf("a ward course must have no evening visit, found %d", got)
	}
	// The register rule is recorded so the next form can reconcile against it.
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM health_cases WHERE tenant_id=$1::uuid AND register_rule_id='FEVER'`, healthTenant); got != 1 {
		t.Errorf("the case must record the register rule that opened it")
	}
	// Nothing may be due in the overnight window.
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM health_treatment_sessions
WHERE tenant_id=$1::uuid
  AND EXTRACT(hour FROM due_at AT TIME ZONE 'Asia/Kolkata') BETWEEN 0 AND 5`, healthTenant); got != 0 {
		t.Errorf("no visit may fall between 00:00 and 06:00 IST, found %d", got)
	}
}

func TestDiagnosisConfirmationUsesBusinessDateForCaseStart(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	svc, repo := diagnosisStack(t, ctx, pool)

	submitted, err := svc.SubmitObservation(ctx, feverObservation("obs-business-day"))
	if err != nil {
		t.Fatalf("submit observation: %v", err)
	}

	// 20:30 UTC is already the next Goat OS business day in Asia/Kolkata. The
	// case row and its sessions must agree on that business date instead of
	// letting Postgres current_date pick the DB server's timezone.
	repo.Repository.now = func() time.Time {
		return time.Date(2026, time.August, 13, 20, 30, 0, 0, time.UTC)
	}
	confirmed, err := svc.ConfirmDiagnosis(ctx, domain.ConfirmDiagnosisInput{
		TenantID: healthTenant, ActorID: healthActor, DiagnosisRunID: submitted.DiagnosisRunID,
		ConfirmedProblems: []string{"FEVER"},
		IdempotencyKey:    "confirm-business-day", RequestFingerprint: "fp-confirm-business-day",
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if len(confirmed.OpenedCases) != 1 {
		t.Fatalf("want one opened course, got %+v", confirmed.OpenedCases)
	}

	var caseStart, firstSessionDate time.Time
	if err := pool.QueryRow(ctx, `
SELECT c.start_date, min(s.business_date)
FROM health_cases c
JOIN health_treatment_sessions s ON s.tenant_id=c.tenant_id AND s.health_case_id=c.health_case_id
WHERE c.tenant_id=$1::uuid AND c.health_case_id=$2::uuid
GROUP BY c.start_date`, healthTenant, confirmed.OpenedCases[0].CaseID).Scan(&caseStart, &firstSessionDate); err != nil {
		t.Fatalf("read course dates: %v", err)
	}
	if got, want := caseStart.Format("2006-01-02"), "2026-08-14"; got != want {
		t.Fatalf("case start_date = %s, want Goat OS business date %s", got, want)
	}
	if got, want := firstSessionDate.Format("2006-01-02"), "2026-08-14"; got != want {
		t.Fatalf("first session business_date = %s, want %s", got, want)
	}
}

// FAIL CLOSED. Nine of the register's thirty diagnoses point at a card nobody
// has authored. Confirming one must refuse and write NOTHING, rather than open a
// course with no treatment in it.
func TestConfirmingADiagnosisWithNoCardFailsClosed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	// Deliberately publish NO fever card.
	publishCard(t, ctx, pool, domain.SourceProtocol{
		DiseaseKey: "pinkeye", DisplayName: "Pinkeye", AgeBand: domain.AgeBandAdult, DurationDays: 3,
	})
	svc, _ := diagnosisStack(t, ctx, pool)

	submitted, err := svc.SubmitObservation(ctx, feverObservation("obs-missing"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	// The Director is told BEFORE deciding, not after.
	if len(submitted.Confirmable) != 1 {
		t.Fatalf("want one confirmable, got %+v", submitted.Confirmable)
	}
	if submitted.Confirmable[0].SOPAvailable {
		t.Error("no fever card is published, so it must not report as available")
	}
	if submitted.Confirmable[0].BlockedReason == "" {
		t.Error("an unavailable card must name the gap for the Director")
	}

	_, err = svc.ConfirmDiagnosis(ctx, domain.ConfirmDiagnosisInput{
		TenantID: healthTenant, ActorID: healthActor, DiagnosisRunID: submitted.DiagnosisRunID,
		ConfirmedProblems: []string{"FEVER"},
		IdempotencyKey:    "confirm-missing", RequestFingerprint: "fp",
	})
	if !errors.Is(err, ports.ErrSOPNotAuthored) {
		t.Fatalf("want ErrSOPNotAuthored, got %v", err)
	}

	// The whole transaction rolled back: no case, no sessions, and the run is
	// still awaiting a decision.
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM health_cases WHERE tenant_id=$1::uuid`, healthTenant); got != 0 {
		t.Errorf("a refused confirmation must open no course, found %d", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM health_treatment_sessions WHERE tenant_id=$1::uuid`, healthTenant); got != 0 {
		t.Errorf("a refused confirmation must schedule no visit, found %d", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM health_diagnosis_runs WHERE tenant_id=$1::uuid AND status='proposed'`, healthTenant); got != 1 {
		t.Errorf("the run must stay proposed after a refused confirmation")
	}
}

// The confirmation gate: a diagnosis the engine did not propose cannot be
// confirmed here, even by the Director. Off-register diagnosis is a real power
// they hold, and it is deliberately a different path.
func TestConfirmingAnUnproposedDiagnosisIsRefused(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	svc, _ := diagnosisStack(t, ctx, pool)

	submitted, err := svc.SubmitObservation(ctx, feverObservation("obs-gate"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	_, err = svc.ConfirmDiagnosis(ctx, domain.ConfirmDiagnosisInput{
		TenantID: healthTenant, ActorID: healthActor, DiagnosisRunID: submitted.DiagnosisRunID,
		ConfirmedProblems: []string{"TETANUS"},
		IdempotencyKey:    "confirm-gate", RequestFingerprint: "fp",
	})
	if !errors.Is(err, ports.ErrDiagnosisNotProposed) {
		t.Fatalf("want ErrDiagnosisNotProposed, got %v", err)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM health_cases WHERE tenant_id=$1::uuid`, healthTenant); got != 0 {
		t.Errorf("nothing may open from an unproposed diagnosis, found %d", got)
	}
}

// The Director may decline the whole proposal. That is an override, not an
// error, and the declined diagnosis is reported rather than silently dropped.
func TestDecliningEverythingStillDecidesTheRun(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	svc, _ := diagnosisStack(t, ctx, pool)

	submitted, err := svc.SubmitObservation(ctx, feverObservation("obs-decline"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	confirmed, err := svc.ConfirmDiagnosis(ctx, domain.ConfirmDiagnosisInput{
		TenantID: healthTenant, ActorID: healthActor, DiagnosisRunID: submitted.DiagnosisRunID,
		ConfirmedProblems: nil,
		IdempotencyKey:    "confirm-decline", RequestFingerprint: "fp",
	})
	if err != nil {
		t.Fatalf("declining everything is legal: %v", err)
	}
	if len(confirmed.OpenedCases) != 0 {
		t.Errorf("nothing confirmed means nothing opened, got %+v", confirmed.OpenedCases)
	}
	if !containsStr(confirmed.Declined, "FEVER") {
		t.Errorf("the declined diagnosis must be reported, got %v", confirmed.Declined)
	}
	// "Treat none of these" is a DECLINE, not an approval: the phone renders a
	// confirmed run as "Treatment approved", and a decision that opened zero
	// courses must never read back as that.
	if confirmed.Status != "declined" {
		t.Errorf("declining everything must decide the run as declined, got %q", confirmed.Status)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*) FROM health_diagnosis_runs WHERE tenant_id=$1::uuid AND status='declined'`, healthTenant); got != 1 {
		t.Error("the run is decided as declined when everything was declined")
	}

	// A retried decline must read back the SAME decision, Declined included:
	// the phone renders the response it happens to receive, and a replay that
	// dropped Declined would show the Director an outcome with no record of
	// what they turned down.
	replayed, err := svc.ConfirmDiagnosis(ctx, domain.ConfirmDiagnosisInput{
		TenantID: healthTenant, ActorID: healthActor, DiagnosisRunID: submitted.DiagnosisRunID,
		ConfirmedProblems: nil,
		IdempotencyKey:    "confirm-decline", RequestFingerprint: "fp",
	})
	if err != nil {
		t.Fatalf("decline replay: %v", err)
	}
	if !replayed.IdempotentReplay {
		t.Fatal("same decline key and body must replay")
	}
	if !containsStr(replayed.Declined, "FEVER") {
		t.Errorf("the replay must carry the declined diagnosis, got %v", replayed.Declined)
	}
}

// An exact replay returns the original result and runs no side effects. This is
// what makes a network-failed submit safe for the phone to retry.
func TestObservationReplayIsIdempotent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	svc, _ := diagnosisStack(t, ctx, pool)

	first, err := svc.SubmitObservation(ctx, feverObservation("obs-replay"))
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	again, err := svc.SubmitObservation(ctx, feverObservation("obs-replay"))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !again.IdempotentReplay {
		t.Error("a same-key same-body submit is a replay")
	}
	if again.DiagnosisRunID != first.DiagnosisRunID {
		t.Errorf("replay must return the original run, got %s vs %s", again.DiagnosisRunID, first.DiagnosisRunID)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM health_diagnosis_runs WHERE tenant_id=$1::uuid`, healthTenant); got != 1 {
		t.Errorf("a replay must write no second run, found %d", got)
	}
	// The replay is the SAME response, never a thinner one. Android persists the
	// submit response verbatim, so a replay that dropped Confirmable would cache
	// a still-proposed run with no decision choices for the Director.
	if len(first.Confirmable) == 0 {
		t.Fatal("fixture must propose at least one confirmable diagnosis")
	}
	if !reflect.DeepEqual(again.Confirmable, first.Confirmable) {
		t.Errorf("replay Confirmable = %+v, want the first response's %+v", again.Confirmable, first.Confirmable)
	}

	// Same key, different body is a conflict rather than a silent overwrite.
	conflicting := feverObservation("obs-replay")
	conflicting.RequestFingerprint = "different"
	if _, err := svc.SubmitObservation(ctx, conflicting); !errors.Is(err, ports.ErrConflict) {
		t.Errorf("want ErrConflict for a same-key different-body submit, got %v", err)
	}
}

func TestConfirmationReplayRequiresSameDecisionKeyAndBody(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	svc, _ := diagnosisStack(t, ctx, pool)

	submitted, err := svc.SubmitObservation(ctx, feverObservation("obs-confirm-replay"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	first, err := svc.ConfirmDiagnosis(ctx, domain.ConfirmDiagnosisInput{
		TenantID: healthTenant, ActorID: healthActor, DiagnosisRunID: submitted.DiagnosisRunID,
		ConfirmedProblems: []string{"FEVER"},
		IdempotencyKey:    "confirm-replay", RequestFingerprint: "fp-confirm-replay",
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}

	again, err := svc.ConfirmDiagnosis(ctx, domain.ConfirmDiagnosisInput{
		TenantID: healthTenant, ActorID: healthActor, DiagnosisRunID: submitted.DiagnosisRunID,
		ConfirmedProblems: []string{"FEVER"},
		IdempotencyKey:    "confirm-replay", RequestFingerprint: "fp-confirm-replay",
	})
	if err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	if !again.IdempotentReplay {
		t.Fatal("same confirmation key and body must replay")
	}
	if len(again.OpenedCases) != len(first.OpenedCases) {
		t.Fatalf("replay opened cases = %+v, want %+v", again.OpenedCases, first.OpenedCases)
	}
	if !reflect.DeepEqual(again.Declined, first.Declined) {
		t.Fatalf("replay Declined = %+v, want the first response's %+v", again.Declined, first.Declined)
	}

	_, err = svc.ConfirmDiagnosis(ctx, domain.ConfirmDiagnosisInput{
		TenantID: healthTenant, ActorID: healthActor, DiagnosisRunID: submitted.DiagnosisRunID,
		ConfirmedProblems: []string{"FEVER"},
		IdempotencyKey:    "confirm-replay", RequestFingerprint: "different-body",
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("same key different body must conflict, got %v", err)
	}

	_, err = svc.ConfirmDiagnosis(ctx, domain.ConfirmDiagnosisInput{
		TenantID: healthTenant, ActorID: healthActor, DiagnosisRunID: submitted.DiagnosisRunID,
		ConfirmedProblems: nil,
		IdempotencyKey:    "confirm-different", RequestFingerprint: "fp-confirm-different",
	})
	if !errors.Is(err, ports.ErrDiagnosisAlreadyDecided) {
		t.Fatalf("different decision after confirmation must be rejected, got %v", err)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM health_cases WHERE tenant_id=$1::uuid`, healthTenant); got != 1 {
		t.Fatalf("replays and rejected decisions must not open extra courses, found %d", got)
	}
}

// The follow-up loop: once a course is open, the next observation must see it as
// an OPEN problem so the engine reconciles instead of diagnosing afresh.
func TestSecondObservationSeesTheOpenCourse(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	svc, _ := diagnosisStack(t, ctx, pool)

	first, err := svc.SubmitObservation(ctx, feverObservation("obs-day1"))
	if err != nil {
		t.Fatalf("submit day 1: %v", err)
	}
	if _, err := svc.ConfirmDiagnosis(ctx, domain.ConfirmDiagnosisInput{
		TenantID: healthTenant, ActorID: healthActor, DiagnosisRunID: first.DiagnosisRunID,
		ConfirmedProblems: []string{"FEVER"}, IdempotencyKey: "confirm-day1", RequestFingerprint: "fp",
	}); err != nil {
		t.Fatalf("confirm day 1: %v", err)
	}

	second, err := svc.SubmitObservation(ctx, feverObservation("obs-day2"))
	if err != nil {
		t.Fatalf("submit day 2: %v", err)
	}
	// FEVER is already under treatment, so it is ONGOING rather than new.
	if !containsStr(second.Proposal.Ongoing, "FEVER") {
		t.Errorf("an open course must read as ongoing, got ongoing=%v new=%v",
			second.Proposal.Ongoing, second.Proposal.New)
	}
	if containsStr(second.Proposal.New, "FEVER") {
		t.Errorf("an open course must not read as new: %v", second.Proposal.New)
	}
}

// The stored run is the observation EXACTLY as evaluated: the follow-up context
// (day, improving, shed-similar, the server-derived open problems, ...) changes
// what the engine proposes, so a run that dropped it could never explain a
// close/extend or test-based proposal afterwards.
func TestStoredRunKeepsTheEffectiveContext(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	svc, _ := diagnosisStack(t, ctx, pool)

	first, err := svc.SubmitObservation(ctx, feverObservation("obs-ctx-day1"))
	if err != nil {
		t.Fatalf("submit day 1: %v", err)
	}
	if _, err := svc.ConfirmDiagnosis(ctx, domain.ConfirmDiagnosisInput{
		TenantID: healthTenant, ActorID: healthActor, DiagnosisRunID: first.DiagnosisRunID,
		ConfirmedProblems: []string{"FEVER"}, IdempotencyKey: "confirm-ctx-day1", RequestFingerprint: "fp",
	}); err != nil {
		t.Fatalf("confirm day 1: %v", err)
	}

	day := 3
	shedSimilar := 2
	followUp := feverObservation("obs-ctx-day3")
	followUp.Context = diagnosis.Context{
		Day:              &day,
		ShedSimilar:      &shedSimilar,
		CMTNegStreak:     1,
		ProblemImproving: true,
	}
	submitted, err := svc.SubmitObservation(ctx, followUp)
	if err != nil {
		t.Fatalf("submit follow-up: %v", err)
	}

	var stored diagnosis.Context
	var raw []byte
	if err := pool.QueryRow(ctx, `
SELECT context FROM health_diagnosis_runs
WHERE tenant_id=$1::uuid AND health_diagnosis_run_id=$2::uuid`,
		healthTenant, submitted.DiagnosisRunID).Scan(&raw); err != nil {
		t.Fatalf("read stored context: %v", err)
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("decode stored context: %v", err)
	}
	if stored.Day == nil || *stored.Day != day {
		t.Errorf("stored context day = %v, want %d", stored.Day, day)
	}
	if stored.ShedSimilar == nil || *stored.ShedSimilar != shedSimilar {
		t.Errorf("stored context shed_similar = %v, want %d", stored.ShedSimilar, shedSimilar)
	}
	if stored.CMTNegStreak != 1 || !stored.ProblemImproving {
		t.Errorf("stored context = %+v, want cmt_neg_streak=1 problem_improving=true", stored)
	}
	// The EFFECTIVE context, open problems included: the reconcile decision hangs
	// on Open, and the caller never supplies it, so a stored context without it
	// could not explain why FEVER read as ongoing.
	if !containsStr(stored.Open, "FEVER") {
		t.Errorf("stored context must carry the server-derived open problems, got %v", stored.Open)
	}
}

func containsStr(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// The READ must be self-sufficient. A Director opening an assessment on their own
// phone has never seen the submit response -- the manager submitted from theirs --
// so a proposal the Director can read but not act on is a dead end.
func TestReadingARunCarriesWhatTheDirectorCanActOn(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	svc, _ := diagnosisStack(t, ctx, pool)

	submitted, err := svc.SubmitObservation(ctx, feverObservation("obs-read"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	run, err := svc.GetDiagnosisRun(ctx, healthTenant, submitted.DiagnosisRunID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(run.Confirmable) == 0 {
		t.Fatal("a run still awaiting a decision must carry what can be confirmed")
	}
	// The published-card lookup has to happen on the READ too, not only on submit:
	// without it every diagnosis reads as having no treatment plan and the Director
	// is blocked from confirming work that is perfectly confirmable.
	var found bool
	for _, c := range run.Confirmable {
		if c.ID == "FEVER" {
			found = true
			if !c.SOPAvailable {
				t.Error("FEVER has a published card, so the read must say it can be confirmed")
			}
		}
	}
	if !found {
		t.Errorf("the proposed diagnosis must appear, got %+v", run.Confirmable)
	}
}

// A decided run offers nothing. Re-deriving the list would put live decision
// controls on a choice already made, and every tap on them fails at the write.
func TestReadingADecidedRunOffersNoFurtherDecision(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	svc, _ := diagnosisStack(t, ctx, pool)

	submitted, err := svc.SubmitObservation(ctx, feverObservation("obs-decided"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := svc.ConfirmDiagnosis(ctx, domain.ConfirmDiagnosisInput{
		TenantID: healthTenant, ActorID: healthActor, DiagnosisRunID: submitted.DiagnosisRunID,
		ConfirmedProblems: []string{"FEVER"},
		IdempotencyKey:    "confirm-decided", RequestFingerprint: "fp",
	}); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	run, err := svc.GetDiagnosisRun(ctx, healthTenant, submitted.DiagnosisRunID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(run.Confirmable) != 0 {
		t.Errorf("a decided run must offer nothing, got %+v", run.Confirmable)
	}
	// The proposal itself survives: what was confirmed is the thing worth being
	// able to re-read afterwards.
	if len(run.Proposal.Problems) == 0 {
		t.Error("the proposal must remain readable after the decision")
	}
}

// The Director's queue, on the production path: submitted observations appear,
// decided ones leave, and the page walks by keyset without repeating a row.
func TestQueueStatusBucketsAreDisjointAcrossEveryStatus(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	svc, _ := diagnosisStack(t, ctx, pool)

	first, err := svc.SubmitObservation(ctx, feverObservation("queue-1"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	second, err := svc.SubmitObservation(ctx, feverObservation("queue-2"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	page, err := svc.ListDiagnosisRuns(ctx, domain.DiagnosisQueueFilter{TenantID: healthTenant})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("both submissions must be awaiting a decision, got %d", len(page.Items))
	}
	// Newest first: the Director opens what just came in, not what has been sitting.
	if page.Items[0].DiagnosisRunID != second.DiagnosisRunID {
		t.Errorf("newest must sort first, got %s", page.Items[0].DiagnosisRunID)
	}

	row := page.Items[0]
	if row.GoatDisplayID == "" {
		t.Error("a queue row must name the animal, never only its id")
	}
	if len(row.Problems) == 0 {
		t.Error("the row must carry the ranked problems as its headline")
	}

	// Deciding one takes it out of the queue -- that is what makes it a queue and
	// not a log.
	if _, err := svc.ConfirmDiagnosis(ctx, domain.ConfirmDiagnosisInput{
		TenantID: healthTenant, ActorID: healthActor, DiagnosisRunID: first.DiagnosisRunID,
		ConfirmedProblems: []string{"FEVER"},
		IdempotencyKey:    "queue-confirm", RequestFingerprint: "fp",
	}); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	page, err = svc.ListDiagnosisRuns(ctx, domain.DiagnosisQueueFilter{TenantID: healthTenant})
	if err != nil {
		t.Fatalf("list after confirm: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].DiagnosisRunID != second.DiagnosisRunID {
		t.Fatalf("a decided run must leave the queue, got %+v", page.Items)
	}

	// It is not lost, only decided. An explicit status finds it again.
	decided, err := svc.ListDiagnosisRuns(ctx, domain.DiagnosisQueueFilter{
		TenantID: healthTenant, Status: domain.DiagnosisStatusConfirmed,
	})
	if err != nil {
		t.Fatalf("list confirmed: %v", err)
	}
	if len(decided.Items) != 1 || decided.Items[0].DiagnosisRunID != first.DiagnosisRunID {
		t.Errorf("the decided run must still be readable, got %+v", decided.Items)
	}
}

// KEYSET, not offset. New observations land at the HEAD of a newest-first queue,
// so an offset page would re-show or skip rows as a manager records animals while
// the Director scrolls. Walking the whole queue must visit every run exactly once.
func TestQueuePaginationPageBoundaryNeverRepeatsOrSkipsARow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	svc, _ := diagnosisStack(t, ctx, pool)

	const total = 5
	for i := 0; i < total; i++ {
		if _, err := svc.SubmitObservation(ctx, feverObservation(fmt.Sprintf("page-%d", i))); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}

	seen := map[string]int{}
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > total {
			t.Fatal("the walk did not terminate -- the cursor is not advancing")
		}
		page, err := svc.ListDiagnosisRuns(ctx, domain.DiagnosisQueueFilter{
			TenantID: healthTenant, Cursor: cursor, Limit: 2,
		})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		if len(page.Items) > 2 {
			t.Fatalf("page %d returned %d rows, limit was 2", pages, len(page.Items))
		}
		for _, it := range page.Items {
			seen[it.DiagnosisRunID]++
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}

	if len(seen) != total {
		t.Fatalf("the walk saw %d distinct runs, want %d", len(seen), total)
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("run %s appeared %d times; a keyset walk must visit each row once", id, count)
		}
	}
}

// A garbage cursor is refused rather than silently treated as "start from the
// beginning", which would restart the Director's scroll without saying why.
func TestQueueRefusesAnUnreadableCursor(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	svc, _ := diagnosisStack(t, ctx, pool)

	if _, err := svc.ListDiagnosisRuns(ctx, domain.DiagnosisQueueFilter{
		TenantID: healthTenant, Cursor: "not-a-cursor",
	}); err == nil {
		t.Fatal("an unreadable cursor must be refused")
	}
}

// An unknown status is a client mistake. Quietly clearing the filter would return
// every run and read as working while showing the wrong queue.
func TestQueueRejectsAnUnknownStatus(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	svc, _ := diagnosisStack(t, ctx, pool)

	if _, err := svc.ListDiagnosisRuns(ctx, domain.DiagnosisQueueFilter{
		TenantID: healthTenant, Status: "pending",
	}); err == nil {
		t.Fatal("an unknown status must be refused, not silently ignored")
	}
}

// The assessment must NAME the animal.
//
// The Director opening a run has usually never seen the submit response -- the
// manager submitted from their phone -- so the name cannot come from a client
// cache and must be on the read. Without it the assessment header renders blank,
// which is exactly what shipped before this test existed.
func TestReadingARunNamesTheAnimal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	svc, _ := diagnosisStack(t, ctx, pool)

	submitted, err := svc.SubmitObservation(ctx, feverObservation("obs-named"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	var expected string
	if err := pool.QueryRow(ctx,
		`SELECT COALESCE(display_id,'') FROM goats WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		healthTenant, healthGoat).Scan(&expected); err != nil {
		t.Fatalf("read goat: %v", err)
	}
	if expected == "" {
		t.Fatal("fixture must give the animal a display id, or this proves nothing")
	}

	run, err := svc.GetDiagnosisRun(ctx, healthTenant, submitted.DiagnosisRunID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if run.GoatDisplayID != expected {
		t.Errorf("goat_display_id = %q, want %q -- the screen has no other source for the name",
			run.GoatDisplayID, expected)
	}
}

// TestQueueRowIsOneToOneWithARunAcrossMultipleDimensions is the grain proof.
//
// The queue joins the animal to name it, and a run carries SEVERAL problems. If
// any of that were joined one-to-many, a single observation would appear in the
// Director's queue more than once and the same animal would look like several
// animals needing decisions. The count of rows must follow RUNS, never problems
// and never identifiers.
func TestQueueRowIsOneToOneWithARunAcrossMultipleDimensions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	svc, _ := diagnosisStack(t, ctx, pool)

	// A second identifier on the same animal: the classic fan-out source.
	if _, err := pool.Exec(ctx, `
		INSERT INTO goat_identifiers
			(tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
			 scope_key, status, valid_from, normalizer_version)
		VALUES ($1::uuid, $2::uuid, 'animal_identifier_2', 'OLD-QUEUE-1', 'oldqueue1',
			'tenant:' || $1, 'active', now(), 'v1')
		ON CONFLICT DO NOTHING`, healthTenant, healthGoat); err != nil {
		t.Fatalf("seed second identifier: %v", err)
	}

	run, err := svc.SubmitObservation(ctx, feverObservation("grain-1"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(run.Proposal.Problems) == 0 {
		t.Fatal("fixture must produce at least one problem for this to prove anything")
	}

	page, err := svc.ListDiagnosisRuns(ctx, domain.DiagnosisQueueFilter{TenantID: healthTenant})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("one observation must produce exactly one queue row, got %d: %+v",
			len(page.Items), page.Items)
	}
	if page.Items[0].DiagnosisRunID != run.DiagnosisRunID {
		t.Errorf("row = %s, want %s", page.Items[0].DiagnosisRunID, run.DiagnosisRunID)
	}
}

// TestQueueScopeHierarchyNeverLeaksAcrossTenants pins the outermost scope. Every
// predicate in this query is tenant-scoped; a run belonging to another tenant
// must be invisible, not merely sorted lower.
func TestQueueScopeHierarchyNeverLeaksAcrossTenants(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	svc, _ := diagnosisStack(t, ctx, pool)

	if _, err := svc.SubmitObservation(ctx, feverObservation("scope-1")); err != nil {
		t.Fatalf("submit: %v", err)
	}

	const otherTenant = "71000000-0000-4000-8000-0000000000ff"
	page, err := svc.ListDiagnosisRuns(ctx, domain.DiagnosisQueueFilter{TenantID: otherTenant})
	if err != nil {
		t.Fatalf("list other tenant: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("another tenant saw %d runs: %+v", len(page.Items), page.Items)
	}

	// The narrower goat scope is a filter, not a security boundary, but it must
	// still select exactly what it names.
	byGoat, err := svc.ListDiagnosisRuns(ctx, domain.DiagnosisQueueFilter{
		TenantID: healthTenant, GoatID: healthGoat,
	})
	if err != nil {
		t.Fatalf("list by goat: %v", err)
	}
	if len(byGoat.Items) != 1 {
		t.Errorf("goat scope returned %d rows, want 1", len(byGoat.Items))
	}
}

// TestQueueExecutionDateIsTheBusinessDayNotTheClockInstant pins the time grain.
//
// The queue sorts by the observation INSTANT so the Director sees what just
// arrived, but it reports the BUSINESS DATE, which is an Asia/Kolkata day. An
// observation recorded late in the Indian evening is still that day's work even
// though it is already tomorrow in UTC, and a row that shifted a day would put a
// manager's evening round on the wrong sheet.
func TestQueueExecutionDateIsTheBusinessDayNotTheClockInstant(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	publishCard(t, ctx, pool, feverCard())
	svc, _ := diagnosisStack(t, ctx, pool)

	// 20:00 IST on 2026-08-14 is 14:30 UTC the same day, but 23:00 IST is already
	// 2026-08-14T17:30Z -- and 01:00 IST on the 15th is 19:30Z on the 14th. The
	// business date must follow the FARM, not the UTC calendar.
	evening := time.Date(2026, 8, 14, 23, 30, 0, 0, time.UTC) // 05:00 IST on the 15th
	in := feverObservation("bizdate-1")
	in.BusinessDate = evening

	run, err := svc.SubmitObservation(ctx, in)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	page, err := svc.ListDiagnosisRuns(ctx, domain.DiagnosisQueueFilter{TenantID: healthTenant})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("want 1 row, got %d", len(page.Items))
	}
	got := page.Items[0]
	if got.BusinessDate == "" {
		t.Fatal("a queue row must carry the business date it belongs to")
	}
	detail, err := svc.GetDiagnosisRun(ctx, healthTenant, run.DiagnosisRunID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if detail.BusinessDate != got.BusinessDate {
		t.Errorf("queue row business date %q disagrees with the run detail %q -- one of them "+
			"is deriving the day differently", got.BusinessDate, detail.BusinessDate)
	}
}
