package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// ONE VIDEO PER TREATMENT STEP, on the production path.
//
// The whole point is that a session with several steps cannot be submitted on one clip, that each
// clip records WHO filmed it (different people do different steps), and that the set the verifier
// receives arrives in step order with each video named by the step it proves.
// A SECOND person, because different people do different steps of one session.
const healthSecondActor = "71000000-0000-4000-8000-00000000000b"

func TestASessionIsSubmittedOnlyWhenEveryStepHasItsVideo(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)

	tylosin, meloxicam := "Tylosin", "Meloxicam"
	dose := "1 ml"
	check := "Check temperature"
	repo := NewRepository(pool, 10*time.Second)
	if err := repo.ReplacePublishedProtocols(ctx, healthTenant, healthActor, "health-test", "hash-steps",
		[]domain.SourceProtocol{{
			DiseaseKey: "fever", DisplayName: "Fever", AgeBand: domain.AgeBandAdult, DurationDays: 1,
			Steps: []domain.ProtocolStep{
				{DayNo: 1, Session: domain.SessionMorning, Seq: 1, RecordType: "action", Instruction: &check},
				{DayNo: 1, Session: domain.SessionMorning, Seq: 2, RecordType: "medication", MedicineName: &tylosin, DosageText: &dose},
				{DayNo: 1, Session: domain.SessionMorning, Seq: 3, RecordType: "medication", MedicineName: &meloxicam, DosageText: &dose},
			},
		}}); err != nil {
		t.Fatalf("publish protocol: %v", err)
	}

	loc, _ := time.LoadLocation("Asia/Kolkata")
	opened, err := repo.OpenCase(ctx, domain.OpenCaseInput{
		TenantID: healthTenant, ActorID: healthActor, GoatID: healthGoat,
		DiseaseKey: "fever", AgeBand: domain.AgeBandAdult, StartDate: time.Now().In(loc),
		IdempotencyKey: "open-steps", RequestFingerprint: "open-steps",
	})
	if err != nil {
		t.Fatalf("open case: %v", err)
	}
	detail, err := repo.GetWorkItem(ctx, healthTenant, opened.FirstSessionID)
	if err != nil || len(detail.Steps) != 3 {
		t.Fatalf("work item steps = %d err=%v, want 3", len(detail.Steps), err)
	}

	// The FIRST clip arms the gate. Two steps still owe one, so the submit is refused and the
	// refusal NAMES them -- an operator told "2 missing" would have to hunt for them.
	if _, err := repo.RecordStepProof(ctx, domain.RecordStepProofInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
		StepID: detail.Steps[0].StepID, ProofRef: "proof-step-1", IdempotencyKey: "sp-1",
	}); err != nil {
		t.Fatalf("record first step proof: %v", err)
	}
	_, err = repo.CompleteWorkItem(ctx, domain.CompleteInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
		IdempotencyKey: "submit-too-early", RequestFingerprint: "submit-too-early",
	})
	if !errors.Is(err, domain.ErrStepProofsIncomplete) {
		t.Fatalf("submit with two steps unfilmed = %v, want a refusal", err)
	}
	var missing domain.StepProofsMissingError
	if !errors.As(err, &missing) || len(missing.Missing) != 2 {
		t.Fatalf("refusal named %+v, want the two unfilmed steps", missing.Missing)
	}
	if got := missing.StepLabels(); got[0] != "Tylosin 1 ml" || got[1] != "Meloxicam 1 ml" {
		t.Fatalf("refusal labels = %v, want the two medicines in working order", got)
	}

	// DIFFERENT PEOPLE film different steps. A single proof_ref on the session could record only
	// one of them, which is why the proof is a row carrying its own captured_by.
	if _, err := repo.RecordStepProof(ctx, domain.RecordStepProofInput{
		TenantID: healthTenant, ActorID: healthSecondActor, SessionID: opened.FirstSessionID,
		StepID: detail.Steps[1].StepID, ProofRef: "proof-step-2", IdempotencyKey: "sp-2",
	}); err != nil {
		t.Fatalf("record second step proof: %v", err)
	}
	if _, err := repo.RecordStepProof(ctx, domain.RecordStepProofInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
		StepID: detail.Steps[2].StepID, ProofRef: "proof-step-3", IdempotencyKey: "sp-3",
	}); err != nil {
		t.Fatalf("record third step proof: %v", err)
	}

	proofs, err := repo.StepProofs(ctx, healthTenant, opened.FirstSessionID)
	if err != nil || len(proofs) != 3 {
		t.Fatalf("step proofs = %d err=%v, want 3", len(proofs), err)
	}
	if proofs[1].CapturedBy != healthSecondActor {
		t.Fatalf("second step was filmed by %q, want the second person who actually did it", proofs[1].CapturedBy)
	}

	completed, err := repo.CompleteWorkItem(ctx, domain.CompleteInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
		IdempotencyKey: "submit-complete", RequestFingerprint: "submit-complete",
	})
	if err != nil {
		t.Fatalf("submit with every step filmed: %v", err)
	}

	// The verifier opens ONE item holding the set, IN STEP ORDER, each clip named by the step it
	// proves. Twelve unnamed videos would not be reviewable.
	if len(completed.StepMedia) != 3 {
		t.Fatalf("step media = %+v, want one per step", completed.StepMedia)
	}
	wantLabels := []string{"Check temperature", "Tylosin 1 ml", "Meloxicam 1 ml"}
	for i, want := range wantLabels {
		if completed.StepMedia[i].Label != want {
			t.Fatalf("clip %d named %q, want %q", i, completed.StepMedia[i].Label, want)
		}
		if completed.StepMedia[i].ProofRef == "" {
			t.Fatalf("clip %d carries no proof ref", i)
		}
	}
}

