package kernelstages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/goatcreatedrecovery"
)

// GoatCreatedRecoveryStage is the SCHEDULED reconciliation for a lost
// `goat.created` event (BUG-016).
//
// `goat.created` is the sole SM-1 trigger for vaccination obligation generation
// (backend/internal/vaccination/app/generation_handler.go). If that write is
// lost, the goat silently receives NO vaccination obligations for an unbounded
// time — and exactly the newly created/procured animals that most need prompt
// PHC vaccination are affected. Until now the only repair was a human noticing
// and running `backfill-goat-created` by hand; the CLI had zero non-dev call
// sites. This stage runs the SAME code path (goatcreatedrecovery, shared with
// that CLI) on the kernel worker's housekeeping cadence.
//
// Behaviour:
//   - always ALERTS when candidates > 0 (a warn-level structured log carrying
//     the count and a sample of goat ids, which is what the log-based alert
//     policies in infra/observability consume);
//   - repairs by default, because leaving a live goat with zero vaccination
//     obligations is the more dangerous state. Set
//     GOATOS_GOAT_CREATED_RECOVERY_MODE=detect for alert-only (no writes).
//
// Bounded: one page of at most GOATOS_GOAT_CREATED_RECOVERY_LIMIT (default 500)
// goats per run, ordered deterministically; repaired rows leave the candidate
// set, so successive runs make forward progress.
type GoatCreatedRecoveryStage struct {
	pool       *pgxpool.Pool
	tenantID   string
	limit      int
	detectOnly bool
	logger     *slog.Logger
}

// NewGoatCreatedRecoveryStage builds the scheduled goat.created reconciliation.
func NewGoatCreatedRecoveryStage(deps Deps, tenantID string) *GoatCreatedRecoveryStage {
	limit := intEnv("GOATOS_GOAT_CREATED_RECOVERY_LIMIT", 500)
	if limit < 1 || limit > 5000 {
		limit = 500
	}
	return &GoatCreatedRecoveryStage{
		pool:       deps.Pool,
		tenantID:   strings.TrimSpace(tenantID),
		limit:      limit,
		detectOnly: strings.EqualFold(getenv("GOATOS_GOAT_CREATED_RECOVERY_MODE"), "detect"),
		logger:     deps.Logger,
	}
}

// Name implements worker.StageRunner.
func (s *GoatCreatedRecoveryStage) Name() string { return "goat-created-recovery" }

// Run scans for live goats with no goat.created event, alerts on any gap, and
// (unless detect-only) republishes the canonical event through the shared
// production path.
func (s *GoatCreatedRecoveryStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return errors.New("goat.created recovery: tenant id is required")
	}
	candidates, err := goatcreatedrecovery.ScanCandidates(ctx, s.pool, s.tenantID, "", s.limit)
	if err != nil {
		return fmt.Errorf("scan goat.created gaps: %w", err)
	}
	if len(candidates) == 0 {
		if s.logger != nil {
			s.logger.Debug("goat_created_recovery_stage_clean", "tenant_id", s.tenantID)
		}
		return nil
	}

	// ALERT: a missing SM-1 trigger is an operational incident, not routine
	// housekeeping. Log a bounded sample, never one line per goat.
	sample := make([]string, 0, 5)
	for _, c := range candidates {
		if len(sample) == 5 {
			break
		}
		sample = append(sample, c.GoatID)
	}
	if s.logger != nil {
		s.logger.Warn("goat_created_missing_detected",
			"tenant_id", s.tenantID,
			"candidates", len(candidates),
			"sample_goat_ids", strings.Join(sample, ","),
			"page_full", len(candidates) == s.limit,
			"mode", s.mode(),
		)
	}
	if s.detectOnly {
		return nil
	}

	applied, err := goatcreatedrecovery.Recover(ctx, s.pool, candidates)
	if s.logger != nil {
		s.logger.Warn("goat_created_recovery_applied",
			"tenant_id", s.tenantID,
			"candidates", len(candidates),
			"repaired", applied,
		)
	}
	if err != nil {
		return fmt.Errorf("recover goat.created: %w", err)
	}
	return nil
}

func (s *GoatCreatedRecoveryStage) mode() string {
	if s.detectOnly {
		return "detect"
	}
	return "repair"
}
