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
// total for a shed — so it is attributed by the shed's own live cohort only when
// that cohort is homogeneous for the dimension being reported. A mixed shed is
// still labelled through ShedComposition, but its one average is never split
// across multiple breed/sex/stage buckets.
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

// WeightGainBucket is the same dimension measured as DAILY GAIN instead of weight.
// It is a separate type, not a field on the weight bucket, because the two range
// over different animals: weight covers every animal weighed once, gain covers only
// those weighed twice. Sharing a row would imply one population.
type WeightGainBucket struct {
	Label string `json:"label"`
	// Animals is the count of animals with at least one computable gain — always
	// fewer than the weight bucket's count, and often far fewer.
	Animals int `json:"animals"`
	// MedianGainGPerDay is the median across those animals. Median, not mean, so one
	// scale misread cannot swing a whole breed.
	MedianGainGPerDay float64 `json:"median_gain_g_per_day"`
}

// ShedCompositionChip is one real breed+sex cohort visible in a shed row. It is
// context, not weight attribution: mixed whole-shed averages are not split across
// these chips.
type ShedCompositionChip struct {
	Breed   string `json:"breed,omitempty"`
	Sex     string `json:"sex,omitempty"`
	Stage   string `json:"stage,omitempty"`
	Animals int    `json:"animals"`
}

// ShedComposition describes the breed+sex mix for one operational shed row.
// Grain matches ShedWeightsRow: (location_id, partition_label).
type ShedComposition struct {
	LocationID     string                `json:"location_id"`
	PartitionLabel string                `json:"partition_label,omitempty"`
	Source         string                `json:"source"`
	TotalAnimals   int                   `json:"total_animals"`
	Chips          []ShedCompositionChip `json:"chips"`
}

type WeightDemographics struct {
	// ByBreed and BySex cover per-animal weighs only.
	ByBreed []WeightDemographicBucket `json:"by_breed"`
	BySex   []WeightDemographicBucket `json:"by_sex"`
	// ByStage covers per-animal weighs PLUS whole-shed weighs attributed to their
	// shed's cohort, which is why its total exceeds the other two.
	ByStage []WeightDemographicBucket `json:"by_stage"`
	// The same three dimensions measured as daily gain. A whole-shed weigh yields no
	// per-animal gain at all, so unlike ByStage these cover scanned animals only.
	GainByBreed []WeightGainBucket `json:"gain_by_breed"`
	GainBySex   []WeightGainBucket `json:"gain_by_sex"`
	GainByStage []WeightGainBucket `json:"gain_by_stage"`
	// Coverage, reported so the difference between the three is visible instead of
	// reading as missing data. An unresolved tag is a real weigh of an animal the
	// herd register does not know.
	ResolvedAnimals            int `json:"resolved_animals"`
	UnresolvedAnimals          int `json:"unresolved_animals"`
	LumpSumAnimals             int `json:"lump_sum_animals"`
	LumpSumUnattributedAnimals int `json:"lump_sum_unattributed_animals"`
	// ShedComposition is keyed by location_id + partition_label so the admin-web
	// weights table can add useful breed+sex chips without doing its own herd lookup.
	ShedComposition []ShedComposition `json:"shed_composition"`
}