// A re-shoot REPLACES that step's clip. Appending would hand the verifier a pile of attempts and
// leave her judging which one counted.
func TestReshootingAStepReplacesItsClip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)

	medicine, dose := "Tylosin", "1 ml"
	repo := NewRepository(pool, 10*time.Second)
	if err := repo.ReplacePublishedProtocols(ctx, healthTenant, healthActor, "health-test", "hash-reshoot",
		[]domain.SourceProtocol{{
			DiseaseKey: "fever", DisplayName: "Fever", AgeBand: domain.AgeBandAdult, DurationDays: 1,
			Steps: []domain.ProtocolStep{{DayNo: 1, Session: domain.SessionMorning, Seq: 1,
				RecordType: "medication", MedicineName: &medicine, DosageText: &dose}},
		}}); err != nil {
		t.Fatalf("publish protocol: %v", err)
	}
	loc, _ := time.LoadLocation("Asia/Kolkata")
	opened, err := repo.OpenCase(ctx, domain.OpenCaseInput{
		TenantID: healthTenant, ActorID: healthActor, GoatID: healthGoat,
		DiseaseKey: "fever", AgeBand: domain.AgeBandAdult, StartDate: time.Now().In(loc),
		IdempotencyKey: "open-reshoot", RequestFingerprint: "open-reshoot",
	})
	if err != nil {
		t.Fatalf("open case: %v", err)
	}
	detail, _ := repo.GetWorkItem(ctx, healthTenant, opened.FirstSessionID)
	step := detail.Steps[0].StepID

	for _, ref := range []string{"first-take", "second-take"} {
		if _, err := repo.RecordStepProof(ctx, domain.RecordStepProofInput{
			TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
			StepID: step, ProofRef: ref, IdempotencyKey: "sp-" + ref,
		}); err != nil {
			t.Fatalf("record %s: %v", ref, err)
		}
	}
	proofs, err := repo.StepProofs(ctx, healthTenant, opened.FirstSessionID)
	if err != nil || len(proofs) != 1 {
		t.Fatalf("after a re-shoot the step holds %d clips, want exactly 1", len(proofs))
	}
	if proofs[0].ProofRef != "second-take" {
		t.Fatalf("step kept %q, want the re-shoot", proofs[0].ProofRef)
	}
}

