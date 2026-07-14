package kernelstages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// VaccinationGenerationStage idempotently generates/rechecks effective
// vaccination obligations for the in-care cohort, reusing
// vaccinationapp.GenerationService.GenerateEffectiveForAllGoats — the same code
// path as the generate-vaccination-obligations one-shot (default, effective
// per-goat resolution). Hourly generation cadence; event-triggered generation
// still runs through the domain-consumer handlers. Recovery-repair remains a
// manual one-shot concern and is intentionally not folded into the scheduled
// stage.
type VaccinationGenerationStage struct {
	gen      *vaccinationapp.GenerationService
	tenantID string
	logger   *slog.Logger
}

// NewVaccinationGenerationStage builds the generation stage.
func NewVaccinationGenerationStage(deps Deps, tenantID string) *VaccinationGenerationStage {
	protocolRepo := protocolpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	obligationRepo := obligationpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	vaccinationRepo := vaccinationpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	gen := vaccinationapp.NewGenerationService(protocolRepo, vaccinationRepo, obligationRepo)
	return &VaccinationGenerationStage{gen: gen, tenantID: tenantID, logger: deps.Logger}
}

// Name implements worker.StageRunner.
func (s *VaccinationGenerationStage) Name() string { return "vaccination-generation" }

// Run generates effective obligations for the current India business day.
func (s *VaccinationGenerationStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return errors.New("vaccination generation: tenant id is required")
	}
	asOf := biztime.BusinessDayStart(time.Now())
	res, err := s.gen.GenerateEffectiveForAllGoats(ctx, s.tenantID, asOf)
	if err != nil && !vaccinationapp.IsGenerationPartialFailure(err) {
		return fmt.Errorf("generate effective cohort: %w", err)
	}
	if s.logger != nil {
		s.logger.Info("vaccination_generation_stage_complete",
			"tenant_id", s.tenantID,
			"generated", res.Generated,
			"deferred", res.Deferred,
			"reopened", res.Reopened,
			"failed_goats", res.FailedGoats,
			"skipped_no_due_date", res.SkippedNoDueDate,
			"suppressed_trusted", res.SuppressedByTrustedHistory,
		)
	}
	// A partial failure (some goats failed) is surfaced so the supervisor logs it,
	// but it must not abort the whole stage/worker: the successful goats committed.
	if err != nil {
		return fmt.Errorf("generate effective cohort (partial): %w", err)
	}
	return nil
}
