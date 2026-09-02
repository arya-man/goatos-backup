package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

type fakePenReconciliationRepo struct {
	completeResult domain.PenReconciliationCompletionResult
	completeReplay bool
	completeErr    error
	completeCalls  []domain.PenReconciliationCompletionCommand
	listCalls      []domain.PenReconciliationQuery
}

func (f *fakePenReconciliationRepo) RaisePenReconciliationCards(context.Context, domain.PenReconciliationRaiseCommand) (int, error) {
	return 0, nil
}
func (f *fakePenReconciliationRepo) ListPenReconciliationCards(_ context.Context, q domain.PenReconciliationQuery) (domain.PenReconciliationPage, error) {
	f.listCalls = append(f.listCalls, q)
	return domain.PenReconciliationPage{}, nil
}
func (f *fakePenReconciliationRepo) CompletePenReconciliationCard(_ context.Context, in domain.PenReconciliationCompletionCommand) (domain.PenReconciliationCompletionResult, bool, error) {
	f.completeCalls = append(f.completeCalls, in)
	return f.completeResult, f.completeReplay, f.completeErr
}
func (f *fakePenReconciliationRepo) ApplyVerifiedPenReconciliation(context.Context, domain.PenReconciliationVerdictCommand) error {
	return nil
}
func (f *fakePenReconciliationRepo) BouncePenReconciliationForRework(context.Context, domain.PenReconciliationVerdictCommand) error {
	return nil
}

type fakePenReconciliationEnqueuer struct {
	calls []PenReconciliationVerificationEnqueueRequest
	err   error
}

func (f *fakePenReconciliationEnqueuer) EnqueuePenReconciliationVerification(
	_ context.Context, in PenReconciliationVerificationEnqueueRequest,
) error {
	f.calls = append(f.calls, in)
	return f.err
}

func penReconciliationCompleteInput() CompletePenReconciliationInput {
	return CompletePenReconciliationInput{
		TenantID:           "tenant-1",
		CardID:             "card-1",
		CompletedByUserID:  "operator-1",
		ProofRef:           "proof-1",
		IdempotencyKey:     "key-1",
		RequestFingerprint: "fp-1",
	}
}

// TestPenReconciliationCompleteEnqueuesVerificationWithProofKeyedIdempotency pins the enqueue
// contract: the verification item carries the video, the registered pen's coordinates, and an
// idempotency key that includes the proof — so a retry collapses onto one item while a
// re-shoot after rework mints the replacement item (the enqueue-keys-must-carry-proofs rule).
func TestPenReconciliationCompleteEnqueuesVerificationWithProofKeyedIdempotency(t *testing.T) {
	park := "park-1"
	repo := &fakePenReconciliationRepo{completeResult: domain.PenReconciliationCompletionResult{
		CardID:                   "card-1",
		Status:                   domain.PenReconciliationStatusPendingVerification,
		ScannedIdentifier:        "1420 0001",
		ProofRef:                 "proof-1",
		RegisteredShedID:         "shed-1",
		RegisteredShedName:       "Mandela 11",
		RegisteredPartitionLabel: "Part 2",
		ParkID:                   &park,
	}}
	enqueuer := &fakePenReconciliationEnqueuer{}
	svc := NewPenReconciliationService(repo, func() time.Time {
		return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	}).WithVerificationEnqueuer(enqueuer)

	result, replay, err := svc.Complete(context.Background(), penReconciliationCompleteInput())
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if replay || result.Status != domain.PenReconciliationStatusPendingVerification {
		t.Fatalf("result = %+v replay=%v", result, replay)
	}
	if len(enqueuer.calls) != 1 {
		t.Fatalf("enqueue calls = %d, want 1", len(enqueuer.calls))
	}
	got := enqueuer.calls[0]
	if got.IdempotencyKey != "counts-pen-reconciliation-verification:card-1:proof-1" {
		t.Fatalf("idempotency key = %q", got.IdempotencyKey)
	}
	if len(got.MediaRefs) != 1 || got.MediaRefs[0] != "proof-1" {
		t.Fatalf("media refs = %v", got.MediaRefs)
	}
	if got.ShedID != "shed-1" || got.PartitionLabel != "Part 2" || got.ParkID != "park-1" {
		t.Fatalf("registered pen coordinates = %+v", got)
	}
	if got.SubjectLabel != "Pen return · 1420 0001 · back to Mandela 11 - Part 2" {
		t.Fatalf("subject = %q", got.SubjectLabel)
	}
}

