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
	milkFeedingStore        ports.MilkFeedingStore
	milkFeedingProofs       MilkFeedingProofValidator
	milkFeedingEnqueuer     MilkFeedingVerificationEnqueuer
	// alerts is the OPTIONAL reader for the counts module's own lifecycle alerts feed
	// (backend/internal/counts/domain/alerts.go). Resolved by type assertion, same as
	// milkPreparationStore/milkFeedingStore above, so adding it cannot break every fake
	// implementing ports.Repository. Without it, ListAlerts fails closed with
	// ErrAlertsUnavailable. See alerts.go.
	alerts ports.AlertsRepository
	now    func() time.Time
}

// NewHerdRegisterService creates a new herd register service.
func NewHerdRegisterService(repo ports.Repository) *HerdRegisterService {
	service := &HerdRegisterService{repo: repo, now: time.Now}
	if store, ok := repo.(ports.MilkPreparationCompletionStore); ok {
		service.milkPreparationStore = store
	}
	if store, ok := repo.(ports.MilkFeedingStore); ok {
		service.milkFeedingStore = store
	}
	if store, ok := repo.(ports.AlertsRepository); ok {
		service.alerts = store
	}
	return service
}

type MilkFeedingProofValidator interface {
	ValidateMilkFeedingProofs(ctx context.Context, tenantID, parkID string, proofs []domain.MilkPreparationStepProof) error
}

type MilkFeedingVerificationEnqueueRequest struct {
	TenantID       string
	CompletionID   string
	ParkID         string
	OperatorID     string
	AttemptNo      int32
	StepProofs     []domain.MilkPreparationStepProof
	CapturedAt     time.Time
	IdempotencyKey string
}

type MilkFeedingVerificationEnqueuer interface {
	EnqueueMilkFeedingVerification(ctx context.Context, in MilkFeedingVerificationEnqueueRequest) error
}

func (s *HerdRegisterService) WithMilkFeedingProofValidator(v MilkFeedingProofValidator) *HerdRegisterService {
	s.milkFeedingProofs = v
	return s
}
func (s *HerdRegisterService) WithMilkFeedingVerificationEnqueuer(e MilkFeedingVerificationEnqueuer) *HerdRegisterService {
	s.milkFeedingEnqueuer = e
	return s
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

// GetShedDirectory returns the shed configuration catalog: what the farm has built, what cohort
// each shed is configured for, and how many head it is meant to hold. Configuration, not census --
// a shed with no animals in it still belongs in the answer.
func (s *HerdRegisterService) GetShedDirectory(ctx context.Context, tenantID string) (domain.ShedDirectory, error) {
	return s.repo.ShedDirectory(ctx, tenantID)
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
		return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: tenant, farm, operator, preparation date, and idempotency key are required")
	}
	if err := in.Proofs.Validate(in.GoatMilkUsed); err != nil {
		return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("%w: %v", ports.ErrMilkPreparationProofs, err)
	}
	if err := in.Answers.Validate(in.GoatMilkUsed); err != nil {
		return domain.MilkPreparationSubmissionResult{}, fmt.Errorf("counts: invalid milk preparation answers: %w", err)
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

func (s *HerdRegisterService) MaterializeMilkFeedingTasks(ctx context.Context, in domain.MilkFeedingMaterializeRequest) (domain.MilkFeedingMaterializeResult, error) {
	if s.milkFeedingStore == nil {
		return domain.MilkFeedingMaterializeResult{}, fmt.Errorf("counts: milk feeding store is not configured")
	}
	return s.milkFeedingStore.MaterializeMilkFeedingTasks(ctx, in)
}

func (s *HerdRegisterService) ListMilkFeedingTasks(ctx context.Context, in domain.MilkFeedingQuery) (domain.MilkFeedingPage, error) {
	if s.milkFeedingStore == nil {
		return domain.MilkFeedingPage{}, fmt.Errorf("counts: milk feeding store is not configured")
	}
	return s.milkFeedingStore.ListMilkFeedingTasks(ctx, in)
}

func (s *HerdRegisterService) SubmitMilkFeeding(ctx context.Context, in domain.MilkFeedingSubmission) (domain.MilkFeedingSubmissionResult, error) {
	if s.milkFeedingStore == nil || s.milkFeedingProofs == nil || s.milkFeedingEnqueuer == nil {
		return domain.MilkFeedingSubmissionResult{}, fmt.Errorf("counts: milk feeding verification is not configured")
	}
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.TaskID) == "" || strings.TrimSpace(in.ParkID) == "" || strings.TrimSpace(in.SubmittedBy) == "" || strings.TrimSpace(in.IdempotencyKey) == "" {
		return domain.MilkFeedingSubmissionResult{}, fmt.Errorf("counts: milk feeding task, farm, operator, and idempotency key are required")
	}
	if _, ok := domain.MilkFeedingSessionDueTime(in.SessionNo); !ok {
		return domain.MilkFeedingSubmissionResult{}, fmt.Errorf("counts: milk feeding session must be 1 through 4")
	}
	if err := in.Proofs.Validate(); err != nil {
		return domain.MilkFeedingSubmissionResult{}, err
	}
	steps := in.Proofs.OrderedStepProofs()
	if err := s.milkFeedingProofs.ValidateMilkFeedingProofs(ctx, in.TenantID, in.ParkID, steps); err != nil {
		return domain.MilkFeedingSubmissionResult{}, err
	}
	if in.SubmittedAt.IsZero() {
		in.SubmittedAt = s.now().UTC()
	}
	result, err := s.milkFeedingStore.SubmitMilkFeeding(ctx, in)
	if err != nil {
		return domain.MilkFeedingSubmissionResult{}, err
	}
	if result.NeedsEnqueue {
		err = s.milkFeedingEnqueuer.EnqueueMilkFeedingVerification(ctx, MilkFeedingVerificationEnqueueRequest{
			TenantID: in.TenantID, CompletionID: result.CompletionID, ParkID: in.ParkID,
			OperatorID: in.SubmittedBy, AttemptNo: result.AttemptNo, StepProofs: steps, CapturedAt: in.SubmittedAt,
			IdempotencyKey: fmt.Sprintf("milk-feeding-verification:%s:%d", result.CompletionID, result.AttemptNo),
		})
		if err != nil {
			return domain.MilkFeedingSubmissionResult{}, fmt.Errorf("counts: enqueue milk feeding verification: %w", err)
		}
	}
	return result, nil
}
