package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

const testCase = "40000000-0000-4000-8000-000000000001"
const testSession = "50000000-0000-4000-8000-000000000001"

// completingRepo returns a completion carrying the verification enqueue context, and records
// the close-case input it received.
type completingRepo struct {
	fakeRepo
	completed domain.CompleteInput
	closed    domain.CloseCaseInput
}

func (r *completingRepo) CompleteWorkItem(_ context.Context, in domain.CompleteInput) (domain.CompleteResult, error) {
	r.completed = in
	return domain.CompleteResult{
		SessionID: in.SessionID, Status: "completed", CompletedAt: time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC),
		CaseID: testCase, GoatID: testGoat, GoatDisplayID: "CPT-00042", DiseaseName: "Fever",
		AgeBand: domain.AgeBandKid, DayNo: 2, ParkID: "", ShedID: "", ShedLabel: "Castro", PartitionLabel: "1",
	}, nil
}
func (r *completingRepo) CloseCase(_ context.Context, in domain.CloseCaseInput) (domain.CloseCaseResult, error) {
	r.closed = in
	return domain.CloseCaseResult{CaseID: in.CaseID, Status: in.Outcome}, nil
}

type recordingEnqueuer struct {
	calls []TreatmentVerificationEnqueueRequest
	err   error
}

func (e *recordingEnqueuer) EnqueueTreatmentVerification(_ context.Context, in TreatmentVerificationEnqueueRequest) error {
	e.calls = append(e.calls, in)
	return e.err
}

func TestCompleteWithProofFailsClosedWhenVerificationSeamIsNotWired(t *testing.T) {
	repo := &completingRepo{}
	_, err := NewService(repo).CompleteWorkItem(context.Background(), domain.CompleteInput{
		TenantID: testTenant, ActorID: testActor, SessionID: testSession, ProofRef: "proof-1", IdempotencyKey: "k1",
	})
	if !errors.Is(err, ErrVerificationEnqueuerNotWired) {
		t.Fatalf("err=%v want ErrVerificationEnqueuerNotWired", err)
	}
	if repo.completed.SessionID != "" {
		t.Fatalf("repo write happened before the seam check; a proof with no review path must not be accepted")
	}
}

func TestCompleteWithProofEnqueuesOneVerificationItemKeyedOnTheProof(t *testing.T) {
	repo := &completingRepo{}
	enq := &recordingEnqueuer{}
	res, err := NewService(repo).WithVerificationEnqueuer(enq).CompleteWorkItem(context.Background(), domain.CompleteInput{
		TenantID: testTenant, ActorID: testActor, SessionID: testSession, ProofRef: "proof-1", IdempotencyKey: "k1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(enq.calls) != 1 {
		t.Fatalf("enqueue calls=%d want 1", len(enq.calls))
	}
	got := enq.calls[0]
	// The key must carry the proof: a subject-only key strands the workflow after reject +
	// re-shoot, because CreateItem is ON CONFLICT DO NOTHING on the key.
	wantKey := "health-treatment-verification:" + testSession + ":proof-1"
	if got.IdempotencyKey != wantKey {
		t.Fatalf("idempotency key=%q want %q (must include the proof ref)", got.IdempotencyKey, wantKey)
	}
	if len(got.MediaRefs) != 1 || got.MediaRefs[0] != "proof-1" {
		t.Fatalf("media refs=%v want the proof", got.MediaRefs)
	}
	if got.AgeBand != domain.AgeBandKid {
		t.Fatalf("age band=%q want kid (routes the item to the health_kids page)", got.AgeBand)
	}
	// Backend-composed subject label: day, disease, animal, and the canonical operational
	// location display (space form for a bare numeric partition).
	wantSubject := "Day 2 · Fever · CPT-00042 · Castro 1"
	if got.SubjectLabel != wantSubject {
		t.Fatalf("subject=%q want %q", got.SubjectLabel, wantSubject)
	}
	if res.Status != "completed" {
		t.Fatalf("status=%q", res.Status)
	}
}

func TestCompleteWithoutProofDoesNotEnqueue(t *testing.T) {
	repo := &completingRepo{}
	enq := &recordingEnqueuer{}
	_, err := NewService(repo).WithVerificationEnqueuer(enq).CompleteWorkItem(context.Background(), domain.CompleteInput{
		TenantID: testTenant, ActorID: testActor, SessionID: testSession, IdempotencyKey: "k1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(enq.calls) != 0 {
		t.Fatalf("a proof-less completion has nothing to review; got %d enqueues", len(enq.calls))
	}
}

func TestCloseCaseValidatesOutcomeAndIdempotency(t *testing.T) {
	svc := NewService(&completingRepo{})
	base := domain.CloseCaseInput{TenantID: testTenant, ActorID: testActor, CaseID: testCase, IdempotencyKey: "k1"}

	for _, outcome := range []string{"recovered", "referred", "canceled", " Recovered "} {
		in := base
		in.Outcome = outcome
		if _, err := svc.CloseCase(context.Background(), in); err != nil {
			t.Fatalf("outcome %q: err=%v", outcome, err)
		}
	}
	for _, outcome := range []string{"", "closed_dead", "held_death_review", "continued", "cured"} {
		in := base
		in.Outcome = outcome
		if _, err := svc.CloseCase(context.Background(), in); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("outcome %q: err=%v want ErrInvalidInput", outcome, err)
		}
	}
	in := base
	in.Outcome = domain.CaseOutcomeRecovered
	in.IdempotencyKey = ""
	if _, err := svc.CloseCase(context.Background(), in); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing idempotency key: err=%v want ErrInvalidInput", err)
	}
}

