package app

import (
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// PlanSessions splits a shed's open vaccination CELLS across consecutive days at the daily cap. It is the
// single source of truth for the shed-detail per-day plan; shedSummaryCanonicalReadSQL
// (adapters/postgres/repository.go) mirrors its sessions-count and capacity classification in SQL so the
// shed-list row and the re-planned detail page agree.
//
//	sessions    = ceil(cells / cap)                 (0 when there are no open cells)
//	allowedDays = maxBufferDays + 1                  (the due day plus the buffer)
//	capacity    = within_cap (<=1 session) | over_cap (<=allowedDays) | capacity_breach (>allowedDays)
//	per-day     = cap vaccinations (last day the remainder); a day beyond allowedDays is capacity_breach
//
// start is the shed's next-due instant; session dates are consecutive Asia/Kolkata calendar days from it.
//
// Config is validate-or-reject (AGENTS.md): an out-of-range capacity value is REJECTED with an error, never
// silently coerced to a default the author never entered. cfg is authored config already validated at
// publish time (protocol publish rejects max_per_day < 1 / max_buffer_days < 0); this is the fail-loud
// backstop for any invalid value that still reaches the planner.
func PlanSessions(cells int, cfg domain.CapacityConfig, start time.Time) (int, domain.CapacityStatus, []domain.PlannedSession, error) {
	if cfg.MaxPerDay < 1 {
		return 0, "", nil, fmt.Errorf("invalid capacity config: max_per_day must be >= 1, got %d", cfg.MaxPerDay)
	}
	if cfg.MaxBufferDays < 0 {
		return 0, "", nil, fmt.Errorf("invalid capacity config: max_buffer_days must be >= 0, got %d", cfg.MaxBufferDays)
	}
	capPerDay := cfg.MaxPerDay
	if cells <= 0 {
		return 0, domain.CapacityWithinCap, []domain.PlannedSession{}, nil
	}
	sessions := (cells + capPerDay - 1) / capPerDay // ceil
	allowedDays := cfg.MaxBufferDays + 1

	status := domain.CapacityWithinCap
	switch {
	case sessions <= 1:
		status = domain.CapacityWithinCap
	case sessions <= allowedDays:
		status = domain.CapacityOverCap
	default:
		status = domain.CapacityBreach
	}

	if start.IsZero() {
		start = time.Now().In(biztime.DefaultLocation())
	}
	planned := make([]domain.PlannedSession, 0, sessions)
	remaining := cells
	for i := 0; i < sessions; i++ {
		vax := capPerDay
		if remaining < capPerDay {
			vax = remaining
		}
		remaining -= vax
		perDay := domain.CapacityWithinCap
		if i >= allowedDays {
			perDay = domain.CapacityBreach
		}
		planned = append(planned, domain.PlannedSession{
			Date:         biztime.BusinessDate(start.AddDate(0, 0, i)),
			Vaccinations: vax,
			DailyLimit:   capPerDay,
			Capacity:     perDay,
		})
	}
	return sessions, status, planned, nil
}