func TestStaleStepProofRetryDoesNotOverwriteAReshoot(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)

	medicine, dose := "Tylosin", "1 ml"
	repo := NewRepository(pool, 10*time.Second)
	if err := repo.ReplacePublishedProtocols(ctx, healthTenant, healthActor, "health-test", "hash-stale-retry",
		[]domain.SourceProtocol{{
			DiseaseKey: "fever", DisplayName: "Fever", AgeBand: domain.AgeBandAdult, DurationDays: 1,
			Steps: []domain.ProtocolStep{{DayNo: 1, Session: domain.SessionMorning, Seq: 1,
				RecordType: "medication", MedicineName: &medicine, DosageText: &dose}},
		}}); err != nil {
		t.Fatalf("publish protocol: %v", err)
	}
	loc, _ := time.LoadLocation("Asia/Kolkata")
	opened, err := repo.OpenCase(ctx, domain.OpenCaseInput{
		TenantID: healthTenant, ActorID: healthActor, GoatID: healthGoat,
		DiseaseKey: "fever", AgeBand: domain.AgeBandAdult, StartDate: time.Now().In(loc),
		IdempotencyKey: "open-stale-retry", RequestFingerprint: "open-stale-retry",
	})
	if err != nil {
		t.Fatalf("open case: %v", err)
	}
	detail, _ := repo.GetWorkItem(ctx, healthTenant, opened.FirstSessionID)
	step := detail.Steps[0].StepID

	if _, err := repo.RecordStepProof(ctx, domain.RecordStepProofInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
		StepID: step, ProofRef: "first-take", IdempotencyKey: "sp-first", RequestFingerprint: "fp-first",
	}); err != nil {
		t.Fatalf("record first take: %v", err)
	}
	if _, err := repo.RecordStepProof(ctx, domain.RecordStepProofInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
		StepID: step, ProofRef: "second-take", IdempotencyKey: "sp-second", RequestFingerprint: "fp-second",
	}); err != nil {
		t.Fatalf("record second take: %v", err)
	}
	replayed, err := repo.RecordStepProof(ctx, domain.RecordStepProofInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
		StepID: step, ProofRef: "first-take", IdempotencyKey: "sp-first", RequestFingerprint: "fp-first",
	})
	if err != nil {
		t.Fatalf("replay first take: %v", err)
	}
	if replayed.ProofRef != "first-take" {
		t.Fatalf("replay returned %q, want original first take", replayed.ProofRef)
	}
	proofs, err := repo.StepProofs(ctx, healthTenant, opened.FirstSessionID)
	if err != nil || len(proofs) != 1 || proofs[0].ProofRef != "second-take" {
		t.Fatalf("stored proof after stale replay = %+v, err=%v; want second-take unchanged", proofs, err)
	}

	_, err = repo.RecordStepProof(ctx, domain.RecordStepProofInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
		StepID: step, ProofRef: "tampered-take", IdempotencyKey: "sp-first", RequestFingerprint: "different-body",
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("same key different fingerprint = %v, want conflict", err)
	}
}

func TestSubmittedSessionRefusesStepProofMutation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)

	medicine, dose := "Tylosin", "1 ml"
	repo := NewRepository(pool, 10*time.Second)
	if err := repo.ReplacePublishedProtocols(ctx, healthTenant, healthActor, "health-test", "hash-closed-proof",
		[]domain.SourceProtocol{{
			DiseaseKey: "fever", DisplayName: "Fever", AgeBand: domain.AgeBandAdult, DurationDays: 1,
			Steps: []domain.ProtocolStep{{DayNo: 1, Session: domain.SessionMorning, Seq: 1,
				RecordType: "medication", MedicineName: &medicine, DosageText: &dose}},
		}}); err != nil {
		t.Fatalf("publish protocol: %v", err)
	}
	loc, _ := time.LoadLocation("Asia/Kolkata")
	today := time.Now().In(loc)
	repo.now = func() time.Time {
		return time.Date(today.Year(), today.Month(), today.Day(), 9, 0, 0, 0, loc)
	}
	opened, err := repo.OpenCase(ctx, domain.OpenCaseInput{
		TenantID: healthTenant, ActorID: healthActor, GoatID: healthGoat,
		DiseaseKey: "fever", AgeBand: domain.AgeBandAdult, StartDate: today,
		IdempotencyKey: "open-closed-proof", RequestFingerprint: "open-closed-proof",
	})
	if err != nil {
		t.Fatalf("open case: %v", err)
	}
	detail, _ := repo.GetWorkItem(ctx, healthTenant, opened.FirstSessionID)
	step := detail.Steps[0].StepID
	if _, err := repo.RecordStepProof(ctx, domain.RecordStepProofInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
		StepID: step, ProofRef: "submitted-take", IdempotencyKey: "sp-submitted",
	}); err != nil {
		t.Fatalf("record step proof: %v", err)
	}
	if _, err := repo.CompleteWorkItem(ctx, domain.CompleteInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
		IdempotencyKey: "close-closed-proof", RequestFingerprint: "close-closed-proof",
	}); err != nil {
		t.Fatalf("complete session: %v", err)
	}

	_, err = repo.RecordStepProof(ctx, domain.RecordStepProofInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
		StepID: step, ProofRef: "late-reshoot", IdempotencyKey: "sp-late",
	})
	if !errors.Is(err, domain.ErrStepProofClosed) {
		t.Fatalf("late reshoot error = %v, want ErrStepProofClosed", err)
	}
	proofs, err := repo.StepProofs(ctx, healthTenant, opened.FirstSessionID)
	if err != nil || len(proofs) != 1 || proofs[0].ProofRef != "submitted-take" {
		t.Fatalf("stored proof after late reshoot = %+v, err=%v; want submitted-take unchanged", proofs, err)
	}
}

