// Package app provides the counts domain services.
package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

// HerdRegisterService exposes the herd register projection-backed read model.
type HerdRegisterService struct {
	repo                    ports.Repository
	milkPreparationStore    ports.MilkPreparationCompletionStore
	milkPreparationProofs   MilkPreparationProofValidator
	milkPreparationEnqueuer MilkPreparationVerificationEnqueuer
	now                     func() time.Time
}

// NewHerdRegisterService creates a new herd register service.
func NewHerdRegisterService(repo ports.Repository) *HerdRegisterService {
	service := &HerdRegisterService{repo: repo, now: time.Now}
	if store, ok := repo.(ports.MilkPreparationCompletionStore); ok {
		service.milkPreparationStore = store
	}
	return service
}

// MilkPreparationProofValidator verifies that every step reference is a completed, step-bound,
// in-app-camera video. The proof module remains the owner of media truth.
type MilkPreparationProofValidator interface {
	ValidateMilkPreparationProofs(ctx context.Context, tenantID, parkID string, proofs []domain.MilkPreparationStepProof) error
}

type MilkPreparationVerificationEnqueueRequest struct {
	TenantID       string
	CompletionID   string
	ParkID         string
	OperatorID     string
	AttemptNo      int32
	StepProofs     []domain.MilkPreparationStepProof
	CapturedAt     time.Time
	IdempotencyKey string
}

// MilkPreparationVerificationEnqueuer bridges the Counts write to the generic verifier queue.
type MilkPreparationVerificationEnqueuer interface {
	EnqueueMilkPreparationVerification(ctx context.Context, in MilkPreparationVerificationEnqueueRequest) error
}

func (s *HerdRegisterService) WithMilkPreparationProofValidator(validator MilkPreparationProofValidator) *HerdRegisterService {
	s.milkPreparationProofs = validator
	return s
}

func (s *HerdRegisterService) WithMilkPreparationVerificationEnqueuer(enqueuer MilkPreparationVerificationEnqueuer) *HerdRegisterService {
	s.milkPreparationEnqueuer = enqueuer
	return s
}

// GetSummary returns exact scoped summary counts.
//
// Despite the name of herd_register_summary_projection, this read is served from canonical
// goats, not from that projection table — see Repository.GetHerdRegisterSummary.
func (s *HerdRegisterService) GetSummary(ctx context.Context, req domain.HerdRegisterSummaryQuery) (domain.HerdRegisterSummary, error) {
	return s.repo.GetHerdRegisterSummary(ctx, req)
}

// GetBreakdown returns the Counts Breakdown census: one page of farm x stage x breed x sex x
// shed grain rows plus whole-result totals, chart series and filter facets.
func (s *HerdRegisterService) GetBreakdown(ctx context.Context, req domain.CountsBreakdownQuery) (domain.CountsBreakdown, error) {
	return s.repo.GetCountsBreakdown(ctx, req)
}

// GetMilkPreparation returns the current live-herd preparation direction. A zero AsOf is filled at
// the service boundary so every downstream date uses the same instant and the India business day.
func (s *HerdRegisterService) GetMilkPreparation(ctx context.Context, req domain.MilkPreparationQuery) (domain.MilkPreparationPage, error) {
	if req.AsOf.IsZero() {
		req.AsOf = time.Now()
	}
	return s.repo.GetMilkPreparation(ctx, req)
}

// SubmitMilkPreparation stores all applicable step videos as one immutable attempt, then creates
// one generic verification item containing those videos in their canonical step order. An exact
// command replay may repeat the idempotent enqueue to heal a previous queue-write failure.
func (s *HerdRegisterService) SubmitMilkPreparation(ctx context.Context, in domain.MilkPreparationSubmission) (domain.MilkPreparationSubmissionResult, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ParkID = strings.TrimSpace(in.ParkID)
	in.SubmittedBy = strings.TrimSpace(in.SubmittedBy)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.TenantID == "" || in.ParkID == "" || in.SubmittedBy == "" || in.IdempotencyKey == "" || in.PreparationDate.IsZero() {
		return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: tenant, park, operator, preparation date, and idempotency key are required")
	}
	if err := in.Proofs.Validate(in.GoatMilkUsed); err != nil {
		return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("%w: %v", ports.ErrMilkPreparationProofs, err)
	}
	if s.milkPreparationStore == nil || s.milkPreparationProofs == nil || s.milkPreparationEnqueuer == nil {
		return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: milk preparation verification is not configured")
	}
	steps := in.Proofs.OrderedStepProofs(in.GoatMilkUsed)
	if err := s.milkPreparationProofs.ValidateMilkPreparationProofs(ctx, in.TenantID, in.ParkID, steps); err != nil {
		return domain.MilkPreparationSubmissionResult{}, err
	}
	if in.SubmittedAt.IsZero() {
		in.SubmittedAt = s.now().UTC()
	}
	in.FeedingDate = in.PreparationDate.AddDate(0, 0, 1)
	result, err := s.milkPreparationStore.SubmitMilkPreparation(ctx, in)
	if err != nil {
		return domain.MilkPreparationSubmissionResult{}, err
	}
	if result.NeedsEnqueue {
		key := fmt.Sprintf("counts-milk-preparation-verification:%s:%d", result.CompletionID, result.AttemptNo)
		if err := s.milkPreparationEnqueuer.EnqueueMilkPreparationVerification(ctx, MilkPreparationVerificationEnqueueRequest{
			TenantID: in.TenantID, CompletionID: result.CompletionID, ParkID: in.ParkID,
			OperatorID: in.SubmittedBy, AttemptNo: result.AttemptNo, StepProofs: steps,
			CapturedAt: in.SubmittedAt, IdempotencyKey: key,
		}); err != nil {
			return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: enqueue milk preparation verification: %w", err)
		}
	}
	return result, nil
}
