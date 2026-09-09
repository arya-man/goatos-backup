package domain

import "math"

// THE VERIFIER IS WARNED, NOT TOLD (maintainer decision 2026-09-09).
//
// Feed packing verification is blind per-item entry (2026-08-21): the verifier types the packed
// weight she can read off the video for each feed item, and the planned quantity is deliberately
// withheld so a copied number cannot pass for a reading. That stays. What changes is that a reading
// which sits MORE THAN 500 g away from the plan is no longer accepted silently: the approve is
// refused once, the screen says only that the entry is more than 500 g ABOVE or BELOW the plan --
// never the planned figure, never the exact gap -- and asks her to check the video again. If she is
// sure, she confirms and presses Approve once more; the confirmation is recorded with the reading.
//
// This is a SECOND threshold beside PackingVarianceToleranceKg (0.2 kg) and they answer different
// questions. The 0.2 kg tolerance is leadership's: it decides which measured bags are worth a second
// look on the Feed Analytics execution view. This 0.5 kg tolerance is the verifier's own guard
// against a slip of the finger -- a 2 read as 12, a decimal in the wrong place -- and it is the only
// place the plan is allowed to influence her screen, and then only as a direction. Do not merge the
// two constants: tightening this one to 0.2 would have the verifier confirming most honest readings,
// and loosening that one to 0.5 would hide real short-packs from leadership.
const PackingEntryConfirmToleranceKg = 0.5

// packingEntryConfirmEpsilonKg absorbs binary-decimal noise on a difference the verifier typed to
// the gram: 2.7 - 2.2 is 0.5000000000000004 in float64 and must read as exactly the tolerance, not a
// breach of it. One microgram is far below anything a hand-entered kg value can express.
const packingEntryConfirmEpsilonKg = 1e-6

// The DIRECTION the screen is allowed to reveal. These are the wire codes on the refusal's field
// errors (`measurement.entries.<key>`), rendered by each client through backend-owned copy.
const (
	PackingEntryAbovePlan = "above_plan"
	PackingEntryBelowPlan = "below_plan"
)

// PackingEntryVariance reports whether one verifier reading sits more than the confirm tolerance away
// from the planned quantity for that feed item, and on which side. Strictly greater-than: a reading
// exactly 500 g out is the tolerance, not a breach of it. An unreadable number on either side is
// never flagged -- a warning built on a parse failure would send her back to a video that was fine.
func PackingEntryVariance(enteredKg, plannedKg float64) (direction string, exceeds bool) {
	if math.IsNaN(enteredKg) || math.IsInf(enteredKg, 0) || math.IsNaN(plannedKg) || math.IsInf(plannedKg, 0) {
		return "", false
	}
	diff := enteredKg - plannedKg
	if math.Abs(diff) <= PackingEntryConfirmToleranceKg+packingEntryConfirmEpsilonKg {
		return "", false
	}
	if diff > 0 {
		return PackingEntryAbovePlan, true
	}
	return PackingEntryBelowPlan, true
}

// PackingEntryConfirmMessage is the refusal's headline, in farm language. It names no figure.
const PackingEntryConfirmMessage = "One or more packed weights are more than 500 g away from the plan. Check the video again, and approve only if you are sure of your readings."

// PackingEntryVarianceMessage is the per-item line under a flagged entry box. Direction only: the
// planned quantity and the size of the gap stay off the verifier's screen (blind entry, 2026-08-21).
func PackingEntryVarianceMessage(direction string) string {
	switch direction {
	case PackingEntryAbovePlan:
		return "More than 500 g above the plan. Check the video again."
	case PackingEntryBelowPlan:
		return "More than 500 g below the plan. Check the video again."
	default:
		return ""
	}
}
