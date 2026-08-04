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

// WeighingReworkDigestStage batches the weighing rework push PER SHED.
//
// A rework verdict is applied by the durable-bus consumer one observation at a time, and
// nothing in the product marks "the verifier finished reviewing this shed" -- there is no
// batch verdict endpoint and no shed-level review action. So the grouping moment has to be
// invented, and this stage is where: it debounces a bucket's un-delivered bounces and emits
// ONE weighing.observation.rework_digest naming the animals, which the notification bridge
// turns into a single push instead of one per rejected animal.
//
// Same lane, same shape, no new worker binary: this sits on the consolidated kernel worker's
// OPERATIONAL 5-minute cadence next to WeighingKernelStage. The lane choice matters for the
// accepted failure mode -- an operator learns of a bounce up to one tick plus the quiet
// window late, which is minutes, against a trip back to a shed that takes longer than that.
//
// Nothing is dropped by the debounce. A rejection that lands after its shed's digest already
// fired is simply another un-stamped row, and the next tick flushes it as its own smaller
// digest. The starvation cap (MaxAge) covers the opposite case: a verifier who keeps
// rejecting steadily never lets the bucket go quiet, so the oldest bounce forces a flush.
type WeighingReworkDigestStage struct {
	store       ports.WeighingReworkDigestStore
	tenantID    string
	quietWindow time.Duration
	maxAge      time.Duration
	namedLimit  int
	chunkSize   int
	maxChunks   int
	logger      *slog.Logger
	now         func() time.Time
}

// NewWeighingReworkDigestStage builds the per-shed rework digest cadence stage.
func NewWeighingReworkDigestStage(deps Deps, tenantID string) *WeighingReworkDigestStage {
	quiet := durationEnv("GOATOS_WEIGHING_REWORK_DIGEST_QUIET", 3*time.Minute)
	maxAge := durationEnv("GOATOS_WEIGHING_REWORK_DIGEST_MAX_AGE", 20*time.Minute)
	if maxAge < quiet {
		maxAge = quiet
	}
	// A notification body must never render fifty tags. This is how many animals the push
	// NAMES; the rest are honestly summarised as "and N more".
	named := intEnv("GOATOS_WEIGHING_REWORK_DIGEST_NAMED_LIMIT", 3)
	if named < 1 || named > 10 {
		named = 3
	}
	chunk := intEnv("GOATOS_WEIGHING_REWORK_DIGEST_CHUNK_SIZE", 50)
	if chunk < 1 || chunk > 1000 {
		chunk = 50
	}
	maxChunks := intEnv("GOATOS_WEIGHING_REWORK_DIGEST_MAX_CHUNKS", 20)
	if maxChunks < 1 || maxChunks > 500 {
		maxChunks = 20
	}
	return &WeighingReworkDigestStage{
		store:       postgres.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout),
		tenantID:    strings.TrimSpace(tenantID),
		quietWindow: quiet,
		maxAge:      maxAge,
		namedLimit:  named,
		chunkSize:   chunk,
		maxChunks:   maxChunks,
		logger:      deps.Logger,
		now:         time.Now,
	}
}

// Name implements worker.StageRunner.
func (s *WeighingReworkDigestStage) Name() string { return "weighing-rework-digest" }

// Run performs one bounded per-shed rework digest tick.
func (s *WeighingReworkDigestStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("weighing rework digest: tenant id is required")
	}
	result, err := s.store.SweepReworkDigests(ctx, domain.ReworkDigestSweepParams{
		TenantID:    s.tenantID,
		AsOf:        s.now(),
		QuietWindow: s.quietWindow,
		MaxAge:      s.maxAge,
		NamedLimit:  s.namedLimit,
		ChunkSize:   s.chunkSize,
		MaxChunks:   s.maxChunks,
	})
	if err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.Info("weighing_rework_digest_stage_complete",
			"digests_emitted", result.DigestsEmitted,
			"observations_named", result.ObservationsNamed,
			"truncated", result.Truncated,
		)
	}
	return nil
}

// withClock / withStore exist for the stage wiring tests.
func (s *WeighingReworkDigestStage) withClock(now func() time.Time) *WeighingReworkDigestStage {
	s.now = now
	return s
}

func (s *WeighingReworkDigestStage) withStore(store ports.WeighingReworkDigestStore) *WeighingReworkDigestStage {
	s.store = store
	return s
}
