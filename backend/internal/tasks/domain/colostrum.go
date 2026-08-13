package domain

import (
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// The Colostrum LENS (docs/decisions/colostrum-milk-module.md).
//
// Colostrum is NOT a workflow, a module row, or a second copy of any state. Every feed it renders
// is an existing `workflow_actions` row on a `birth_kid` workflow: the immediate 1st Colostrum plus
// the birth-time-derived 07:00/11:00/15:00/18:30/22:00 series (TemplateBirthKidAt). Completing a
// feed from the Colostrum page writes THE SAME ROW, through the same answer/complete endpoints, as
// completing it from Birth — one state, two entry points. There is deliberately no colostrum table,
// no colostrum event, and no mirrored completion.
//
// What the lens changes is only WHICH rows are on screen and HOW they are counted:
//
//	Birth card    -> keyed on workflow_instances.event_date, counters over ALL operator actions
//	Colostrum card -> keyed on the date each FEED is due, counters over THAT DAY's feeds only
//
// Both differences are load-bearing. A kid born on 5 Aug has feeds on 6 Aug, so an event_date filter
// would hide it on the 6th; and a whole-series denominator would count iodine dipping, weight and
// tagging as colostrum work. Maintainer decision 2026-08-06: a card shows that day's feeds only —
// tomorrow's progress is seen tomorrow.
const (
	// ModuleColostrum is a LENS keyword accepted by GET /app/workflows?module=... It is NOT a value
	// of workflow_instances.module (which stays 'birth'), so do not go looking for a 'colostrum' row
	// in that table. The handler branches on it into the colostrum-day read.
	ModuleColostrum = "colostrum"
)

// IsColostrumAction reports whether one action row is a colostrum feed. Two shapes qualify: every
// scheduled session row, and the immediate 1st Colostrum — which lives in SectionMain (it is part
// of the delivery sequence) but IS a colostrum feed and is numbered "1st" in the series the operator
// sees. This predicate is the single definition; the SQL WHERE clause mirrors it exactly.
func IsColostrumAction(section, actionKey string) bool {
	return section == SectionColostrumSession || actionKey == ActionKeyFirstColostrum
}

// ColostrumDayWindow resolves an Asia/Kolkata business date (YYYY-MM-DD) to the half-open instant
// range [start, end) that selects that day's feeds.
//
// The range shape is the point: the query filters `due_at >= start AND due_at < end`, which is
// SARGable and uses an ordinary index. Do NOT rewrite it as
// `(due_at AT TIME ZONE 'Asia/Kolkata')::date = $1` — that expression is STABLE rather than
// IMMUTABLE, cannot be indexed, and puts a function on the indexed column (the column-side-cast
// anti-pattern in docs/decisions/scale-anti-patterns.md).
func ColostrumDayWindow(date string) (start, end time.Time, err error) {
	parsed, err := time.ParseInLocation("2006-01-02", date, biztime.DefaultLocation())
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("tasks: colostrum day %q: %w", date, err)
	}
	start = biztime.BusinessDayStart(parsed)
	return start, start.AddDate(0, 0, 1), nil
}

// ColostrumDaySummary is one kid's colostrum standing for ONE business date: how many feeds that
// date holds, how many are done, and which feed is next. It is the card's whole numeric content.
type ColostrumDaySummary struct {
	// Total is the feeds due on this date. Canceled rows never count.
	Total int
	// Done is the feeds completed. A feed sent back for a re-shoot (rework) is NOT done.
	Done int
	// Next is the first incomplete feed by seq, or nil when the day is finished.
	Next *WorkflowNextAction
}

// Complete reports a finished day: every feed due on this date is done. It is deliberately
// `Total > 0 && Done == Total` — a kid with no feeds on this date has no card at all, rather than
// an empty card that reads as "finished".
func (s ColostrumDaySummary) Complete() bool { return s.Total > 0 && s.Done == s.Total }

// ColostrumDayCard is the PURE SPEC of one colostrum card, mirrored by the aggregate in
// postgres.ListColostrumDay. Both are asserted against the same fixtures so the SQL cannot drift
// from this function (the same producer/consumer pairing ResolveShiftingDestinationStage uses).
//
// Inputs are one workflow's action rows — any sections, any dates — plus the day window. Filtering
// happens here so the rule lives in one place.
func ColostrumDayCard(actions []WorkflowAction, start, end, now time.Time) ColostrumDaySummary {
	var summary ColostrumDaySummary
	var next *WorkflowAction
	for i := range actions {
		a := actions[i]
		if !IsColostrumAction(a.Section, a.ActionKey) || a.Status == ActionStatusCanceled {
			continue
		}
		if a.DueAt == nil || a.DueAt.Before(start) || !a.DueAt.Before(end) {
			continue
		}
		summary.Total++
		if a.Status == ActionStatusCompleted {
			summary.Done++
			continue
		}
		// in_review counts as outstanding operator work here for the same reason rework does: the
		// operator's next tap is still on this row.
		if next == nil || a.Seq < next.Seq {
			next = &actions[i]
		}
	}
	if next != nil {
		summary.Next = &WorkflowNextAction{
			Key:     next.ActionKey,
			Title:   next.Title,
			DueAt:   next.DueAt,
			Overdue: next.DueAt != nil && next.DueAt.Before(now),
		}
	}
	return summary
}

// ColostrumDayQuery is the colostrum list read input. Date is the Asia/Kolkata business date on
// screen; TodayDate anchors the previous-day attention bell.
type ColostrumDayQuery struct {
	TenantID  string
	Date      string // YYYY-MM-DD
	TodayDate string // current Asia/Kolkata business date
	Filter    string
	PageSize  int
	Cursor    *WorkflowCursor
	Now       time.Time
}

// ColostrumFilterAllowed reports whether a filter key applies to the colostrum lens.
//
// FilterAwaitingVideo is deliberately absent. Verification is enqueued at WHOLE-WORKFLOW grain —
// one item once every task for a kid is complete (docs/decisions/birth-death-workflows.md) — so a
// single day's feeds can never occupy that bucket. Offering the chip would show a permanent zero
// and imply a state the grain cannot produce.
func ColostrumFilterAllowed(filter string) bool {
	switch filter {
	case "", FilterAll, FilterOverdue, FilterDue, FilterCompleted:
		return true
	}
	return false
}
