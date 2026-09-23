package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/health/domain"
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