// --- verdict consumer ---

type recordingVerdictStore struct {
	applied  []string
	bounced  []string
	reason   string
	verifier string
}

func (s *recordingVerdictStore) ApplyVerifiedTreatment(_ context.Context, _, sessionID, verifiedBy string, _ time.Time) error {
	s.applied = append(s.applied, sessionID)
	s.verifier = verifiedBy
	return nil
}
func (s *recordingVerdictStore) BounceTreatmentForRework(_ context.Context, _, sessionID, verifiedBy, reason string) error {
	s.bounced = append(s.bounced, sessionID)
	s.verifier = verifiedBy
	s.reason = reason
	return nil
}

func verdictEvent(t *testing.T, eventType, module, refType string) eventbus.Event {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"verified_by": testActor,
		"reason":      "blurred video",
		"source":      map[string]any{"module": module, "ref_type": refType, "ref_id": testSession},
	})
	if err != nil {
		t.Fatal(err)
	}
	return eventbus.Event{Type: eventType, TenantID: testTenant, Payload: payload, OccurredAt: time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC)}
}

func TestHealthVerdictApprovedStampsTheSession(t *testing.T) {
	store := &recordingVerdictStore{}
	h := NewHealthVerificationHandler(store, nil)
	if err := h.HandleEvent(context.Background(), verdictEvent(t, "verification.verdict.approved", "health", "health_treatment_session")); err != nil {
		t.Fatal(err)
	}
	if len(store.applied) != 1 || store.applied[0] != testSession || store.verifier != testActor {
		t.Fatalf("applied=%v verifier=%q", store.applied, store.verifier)
	}
	if len(store.bounced) != 0 {
		t.Fatalf("approve must not bounce; got %v", store.bounced)
	}
}

func TestHealthVerdictReworkBouncesTheSession(t *testing.T) {
	store := &recordingVerdictStore{}
	h := NewHealthVerificationHandler(store, nil)
	if err := h.HandleEvent(context.Background(), verdictEvent(t, "verification.verdict.rework", "health", "health_treatment_session")); err != nil {
		t.Fatal(err)
	}
	if len(store.bounced) != 1 || store.bounced[0] != testSession || store.reason != "blurred video" {
		t.Fatalf("bounced=%v reason=%q", store.bounced, store.reason)
	}
}

func TestHealthVerdictIgnoresOtherModulesAndRefTypes(t *testing.T) {
	store := &recordingVerdictStore{}
	h := NewHealthVerificationHandler(store, nil)
	for _, tc := range [][2]string{
		{"vaccination", "sop_submission"},
		{"counts", "shifting_event"},
		{"health", "health_case"},
		{"feed", "health_treatment_session"},
	} {
		if err := h.HandleEvent(context.Background(), verdictEvent(t, "verification.verdict.approved", tc[0], tc[1])); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.applied) != 0 || len(store.bounced) != 0 {
		t.Fatalf("foreign verdicts must pass through untouched; applied=%v bounced=%v", store.applied, store.bounced)
	}
}

