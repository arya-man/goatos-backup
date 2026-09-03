package kernelstages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsbridge "github.com/vgoats/goatos/backend/internal/countsbridge"
	verificationpg "github.com/vgoats/goatos/backend/internal/verification/adapters/postgres"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	"github.com/vgoats/goatos/backend/internal/verificationcatalog"
)

// PenReconciliationEnqueueRecoveryStage drains submitted Reconcile cards whose mandatory
// verifier item was not confirmed created after the card reached pending_verification.
//
// This is the durable half of the non-atomic card-store -> verifier-store boundary: phone
// idempotent retries can heal the same marker, but the kernel worker also drains it so recovery
// does not depend on the original caller still retrying.
type PenReconciliationEnqueueRecoveryStage struct {
	service  *countsapp.PenReconciliationService
	tenantID string
	limit    int
	initErr  error
	logger   *slog.Logger
}

// NewPenReconciliationEnqueueRecoveryStage builds the operational-lane recovery stage.
func NewPenReconciliationEnqueueRecoveryStage(deps Deps, tenantID string) *PenReconciliationEnqueueRecoveryStage {
	limit := intEnv("GOATOS_PEN_RECONCILIATION_ENQUEUE_RECOVERY_LIMIT", 100)
	if limit < 1 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	countsRepo := countspg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	verificationService := verificationapp.NewService(verificationpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout), nil)
	initErr := verificationService.RegisterCategory(verificationcatalog.PenReconciliation)
	service := countsapp.NewPenReconciliationService(countsRepo, nil).
		WithVerificationEnqueuer(countsbridge.NewPenReconciliationVerificationEnqueuer(verificationService))
	return &PenReconciliationEnqueueRecoveryStage{
		service:  service,
		tenantID: tenantID,
		limit:    limit,
		initErr:  initErr,
		logger:   deps.Logger,
	}
}

// Name implements worker.StageRunner.
func (s *PenReconciliationEnqueueRecoveryStage) Name() string {
	return "pen-reconciliation-enqueue-recovery"
}

// Run performs one bounded enqueue-debt drain.
func (s *PenReconciliationEnqueueRecoveryStage) Run(ctx context.Context) error {
	if s.initErr != nil {
		return fmt.Errorf("pen reconciliation enqueue recovery: category catalog: %w", s.initErr)
	}
	if s.tenantID == "" {
		return errors.New("pen reconciliation enqueue recovery: tenant id is required")
	}
	recovered, err := s.service.RecoverVerificationEnqueues(ctx, s.tenantID, s.limit)
	if err != nil {
		return fmt.Errorf("pen reconciliation enqueue recovery: %w", err)
	}
	if s.logger != nil && recovered > 0 {
		s.logger.Info("pen_reconciliation_enqueue_recovery_stage_complete",
			"tenant_id", s.tenantID,
			"recovered", recovered,
		)
	}
	return nil
}