// A step from ANOTHER session is refused, not ignored. A caller that could aim at one would file
// one animal's treatment evidence onto another animal's card.
func TestAStepFromAnotherSessionIsRefused(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)

	medicine, dose := "Tylosin", "1 ml"
	repo := NewRepository(pool, 10*time.Second)
	if err := repo.ReplacePublishedProtocols(ctx, healthTenant, healthActor, "health-test", "hash-cross",
		[]domain.SourceProtocol{{
			DiseaseKey: "fever", DisplayName: "Fever", AgeBand: domain.AgeBandAdult, DurationDays: 2,
			Steps: []domain.ProtocolStep{
				{DayNo: 1, Session: domain.SessionMorning, Seq: 1, RecordType: "medication", MedicineName: &medicine, DosageText: &dose},
				{DayNo: 2, Session: domain.SessionMorning, Seq: 2, RecordType: "medication", MedicineName: &medicine, DosageText: &dose},
			},
		}}); err != nil {
		t.Fatalf("publish protocol: %v", err)
	}
	loc, _ := time.LoadLocation("Asia/Kolkata")
	opened, err := repo.OpenCase(ctx, domain.OpenCaseInput{
		TenantID: healthTenant, ActorID: healthActor, GoatID: healthGoat,
		DiseaseKey: "fever", AgeBand: domain.AgeBandAdult, StartDate: time.Now().In(loc),
		IdempotencyKey: "open-cross", RequestFingerprint: "open-cross",
	})
	if err != nil {
		t.Fatalf("open case: %v", err)
	}
	var otherStep string
	if err := pool.QueryRow(ctx,
		`SELECT health_session_step_id::text FROM health_session_steps
		 WHERE tenant_id = $1::uuid AND health_session_id <> $2::uuid LIMIT 1`,
		healthTenant, opened.FirstSessionID).Scan(&otherStep); err != nil {
		t.Fatalf("find a step of the other day: %v", err)
	}
	_, err = repo.RecordStepProof(ctx, domain.RecordStepProofInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
		StepID: otherStep, ProofRef: "wrong-card", IdempotencyKey: "sp-cross",
	})
	if !errors.Is(err, domain.ErrStepNotInSession) {
		t.Fatalf("attaching another session's step = %v, want a refusal", err)
	}
}

// THE CARD EARNS ITS OWN VISITS, on the production path (maintainer decision 2026-09-23).
//
// A ward animal earns one housing visit a day. A card that also authors an afternoon and an
// evening dose earns those visits too, and each one carries only its own work -- which is the
// whole point: an evening dose handed to an operator at 07:00 is not an evening dose.
func TestACardAuthoringThreeSessionsEarnsThreeVisits(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)

	morning, afternoon, evening := "Morning dose", "Afternoon dose", "Evening dose"
	anytime := "Give with feed"
	repo := NewRepository(pool, 10*time.Second)
	if err := repo.ReplacePublishedProtocols(ctx, healthTenant, healthActor, "health-test", "hash-visits",
		[]domain.SourceProtocol{{
			DiseaseKey: "fever", DisplayName: "Fever", AgeBand: domain.AgeBandAdult, DurationDays: 1,
			Steps: []domain.ProtocolStep{
				{DayNo: 1, Session: domain.SessionMorning, Seq: 1, RecordType: "action", Instruction: &morning},
				{DayNo: 1, Session: domain.SessionAfternoon, Seq: 2, RecordType: "action", Instruction: &afternoon},
				{DayNo: 1, Session: domain.SessionEvening, Seq: 3, RecordType: "action", Instruction: &evening},
				{DayNo: 1, Session: "unscheduled", Seq: 4, RecordType: "action", Instruction: &anytime},
			},
		}}); err != nil {
		t.Fatalf("publish protocol: %v", err)
	}

	loc, _ := time.LoadLocation("Asia/Kolkata")
	opened, err := repo.OpenCase(ctx, domain.OpenCaseInput{
		TenantID: healthTenant, ActorID: healthActor, GoatID: healthGoat,
		DiseaseKey: "fever", AgeBand: domain.AgeBandAdult, StartDate: time.Now().In(loc),
		IdempotencyKey: "open-visits", RequestFingerprint: "open-visits",
	})
	if err != nil {
		t.Fatalf("open case: %v", err)
	}

	rows, err := pool.Query(ctx,
		`SELECT ts.session, count(st.*)
		   FROM health_treatment_sessions ts
		   LEFT JOIN health_session_steps st ON st.health_session_id = ts.health_session_id
		  WHERE ts.tenant_id = $1::uuid AND ts.health_case_id = $2::uuid AND ts.day_no = 1
		  GROUP BY ts.session`, healthTenant, opened.CaseID)
	if err != nil {
		t.Fatalf("read day 1 visits: %v", err)
	}
	defer rows.Close()
	steps := map[string]int{}
	for rows.Next() {
		var session string
		var n int
		if err := rows.Scan(&session, &n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		steps[session] = n
	}
	if len(steps) != 3 {
		t.Fatalf("day 1 produced %v, want a morning, an afternoon and an evening visit", steps)
	}
	// Morning carries its own dose AND the one with no hour on it; the other two carry only
	// their own. Piling all four onto the morning is exactly what this replaced.
	if steps[domain.SessionMorning] != 2 {
		t.Errorf("morning holds %d steps, want its dose plus the unscheduled one", steps[domain.SessionMorning])
	}
	if steps[domain.SessionAfternoon] != 1 || steps[domain.SessionEvening] != 1 {
		t.Errorf("afternoon/evening hold %d/%d, want exactly their own dose",
			steps[domain.SessionAfternoon], steps[domain.SessionEvening])
	}
}

