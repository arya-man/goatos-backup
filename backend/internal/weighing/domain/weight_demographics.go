package domain

import (
	"strconv"
	"strings"
)

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

// WeightGainOriginBucket is one breed's daily gain for ONE origin -- farm born, procured (no load) or procured (load).
//
// The farm both breeds its own kids and buys them in loads, and the two grow differently enough
// that reading them together answers nothing. Same measure and same population rule as
// WeightGainBucket: an animal weighed twice at the median of its own pairs, plus every whole-shed
// pen whose average moved, each pen contributing once per animal it holds.
//
// PER ANIMAL where the evidence allows it, AGREE-OR-NEITHER where it does not -- the identical rule
// the Weights page's origin filter follows, resolved by the same origin_scope.go. A
// scanned weigh carries a tag and is claimed through the animal that tag resolves to; a whole-shed
// weigh carries none and is claimed only when every live resident of its pen agrees.
//
// THE TWO SIDES NEED NOT ADD UP to the breed's own WeightGainBucket, and that gap is honest rather
// than missing data: a kid whose load is not recorded is claimed by NEITHER side, while still being
// counted in the breed total. Rendering the halves as a partition of the whole would be the lie.
type WeightGainOriginBucket struct {
	// Label is the breed as stored ("Anantapur Sheep"). Clients render it; they do not re-map it.
	Label string `json:"label"`
	// Origin is exactly "farm_born", "procured_no_load" or "procured_load" (platform/animalorigin). A bucket is never emitted for an animal or pen
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
// The farm compares its pen TYPES (elevated, non-elevated, and whatever else it authors).
// Unlike the per-shed leaderboard, this is not a list of pens: it is an aggregate by breed and pen
// type.
//
// The type is CONFIGURED per partition on Configuration -> Items and settings -> Partitions, from
// the farm's own Pen types register (migration 000437), and is read here as the stored CODE on
// shed_partitions.shed_type. Weighing never reads the register itself (it is not on weighing's
// table allowlist): the code is the bucket key, and the chart takes each code's NAME and order
// from the page contract's pen_types option group. It is never guessed from the pen's name or
// notes: a pen nobody has typed is unclassified and is absent from every bar.
type WeightGainShedTypeBucket struct {
	Label              string  `json:"label"`
	ShedType           string  `json:"shed_type"`
	Animals            int     `json:"animals"`
	AverageGainGPerDay float64 `json:"average_gain_g_per_day"`
}

// ShedTypeMember is one operational shed (pen) behind ONE BAR of the Shed-wise comparison, named
// so a reader can see which sheds that bar actually counted.
//
// The classification is configured per pen and is not visible on the chart itself, so the two
// bars ask the reader to trust a setting they cannot see. This is that setting, enumerated.
//
// It lists the sheds that CONTRIBUTED, not every shed carrying the profile: a classified pen that
// was weighed once, or whose cohort is too mixed to claim for a breed, is absent from the bars and
// would be a lie in the list beside them. Same window, same park, same sex/origin/weighing filters
// as the bars themselves, because the list is generated by the same query in the same pass.
type ShedTypeMember struct {
	// Label is the BREED whose bar this pen sits behind, matching WeightGainShedTypeBucket.Label.
	// The grain is per bar, not per class (maintainer, 2026-09-02): a pen holds one breed, so
	// naming every elevated pen beside one breed's elevated bar would name mostly other breeds'
	// pens.
	Label string `json:"label"`
	// ShedType is the stable key the bars are grouped by: a code from the farm's Pen types register
	// (elevated, non_elevated, or any type the farm adds). Never copy.
	ShedType string `json:"shed_type"`
	// LocationID and PartitionLabel are the weighing bucket's own grain, carried so a client can
	// key on identity rather than on the composed name.
	LocationID     string `json:"location_id"`
	PartitionLabel string `json:"partition_label,omitempty"`
	// ParkID and ParkName are part of the pen's IDENTITY, not decoration: this farm has a
	// "Castro 1" in both parks, and Gandhi and Yashoda repeat the same way, so a list keyed on the
	// name alone merges two real pens into one line. Empty when the shed's parent does not resolve
	// to a park, in which case the client simply does not group that row.
	ParkID   string `json:"park_id,omitempty"`
	ParkName string `json:"park_name,omitempty"`
	// OperationalLocationDisplay is the backend-composed farm name -- "Castro 2",
	// "Godel 2 - Part 1" -- built through platform/oploc like every other location label.
	OperationalLocationDisplay string `json:"operational_location_display"`
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
	// Band is a stable KEY derived from the caller's band edges: under_15, 15_20, ..., 35_plus for
	// the default edges (WeightBandKey). Label is the farm words for the same bracket ("15 – 20 kg"),
	// composed by the backend from the same edges (WeightBandLabel) -- since the edges are the
	// tenant's assumption (2026-09-19) a page contract cannot carry one label per key any more.
	Band  string `json:"band"`
	Label string `json:"label"`
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

// WeightGainPenWeekBucket is one pen's daily gain in one calendar week, for the Time-wise tab's
// per-pen table (maintainer request 2026-09-08). Same statistic and claim rules as every other
// gain figure on the page: a scanned animal at the median of its own pairs that week, claimed by
// the pen of its latest weigh; a whole-shed pen at its average-weight movement once per animal. A
// pen is listed whatever it holds -- the breed rows' single-cohort claim does not apply, because
// a pen needs no claim to be itself -- and only the page's Sex filter narrows it, under which a
// whole-shed pen counts when its live cohort is entirely that sex. A pen with no gain in a week
// is simply ABSENT: the screen leaves the cell blank rather than writing a zero, which would read
// as a week the pen stopped growing.
type WeightGainPenWeekBucket struct {
	// LocationID is the weighing bucket's shed location; with PartitionLabel it is the pen.
	LocationID     string `json:"location_id"`
	ParkID         string `json:"park_id"`
	ParkName       string `json:"park_name"`
	ShedName       string `json:"shed_name"`
	PartitionLabel string `json:"partition_label"`
	// OperationalLocationDisplay is the backend-composed pen label ("Godel 2 - Part 1",
	// "Castro 1"); the screen renders it verbatim and composes nothing of its own.
	OperationalLocationDisplay string `json:"operational_location_display"`
	// WeekStart is the Monday (ISO week) in Asia/Kolkata, YYYY-MM-DD, bucketed by the LATER weigh.
	WeekStart string `json:"week_start"`
	// Animals is the denominator: the pen's scanned kids with a gain that week, or its head count
	// when it was weighed whole.
	Animals int `json:"animals"`
	// AverageGainGPerDay is the animal-weighted mean for this pen and week.
	AverageGainGPerDay float64 `json:"average_gain_g_per_day"`
}

// WeightGainLoadWeekBucket is one purchased load's daily gain in one calendar week, for the
// Time-wise tab's per-load table (maintainer request 2026-09-14, "Time-wise ADG for each
// shed/load"). It is the per-pen series one grain up: the SAME pen-week rows, attributed to the
// load whose pens they are through the weighing-owned load mapping (weighing_shed_load_tags) --
// the identical attribution the Load-wise tab's own by-load read uses, so a load's weekly rows
// here and its figures there can never disagree about which pens are its. A shed tagged to two
// loads is claimed by NEITHER (its one average cannot be split between two suppliers), and a load
// with no gain in a week is ABSENT rather than zero.
type WeightGainLoadWeekBucket struct {
	// LoadRef is the farm's own load number, rendered verbatim.
	LoadRef string `json:"load_ref"`
	// OwnerName is the supplier the load was bought from; empty when unrecorded.
	OwnerName string `json:"owner_name"`
	// WeekStart is the Monday (ISO week) in Asia/Kolkata, YYYY-MM-DD, bucketed by the LATER weigh.
	WeekStart string `json:"week_start"`
	// Animals is the denominator: the load's pens' scanned kids with a gain that week, plus the
	// head counts of its pens weighed whole.
	Animals int `json:"animals"`
	// AverageGainGPerDay is the animal-weighted mean for this load and week.
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
	// Which sheds each BAR actually counted, so the classification behind it is inspectable
	// instead of taken on trust. Ordered by breed, then class, then park, then shed name in natural
	// order ("Part 2" before "Part 7", "Yashoda 1" before "Yashoda 2").
	ShedTypeMembers []ShedTypeMember `json:"shed_type_members"`
	// How many animals stand in each weight bracket and how fast each is growing, counting BOTH
	// ways of weighing. Ascending by bracket.
	ByWeightBand []WeightBandBucket `json:"by_weight_band"`
	// The same gain cut by breed AND calendar week, for the Time-wise tab's per-breed trend.
	GainByBreedWeek []WeightGainBreedWeekBucket `json:"gain_by_breed_week"`
	// The same gain cut by PEN and calendar week, for the Time-wise tab's per-pen table.
	GainByPenWeek []WeightGainPenWeekBucket `json:"gain_by_pen_week"`
	// The same gain cut by purchased LOAD and calendar week, for the Time-wise tab's per-load table.
	GainByLoadWeek []WeightGainLoadWeekBucket `json:"gain_by_load_week"`
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

// DefaultWeightBandEdgesKg are the band edges the Weight-wise tab carried as a CASE block before
// they became the tenant's weight_band_edges_kg assumption (maintainer decision 2026-09-19). The
// caller (the Weights pages, from the assumptions read) passes the live edges as band_edges_kg;
// this read never consults the assumptions table itself.
var DefaultWeightBandEdgesKg = []float64{15, 20, 25, 30, 35}

// ValidWeightBandEdgesKg reports whether caller-supplied edges are usable: 2..8 edges, strictly
// rising, each inside 1..200 kg. Empty means the defaults and is valid.
func ValidWeightBandEdgesKg(edges []float64) bool {
	if len(edges) == 0 {
		return true
	}
	if len(edges) < 2 || len(edges) > 8 {
		return false
	}
	for i, e := range edges {
		if e < 1 || e > 200 || (i > 0 && e <= edges[i-1]) {
			return false
		}
	}
	return true
}

// WeightBandKey is the stable wire key for band idx over edges: "under_15", "15_20", "35_plus".
// A decimal edge writes its point as "p" ("17p5") so the key stays a single token.
func WeightBandKey(edges []float64, idx int) string {
	if len(edges) == 0 {
		edges = DefaultWeightBandEdgesKg
	}
	switch {
	case idx <= 0:
		return "under_" + bandNum(edges[0])
	case idx >= len(edges):
		return bandNum(edges[len(edges)-1]) + "_plus"
	default:
		return bandNum(edges[idx-1]) + "_" + bandNum(edges[idx])
	}
}

// WeightBandLabel is the farm wording for band idx: "Under 15 kg", "15 – 20 kg", "35 kg and over".
func WeightBandLabel(edges []float64, idx int) string {
	if len(edges) == 0 {
		edges = DefaultWeightBandEdgesKg
	}
	switch {
	case idx <= 0:
		return "Under " + bandText(edges[0]) + " kg"
	case idx >= len(edges):
		return bandText(edges[len(edges)-1]) + " kg and over"
	default:
		return bandText(edges[idx-1]) + " – " + bandText(edges[idx]) + " kg"
	}
}

func bandText(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func bandNum(v float64) string { return strings.ReplaceAll(bandText(v), ".", "p") }

// BandIndexFor places a weight in the edges the way SQL width_bucket does: 0 below the first
// edge, len(edges) at or above the last. Lower-inclusive, upper-exclusive.
func BandIndexFor(kg float64, edges []float64) int {
	idx := 0
	for _, e := range edges {
		if kg >= e {
			idx++
		}
	}
	return idx
}
