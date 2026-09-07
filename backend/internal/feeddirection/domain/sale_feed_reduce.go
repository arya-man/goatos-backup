package domain

import (
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Sale -> feed reduction day (maintainer decision 2026-09-07).
//
// When animals are TAGGED to a sale they leave the herd register, and the feed sheet follows the
// register: the next sheet issued or corrected after that instant counts the pen with fewer
// mouths. This file answers ONE question for the Feed Director's push -- "from which feed day
// should this pen's feed be smaller?" -- so the notification can name a date the farm can check
// rather than a vague "soon".
//
// THE RULE IS THE CLOCK'S, NOT THIS FILE'S. A sheet for feed day F is issued at direction_time on
// F-1 and may be AMENDED ONCE at correction_time on F-1 (the 14:00 afternoon correction). So a sale
// confirmed on business date D:
//
//	before D's correction cutoff  -> feed day D+1 (its sheet is corrected at D 14:00 with the new count)
//	at or after that cutoff       -> feed day D+2 (D+1's sheet is already frozen and loaded; the
//	                                 next morning's issue is the first to see the register)
//
// The cutoff comes from the park's own feed_schedule_config row (WorkflowClock), never a constant
// written here: a park that corrects at 13:30 and one that corrects at 14:00 get different answers
// from the same sale instant. With NO clock on record the rule falls back to D+1, which is the
// day the register is next read for a sheet at all; that fallback is stated in the decision doc
// and is deliberately the earlier date, so a director is told to check a day early rather than a
// day late.

// SaleFeedReductionClock picks the clock that governs a sale's feed reduction from a park's clocks:
// the NORMAL workflow's, because that is the herd ration the register drives. Nil when the park
// has no normal clock in force.
func SaleFeedReductionClock(clocks []WorkflowClock) *WorkflowClock {
	for i := range clocks {
		if strings.EqualFold(strings.TrimSpace(clocks[i].Workflow), WorkflowNormal) {
			return &clocks[i]
		}
	}
	return nil
}

// SaleFeedReductionDay returns the YYYY-MM-DD feed day from which a pen's feed should reflect a sale
// confirmed at allocatedAt, under the park's correction clock (nil -> D+1 fallback, see above).
func SaleFeedReductionDay(allocatedAt time.Time, clock *WorkflowClock) (string, error) {
	loc := biztime.DefaultLocation()
	saleDate := biztime.BusinessDate(allocatedAt)
	base, err := time.ParseInLocation("2006-01-02", saleDate, loc)
	if err != nil {
		return "", err
	}
	nextDay := base.AddDate(0, 0, 1).Format("2006-01-02")
	if clock == nil || strings.TrimSpace(clock.CorrectionTime) == "" {
		return nextDay, nil
	}
	// The correction cutoff FOR feed day D+1 is on D, at correction_time.
	cutoff, err := clock.ExpectedCorrectionInstant(nextDay)
	if err != nil {
		return "", err
	}
	if allocatedAt.Before(cutoff) {
		return nextDay, nil
	}
	return base.AddDate(0, 0, 2).Format("2006-01-02"), nil
}
