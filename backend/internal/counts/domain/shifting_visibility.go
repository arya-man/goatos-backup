package domain

import (
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Shifting Actions LEAD TIME (maintainer decision 2026-08-09).
//
// An approved movement does not always become the operator's work the instant it is approved. A
// HIGH-priority movement does -- it is the urgent case, and it carries its own feed evidence, so
// nothing has to be prepared for it in advance. A LOW-priority movement is planned work: the
// destination has to be fed, and the feed sheet has a packing day, so a low-priority movement
// raised before the afternoon cutoff makes the next day's plan and one raised after it does not.
//
// This binds the ACTIONS QUEUE ONLY. It deliberately does NOT change the feed-direction shifting
// projection, which by the 2026-07-27 maintainer decision has NO lead time and NO priority branch
// ("forget high priority") and counts every authorized-but-unexecuted movement immediately. The two
// rules look similar and are not: this one decides when an operator is SHOWN the work, that one
// decides how many mouths the destination shed is fed for. Do not collapse them.
//
// It is also NOT an authority gate. Completion is not blocked before the due date: the animals may
// genuinely have walked today, and refusing to record a movement that physically happened would
// make the herd register lie. Approval remains the only thing completion is gated on.

const (
	// ShiftingLeadCutoffHourIST / ShiftingLeadCutoffMinuteIST is the afternoon cutoff, in
	// Asia/Kolkata, that decides whether a low-priority movement makes the next day's plan.
	ShiftingLeadCutoffHourIST   = 13
	ShiftingLeadCutoffMinuteIST = 30

	// ShiftingPriorityHigh is the urgent movement: no lead time at all.
	ShiftingPriorityHigh = "high"
)

// ShiftingActionsDueFrom returns the instant an approved movement enters the operator's Actions
// work list.
//
//	high priority          -> raisedAt          (immediately, to the second)
//	low, raised  < 13:30   -> next day,      00:00 IST
//	low, raised >= 13:30   -> day after next, 00:00 IST
//
// The answer is anchored on the RAISE time, not on the approval time, so the due date a park head
// sees when they approve is the one the operator gets. A late approval needs no special case: if
// approval only lands after the due instant has already passed, `now` is past it too and the row is
// visible the moment it is approved, which is the "never hide approved work in the past" rule.
//
// The cutoff compares the raise time's Asia/Kolkata WALL CLOCK, because a Goat OS business day is an
// India business day -- a movement raised at 14:00 IST is after the cutoff regardless of what the
// stored UTC instant reads.
func ShiftingActionsDueFrom(priority string, raisedAt time.Time) time.Time {
	if strings.EqualFold(strings.TrimSpace(priority), ShiftingPriorityHigh) {
		return raisedAt
	}
	local := raisedAt.In(biztime.DefaultLocation())
	cutoff := time.Date(local.Year(), local.Month(), local.Day(),
		ShiftingLeadCutoffHourIST, ShiftingLeadCutoffMinuteIST, 0, 0, biztime.DefaultLocation())
	leadDays := 1
	if !local.Before(cutoff) {
		leadDays = 2
	}
	// Start of the due business day, so a movement becomes due at 00:00 IST rather than at the
	// hour it happened to be raised.
	return biztime.BusinessDayStart(local).AddDate(0, 0, leadDays)
}

// ShiftingActionsDue reports whether an approved movement's lead time has elapsed at now.
func ShiftingActionsDue(priority string, raisedAt, now time.Time) bool {
	return !now.Before(ShiftingActionsDueFrom(priority, raisedAt))
}