// TestPenReconciliationCompleteFailsClosedWithoutEnqueuerOrProof pins the two refusals that
// keep evidence reviewable: no video means nothing to verify, and a missing enqueue seam
// would strand a pending_verification card on a verdict that is never coming.
func TestPenReconciliationCompleteFailsClosedWithoutEnqueuerOrProof(t *testing.T) {
	repo := &fakePenReconciliationRepo{}
	svc := NewPenReconciliationService(repo, nil)

	in := penReconciliationCompleteInput()
	if _, _, err := svc.Complete(context.Background(), in); !errors.Is(err, ErrPenReconciliationEnqueuerNotWired) {
		t.Fatalf("no enqueuer: err = %v", err)
	}

	svc = svc.WithVerificationEnqueuer(&fakePenReconciliationEnqueuer{})
	in.ProofRef = "  "
	if _, _, err := svc.Complete(context.Background(), in); !errors.Is(err, ports.ErrPenReconciliationProofRequired) {
		t.Fatalf("blank proof: err = %v", err)
	}
	if len(repo.completeCalls) != 0 {
		t.Fatalf("repository written despite refusals: %d calls", len(repo.completeCalls))
	}
}

// TestPenReconciliationCompleteReplayDoesNotDependOnRepoProof pins that an idempotent replay
// (repo echoes the original without re-reading the proof) still enqueues with the SAME key so
// CreateItem dedupes, rather than minting a second review item.
func TestPenReconciliationCompleteReplayEnqueuesSameKey(t *testing.T) {
	repo := &fakePenReconciliationRepo{
		completeReplay: true,
		completeResult: domain.PenReconciliationCompletionResult{
			CardID:   "card-1",
			Status:   domain.PenReconciliationStatusPendingVerification,
			ProofRef: "proof-1",
		},
	}
	enqueuer := &fakePenReconciliationEnqueuer{}
	svc := NewPenReconciliationService(repo, nil).WithVerificationEnqueuer(enqueuer)
	_, replay, err := svc.Complete(context.Background(), penReconciliationCompleteInput())
	if err != nil || !replay {
		t.Fatalf("replay = %v err = %v", replay, err)
	}
	if len(enqueuer.calls) != 1 ||
		enqueuer.calls[0].IdempotencyKey != "counts-pen-reconciliation-verification:card-1:proof-1" {
		t.Fatalf("enqueue calls = %+v", enqueuer.calls)
	}
}

// TestPenReconciliationListValidatesTheFilter pins the strict bucket/cursor validation.
func TestPenReconciliationListValidatesTheFilter(t *testing.T) {
	repo := &fakePenReconciliationRepo{}
	svc := NewPenReconciliationService(repo, nil)

	if _, err := svc.List(context.Background(), "tenant-1", "not_a_bucket", 20, ""); !errors.Is(err, ErrInvalidPenReconciliationFilter) {
		t.Fatalf("bad status: err = %v", err)
	}
	if _, err := svc.List(context.Background(), "tenant-1", "", 20, "!!!not-a-cursor"); !errors.Is(err, ErrInvalidPenReconciliationFilter) {
		t.Fatalf("bad cursor: err = %v", err)
	}
	if _, err := svc.List(context.Background(), "tenant-1", "open", 20, ""); err != nil {
		t.Fatalf("valid: err = %v", err)
	}
	if len(repo.listCalls) != 1 || repo.listCalls[0].Status != domain.PenReconciliationBucketOpen {
		t.Fatalf("list calls = %+v", repo.listCalls)
	}
}
