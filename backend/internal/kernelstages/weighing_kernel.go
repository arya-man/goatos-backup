package kernelstages

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/weighing/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// WeighingKernelStage is the weighing PHASE 2 cadence inside the consolidated
// 5k-50k kernel worker. There is NO new worker binary: this is a thin
// StageRunner wrapper around the weighing repository's bounded sweep, registered
// on the worker's OPERATIONAL cadence class (5-minute lane) alongside the other
// light operational stages.
//
// Why the operational lane and not generation/housekeeping:
//   - it is not obligation GENERATION from protocol rules (hourly lane);
//   - it is not retention/cleanup (housekeeping lane);
//   - it is the same shape as the feed-transport day-task/reminder cadence
//     already on the operational lane: bounded per-tick claim work that must be
//     surfaced to operators within minutes of a business-day boundary, and whose
//     escalation must not wait an hour.
//
// The 5-minute cadence is what makes day-start surfacing land shortly after the
// Asia/Kolkata business-day boundary without any cron job.
type WeighingKernelStage struct {
	store     ports.WeighingKernelStore
	tenantID  string
	chunkSize int
	maxChunks int
	logger    *slog.Logger
	now       func() time.Time
}

// NewWeighingKernelStage builds the weighing kernel cadence stage.
func NewWeighingKernelStage(deps Deps, tenantID string) *WeighingKernelStage {
	chunk := intEnv("GOATOS_WEIGHING_KERNEL_CHUNK_SIZE", 200)
	if chunk < 1 || chunk > 5000 {
		chunk = 200
	}
	maxChunks := intEnv("GOATOS_WEIGHING_KERNEL_MAX_CHUNKS", 50)
	if maxChunks < 1 || maxChunks > 1000 {
		maxChunks = 50
	}
	return &WeighingKernelStage{
		store:     postgres.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout),
		tenantID:  strings.TrimSpace(tenantID),
		chunkSize: chunk,
		maxChunks: maxChunks,
		logger:    deps.Logger,
		now:       time.Now,
	}
}

// Name implements worker.StageRunner.
func (s *WeighingKernelStage) Name() string { return "weighing-kernel" }

// Run performs one bounded weighing kernel tick.
func (s *WeighingKernelStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("weighing kernel: tenant id is required")
	}
	result, err := s.store.SweepWorkItems(ctx, domain.KernelSweepParams{
		TenantID:  s.tenantID,
		AsOf:      s.now(),
		ChunkSize: s.chunkSize,
		MaxChunks: s.maxChunks,
	})
	if err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.Info("weighing_kernel_stage_complete",
			"business_date", result.BusinessDate,
			"reconciled_terminal", result.ReconciledTerminal,
			"rolled_forward", result.RolledForward,
			"marked_delayed", result.MarkedDelayed,
			"day_start_surfaced", result.DayStartSurfaced,
			"cadence_events", result.CadenceEvents,
			"truncated", result.Truncated,
		)
	}
	return nil
}

// withClock / withStore exist for the stage wiring tests.
func (s *WeighingKernelStage) withClock(now func() time.Time) *WeighingKernelStage {
	s.now = now
	return s
}

func (s *WeighingKernelStage) withStore(store ports.WeighingKernelStore) *WeighingKernelStage {
	s.store = store
	return s
}
