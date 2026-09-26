// Package animalorigin is the ONE answer to "where did this animal come from" for every
// reporting screen that splits the herd by origin (maintainer decision 2026-09-26).
//
// THREE COHORTS, NOT TWO. The farm breeds its own kids, buys animals on recorded purchase loads,
// and has bought animals that sit on no recorded load at all. The Sales pages have always kept
// the three apart -- Farm born reads goats.origin_type = 'birth', Load wise reads
// procurement_load_goats -- but the Weights page, its Growth Director widgets, the FCR tab and
// the feed weight-band read answered "farm born" with "carries no load row", which filed every
// procured-without-a-load animal (659 of the live herd on 2026-09-26) under Farm born.
//
// THE RULE, per animal, in this order:
//
//  1. on any procurement load                  -> ProcuredLoad   ("Procured (load)")
//  2. else goats.origin_type = 'birth'         -> FarmBorn       ("Farm born")
//  3. else goats.origin_type = 'procured'      -> ProcuredNoLoad ("Procured (no load)")
//  4. anything else (blank, 'imported')        -> no cohort
//
// The LOAD WINS over the recorded origin because load membership is the evidence the procurement
// desk wrote while buying; 333 loaded animals carry no origin at all. An animal answering none of
// the three is still recorded and still counted in an unfiltered view -- it simply cannot answer
// the question, and guessing on its behalf is how a bought animal ends up counted as the farm's
// own (the Sales Farm born rule, docs/decisions/sales-farm-born.md).
//
// SQL readers mirror Classify exactly; each such site names this package in a comment as the
// contract it mirrors. The SQL is deliberately NOT built here: the weighing isolation guard keys
// its table allowlist per FILE, and SQL assembled in another package would hide a weighing read
// of procurement_load_goats from it.
package animalorigin

import "strings"

// The three cohort keys. They are wire values (query parameters, JSON bucket keys) and never copy.
const (
	FarmBorn       = "farm_born"
	ProcuredNoLoad = "procured_no_load"
	ProcuredLoad   = "procured_load"
)

// LegacyPurchased is the key the two-way filter used for "on a load". It is still ACCEPTED as a
// filter value -- a bookmarked URL or an older client keeps working -- and means exactly
// ProcuredLoad, which is what it always meant. It is never emitted.
const LegacyPurchased = "purchased"

// Keys lists the cohorts in the order every surface shows them.
var Keys = []string{FarmBorn, ProcuredNoLoad, ProcuredLoad}

// Normalize maps a filter value to its cohort key. "" means no filter and returns ("", true). An unknown value returns ("", false): a caller must refuse it rather than widen it,
// because a silently dropped filter shows more animals than the heading says.
func Normalize(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", true
	case FarmBorn:
		return FarmBorn, true
	case ProcuredNoLoad:
		return ProcuredNoLoad, true
	case ProcuredLoad, LegacyPurchased:
		return ProcuredLoad, true
	default:
		return "", false
	}
}

// Classify is the per-animal rule, the oracle every SQL mirror is tested against. It returns ""
// for an animal that answers no cohort.
func Classify(onLoad bool, originType string) string {
	if onLoad {
		return ProcuredLoad
	}
	switch strings.ToLower(strings.TrimSpace(originType)) {
	case "birth":
		return FarmBorn
	case "procured":
		return ProcuredNoLoad
	default:
		return ""
	}
}
