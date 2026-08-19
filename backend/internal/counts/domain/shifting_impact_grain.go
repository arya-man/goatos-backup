package domain

import "strings"

// ShiftingImpactGrainKey is the ONE builder of a shifting_event_impacts grain key, shared by the
// explicit-impacts transport path and the server-side derivation so the two can never drift into
// different key shapes for the same cohort.
//
// The key is the full COHORT identity -- destination shed plus breed plus the three descriptor
// fields (stage, age class, sex) -- not just shed:breed. That is what lets one movement carry a
// MIXED group (a buck and a doe of the same breed, a kid and an adult) as separate impact rows
// under shifting_event_impacts_grain_unique (tenant, event, grain_key): with the descriptors in the
// key, each distinct cohort has its own row instead of colliding on the shed:breed prefix.
//
// The key is identity WITHIN one event only. Nothing joins events to each other or to the count
// projection by this string: the projection recomputes its own shed:breed grain from the impact
// row's columns (countProjectionGrainKey), so splitting cohorts here still sums onto the same
// projection row a merged impact would have reached.
//
// A nil/blank descriptor stays an empty segment rather than being dropped, so "no stage recorded"
// and "stage K2" can never produce the same key by accident.
func ShiftingImpactGrainKey(destinationShedID, breedKey string, stageTag, ageClass, sex *string) string {
	segment := func(v *string) string {
		if v == nil {
			return ""
		}
		return strings.ToLower(strings.TrimSpace(*v))
	}
	return strings.ToLower(strings.TrimSpace(destinationShedID)) + ":" +
		strings.ToLower(strings.TrimSpace(breedKey)) + ":" +
		segment(stageTag) + ":" + segment(ageClass) + ":" + segment(sex)
}
