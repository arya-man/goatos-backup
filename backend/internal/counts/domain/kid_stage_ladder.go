package domain

import (
	"strings"
	"time"
)

// The kid stage ladder: age-triggered K-stage moves (maintainer decision 2026-08-20).
//
// This deliberately AMENDS the 2026-08-05 "placement, not birthday" rule (migration
// 000109_stage_age_band) for the EARLY MILK LADDER ONLY: a newborn's K0 -> K1 -> K2 walk is a
// clock-driven husbandry fact (colostrum ends, bottle volumes change — see
// milkPreparationRuleForStage), so the SYSTEM RAISES the movement when the kid's age crosses the
// step. Everything downstream is unchanged: the raise still needs Park Head approval, an operator
// still physically moves the kid and records the mandatory video, and verification still reviews
// it — the ladder automates the RAISE, never the move. Later-life stages (K2 -> K3, F2, adult
// cohorts) remain placement-driven exactly as 000109 records; nothing here touches them.
//
// Ages are BUSINESS-DAY grain in Asia/Kolkata, never hour arithmetic: a kid born any time on day D
// is due K1 at the start of business day D+2 and K2 at the start of D+7, the same way every other
// Goat OS clock works.
type KidStageLadderStep struct {
	// FromStage / ToStage are management_stage cohort tags, matched case-insensitively the same
	// way the milk volume matrix matches them.
	FromStage string
	ToStage   string
	// AtAgeDays is the kid's age, in whole business days, at which the move becomes due.
	AtAgeDays int
}

// KidStageLadder is the authored ladder, in walk order. Maintainer numbers 2026-08-20:
// K0 -> K1 at 2 days, K1 -> K2 at 7 days.
func KidStageLadder() []KidStageLadderStep {
	return []KidStageLadderStep{
		{FromStage: "K0", ToStage: "K1", AtAgeDays: 2},
		{FromStage: "K1", ToStage: "K2", AtAgeDays: 7},
	}
}

// KidStageBornOnOrBefore is the birth-instant cutoff for a step on a business date: a kid is due
// when it was born on or before (businessDayStart - AtAgeDays). businessDayStart must be the
// start-of-day instant of the evaluation business date in the business timezone; passing a raw
// "now" would smuggle hour arithmetic into a day-grain rule.
func KidStageBornOnOrBefore(step KidStageLadderStep, businessDayStart time.Time) time.Time {
	return businessDayStart.AddDate(0, 0, -step.AtAgeDays)
}

// KidStageStepFor returns the ladder step whose FromStage matches the given management stage.
func KidStageStepFor(managementStage string) (KidStageLadderStep, bool) {
	needle := strings.ToUpper(strings.TrimSpace(managementStage))
	for _, step := range KidStageLadder() {
		if step.FromStage == needle {
			return step, true
		}
	}
	return KidStageLadderStep{}, false
}

// KidStageDueGoat is one kid whose age has crossed its ladder step: the raw material of an
// auto-raised shifting (or of the operator-facing due card when the destination is ambiguous).
type KidStageDueGoat struct {
	GoatID          string
	DisplayID       string
	Tag             string
	ParkID          string
	ShedID          string
	PartitionLabel  *string
	// ParkName/ShedName are the display names of the kid's CURRENT location, resolved here so the
	// operator card and the prefilled basket render backend-owned labels, never ids.
	ParkName        string
	ShedName        string
	ManagementStage string
	// BornOn is the kid's date of birth (goats.dob, falling back to approx_dob) at DATE grain —
	// the ladder is a business-day rule, so the birth's time of day never moves a cutoff.
	BornOn time.Time
}

// KidStageDueGroup is one park's due kids for one ladder step, plus the destination answer:
// exactly one candidate pen in the park carries the step's target tag -> the group is
// AUTO-RAISABLE; zero or several -> the group is surfaced as an operator card instead
// (maintainer decision 2026-08-20: "operator will select any one"), because auto-picking one of
// two K1 pens moves animals to a pen nobody chose, and refusing outright would hide due work.
type KidStageDueGroup struct {
	ParkID    string
	ParkName  string
	FromStage string
	ToStage   string
	Goats     []KidStageDueGoat
	// Candidates are the park's pens whose resolved destination tag equals ToStage.
	Candidates []ShiftingDestinationShed
}

// AutoRaisable reports whether the group has exactly one truthful destination.
func (g KidStageDueGroup) AutoRaisable() bool { return len(g.Candidates) == 1 }
