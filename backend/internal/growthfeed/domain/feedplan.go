package domain

// FeedPlanInput is what the ration lookup found for one pen, before any judgement
// is applied to it. Keeping the judgement out of SQL is deliberate: the rules below
// are safety rules about feeding animals, and they are worth being able to test
// without a database.
type FeedPlanInput struct {
	// CohortResolved is false when the pen holds more than one breed or stage, so
	// there is no single (ration group, shed tag) to look a ration up by.
	CohortResolved bool
	// RationGroupLabel / ShedTagLabel are empty when the cohort resolved but did not
	// match the authored vocabulary — a stage the farm uses that feed config has no
	// tag for, or a breed with no ration group.
	RationGroupLabel string
	ShedTagLabel     string

	// ItemsConfigured / ItemsBlocked count active feed items that DID and DID NOT
	// have an authored rate for this pen. Blocked is not zero: an absent rate row is
	// the only encoding feed config has for "nobody configured this".
	ItemsConfigured int
	ItemsBlocked    int

	// PlannedGramsPerHeadDay is the sum over CONFIGURED items only.
	PlannedGramsPerHeadDay float64
	// EnergyKcalPerHeadDay is the matching energy sum, valid only when
	// EnergyComplete is true.
	EnergyKcalPerHeadDay float64
	// EnergyComplete is false when any contributing item has no authored energy
	// value. The partial sum is then discarded rather than shown: a smaller number
	// presented as a complete one is worse than no number.
	EnergyComplete bool

	// ExperimentTotalKgPerDay is the hand-entered shed TOTAL for experiment pens.
	// It is never a per-head rate (migration 000001 says so explicitly), so a
	// per-head figure exists only when LiveAnimals is known to divide by.
	ExperimentTotalKgPerDay float64
	IsExperiment            bool
	LiveAnimals             int
}

// ResolveFeedPlan turns the raw lookup into the pen's reported ration.
//
// The one rule that matters more than the others: a MISSING rate row must never
// be read as zero. Feed config stores an authored 0 for pens that genuinely eat
// none of an item (milk-fed kids), so zero and absent are different facts. If they
// were merged, a pen whose ration was never configured would be reported as fully
// fed at 0 g and its conversion ratio would rank it as the most efficient pen on
// the farm.
//
// THE RATION GRID IS SPARSE BY DESIGN, and that is why "some items have no rate"
// is NOT treated as an incomplete ration. Checked against live staging on
// 2026-08-10: of the 154 authored (park, ration group, shed tag) combinations, not
// one covers all 14 active catalog items — they carry 5, 6 or 10. A K1 kid ration
// authors the ten things kids eat and says nothing about adult concentrates,
// because kids do not eat them.
//
// An earlier version of this function downgraded any pen with an unauthored item
// to "partial" and refused it a conversion ratio. On real data that would have
// suppressed the screen's headline number on EVERY PEN IN THE ESTATE. It is also
// not a per-pen signal even in principle: rates are keyed by (group, tag), so every
// pen sharing a cohort has identical item coverage and the flag could never
// distinguish one pen from another.
//
// What is genuinely lost: a ration mis-authored by omission (somebody forgets maize
// for growers) now reports a total that is too low, and a too-low planned ration
// makes that pen look efficient. The data cannot separate that from a deliberately
// sparse ration, so ItemsConfigured and ItemsBlocked travel on the row for a reader
// to judge. Closing that gap needs an authored "this ration is complete" signal in
// feed config, which is a MAINTAINER decision, not something to infer here.
func ResolveFeedPlan(in FeedPlanInput) (status FeedPlanStatus, plannedGrams, energyKcal *float64) {
	switch {
	case in.IsExperiment:
		status = FeedPlanExperiment
		// absolute_kg is a shed total. Dividing by a head count of zero (or an
		// unknown one) would be an infinity, so the per-head figure is simply absent.
		if in.LiveAnimals > 0 {
			perHead := in.ExperimentTotalKgPerDay * 1000 / float64(in.LiveAnimals)
			plannedGrams = &perHead
		}
		return status, plannedGrams, nil

	case !in.CohortResolved:
		return FeedPlanUnknownCohort, nil, nil

	case in.RationGroupLabel == "" || in.ShedTagLabel == "":
		// The cohort is known but feed config has no vocabulary entry for it. That is
		// a real, actionable configuration gap, not a data error.
		return FeedPlanNoConfig, nil, nil

	case in.ItemsConfigured == 0:
		// NOTHING authored for this pen's (ration group, shed tag). This is the real
		// "not configured" case and the only one that suppresses a ration, because a
		// planned total of 0 would read as "this pen is meant to eat nothing".
		return FeedPlanNoConfig, nil, nil

	default:
		grams := in.PlannedGramsPerHeadDay
		status = FeedPlanResolved
		if in.EnergyComplete {
			kcal := in.EnergyKcalPerHeadDay
			energyKcal = &kcal
		}
		return status, &grams, energyKcal
	}
}
