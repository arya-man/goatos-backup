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

// WeightGainThresholdBucket counts how many animals of one BREED fell into each daily
// gain band. Same population and same measure as WeightGainBucket — an animal with at
// least one computable gain, at its median g/day — reported as a distribution instead
// of a single middle figure, because a breed's median says nothing about how many of
// its kids are actually growing well.
//
// THE BANDS ARE DISJOINT (maintainer, 2026-08-24, superseding the cumulative marks the
// same day). An animal at 260 g/day is counted in Above250 ONLY. Every animal in the
// denominator lands in exactly one band, so the four counts sum to Animals and may be
// read as a real distribution. Boundaries are strictly-greater at the top of each band,
// so an animal at exactly 200 g/day sits in Band180To200, not Band200To250.
//
// ONE GRAIN, not one per sex: the Weights page's Sex filter narrows the whole read upstream
// (maintainer, 2026-08-26), so these rows already describe the kids the reader asked for.
type WeightGainThresholdBucket struct {
	Label string `json:"label"`
	// Animals is the denominator: animals of this breed with a computable gain in the
	// window. It is the SAME key set the four bands partition, so a percentage may be
	// taken against it row-locally, and the four bands add up to it exactly.
	Animals int `json:"animals"`
	// AtOrBelow180 is the only band that is not strictly-greater at its floor: it is
	// everything left over, so no animal with a gain can fall outside the four bands.
	AtOrBelow180 int `json:"at_or_below_180_g_per_day"`
	Band180To200 int `json:"band_180_to_200_g_per_day"`
	Band200To250 int `json:"band_200_to_250_g_per_day"`
	Above250     int `json:"above_250_g_per_day"`
}

// WeightGainOriginBucket is one breed's daily gain for ONE origin -- farm born or purchased.
//
// The farm both breeds its own kids and buys them in loads, and the two grow differently enough
// that reading them together answers nothing. Same measure and same population rule as
// WeightGainBucket: an animal weighed twice at the median of its own pairs, plus every whole-shed
// pen whose average moved, each pen contributing once per animal it holds.
//
// PER ANIMAL where the evidence allows it, AGREE-OR-NEITHER where it does not -- the identical rule
// the Weights page's Farm born / Purchased filter follows, resolved by the same origin_scope.go. A
// scanned weigh carries a tag and is claimed through the animal that tag resolves to; a whole-shed
// weigh carries none and is claimed only when every live resident of its pen agrees.
//
// THE TWO SIDES NEED NOT ADD UP to the breed's own WeightGainBucket, and that gap is honest rather
// than missing data: a kid whose load is not recorded is claimed by NEITHER side, while still being
// counted in the breed total. Rendering the halves as a partition of the whole would be the lie.
type WeightGainOriginBucket struct {
	// Label is the breed as stored ("Anantapur Sheep"). Clients render it; they do not re-map it.
	Label string `json:"label"`
	// Origin is exactly "farm_born" or "purchased". A bucket is never emitted for an animal or pen
	// that is on neither side.
	Origin string `json:"origin"`
	// Animals is the count behind MedianGainGPerDay: scanned kids of this breed and origin with a
	// computable gain, plus the head counts of the pens claimed for this origin.
	Animals int `json:"animals"`
	// MedianGainGPerDay is the animal-weighted mean for this breed and origin.
	MedianGainGPerDay float64 `json:"median_gain_g_per_day"`
}

// WeightGainShedTypeBucket is one breed's daily gain for one physical shed class.
//
// The farm wants this as Elevated shed versus Crown/Ground shed. Unlike the per-shed
// leaderboard, this is not a list of pens: it is an aggregate by breed and shed class. The
// class must come from explicit shed metadata; an unclassified shed is not guessed from its name.
type WeightGainShedTypeBucket struct {
	Label              string  `json:"label"`
	ShedType           string  `json:"shed_type"`
	Animals            int     `json:"animals"`
	AverageGainGPerDay float64 `json:"average_gain_g_per_day"`
}

