package domain

import "math"

// THE DISTRIBUTION VERIFIER RECORDS THE TOTAL FEED (maintainer decision 2026-09-28).
//
// Feed distribution's operator already photographs the feed on the scale before it is given out
// (migration 000151), and the verifier used to only LOOK at that reading. She now TYPES it: one box,
// "Total feed given (kg)", and Approve stays held until it is filled. Distribution is not packing --
// by the time feed reaches the trough the items are MIXED, so there is one combined quantity per
// pen-session, never one per feed item.
//
// The box is BLIND and the check is WARN, NOT TELL, exactly the 2026-09-09 packing shape: the planned
// total never reaches her screen, and a reading more than DistributionEntryConfirmTolerancePct away
// from it is refused ONCE with a direction only ("more than 5% above the plan"). If she is sure, she
// ticks "I checked the video again" and approves; the reading is stored with the plan it was checked
// against and her confirmation.
//
// THE TOLERANCE IS A PERCENTAGE, NOT PACKING'S 500 g, on purpose: a pen-session total runs from a few
// kilograms to a few hundred, so a fixed 500 g would flag nearly every large pen on an honest reading
// and a fixed kilo-scale band would wave through a small pen fed double. Do not "unify" it with
// PackingEntryConfirmToleranceKg -- they guard different grains.

// DistributionTotalFeedKey is the ONE entry box a distribution verification item carries. It is a
// stable wire key (echoed back in the approve's measurement entries and in the confirm refusal's
// field errors); the label below is what the verifier reads.
const DistributionTotalFeedKey = "total_feed"

// DistributionTotalFeedLabel names the box. Backend-owned copy, rendered verbatim on the phone and
// the admin-web drawer.
const DistributionTotalFeedLabel = "Total feed given (kg)"

// DistributionEntryConfirmTolerancePct is how far, as a percentage of the planned pen-session total,
// a reading may sit before she is asked to look again. Strictly greater-than: exactly 5% is the
// tolerance, not a breach of it.
const DistributionEntryConfirmTolerancePct = 5.0

// distributionEntryConfirmEpsilonKg absorbs binary-decimal noise at the boundary (105 kg typed
// against a 100 kg plan must read as exactly 5%, not a hair over).
const distributionEntryConfirmEpsilonKg = 1e-6

// MaxDistributionTotalFeedKg is the typo ceiling for one pen-session's combined feed. No pen on the
// farm is fed anywhere near it; a fat-fingered extra digit is refused rather than recorded. The DB
// CHECK agrees (migration 000455).
const MaxDistributionTotalFeedKg = 20000

// The DIRECTION codes the screen may reveal. Distinct from packing's above_plan/below_plan because
// each client renders copy keyed by the code, and packing's copy says "500 g".
const (
	DistributionEntryAbovePlan = "total_above_plan"
	DistributionEntryBelowPlan = "total_below_plan"
)

// DistributionEntryVariance reports whether the verifier's total sits more than the confirm
// tolerance away from the planned total, and on which side. A plan that is not a positive number
// checks nothing: a percentage of zero is undefined, and a warning built on an unreadable plan would
// send her back to a video that was fine.
func DistributionEntryVariance(enteredKg, plannedKg float64) (direction string, exceeds bool) {
	if math.IsNaN(enteredKg) || math.IsInf(enteredKg, 0) || math.IsNaN(plannedKg) || math.IsInf(plannedKg, 0) || plannedKg <= 0 {
		return "", false
	}
	diff := enteredKg - plannedKg
	band := plannedKg * DistributionEntryConfirmTolerancePct / 100
	if math.Abs(diff) <= band+distributionEntryConfirmEpsilonKg {
		return "", false
	}
	if diff > 0 {
		return DistributionEntryAbovePlan, true
	}
	return DistributionEntryBelowPlan, true
}

// DistributionEntryConfirmMessage is the refusal's headline. It names no figure.
const DistributionEntryConfirmMessage = "The total feed weight is more than 5% away from the plan. Check the video again, and approve only if you are sure of your reading."

// DistributionEntryVarianceMessage is the line under the flagged box. Direction only.
func DistributionEntryVarianceMessage(direction string) string {
	switch direction {
	case DistributionEntryAbovePlan:
		return "More than 5% above the plan. Check the video again."
	case DistributionEntryBelowPlan:
		return "More than 5% below the plan. Check the video again."
	default:
		return ""
	}
}
