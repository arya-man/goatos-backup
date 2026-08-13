package kernelstages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	"github.com/vgoats/goatos/backend/internal/sopbridge"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	verificationpg "github.com/vgoats/goatos/backend/internal/verification/adapters/postgres"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// SopReviewFanoutRetryStage durably retries accepted/reworked SOP review
// fanouts that committed before their post-commit vaccination-completion fanout
// ran, reusing sopapp.Service.RetryReviewFanouts wired to the same verify-fanout
// as the sop-review-fanout-retry one-shot. Operational (15m) cadence.
type SopReviewFanoutRetryStage struct {
	service  *sopapp.Service
	tenantID string
	limit    int
	logger   *slog.Logger
}

// NewSopReviewFanoutRetryStage builds the retry stage. The verify-fanout wiring
// (vaccination completion handler on an in-process bus) is constructed once and
// reused across ticks.
func NewSopReviewFanoutRetryStage(deps Deps, tenantID string) *SopReviewFanoutRetryStage {
	limit := intEnv("GOATOS_SOP_REVIEW_FANOUT_RETRY_LIMIT", 100)
	if limit < 1 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	sopRepo := soppg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	vaccinationRepo := vaccinationpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	obligationRepo := obligationpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	inventoryService := inventoryapp.NewService(inventorypg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	vaccinationService := vaccinationapp.NewService(vaccinationRepo)
	vaccinationCompletion := vaccinationapp.NewCompletionService(vaccinationService, obligationRepo, inventoryService)
	bus := eventbus.NewInProcessBus()
	vaccinationapp.NewVerificationHandler(vaccinationCompletion).Register(bus)
	service := sopapp.NewService(sopRepo).WithTaskReviewFanout(sopbridge.NewVerifyFanout(vaccinationService, bus))
	return &SopReviewFanoutRetryStage{service: service, tenantID: tenantID, limit: limit, logger: deps.Logger}
}

// Name implements worker.StageRunner.
func (s *SopReviewFanoutRetryStage) Name() string { return "sop-review-fanout-retry" }

// Run retries pending SOP review fanouts for the tenant.
func (s *SopReviewFanoutRetryStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return errors.New("sop review fanout retry: tenant id is required")
	}
	applied, err := s.service.RetryReviewFanouts(ctx, s.tenantID, s.limit)
	if err != nil {
		return fmt.Errorf("sop review fanout retry: %w", err)
	}
	if s.logger != nil {
		s.logger.Info("sop_review_fanout_retry_stage_complete", "tenant_id", s.tenantID, "applied", applied)
	}
	return nil
}

// SopSubmissionFanoutRetryStage durably retries submitted SOP task fanouts
// that committed before vaccination completions / verification items were
// materialized. Without this scheduled retry, a transient submit-time failure
// leaves uploaded proof invisible to the verifier queue until manual repair.
type SopSubmissionFanoutRetryStage struct {
	service  *sopapp.Service
	tenantID string
	limit    int
	logger   *slog.Logger
}

func NewSopSubmissionFanoutRetryStage(deps Deps, tenantID string) *SopSubmissionFanoutRetryStage {
	limit := intEnv("GOATOS_SOP_SUBMISSION_FANOUT_RETRY_LIMIT", 100)
	if limit < 1 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	sopRepo := soppg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	vaccinationRepo := vaccinationpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	verificationService := verificationapp.NewService(
		verificationpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout),
		nil,
	)
	_ = verificationService.RegisterCategory(verificationdomain.CategoryDefinition{
		Vertical:      "preventive_care",
		Module:        "vaccination",
		Category:      sopbridge.VaccinationVerificationCategory,
		ExpectedMedia: []string{"video"},
	})
	service := sopapp.NewService(sopRepo).WithSubmissionHook(
		sopbridge.NewVaccinationSubmissionBridge(vaccinationapp.NewService(vaccinationRepo)).
			WithVerificationProducer(verificationService),
	)
	return &SopSubmissionFanoutRetryStage{service: service, tenantID: tenantID, limit: limit, logger: deps.Logger}
}

func (s *SopSubmissionFanoutRetryStage) Name() string { return "sop-submission-fanout-retry" }

func (s *SopSubmissionFanoutRetryStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return errors.New("sop submission fanout retry: tenant id is required")
	}
	applied, err := s.service.RetrySubmissionFanouts(ctx, s.tenantID, s.limit)
	if err != nil {
		return fmt.Errorf("sop submission fanout retry: %w", err)
	}
	if s.logger != nil {
		s.logger.Info("sop_submission_fanout_retry_stage_complete", "tenant_id", s.tenantID, "applied", applied)
	}
	return nil
}
