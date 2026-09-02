package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// PEN RECONCILIATION service (maintainer decision 2026-09-02): list the Reconcile queue and
// accept the operator's "the animal is back in its registered pen" submission. The register
// is truth and is never rewritten here; there is deliberately NO approver step — the mandatory
// video goes straight to the tenant verifier.

var (
	// ErrPenReconciliationEnqueuerNotWired is returned when a completion cannot enqueue its
	// verification item because the enqueue seam was never wired — a composition bug, surfaced
	// loudly rather than silently stranding a pending_verification card.
	ErrPenReconciliationEnqueuerNotWired = errors.New("counts: pen reconciliation verification enqueuer is not wired")
	// ErrInvalidPenReconciliationFilter is returned for a malformed cursor or status bucket.
	ErrInvalidPenReconciliationFilter = errors.New("counts: invalid pen reconciliation filter")
	// ErrInvalidPenReconciliationRecoveryLimit is returned when a recovery tick asks for an
	// unbounded or nonsensical enqueue-debt drain.
	ErrInvalidPenReconciliationRecoveryLimit = errors.New("counts: invalid pen reconciliation recovery limit")
)

// PenReconciliationVerificationEnqueuer enqueues the mandatory-video verification item for a
// submitted card. The composition layer adapts the verification module's CreateItem to this
// narrow port so counts never touches verification's tables directly.
type PenReconciliationVerificationEnqueuer interface {
	EnqueuePenReconciliationVerification(ctx context.Context, in PenReconciliationVerificationEnqueueRequest) error
}

// PenReconciliationVerificationEnqueueRequest is one return video handed to the verification
// queue.
type PenReconciliationVerificationEnqueueRequest struct {
	TenantID   string
	CardID     string
	OperatorID string
	ParkID     string
	ShedID     string
	// PartitionLabel is the registered pen's partition, carried as its own field (not only
	// folded into the label) so the verifier queue can filter by pen.
	PartitionLabel string
	MediaRefs      []string
	SubjectLabel   string
	CapturedAt     time.Time
	IdempotencyKey string
}

// PenReconciliationService owns the operator half of a reconciliation card.
type PenReconciliationService struct {
	repo     ports.PenReconciliationRepository
	now      func() time.Time
	enqueuer PenReconciliationVerificationEnqueuer
}

// NewPenReconciliationService constructs the service. now may be nil (defaults to time.Now).
func NewPenReconciliationService(repo ports.PenReconciliationRepository, now func() time.Time) *PenReconciliationService {
	if now == nil {
		now = time.Now
	}
	return &PenReconciliationService{repo: repo, now: now}
}

// WithVerificationEnqueuer wires the evidence-review enqueue seam. Without it, Complete fails
// closed rather than accepting operator evidence that can never reach the verifier queue.
func (s *PenReconciliationService) WithVerificationEnqueuer(enqueuer PenReconciliationVerificationEnqueuer) *PenReconciliationService {
	s.enqueuer = enqueuer
	return s
}

// CompletePenReconciliationInput is one "the animal is back in its pen" submission.
type CompletePenReconciliationInput struct {
	TenantID string
	CardID   string

	CompletedByUserID string
	TraceID           string

	// ProofRef is the MANDATORY video proving the animal was physically returned to its
	// registered pen. A blank value is rejected with ports.ErrPenReconciliationProofRequired.
	ProofRef string

	IdempotencyKey     string
	RequestFingerprint string
}

// Complete records the operator submission and enqueues the verifier's evidence review.
func (s *PenReconciliationService) Complete(
	ctx context.Context, in CompletePenReconciliationInput,
) (domain.PenReconciliationCompletionResult, bool, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.CardID) == "" ||
		strings.TrimSpace(in.CompletedByUserID) == "" || strings.TrimSpace(in.IdempotencyKey) == "" ||
		strings.TrimSpace(in.RequestFingerprint) == "" {
		return domain.PenReconciliationCompletionResult{}, false, ErrMissingRequiredField
	}
	if strings.TrimSpace(in.ProofRef) == "" {
		return domain.PenReconciliationCompletionResult{}, false, ports.ErrPenReconciliationProofRequired
	}
	if s.enqueuer == nil {
		// Fail closed: without the verification queue seam the mandatory evidence would have no
		// review path, and a pending_verification card would wait forever on a verdict that is
		// never coming.
		return domain.PenReconciliationCompletionResult{}, false, ErrPenReconciliationEnqueuerNotWired
	}

	result, replay, err := s.repo.CompletePenReconciliationCard(ctx, domain.PenReconciliationCompletionCommand{
		TenantID:           in.TenantID,
		CardID:             in.CardID,
		CompletedByUserID:  in.CompletedByUserID,
		CompletedAt:        s.now().UTC(),
		TraceID:            in.TraceID,
		ProofRef:           strings.TrimSpace(in.ProofRef),
		IdempotencyKey:     in.IdempotencyKey,
		RequestFingerprint: in.RequestFingerprint,
	})
	if err != nil {
		return domain.PenReconciliationCompletionResult{}, false, err
	}

	// Enqueue one evidence-review item. The repository keeps a durable
	// NeedsVerificationEnqueue marker on the pending card until this succeeds, so an enqueue
	// outage cannot strand a no-longer-actionable card without a repair path: an exact retry
	// replays the completion and retries this idempotent enqueue.
	if result.Status == domain.PenReconciliationStatusPendingVerification && result.NeedsVerificationEnqueue {
		proofRef := result.ProofRef
		if proofRef == "" {
			proofRef = strings.TrimSpace(in.ProofRef)
		}
		if err := s.enqueueVerification(ctx, in.TenantID, PenReconciliationVerificationEnqueueRequest{
			CardID:         in.CardID,
			OperatorID:     in.CompletedByUserID,
			ParkID:         derefString(result.ParkID),
			ShedID:         result.RegisteredShedID,
			PartitionLabel: result.RegisteredPartitionLabel,
			MediaRefs:      []string{proofRef},
			SubjectLabel:   penReconciliationSubject(result.ScannedIdentifier, result.RegisteredShedName, result.RegisteredPartitionLabel),
			CapturedAt:     s.now().UTC(),
			// Keyed to the CARD + proof so a retry collapses onto one queue item while a
			// re-shoot after rework mints the replacement item.
			IdempotencyKey: "counts-pen-reconciliation-verification:" + in.CardID + ":" + proofRef,
		}); err != nil {
			return domain.PenReconciliationCompletionResult{}, false, err
		}
		result.NeedsVerificationEnqueue = false
	}
	return result, replay, nil
}

