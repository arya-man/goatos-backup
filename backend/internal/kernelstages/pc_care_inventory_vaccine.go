package kernelstages

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	pccarepg "github.com/vgoats/goatos/backend/internal/pccare/adapters/postgres"
)

// PcCareInventoryVaccineStage creates fridge-stock proof tasks for scheduled vaccination drives.
// It runs periodically because direct DB edits may not emit domain events.
type PcCareInventoryVaccineStage struct {
	store    *pccarepg.Repository
	tenantID string
	logger   *slog.Logger
	now      func() time.Time
}

// NewPcCareInventoryVaccineStage builds the inventory-vaccine task reconciler.
func NewPcCareInventoryVaccineStage(deps Deps, tenantID string) *PcCareInventoryVaccineStage {
	return &PcCareInventoryVaccineStage{
		store:    pccarepg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout),
		tenantID: strings.TrimSpace(tenantID),
		logger:   deps.Logger,
		now:      time.Now,
	}
}

// Name implements worker.StageRunner.
func (s *PcCareInventoryVaccineStage) Name() string { return "pc-care-inventory-vaccine" }

// Run performs one idempotent reconciliation pass.
func (s *PcCareInventoryVaccineStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return errors.New("pc care inventory vaccine: tenant id is required")
	}
	result, err := s.store.ReconcileInventoryVaccineTasks(ctx, s.tenantID, s.now())
	if err != nil {
		return err
	}
	// A park that needs a stock task but has no vaccination operator is a park-config gap:
	// its task is deliberately NOT created (never a fallback assignee), so shout about it on
	// every pass until someone fixes the roster/duty config.
	if s.logger != nil && result.ParksMissingOperators > 0 {
		s.logger.Error("pc_care_inventory_vaccine_parks_missing_operators",
			"tenant_id", s.tenantID,
			"parks_missing_operators", result.ParksMissingOperators,
		)
	}
	if s.logger != nil && (result.TasksCreated > 0 || result.AssigneesInserted > 0 || result.RequirementsUpserted > 0 || result.DirectorAssigneesRemoved > 0) {
		s.logger.Info("pc_care_inventory_vaccine_stage_complete",
			"tenant_id", s.tenantID,
			"tasks_created", result.TasksCreated,
			"assignees_inserted", result.AssigneesInserted,
			"requirements_upserted", result.RequirementsUpserted,
			"director_assignees_removed", result.DirectorAssigneesRemoved,
		)
	}
	return nil
}