// A VISIT CANNOT BE DONE BEFORE ITS TIME (maintainer decision 2026-09-23).
//
// Now that a card earns a morning, an afternoon and an evening visit, all three are visible from
// first light. Nothing stopped an operator closing the evening one at 07:00 -- a dose recorded as
// given hours before anyone gives it, with a video proving only that the animal was filmed in the
// morning.
func TestAnEveningVisitCannotBeClosedInTheMorning(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)

	morningWork, eveningWork := "Morning check", "Evening check"
	repo := NewRepository(pool, 10*time.Second)
	if err := repo.ReplacePublishedProtocols(ctx, healthTenant, healthActor, "health-test", "hash-notdue",
		[]domain.SourceProtocol{{
			DiseaseKey: "fever", DisplayName: "Fever", AgeBand: domain.AgeBandAdult, DurationDays: 1,
			Steps: []domain.ProtocolStep{
				{DayNo: 1, Session: domain.SessionMorning, Seq: 1, RecordType: "action", Instruction: &morningWork},
				{DayNo: 1, Session: domain.SessionEvening, Seq: 2, RecordType: "action", Instruction: &eveningWork},
			},
		}}); err != nil {
		t.Fatalf("publish protocol: %v", err)
	}

	loc, _ := time.LoadLocation("Asia/Kolkata")
	today := time.Now().In(loc)
	opened, err := repo.OpenCase(ctx, domain.OpenCaseInput{
		TenantID: healthTenant, ActorID: healthActor, GoatID: healthGoat,
		DiseaseKey: "fever", AgeBand: domain.AgeBandAdult, StartDate: today,
		IdempotencyKey: "open-notdue", RequestFingerprint: "open-notdue",
	})
	if err != nil {
		t.Fatalf("open case: %v", err)
	}

	var eveningID string
	if err := pool.QueryRow(ctx,
		`SELECT health_session_id::text FROM health_treatment_sessions
		  WHERE tenant_id = $1::uuid AND health_case_id = $2::uuid AND session = 'evening' AND day_no = 1`,
		healthTenant, opened.CaseID).Scan(&eveningID); err != nil {
		t.Fatalf("find the evening visit: %v", err)
	}

	// 09:00 on the farm's clock: the morning round. The evening visit opens at 17:00.
	morning := time.Date(today.Year(), today.Month(), today.Day(), 9, 0, 0, 0, loc)
	atMorning := NewRepository(pool, 10*time.Second)
	atMorning.now = func() time.Time { return morning }

	_, err = atMorning.CompleteWorkItem(ctx, domain.CompleteInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: eveningID,
		IdempotencyKey: "close-evening-early", RequestFingerprint: "close-evening-early",
	})
	if !errors.Is(err, domain.ErrSessionNotDue) {
		t.Fatalf("closing the evening visit at 09:00 = %v, want a refusal", err)
	}
	var notDue domain.SessionNotDueError
	if !errors.As(err, &notDue) || notDue.DueLabel() != "17:00" {
		t.Fatalf("refusal = %+v, want it to name 17:00 so the operator knows when to come back", notDue)
	}

	// The same visit closes once its hour has come.
	evening := time.Date(today.Year(), today.Month(), today.Day(), 17, 30, 0, 0, loc)
	atEvening := NewRepository(pool, 10*time.Second)
	atEvening.now = func() time.Time { return evening }
	if _, err := atEvening.CompleteWorkItem(ctx, domain.CompleteInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: eveningID,
		IdempotencyKey: "close-evening-ontime", RequestFingerprint: "close-evening-ontime",
	}); err != nil {
		t.Fatalf("closing the evening visit at 17:30: %v", err)
	}
}

