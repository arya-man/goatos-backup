package kernelstages

import (
	"context"
	"fmt"
	"strings"
	"time"

	growthdirectorpg "github.com/vgoats/goatos/backend/internal/growthdirector/adapters/postgres"
)

// GrowthFCRRollupStage keeps the FCR feed-day rollup (migration 000418) current off the request
// path: every tick it drains the dirty log (parks whose feed sheet or feed loads changed), and
// once per business day it reconciles every park against a fresh recompute, re-marking any park
// whose stored rows drifted (a writer that bypassed the triggers) so the next drain repairs it.
// The FCR read path still refreshes the parks it is about to read, so this stage is what keeps
// that first read fast, not what makes it correct. Housekeeping cadence.
type GrowthFCRRollupStage struct {
	repo     *growthdirectorpg.Repository
	tenantID string
	deps     Deps
}

// NewGrowthFCRRollupStage builds the stage. An empty tenant id covers all tenants.
func NewGrowthFCRRollupStage(deps Deps, tenantID string) *GrowthFCRRollupStage {
	return &GrowthFCRRollupStage{
		repo:     growthdirectorpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout),
		tenantID: strings.TrimSpace(tenantID),
		deps:     deps,
	}
}

// Name implements worker.StageRunner.
func (s *GrowthFCRRollupStage) Name() string { return "growth-fcr-rollup" }

// Run drains the dirty log, then runs the daily reconcile (a no-op after the first run of the day).
func (s *GrowthFCRRollupStage) Run(ctx context.Context) error {
	refreshed, err := s.repo.RefreshFCRRollup(ctx, s.tenantID)
	if err != nil {
		return fmt.Errorf("growth fcr rollup refresh: %w", err)
	}
	drift, err := s.repo.ReconcileFCRRollup(ctx, s.tenantID, time.Now())
	if err != nil {
		return fmt.Errorf("growth fcr rollup reconcile: %w", err)
	}
	if drift > 0 {
		// Re-marked dirty by the reconcile: repair now rather than on the next tick.
		if _, err := s.repo.RefreshFCRRollup(ctx, s.tenantID); err != nil {
			return fmt.Errorf("growth fcr rollup repair: %w", err)
		}
	}
	if s.deps.Logger != nil {
		level := s.deps.Logger.Info
		if drift > 0 {
			level = s.deps.Logger.Warn
		}
		level("growth_fcr_rollup_stage_complete", "parks_refreshed", refreshed, "reconcile_drift_rows", drift)
	}
	return nil
}