func (*completingRepo) RecordStepProof(context.Context, domain.RecordStepProofInput) (domain.StepProof, error) {
	return domain.StepProof{}, nil
}
func (*completingRepo) StepProofs(context.Context, string, string) ([]domain.StepProof, error) {
	return nil, nil
}

// stepMediaRepo completes a card the way a CURRENT phone does: every step carries its own clip
// and the session carries NO video of its own, so `CompleteInput.ProofRef` is empty.
type stepMediaRepo struct{ completingRepo }

func (r *stepMediaRepo) CompleteWorkItem(ctx context.Context, in domain.CompleteInput) (domain.CompleteResult, error) {
	res, err := r.completingRepo.CompleteWorkItem(ctx, in)
	if err != nil {
		return res, err
	}
	res.StepMedia = []domain.StepMedia{
		{StepID: "s1", Label: "Prepare electrolyte solution", ProofRef: "clip-1"},
		{StepID: "s2", Label: "Assess first attempt intake", ProofRef: "clip-2"},
		{StepID: "s3", Label: "Tonoboost", ProofRef: "clip-3"},
	}
	return res, nil
}

// A PER-STEP CARD MUST STILL REACH THE VERIFIER.
//
// The enqueue was gated on the SESSION's own proof ref, which is exactly the field a per-step
// completion does not send: the phone passes it only for a legacy one-video card. So an operator
// filmed every step, the session completed, the clips sat on the server -- and nothing was ever
// queued for review. Found on a real device on 2026-09-24, after five clips and a green submit
// produced no verification item at all.
func TestPerStepCompletionWithNoSessionVideoStillReachesTheVerifier(t *testing.T) {
	repo := &stepMediaRepo{}
	enq := &recordingEnqueuer{}
	_, err := NewService(repo).WithVerificationEnqueuer(enq).CompleteWorkItem(context.Background(), domain.CompleteInput{
		TenantID: testTenant, ActorID: testActor, SessionID: testSession, IdempotencyKey: "k1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(enq.calls) != 1 {
		t.Fatalf("enqueue calls=%d want 1 -- a session filmed step by step must still be reviewed", len(enq.calls))
	}
	got := enq.calls[0]
	if len(got.MediaRefs) != 3 {
		t.Fatalf("media refs=%v want all three clips, in step order", got.MediaRefs)
	}
	for i, want := range []string{"clip-1", "clip-2", "clip-3"} {
		if got.MediaRefs[i] != want {
			t.Fatalf("media ref %d=%q want %q -- the verifier steps through them in the order the work was done", i, got.MediaRefs[i], want)
		}
	}
	if len(got.Captures) != 3 || got.Captures[0].Title != "Prepare electrolyte solution" {
		t.Fatalf("captures=%+v want one per step, each named by the step it proves", got.Captures)
	}
	// The key must carry WHAT was filmed. An empty tail collapses every re-shoot of this session
	// onto one queue row, and CreateItem is ON CONFLICT DO NOTHING -- so the replacement item is
	// silently dropped and the rework is never reviewed.
	if got.IdempotencyKey == "health-treatment-verification:"+testSession+":" {
		t.Fatalf("idempotency key=%q has an empty proof tail; a re-shoot would be dropped", got.IdempotencyKey)
	}
	if got.IdempotencyKey != "health-treatment-verification:"+testSession+":clip-1,clip-2,clip-3" {
		t.Fatalf("idempotency key=%q want the step clip set", got.IdempotencyKey)
	}
}

func TestPerStepCompletionFailsClosedWhenVerificationSeamIsNotWired(t *testing.T) {
	repo := &stepMediaRepo{}
	_, err := NewService(repo).CompleteWorkItem(context.Background(), domain.CompleteInput{
		TenantID: testTenant, ActorID: testActor, SessionID: testSession, IdempotencyKey: "k1",
	})
	if !errors.Is(err, ErrVerificationEnqueuerNotWired) {
		t.Fatalf("err=%v want ErrVerificationEnqueuerNotWired", err)
	}
	if repo.completed.SessionID != "" {
		t.Fatalf("repo write happened before the seam check; per-step evidence with no review path must not be accepted")
	}
}