// EVERY GAP MUST BE FIXABLE FROM THE PICKER THAT REPORTS IT.
//
// The stage picker is built from animal_stage_lookup filtered to rows whose own age_band is adult
// or kid. A stage whose lookup row has a NULL band is absent from it -- while the gap list, which
// derives the band from the ANIMALS standing on the stage, names it loudly. On 2026-09-23 that was
// ICU-Kid with 25 live animals: the console said those animals could not be diagnosed and offered
// no way to map them, so the only route out was SQL.
func TestAStageTheConsoleCallsAGapCanBeMappedFromTheConsole(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	if _, err := repo.SaveDiagnosisType(ctx, domain.SaveDiagnosisTypeCommand{
		TenantID: healthTenant, ActorID: healthActor, IdempotencyKey: "type-gap",
		RequestFingerprint: "fp-type-gap", TypeKey: "adult", Label: "Adults", Status: "active",
	}); err != nil {
		t.Fatalf("author a type: %v", err)
	}

	// A stage the farm really uses, whose LOOKUP ROW carries no age band -- the shape that hid
	// ICU-Kid from the picker.
	if _, err := pool.Exec(ctx,
		`INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, age_band, status)
		 VALUES ($1::uuid, 'ICU-Kid', 'ICU-Kid', NULL, 'active') ON CONFLICT DO NOTHING`,
		healthTenant); err != nil {
		t.Fatalf("seed the stage catalog: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE goats SET management_stage = 'ICU-Kid', age_band = 'kid'
		  WHERE goat_id = $1::uuid AND tenant_id = $2::uuid`, healthGoat, healthTenant); err != nil {
		t.Fatalf("stand an animal on it: %v", err)
	}

	view, err := repo.DiagnosisRouting(ctx, healthTenant)
	if err != nil {
		t.Fatalf("read the routing view: %v", err)
	}

	var named bool
	for _, g := range view.UnroutedStages {
		if g.StageCode == "icu-kid" {
			named = true
		}
	}
	if !named {
		t.Fatalf("the gap list does not name icu-kid, so this test is checking the wrong thing: %+v", view.UnroutedStages)
	}

	var offered bool
	for _, s := range view.Stages {
		if s.StageCode == "icu-kid" {
			offered = true
			if s.AgeBand != "kid" {
				t.Errorf("offered icu-kid in band %q, want the band its animals are in", s.AgeBand)
			}
		}
	}
	if !offered {
		t.Fatal("the console names icu-kid as a gap and will not offer it in the picker; the manager cannot fix what they are shown")
	}

	// AND THE WRITE MUST ACCEPT WHAT THE PICKER OFFERS. Offering a stage the server then refuses
	// is worse than not offering it: the manager taps Save, nothing happens, and no request they
	// can see failed. That shipped -- the picker was fixed and the write was not.
	if _, err := repo.SaveStageRoute(ctx, domain.SaveStageRouteCommand{
		TenantID: healthTenant, ActorID: healthActor, IdempotencyKey: "route-icu",
		RequestFingerprint: "fp-route-icu", AgeBand: "kid", StageCode: "icu-kid", TypeKey: "adult",
	}); err != nil {
		t.Fatalf("saving the route the picker offered: %v", err)
	}

	after, err := repo.DiagnosisRouting(ctx, healthTenant)
	if err != nil {
		t.Fatalf("re-read the routing view: %v", err)
	}
	for _, g := range after.UnroutedStages {
		if g.StageCode == "icu-kid" {
			t.Fatal("icu-kid is still reported as a gap after being mapped")
		}
	}
}
