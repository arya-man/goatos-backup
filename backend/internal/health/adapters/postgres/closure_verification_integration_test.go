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

// publishFeverAndOpenCase publishes a 3-day adult Fever course and opens one case for the
// shared seeded goat, returning the opened result.
func publishFeverAndOpenCase(t *testing.T, ctx context.Context, repo *Repository) domain.OpenCaseResult {
	t.Helper()
	medicine := "Meloxicam"
	dose := "1 ml"
	steps := make([]domain.ProtocolStep, 0, 3)
	for day := 1; day <= 3; day++ {
		steps = append(steps, domain.ProtocolStep{
			DayNo: day, Session: domain.SessionMorning, Seq: day,
			RecordType: "medication", MedicineName: &medicine, DosageText: &dose,
		})
	}
	if err := repo.ReplacePublishedProtocols(ctx, healthTenant, healthActor, "health-test", "hash-v1", []domain.SourceProtocol{{
		DiseaseKey: "fever", DisplayName: "Fever", AgeBand: domain.AgeBandAdult, Steps: steps,
	}}); err != nil {
		t.Fatalf("publish protocol: %v", err)
	}
	loc, _ := time.LoadLocation("Asia/Kolkata")
	opened, err := repo.OpenCase(ctx, domain.OpenCaseInput{
		TenantID: healthTenant, ActorID: healthActor, GoatID: healthGoat,
		DiseaseKey: "fever", AgeBand: domain.AgeBandAdult, StartDate: time.Now().In(loc),
		IdempotencyKey: "closure-open", RequestFingerprint: "open-fp",
	})
	if err != nil {
		t.Fatalf("open case: %v", err)
	}
	return opened
}

