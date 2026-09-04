package kernelstages

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	pccarepg "github.com/vgoats/goatos/backend/internal/pccare/adapters/postgres"
)

// PcCareKernelStage is the PC Care roll-forward cadence inside the consolidated kernel worker
// (weighing kernel stage twin, minimal form): an unfinished PC Care task whose due date has
// passed rolls forward to today as 'delayed', so the operator's worklist keeps carrying it and
// the delay is visible. Registered on the OPERATIONAL 5-minute lane for the same reason the
// weighing kernel is: day-boundary surfacing must land within minutes of the Asia/Kolkata
// business-day boundary.
type PcCareKernelStage struct {
	store     *pccarepg.Repository
	tenantID  string
	chunkSize int
	maxChunks int
	logger    *slog.Logger
	now       func() time.Time
}

// NewPcCareKernelStage builds the PC Care kernel cadence stage.
func NewPcCareKernelStage(deps Deps, tenantID string) *PcCareKernelStage {
	chunk := intEnv("GOATOS_PC_CARE_KERNEL_CHUNK_SIZE", 200)
	if chunk < 1 || chunk > 5000 {
		chunk = 200
	}
	maxChunks := intEnv("GOATOS_PC_CARE_KERNEL_MAX_CHUNKS", 50)
	if maxChunks < 1 || maxChunks > 1000 {
		maxChunks = 50
	}
	return &PcCareKernelStage{
		store:     pccarepg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout),
		tenantID:  strings.TrimSpace(tenantID),
		chunkSize: chunk,
		maxChunks: maxChunks,
		logger:    deps.Logger,
		now:       time.Now,
	}
}

// Name implements worker.StageRunner.
func (s *PcCareKernelStage) Name() string { return "pc-care-kernel" }

// Run performs one bounded PC Care roll-forward tick.
func (s *PcCareKernelStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("pc care kernel: tenant id is required")
	}
	result, err := s.store.SweepTaskRollForward(ctx, s.tenantID, s.now(), s.chunkSize, s.maxChunks)
	if err != nil {
		return err
	}
	if result.RolledForward > 0 || result.HeldForRemoval > 0 || result.Truncated {
		s.logger.Info("pc care kernel tick",
			"rolled_forward", result.RolledForward,
			"held_for_removal", result.HeldForRemoval,
			"truncated", result.Truncated,
		)
	}
	return nil
}