// WeightBandBucket is one weight bracket: how many animals stand in it, and how fast it is growing.
//
// BOTH WAYS OF WEIGHING COUNT. A scanned animal is banded by its OWN latest weight in the window
// and counts as one. A whole-shed pen is banded by the pen's own latest average weight and counts
// as ALL the animals it holds -- the shed average is the only measured fact, so the pen's animals
// are kept whole in the one band that average falls into rather than spread across neighbouring
// bands, which would invent a distribution nobody measured. Same rule the daily-gain bands use.
//
// Bands are lower-inclusive and upper-exclusive, so every animal lands in exactly one and Animals
// sums to the weighed population.
type WeightBandBucket struct {
	// Band is a stable KEY, never display copy: under_15, 15_20, 20_25, 25_30, 30_35, 35_plus.
	// The farm words live in the page contract.
	Band string `json:"band"`
	// Animals is everything standing in this bracket -- scanned kids plus the head counts of the
	// pens whose average lands here.
	Animals int `json:"animals"`
	// GainAnimals is the smaller set behind AverageGainGPerDay: those weighed TWICE, plus the head
	// counts of pens whose average moved. Always <= Animals, and reported separately because an
	// animal weighed once is a real animal in this bracket with no growth to report.
	GainAnimals int `json:"gain_animals"`
	// AverageGainGPerDay is the animal-weighted mean for the bracket -- the same statistic every
	// other gain figure on these screens reports. NIL when nothing here was weighed twice: a
	// bracket nobody measured twice has NO growth rate, and 0 would read as one that stopped.
	AverageGainGPerDay *float64 `json:"average_gain_g_per_day,omitempty"`
}

// WeightGainBreedWeekBucket is one breed's daily gain in one calendar week, for the per-breed
// trend beside the overall weekly series.
//
// Same statistic and same claim rules as every other gain figure: a scanned animal at the median of
// its own pairs for that week, a pen at its average-weight movement once per animal, and a pen
// joins a breed only when its live cohort is entirely that breed. A mixed pen names nothing, so the
// per-breed weeks need not add up to the overall week -- that gap is honest rather than missing.
//
// A breed with no gain in a week is simply ABSENT: never interpolated, never carried forward, and
// never a fabricated zero, which would read as a week that breed stopped growing.
type WeightGainBreedWeekBucket struct {
	Label string `json:"label"`
	// WeekStart is the Monday (ISO week) in Asia/Kolkata, YYYY-MM-DD. A pair spanning weeks is
	// bucketed by its LATER weigh, the week the movement was observed in.
	WeekStart string `json:"week_start"`
	// Animals is the denominator: this breed's kids with a gain that week, plus the head counts of
	// its single-breed pens that moved.
	Animals int `json:"animals"`
	// AverageGainGPerDay is the animal-weighted mean for this breed and week.
	AverageGainGPerDay float64 `json:"average_gain_g_per_day"`
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
	// The same three dimensions measured as daily gain: same-animal pairs PLUS
	// lump-sum sheds (maintainer decision 2026-08-25). A whole-shed weigh has no
	// per-animal identity, so its animals ride at the SHED grain — every animal of
	// the shed carries the shed's own average-weight change between its first and
	// latest lump-sum weigh in the window — and the shed joins a bucket only when
	// its live cohort is homogeneous for that dimension. Mixed sheds join nothing.
	GainByBreed []WeightGainBucket `json:"gain_by_breed"`
	GainBySex   []WeightGainBucket `json:"gain_by_sex"`
	GainByStage []WeightGainBucket `json:"gain_by_stage"`
	// The same gain, split by where the animals came from, for the Weights analytics page's
	// Birth-wise tab. Computed in the SAME query so it can never disagree with GainByBreed above.
	GainByBreedOrigin []WeightGainOriginBucket `json:"gain_by_breed_origin"`
	// The same gain, split by breed and physical shed class, for the Shed-wise tab.
	GainByBreedShedType []WeightGainShedTypeBucket `json:"gain_by_breed_shed_type"`
	// How many animals stand in each weight bracket and how fast each is growing, counting BOTH
	// ways of weighing. Ascending by bracket.
	ByWeightBand []WeightBandBucket `json:"by_weight_band"`
	// The same gain cut by breed AND calendar week, for the Time-wise tab's per-breed trend.
	GainByBreedWeek []WeightGainBreedWeekBucket `json:"gain_by_breed_week"`
	// How many animals of each breed fell into each daily-gain band. DISJOINT bands
	// over the same population GainByBreed uses — same-animal pairs plus
	// homogeneous lump-sum sheds, each shed's animals landing whole in the ONE band
	// its shed-average change falls into (maintainer decision 2026-08-25: the shed
	// average is the only measured fact, so every animal is kept in that range).
	GainThresholdsByBreed []WeightGainThresholdBucket `json:"gain_thresholds_by_breed"`
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