// RecoverVerificationEnqueues drains durable enqueue debt for cards that already reached
// pending_verification but whose mandatory verifier item was not confirmed created. It is safe to
// call from the kernel worker: each enqueue is idempotent on card+proof, and the marker clears only
// after the producer succeeds.
func (s *PenReconciliationService) RecoverVerificationEnqueues(ctx context.Context, tenantID string, limit int) (int, error) {
	if strings.TrimSpace(tenantID) == "" {
		return 0, ErrMissingRequiredField
	}
	if limit < 1 || limit > 1000 {
		return 0, ErrInvalidPenReconciliationRecoveryLimit
	}
	if s.enqueuer == nil {
		return 0, ErrPenReconciliationEnqueuerNotWired
	}
	debts, err := s.repo.ListPenReconciliationVerificationEnqueueDebt(ctx, tenantID, limit)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, debt := range debts {
		if err := s.enqueueVerification(ctx, tenantID, PenReconciliationVerificationEnqueueRequest{
			CardID:         debt.CardID,
			OperatorID:     debt.CompletedBy,
			ParkID:         derefString(debt.ParkID),
			ShedID:         debt.RegisteredShedID,
			PartitionLabel: debt.RegisteredPartitionLabel,
			MediaRefs:      []string{debt.ProofRef},
			SubjectLabel:   penReconciliationSubject(debt.ScannedIdentifier, debt.RegisteredShedName, debt.RegisteredPartitionLabel),
			CapturedAt:     debt.CompletedAt.UTC(),
			IdempotencyKey: "counts-pen-reconciliation-verification:" + debt.CardID + ":" + debt.ProofRef,
		}); err != nil {
			return recovered, err
		}
		recovered++
	}
	return recovered, nil
}

// List returns one keyset page of the Reconcile queue. Status buckets are disjoint
// backend-owned workflow states; the summary counts are whole-filter truth, never page-local.
func (s *PenReconciliationService) List(
	ctx context.Context, tenantID, status string, pageSize int, cursor string,
) (domain.PenReconciliationPage, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.PenReconciliationPage{}, ErrMissingRequiredField
	}
	decoded, err := domain.DecodePenReconciliationCursor(cursor)
	if err != nil {
		return domain.PenReconciliationPage{}, ErrInvalidPenReconciliationFilter
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		status = domain.PenReconciliationBucketAll
	}
	if !domain.ValidPenReconciliationBucket(status) {
		return domain.PenReconciliationPage{}, ErrInvalidPenReconciliationFilter
	}
	return s.repo.ListPenReconciliationCards(ctx, domain.PenReconciliationQuery{
		TenantID: tenantID,
		Status:   status,
		PageSize: pageSize,
		Cursor:   decoded,
	})
}

func (s *PenReconciliationService) enqueueVerification(ctx context.Context, tenantID string, in PenReconciliationVerificationEnqueueRequest) error {
	in.TenantID = tenantID
	if err := s.enqueuer.EnqueuePenReconciliationVerification(ctx, in); err != nil {
		return err
	}
	return s.repo.MarkPenReconciliationVerificationEnqueued(ctx, tenantID, in.CardID)
}

func penReconciliationSubject(scannedIdentifier, shedName, partitionLabel string) string {
	registered := oploc.OperationalLocation{
		ShedName:       shedName,
		PartitionLabel: partitionLabel,
	}.Display()
	subject := "Pen return · " + scannedIdentifier
	if registered != "" {
		subject += " · back to " + registered
	}
	return subject
}
