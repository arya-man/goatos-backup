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
	markCalls      []string
	markErr        error
	listCalls      []domain.PenReconciliationQuery
	parks          []domain.PenReconciliationParkOption
	parkCalls      [][]string
	debts          []domain.PenReconciliationVerificationEnqueueDebt
	debtErr        error
	// cardIDByWorkflow resolves the workflow-completion path's card (blank = not found).
	cardIDByWorkflow string
}

func (f *fakePenReconciliationRepo) RaisePenReconciliationCards(context.Context, domain.PenReconciliationRaiseCommand) (int, error) {
	return 0, nil
}
func (f *fakePenReconciliationRepo) ListPenReconciliationCards(_ context.Context, q domain.PenReconciliationQuery) (domain.PenReconciliationPage, error) {
	f.listCalls = append(f.listCalls, q)
	return domain.PenReconciliationPage{}, nil
}
func (f *fakePenReconciliationRepo) ListPenReconciliationParkOptions(_ context.Context, _ string, parkIDs []string) ([]domain.PenReconciliationParkOption, error) {
	f.parkCalls = append(f.parkCalls, parkIDs)
	if len(parkIDs) == 0 {
		return f.parks, nil
	}
	out := []domain.PenReconciliationParkOption{}
	for _, park := range f.parks {
		for _, id := range parkIDs {
			if park.ParkID == id {
				out = append(out, park)
			}
		}
	}
	return out, nil
}
func (f *fakePenReconciliationRepo) CompletePenReconciliationCard(_ context.Context, in domain.PenReconciliationCompletionCommand) (domain.PenReconciliationCompletionResult, bool, error) {
	f.completeCalls = append(f.completeCalls, in)
	result := f.completeResult
	// Mirror the adapter: a first completion echoes the stored proof set, meta and rows.
	if len(in.ProofRefs) > 0 && len(result.ProofRefs) == 0 {
		result.ProofRefs, result.MediaMeta, result.ContextRows = in.ProofRefs, in.MediaMeta, in.ContextRows
	}
	return result, f.completeReplay, f.completeErr
}
func (f *fakePenReconciliationRepo) MarkPenReconciliationVerificationEnqueued(_ context.Context, tenantID, cardID string) error {
	f.markCalls = append(f.markCalls, tenantID+":"+cardID)
	return f.markErr
}
func (f *fakePenReconciliationRepo) ListPenReconciliationVerificationEnqueueDebt(context.Context, string, int) ([]domain.PenReconciliationVerificationEnqueueDebt, error) {
	return f.debts, f.debtErr
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
		NeedsVerificationEnqueue: true,
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
	if len(repo.markCalls) != 1 || repo.markCalls[0] != "tenant-1:card-1" {
		t.Fatalf("mark calls = %+v", repo.markCalls)
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
			CardID:                   "card-1",
			Status:                   domain.PenReconciliationStatusPendingVerification,
			ProofRef:                 "proof-1",
			NeedsVerificationEnqueue: true,
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
	if len(repo.markCalls) != 1 {
		t.Fatalf("mark calls = %+v", repo.markCalls)
	}
}

// TestPenReconciliationCompleteEnqueueFailureStaysRetryable pins the recovery path for the
// non-atomic card/store -> verifier-store boundary: the repository returns durable enqueue debt
// until the idempotent verifier enqueue succeeds and is marked clear.
func TestPenReconciliationCompleteEnqueueFailureStaysRetryable(t *testing.T) {
	repo := &fakePenReconciliationRepo{
		completeResult: domain.PenReconciliationCompletionResult{
			CardID:                   "card-1",
			Status:                   domain.PenReconciliationStatusPendingVerification,
			ScannedIdentifier:        "1420 0001",
			ProofRef:                 "proof-1",
			RegisteredShedID:         "shed-1",
			RegisteredShedName:       "Mandela 11",
			RegisteredPartitionLabel: "Part 2",
			NeedsVerificationEnqueue: true,
		},
	}
	enqueuer := &fakePenReconciliationEnqueuer{err: errors.New("verification unavailable")}
	svc := NewPenReconciliationService(repo, nil).WithVerificationEnqueuer(enqueuer)

	if _, _, err := svc.Complete(context.Background(), penReconciliationCompleteInput()); err == nil {
		t.Fatalf("first completion unexpectedly succeeded")
	}
	if len(enqueuer.calls) != 1 {
		t.Fatalf("enqueue attempts after failure = %d, want 1", len(enqueuer.calls))
	}
	if len(repo.markCalls) != 0 {
		t.Fatalf("enqueue marker was cleared on failure: %+v", repo.markCalls)
	}

	repo.completeReplay = true
	enqueuer.err = nil
	result, replay, err := svc.Complete(context.Background(), penReconciliationCompleteInput())
	if err != nil || !replay {
		t.Fatalf("retry replay = %v err = %v", replay, err)
	}
	if result.NeedsVerificationEnqueue {
		t.Fatalf("retry result still reports enqueue debt: %+v", result)
	}
	if len(enqueuer.calls) != 2 {
		t.Fatalf("enqueue attempts after retry = %d, want 2", len(enqueuer.calls))
	}
	if len(repo.markCalls) != 1 || repo.markCalls[0] != "tenant-1:card-1" {
		t.Fatalf("marker clear calls = %+v", repo.markCalls)
	}
}

func TestPenReconciliationRecoverVerificationEnqueuesDrainsDurableDebt(t *testing.T) {
	completedAt := time.Date(2026, 9, 2, 10, 30, 0, 0, time.UTC)
	park := "park-1"
	repo := &fakePenReconciliationRepo{
		debts: []domain.PenReconciliationVerificationEnqueueDebt{{
			CardID:                   "card-1",
			ScannedIdentifier:        "1420 0001",
			RegisteredShedID:         "shed-1",
			RegisteredShedName:       "Mandela 11",
			RegisteredPartitionLabel: "Part 2",
			ParkID:                   &park,
			ProofRef:                 "proof-1",
			CompletedBy:              "operator-1",
			CompletedAt:              completedAt,
		}},
	}
	enqueuer := &fakePenReconciliationEnqueuer{}
	svc := NewPenReconciliationService(repo, nil).WithVerificationEnqueuer(enqueuer)

	recovered, err := svc.RecoverVerificationEnqueues(context.Background(), "tenant-1", 50)
	if err != nil {
		t.Fatalf("RecoverVerificationEnqueues: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	if len(enqueuer.calls) != 1 {
		t.Fatalf("enqueue calls = %d, want 1", len(enqueuer.calls))
	}
	got := enqueuer.calls[0]
	if got.TenantID != "tenant-1" || got.OperatorID != "operator-1" || got.CapturedAt != completedAt {
		t.Fatalf("enqueue request = %+v", got)
	}
	if got.IdempotencyKey != "counts-pen-reconciliation-verification:card-1:proof-1" {
		t.Fatalf("idempotency key = %q", got.IdempotencyKey)
	}
	if got.SubjectLabel != "Pen return · 1420 0001 · back to Mandela 11 - Part 2" {
		t.Fatalf("subject = %q", got.SubjectLabel)
	}
	if len(repo.markCalls) != 1 || repo.markCalls[0] != "tenant-1:card-1" {
		t.Fatalf("marker clear calls = %+v", repo.markCalls)
	}
}

// TestPenReconciliationListValidatesTheFilter pins the strict bucket/cursor validation.
func TestPenReconciliationListValidatesTheFilter(t *testing.T) {
	repo := &fakePenReconciliationRepo{}
	svc := NewPenReconciliationService(repo, nil)

	if _, err := svc.List(context.Background(), ListPenReconciliationInput{TenantID: "tenant-1", Status: "not_a_bucket", PageSize: 20}); !errors.Is(err, ErrInvalidPenReconciliationFilter) {
		t.Fatalf("bad status: err = %v", err)
	}
	if _, err := svc.List(context.Background(), ListPenReconciliationInput{TenantID: "tenant-1", PageSize: 20, Cursor: "!!!not-a-cursor"}); !errors.Is(err, ErrInvalidPenReconciliationFilter) {
		t.Fatalf("bad cursor: err = %v", err)
	}
	if _, err := svc.List(context.Background(), ListPenReconciliationInput{TenantID: "tenant-1", Status: "open", PageSize: 20}); err != nil {
		t.Fatalf("valid: err = %v", err)
	}
	if len(repo.listCalls) != 1 || repo.listCalls[0].Status != domain.PenReconciliationBucketOpen {
		t.Fatalf("list calls = %+v", repo.listCalls)
	}
}

func (r *fakePenReconciliationRepo) PenReconciliationCardForWorkflow(context.Context, string, string) (domain.PenReconciliationWorkflowFacts, error) {
	return domain.PenReconciliationWorkflowFacts{}, ports.ErrPenReconciliationCardNotFound
}

func (r *fakePenReconciliationRepo) SetPenReconciliationWorkflow(context.Context, string, string, string) error {
	return nil
}

func (r *fakePenReconciliationRepo) PenReconciliationCardIDByWorkflow(context.Context, string, string) (string, error) {
	if r.cardIDByWorkflow != "" {
		return r.cardIDByWorkflow, nil
	}
	return "", ports.ErrPenReconciliationCardNotFound
}

// TestPenReconciliationListParkScopeIsBackendOwned pins the three park-scope shapes the
// Reconcile tab serves: a single-park operator is auto-selected onto their park and clamped to
// it; a tenant-wide reader is offered every park with nothing selected and an unclamped page;
// and a tenant-wide reader's ?park_id narrows BOTH the page and (through the same query) the
// chip counts, never the options.
func TestPenReconciliationListParkScopeIsBackendOwned(t *testing.T) {
	cbe := domain.PenReconciliationParkOption{ParkID: "park-cbe", Name: "Coimbatore", Code: "CBE"}
	cpt := domain.PenReconciliationParkOption{ParkID: "park-cpt", Name: "Channapatna", Code: "CPT"}

	t.Run("single-park operator is auto-selected and clamped", func(t *testing.T) {
		repo := &fakePenReconciliationRepo{parks: []domain.PenReconciliationParkOption{cbe, cpt}}
		svc := NewPenReconciliationService(repo, nil)
		page, err := svc.List(context.Background(), ListPenReconciliationInput{
			TenantID: "tenant-1", PageSize: 20, AuthorizedParkIDs: []string{"park-cpt"},
		})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if page.SelectedParkID != "park-cpt" {
			t.Fatalf("selected = %q, want the operator's only park", page.SelectedParkID)
		}
		if len(page.Parks) != 1 || page.Parks[0].Code != "CPT" {
			t.Fatalf("options = %+v, want exactly the operator's park", page.Parks)
		}
		if got := repo.listCalls[0].ParkIDs; len(got) != 1 || got[0] != "park-cpt" {
			t.Fatalf("page clamp = %v, want [park-cpt]", got)
		}
	})

	t.Run("tenant-wide reader sees every park, none selected, page unclamped", func(t *testing.T) {
		repo := &fakePenReconciliationRepo{parks: []domain.PenReconciliationParkOption{cbe, cpt}}
		svc := NewPenReconciliationService(repo, nil)
		page, err := svc.List(context.Background(), ListPenReconciliationInput{TenantID: "tenant-1", PageSize: 20})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if page.SelectedParkID != "" {
			t.Fatalf("selected = %q, want none", page.SelectedParkID)
		}
		if len(page.Parks) != 2 {
			t.Fatalf("options = %+v, want both parks", page.Parks)
		}
		if got := repo.listCalls[0].ParkIDs; len(got) != 0 {
			t.Fatalf("page clamp = %v, want none", got)
		}
	})

	t.Run("a park the caller is not offered is a bad filter, never an empty page", func(t *testing.T) {
		repo := &fakePenReconciliationRepo{parks: []domain.PenReconciliationParkOption{cbe, cpt}}
		svc := NewPenReconciliationService(repo, nil)
		_, err := svc.List(context.Background(), ListPenReconciliationInput{
			TenantID: "tenant-1", PageSize: 20, RequestedParkID: "not-a-park",
		})
		if !errors.Is(err, ErrInvalidPenReconciliationFilter) {
			t.Fatalf("err = %v, want ErrInvalidPenReconciliationFilter", err)
		}
		if len(repo.listCalls) != 0 {
			t.Fatalf("list ran %d times for a refused park", len(repo.listCalls))
		}
	})

	t.Run("tenant-wide reader picking a park narrows the page but keeps every option", func(t *testing.T) {
		repo := &fakePenReconciliationRepo{parks: []domain.PenReconciliationParkOption{cbe, cpt}}
		svc := NewPenReconciliationService(repo, nil)
		page, err := svc.List(context.Background(), ListPenReconciliationInput{
			TenantID: "tenant-1", PageSize: 20, RequestedParkID: "park-cbe",
		})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if page.SelectedParkID != "park-cbe" || len(page.Parks) != 2 {
			t.Fatalf("selected = %q options = %+v", page.SelectedParkID, page.Parks)
		}
		if got := repo.listCalls[0].ParkIDs; len(got) != 1 || got[0] != "park-cbe" {
			t.Fatalf("page clamp = %v, want [park-cbe]", got)
		}
	})
}
