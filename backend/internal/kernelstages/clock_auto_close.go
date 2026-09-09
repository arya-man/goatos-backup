package kernelstages

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
)

// ClockAutoCloseStage clocks out the people who forgot (maintainer decision
// 2026-09-10, superseding the no-invented-hours half of the clock plan's D4).
// Every workforce_clock_entries row still open from a business day BEFORE
// today (IST) is closed at 23:59:59 of its own day with the hours counted to
// that instant and status auto_closed, so the row still says it was not the
// person's own punch. The instant written is the day's last second regardless
// of when the tick runs, so the hourly housekeeping cadence is enough.
type ClockAutoCloseStage struct {
	repo     *workforcepg.Repository
	tenantID string
	chunk    int
	maxLoops int
}

func NewClockAutoCloseStage(deps Deps, tenantID string) *ClockAutoCloseStage {
	chunk := intEnv("GOATOS_CLOCK_AUTO_CLOSE_CHUNK", 200)
	if chunk < 1 {
		chunk = 200
	}
	if chunk > 1000 {
		chunk = 1000
	}
	return &ClockAutoCloseStage{
		repo:     workforcepg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout),
		tenantID: tenantID,
		chunk:    chunk,
		maxLoops: 50,
	}
}

func (s *ClockAutoCloseStage) Name() string { return "clock-auto-close" }

func (s *ClockAutoCloseStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return errors.New("clock auto close: tenant id is required")
	}
	today := biztime.BusinessDate(time.Now())
	total := 0
	// Bounded loop: each chunk is its own short transaction and the loop stops
	// on the first empty chunk, so a tick can never run unbounded.
	for i := 0; i < s.maxLoops; i++ {
		closed, err := s.repo.AutoCloseStaleClockEntries(ctx, s.tenantID, today, s.chunk) // scale-guard:ignore: bounded chunk loop -- each call is ONE set-based UPDATE over up to `chunk` rows (SKIP LOCKED keyset), at most maxLoops chunks per tick; not a per-row round trip
		if err != nil {
			return fmt.Errorf("clock auto close: %w", err)
		}
		total += closed
		if closed < s.chunk {
			break
		}
	}
	return nil
}
