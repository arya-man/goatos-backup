package domain

// Weight demographics: average weight by BREED, SEX and MANAGEMENT STAGE.
//
// THIS READ IS THE ONE RECORDED EXCEPTION TO WEIGHING ISOLATION (maintainer
// decision 2026-08-07). Every other weighing path records a scanned string and a
// weight and never asks what animal that is. Breed, sex and stage exist only on
// the animal, so this read resolves the tag — and nothing else does.
//
// The exception stays safe because of what it is NOT: read-only, a reporting path
// with no capture/submit/close behaviour, no scan gated on identity, and a tag
// that resolves to nothing is counted and reported rather than rejected. It is
// file-scoped in check-weighing-free-flow-guard.mjs, so the write path and every
// other weighing file remain locked.
//
// WHAT A WHOLE-SHED WEIGH CONTRIBUTES. A lump-sum weigh has no tags — it is one
// total for a shed — so it is attributed by the shed's own cohort: the single
// management stage its live residents share. That yields a stage figure but never
// a breed or sex one, because a shed holds a mix and splitting one average across
// them would invent a distribution nobody measured. A shed whose residents do not
// share one stage is attributed nowhere, the same "cannot be guessed, so do not
// guess" rule the shifting destination-stage resolution already uses.
type WeightDemographicBucket struct {
	// Label is the value as stored ("Anantapur Sheep", "female", "F2-Male").
	// Clients render it; they do not re-map it.
	Label string `json:"label"`
	// Animals is the count of DISTINCT animals behind AverageWeightKg.
	Animals int `json:"animals"`
	// AverageWeightKg is the mean of each animal's LATEST weight in the window — a
	// weighted mean over animals, never a mean of per-shed averages.
	AverageWeightKg float64 `json:"average_weight_kg"`
}

type WeightDemographics struct {
	// ByBreed and BySex cover per-animal weighs only.
	ByBreed []WeightDemographicBucket `json:"by_breed"`
	BySex   []WeightDemographicBucket `json:"by_sex"`
	// ByStage covers per-animal weighs PLUS whole-shed weighs attributed to their
	// shed's cohort, which is why its total exceeds the other two.
	ByStage []WeightDemographicBucket `json:"by_stage"`
	// Coverage, reported so the difference between the three is visible instead of
	// reading as missing data. An unresolved tag is a real weigh of an animal the
	// herd register does not know.
	ResolvedAnimals            int `json:"resolved_animals"`
	UnresolvedAnimals          int `json:"unresolved_animals"`
	LumpSumAnimals             int `json:"lump_sum_animals"`
	LumpSumUnattributedAnimals int `json:"lump_sum_unattributed_animals"`
}
