package kernelstages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	verificationpg "github.com/vgoats/goatos/backend/internal/verification/adapters/postgres"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	"github.com/vgoats/goatos/backend/internal/verificationcatalog"
)

// VerificationSamplingCloseoutStage settles the proof videos the RANDOMIZATION policy did not draw
// (maintainer decision 2026-08-26).
//
// WHY THIS STAGE HAS TO EXIST. Verifier approval is not merely review for feed and weighing -- it
// is the gate that COMPLETES the work: a feed pen-session stays pending_verification until an
// approve lands, and a weighing bucket cannot close while any verification is pending, under a
// close gate with no override. Sampling narrows what reaches the verifier, so without this stage
// the videos she is no longer shown would hold those workflows open forever. Settling them emits
// the ORDINARY verification.verdict.approved event, so every producer's consumer applies exactly as
// it does for a human approve -- there is no second apply path to keep in step.
//
// WHY IT RUNS ON A CADENCE INSTEAD OF AT ENQUEUE. The percentage is live: raising it at 15:00 must
// pull more of TODAY's already-captured videos into her queue. A video waived the moment it arrived
// could never be recruited back, so waiving waits until the business day -- and with it that day's
// percentage -- can no longer change. The service passes today's business-day start as the cutoff.
type VerificationSamplingCloseoutStage struct {
	service  *verificationapp.Service
	tenantID string
	limit    int
	initErr  error
	logger   *slog.Logger
}

func NewVerificationSamplingCloseoutStage(deps Deps, tenantID string) *VerificationSamplingCloseoutStage {
	limit := intEnv("GOATOS_VERIFICATION_SAMPLING_SETTLE_LIMIT", 100)
	if limit < 1 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	service := verificationapp.NewService(verificationpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout), nil)
	// The SHARED catalog, never a hand-copied subset. A category missing from this worker's
	// registry would not error -- it would silently never be settled, so its producer's records
	// would wait forever on a review the policy already decided nobody would do. See
	// verificationcatalog's package comment.
	var initErr error
	for _, def := range verificationcatalog.All() {
		if err := service.RegisterCategory(def); err != nil {
			initErr = errors.Join(initErr, err)
		}
	}
	return &VerificationSamplingCloseoutStage{service: service, tenantID: tenantID, limit: limit, initErr: initErr, logger: deps.Logger}
}

func (s *VerificationSamplingCloseoutStage) Name() string { return "verification-sampling-closeout" }

func (s *VerificationSamplingCloseoutStage) Run(ctx context.Context) error {
	if s.initErr != nil {
		// Fail loudly rather than settle a partial catalog: a half-registered worker would waive
		// some modules and silently stall the rest.
		return fmt.Errorf("verification sampling closeout: category catalog: %w", s.initErr)
	}
	if s.tenantID == "" {
		return errors.New("verification sampling closeout: tenant id is required")
	}
	settled, err := s.service.SettleUnsampledItems(ctx, s.tenantID, s.limit)
	if err != nil {
		return fmt.Errorf("verification sampling closeout: %w", err)
	}
	if s.logger != nil && settled > 0 {
		s.logger.Info("verification_sampling_closeout_stage_complete", "tenant_id", s.tenantID, "settled", settled)
	}
	return nil
}
