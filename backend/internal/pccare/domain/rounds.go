package domain

import "errors"

// PC CARE PLANS A ROUND (maintainer decision 2026-09-05).
//
// The planner ticks the pens once and the create makes ONE round holding one task per
// pen — the weighing campaign/campaign_sheds shape (see migration 000256 for the
// table-by-table read-across). The pen task is unchanged: it still owns its scanned
// animals, its own submit and its own verification item, because the review grain
// follows the EVIDENCE and the evidence is shot pen by pen.
//
// What the round adds is the thing that was missing: a record that these pens are one
// piece of planned work, so the operator gets one card to open instead of four
// unrelated ones, and so a deworming's feed & water removal can be one evening's job
// rather than one card per pen.

// MaxPensPerRound bounds a single create. A park's pen catalog is the natural ceiling
// (the planner list itself pages at 100), and an unbounded pens[] would let one request
// open an unbounded transaction. A create naming more pens than this is refused rather
// than silently truncated — truncation would plan work the CEO did not see planned.
const MaxPensPerRound = 100

var (
	// ErrNoPens is returned when a round create names no pen at all.
	ErrNoPens = errors.New("pccare: at least one pen is required")
	// ErrTooManyPens is returned when a round create names more than MaxPensPerRound pens.
	ErrTooManyPens = errors.New("pccare: too many pens for one round")
	// ErrDuplicatePen is returned when the same pen appears twice in one round create.
	// Two buckets for one pen-day cannot both be worked, and silently de-duplicating
	// would leave the planner believing they planned something they did not.
	ErrDuplicatePen = errors.New("pccare: the same pen is named twice in this round")
	// ErrRoundCategoryNotPlannable is returned when a round create names a category the
	// planner does not own (inventory_vaccine is per vaccine; feed_water_removal is born
	// inside a deworming create and gates a round rather than being one).
	ErrRoundCategoryNotPlannable = errors.New("pccare: this category is not planned as a round")
	// ErrRemovalPenNotInRound is returned when a removal proof names a pen the gated
	// round does not contain.
	ErrRemovalPenNotInRound = errors.New("pccare: this pen is not part of the gated round")
	// ErrVerificationPending is the CLOSE GATE (maintainer decision 2026-09-05, weighing's
	// ledger D-5 shape): a task cannot close while its evidence is awaiting a verdict, and
	// there is no caller-supplied way past it. If a task will not close, the answer is to
	// RESOLVE the verification — get the verdict — never to add a path around the gate.
	ErrVerificationPending = errors.New("pccare: this work is waiting for a video review")
	// ErrNotClosed is returned when a reopen names a task that is not closed. Reopen undoes a
	// CLOSE and nothing else: a completed task is accepted work and is never reopened this way.
	ErrNotClosed = errors.New("pccare: only a closed task can be reopened")
	// ErrCloseReasonRequired is returned when a close carries no reason. Whoever asks later why
	// a pen's work never happened is owed an answer in the closer's own words.
	ErrCloseReasonRequired = errors.New("pccare: a reason is required to close")
	// ErrRemovalProofIncomplete is returned when a removal submit leaves any pen without
	// BOTH its feed and its water video. One clip stretched over several pens proves
	// nothing, which is exactly why the evidence is per pen.
	ErrRemovalProofIncomplete = errors.New("pccare: some pens are missing their feed or water video")
)

// RoundPen is one pen named by a round create. The label is resolved server-side from
// the pen catalog; the client sends identity only.
type RoundPen struct {
	ShedID string
	// PartitionLabel is the pen ("2", "Part 3"); empty for an undivided shed.
	PartitionLabel string
}

// PenKey is the stable de-duplication key for a pen inside one round: the shed plus the
// NORMALIZED partition, because "Part 3", "part 3" and " Part 3 " are one pen and
// pc_care_tasks.partition_key stores exactly this normalization.
func (p RoundPen) PenKey() string {
	return p.ShedID + "|" + PartitionMatchKey(p.PartitionLabel)
}

// IsRoundPlannableCategory reports whether c may be planned as a round. This is the
// SAME set as PlannerCategories — a round is simply how the planner now plans — and is
// derived from it rather than restated, so a future planner category cannot be added to
// one list and forgotten in the other.
func IsRoundPlannableCategory(c string) bool {
	for _, planner := range PlannerCategories {
		if planner == c {
			return true
		}
	}
	return false
}

// ValidateRoundPens checks the pen list a round create names: at least one, no more than
// MaxPensPerRound, and no pen twice. It returns the de-duplication key set so the caller
// need not walk the list again.
func ValidateRoundPens(pens []RoundPen) error {
	if len(pens) == 0 {
		return ErrNoPens
	}
	if len(pens) > MaxPensPerRound {
		return ErrTooManyPens
	}
	seen := make(map[string]struct{}, len(pens))
	for _, pen := range pens {
		key := pen.PenKey()
		if _, dup := seen[key]; dup {
			return ErrDuplicatePen
		}
		seen[key] = struct{}{}
	}
	return nil
}

// RoundStatusRollup reduces a round's pen statuses to the ONE status its card shows.
// The rule reads the way the farm reads it: a round is finished only when every pen is
// finished, and until then the card says what is still OWED rather than what is already
// done.
//
// The order is deliberate, and the last two are the ones a later author will be tempted
// to swap. 'rework' outranks everything below it because a bounced pen is work somebody
// must redo tonight. 'open' outranks 'pending_verification' because a round holding one
// unworked pen beside three submitted ones is NOT awaiting a verdict — it is awaiting an
// operator, and a card reading "in review" would tell them their evening is finished
// when a pen is still full. This is the vaccination-progress trap one grain up: never let
// a roll-up report work as done while any of it is still owed.
func RoundStatusRollup(penStatuses []string) string {
	if len(penStatuses) == 0 {
		return StatusOpen
	}
	completed := 0
	anyRework := false
	anyOpen := false
	for _, s := range penStatuses {
		switch s {
		case StatusCompleted:
			completed++
		case StatusRework:
			anyRework = true
		case StatusOpen:
			anyOpen = true
		}
	}
	switch {
	case completed == len(penStatuses):
		return StatusCompleted
	case anyRework:
		return StatusRework
	case anyOpen:
		return StatusOpen
	default:
		return StatusPendingVerification
	}
}

// RoundWorkStateRollup reduces a round's pen WORK STATES to the one its card shows. This is a
// different question from RoundStatusRollup: status answers "where is the evidence", work state
// answers "is this work still owed". A card that reads its status alone cannot tell ENDED work
// from open work — closing a round leaves each pen's status untouched at 'open' and moves only
// its work_state, so the chip said "Open" on a round somebody had deliberately ended.
//
// Returns "" when the round is still live, so the caller falls back to the status roll-up.
func RoundWorkStateRollup(penWorkStates []string) string {
	if len(penWorkStates) == 0 {
		return ""
	}
	closed, completed := 0, 0
	for _, state := range penWorkStates {
		switch state {
		case WorkStateClosed:
			closed++
		case WorkStateCompleted:
			completed++
		}
	}
	switch {
	case closed == len(penWorkStates):
		return WorkStateClosed
	// Mixed terminal pens read as COMPLETED: some of the work was done and accepted, and
	// "ended" would erase that. A round is only ENDED when nothing in it was finished.
	case closed+completed == len(penWorkStates):
		return WorkStateCompleted
	default:
		return ""
	}
}
