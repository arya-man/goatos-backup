package kernelstages

import (
	"context"
	"errors"
	"fmt"

	vaccexecpg "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/postgres"
)

const defaultVaccinationScheduleProjectionDirtyLimit = 200

// VaccinationScheduleProjectionStage refreshes dirty materialized Full Schedule
// windows off the request path. Deploy-time horizon prebuild remains owned by
// backend/cmd/vaccination-schedule-projection-recompute.
type VaccinationScheduleProjectionStage struct {
	repo        *vaccexecpg.Repository
	tenantID    string
	dirtyLimit  int
	generatedBy string
	deps        Deps
}

// NewVaccinationScheduleProjectionStage builds the dirty-window projector stage.
func NewVaccinationScheduleProjectionStage(deps Deps, tenantID string) *VaccinationScheduleProjectionStage {
	limit := intEnv("GOATOS_VACCINATION_SCHEDULE_PROJECTION_DIRTY_LIMIT", defaultVaccinationScheduleProjectionDirtyLimit)
	if limit <= 0 || limit > 500 {
		limit = defaultVaccinationScheduleProjectionDirtyLimit
	}
	return &VaccinationScheduleProjectionStage{
		repo:        vaccexecpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout),
		tenantID:    tenantID,
		dirtyLimit:  limit,
		generatedBy: "kernel-worker.vaccination-schedule-projection",
		deps:        deps,
	}
}

// Name implements worker.StageRunner.
func (s *VaccinationScheduleProjectionStage) Name() string {
	return "vaccination-schedule-projection"
}

// Run rebuilds a bounded batch of dirty schedule windows.
func (s *VaccinationScheduleProjectionStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return errors.New("vaccination schedule projection: tenant id is required")
	}
	summary, err := s.repo.RebuildDirtyVaccinationScheduleWindows(ctx, s.tenantID, s.dirtyLimit, s.generatedBy)
	if err != nil {
		return fmt.Errorf("rebuild dirty vaccination schedule windows: %w", err)
	}
	if s.deps.Logger != nil {
		s.deps.Logger.Info("vaccination_schedule_projection_stage_complete",
			"tenant_id", s.tenantID,
			"dirty_limit", s.dirtyLimit,
			"windows", summary.Windows,
			"rows", summary.Rows,
		)
	}
	return nil
}
