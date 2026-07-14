package kernelstages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
)

// InventoryBatchReconcilerStage releases excess reserved stock for obligation
// batches marked stock_reconcile_required, reusing
// inventoryapp.Service.ReleaseBatchReconcileRemainders — the same code path as
// the inventory-batch-reconciler one-shot. Operational (15m) cadence.
type InventoryBatchReconcilerStage struct {
	service  *inventoryapp.Service
	tenantID string
	limit    int
	logger   *slog.Logger
}

// NewInventoryBatchReconcilerStage builds the reconciler stage.
func NewInventoryBatchReconcilerStage(deps Deps, tenantID string) *InventoryBatchReconcilerStage {
	limit := intEnv("GOATOS_INVENTORY_BATCH_RECONCILE_LIMIT", 1000)
	if limit <= 0 {
		limit = 1000
	}
	if limit > 5000 {
		limit = 5000
	}
	service := inventoryapp.NewService(inventorypg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	return &InventoryBatchReconcilerStage{service: service, tenantID: tenantID, limit: limit, logger: deps.Logger}
}

// Name implements worker.StageRunner.
func (s *InventoryBatchReconcilerStage) Name() string { return "inventory-batch-reconciler" }

// Run reconciles batch reservation remainders for the tenant.
func (s *InventoryBatchReconcilerStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return errors.New("inventory batch reconciler: tenant id is required")
	}
	summary, err := s.service.ReleaseBatchReconcileRemainders(ctx, s.tenantID, s.limit)
	if err != nil {
		return fmt.Errorf("inventory batch reconcile: %w", err)
	}
	if s.logger != nil {
		s.logger.Info("inventory_batch_reconcile_stage_complete",
			"tenant_id", s.tenantID,
			"batches", summary.Batches,
			"movements", summary.Movements,
			"released", summary.Released,
		)
	}
	return nil
}