func TestCloseCaseRecoveredCancelsRemainingSessionsAndReplays(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	opened := publishFeverAndOpenCase(t, ctx, repo)

	// Day 1 is worked; days 2 and 3 are still owed when the animal recovers.
	if _, err := repo.CompleteWorkItem(ctx, domain.CompleteInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
		IdempotencyKey: "closure-complete-1", RequestFingerprint: "complete-fp",
	}); err != nil {
		t.Fatalf("complete day 1: %v", err)
	}

	closed, err := repo.CloseCase(ctx, domain.CloseCaseInput{
		TenantID: healthTenant, ActorID: healthActor, CaseID: opened.CaseID,
		Outcome: domain.CaseOutcomeRecovered, Note: "eating normally again",
		IdempotencyKey: "closure-close-1", RequestFingerprint: "close-fp",
	})
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if closed.Status != "recovered" || closed.CanceledSessionCount != 2 || closed.IdempotentReplay {
		t.Fatalf("close=%+v want recovered with 2 canceled sessions", closed)
	}

	var caseStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM health_cases WHERE tenant_id=$1 AND health_case_id=$2`, healthTenant, opened.CaseID).Scan(&caseStatus); err != nil {
		t.Fatal(err)
	}
	if caseStatus != "recovered" {
		t.Fatalf("case status=%q want recovered", caseStatus)
	}
	assertHealthCount(t, ctx, pool, "canceled sessions", `SELECT count(*) FROM health_treatment_sessions WHERE tenant_id=$1 AND health_case_id=$2 AND status='canceled'`, 2, healthTenant, opened.CaseID)
	assertHealthCount(t, ctx, pool, "completed session kept", `SELECT count(*) FROM health_treatment_sessions WHERE tenant_id=$1 AND health_case_id=$2 AND status='completed'`, 1, healthTenant, opened.CaseID)
	assertHealthCount(t, ctx, pool, "medicine history kept", `SELECT count(*) FROM health_medicine_administrations WHERE tenant_id=$1 AND goat_id=$2`, 1, healthTenant, healthGoat)
	assertHealthCount(t, ctx, pool, "closure outbox event", `SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='health.case.closed' AND aggregate_id=$2::uuid`, 1, healthTenant, opened.CaseID)

	// The canceled sessions leave the worklist: tomorrow's page and summary read empty.
	loc, _ := time.LoadLocation("Asia/Kolkata")
	tomorrow := time.Now().In(loc).AddDate(0, 0, 1).Format("2006-01-02")
	page, err := repo.ListWorkItems(ctx, domain.ListFilter{TenantID: healthTenant, AgeBand: domain.AgeBandAdult, Date: tomorrow, Limit: 20})
	if err != nil || len(page.Items) != 0 || page.Summary.Total != 0 {
		t.Fatalf("canceled sessions still on the worklist: page=%+v err=%v", page, err)
	}
	if len(page.DateMarkers) != 0 {
		t.Fatalf("canceled sessions still marked on the calendar: %+v", page.DateMarkers)
	}

	// Exact replay returns the original closure without new writes.
	replay, err := repo.CloseCase(ctx, domain.CloseCaseInput{
		TenantID: healthTenant, ActorID: healthActor, CaseID: opened.CaseID,
		Outcome: domain.CaseOutcomeRecovered, Note: "eating normally again",
		IdempotencyKey: "closure-close-1", RequestFingerprint: "close-fp",
	})
	if err != nil || !replay.IdempotentReplay || replay.Status != "recovered" || replay.CanceledSessionCount != 2 {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	// Same key, different payload is a conflict; a fresh key on a closed case is refused.
	if _, err := repo.CloseCase(ctx, domain.CloseCaseInput{
		TenantID: healthTenant, ActorID: healthActor, CaseID: opened.CaseID,
		Outcome: domain.CaseOutcomeCanceled, IdempotencyKey: "closure-close-1", RequestFingerprint: "other-fp",
	}); !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("same-key different-payload err=%v want ErrConflict", err)
	}
	if _, err := repo.CloseCase(ctx, domain.CloseCaseInput{
		TenantID: healthTenant, ActorID: healthActor, CaseID: opened.CaseID,
		Outcome: domain.CaseOutcomeCanceled, IdempotencyKey: "closure-close-2", RequestFingerprint: "close-fp-2",
	}); !errors.Is(err, ports.ErrCaseNotOpen) {
		t.Fatalf("second closure err=%v want ErrCaseNotOpen", err)
	}

	// A closure-canceled session can never be completed afterwards.
	var canceledSession string
	if err := pool.QueryRow(ctx, `SELECT health_session_id::text FROM health_treatment_sessions WHERE tenant_id=$1 AND health_case_id=$2 AND status='canceled' LIMIT 1`, healthTenant, opened.CaseID).Scan(&canceledSession); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CompleteWorkItem(ctx, domain.CompleteInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: canceledSession,
		IdempotencyKey: "closure-complete-late", RequestFingerprint: "late-fp",
	}); !errors.Is(err, ports.ErrCaseNotOpen) {
		t.Fatalf("completing a canceled session err=%v want ErrCaseNotOpen", err)
	}
}

func TestCloseCaseRefusesDeathHeldCases(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	opened := publishFeverAndOpenCase(t, ctx, repo)

	if err := repo.HoldForDeathReview(ctx, healthTenant, healthGoat); err != nil {
		t.Fatalf("hold: %v", err)
	}
	if _, err := repo.CloseCase(ctx, domain.CloseCaseInput{
		TenantID: healthTenant, ActorID: healthActor, CaseID: opened.CaseID,
		Outcome: domain.CaseOutcomeRecovered, IdempotencyKey: "closure-held", RequestFingerprint: "fp",
	}); !errors.Is(err, ports.ErrCaseNotOpen) {
		t.Fatalf("death-held case closure err=%v want ErrCaseNotOpen (the death workflow owns it)", err)
	}
}

func TestVerifiedTreatmentStampAndReworkRoundTrip(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	opened := publishFeverAndOpenCase(t, ctx, repo)

	completed, err := repo.CompleteWorkItem(ctx, domain.CompleteInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
		ProofRef: "proof-clip-1", IdempotencyKey: "verify-complete-1", RequestFingerprint: "fp-1",
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	// The completion returns the enqueue context read under the same row lock.
	if completed.AgeBand != domain.AgeBandAdult || completed.DiseaseName != "Fever" || completed.GoatDisplayID == "" || completed.DayNo != 1 {
		t.Fatalf("enqueue context=%+v", completed)
	}

	verifiedAt := time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC)
	if err := repo.ApplyVerifiedTreatment(ctx, healthTenant, opened.FirstSessionID, healthActor, verifiedAt); err != nil {
		t.Fatalf("apply verified: %v", err)
	}
	var verifiedBy *string
	var storedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT verified_by::text, verified_at FROM health_treatment_sessions WHERE tenant_id=$1 AND health_session_id=$2`, healthTenant, opened.FirstSessionID).Scan(&verifiedBy, &storedAt); err != nil {
		t.Fatal(err)
	}
	if verifiedBy == nil || *verifiedBy != healthActor || storedAt == nil || !storedAt.Equal(verifiedAt) {
		t.Fatalf("verified stamp: by=%v at=%v", verifiedBy, storedAt)
	}
	// Redelivery is a no-op, not an error, and does not restamp.
	if err := repo.ApplyVerifiedTreatment(ctx, healthTenant, opened.FirstSessionID, healthParty, verifiedAt.Add(time.Hour)); err != nil {
		t.Fatalf("redelivered apply: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT verified_by::text FROM health_treatment_sessions WHERE tenant_id=$1 AND health_session_id=$2`, healthTenant, opened.FirstSessionID).Scan(&verifiedBy); err != nil {
		t.Fatal(err)
	}
	if *verifiedBy != healthActor {
		t.Fatalf("redelivery restamped verified_by to %q", *verifiedBy)
	}

	// Rework flips the completed session back and clears the stamp; history stays.
	if err := repo.BounceTreatmentForRework(ctx, healthTenant, opened.FirstSessionID, healthActor, "camera covered"); err != nil {
		t.Fatalf("bounce: %v", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM health_treatment_sessions WHERE tenant_id=$1 AND health_session_id=$2`, healthTenant, opened.FirstSessionID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "rework" {
		t.Fatalf("status=%q want rework", status)
	}
	assertHealthCount(t, ctx, pool, "medicine history survives rework", `SELECT count(*) FROM health_medicine_administrations WHERE tenant_id=$1 AND goat_id=$2`, 1, healthTenant, healthGoat)
	// Redelivered rework on a non-completed session is a no-op.
	if err := repo.BounceTreatmentForRework(ctx, healthTenant, opened.FirstSessionID, healthActor, "again"); err != nil {
		t.Fatalf("redelivered bounce: %v", err)
	}

	// The operator re-does the session with a NEW proof through the ordinary complete path.
	recompleted, err := repo.CompleteWorkItem(ctx, domain.CompleteInput{
		TenantID: healthTenant, ActorID: healthActor, SessionID: opened.FirstSessionID,
		ProofRef: "proof-clip-2", IdempotencyKey: "verify-complete-2", RequestFingerprint: "fp-2",
	})
	if err != nil || recompleted.Status != "completed" {
		t.Fatalf("re-complete=%+v err=%v", recompleted, err)
	}
}
