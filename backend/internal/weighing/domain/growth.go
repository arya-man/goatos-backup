package domain

// GrowthADGHeadline is the herd-level ADG (Average Daily Gain) summary for the
// requested period, scoped to a park.
//
// ADG is computed per animal between CONSECUTIVE accepted weighs (see the
// package comment on GrowthADG for the full business-rule list). The headline
// reports the herd MEDIAN, not the mean: a handful of scale-glitch outliers
// (a mis-scanned kid weighed on an adult platform, say) would otherwise swing
// a mean by an amount no shed manager could act on. Median is robust to that.
type GrowthADGHeadline struct {
	// Status is an explicit, unambiguous data-availability marker so a client never has to infer
	// "no data yet" from a numeric zero. "ok" means at least one qualifying ADG pair exists in the
	// period and every *ADGGPerDay/PositiveADGPercent field below is populated. "insufficient_data"
	// means PairCount is 0 -- no animal was weighed twice in the period, so there is no ADG to
	// report -- and every pointer field below is nil rather than a fabricated 0.
	Status string `json:"status"`
	// AverageADGGPerDay is the farm's daily gain: the ANIMAL-WEIGHTED MEAN grams/day across every
	// qualifying consecutive-weigh pair whose LATER weigh falls in the requested period, PLUS every
	// whole-shed pen weighed at least twice in the period, each contributing its own average-weight
	// movement once per animal it holds (maintainer decision 2026-08-26).
	//
	// It was previously the MEDIAN of scanned pairs alone, which excluded the whole-shed pens that
	// hold most of this farm's kids and disagreed with the by-breed/by-sex/by-stage gain charts --
	// the same page reported male kids at 133 g/day and 200 g/day at once. This is now the SINGLE
	// definition of daily gain; the charts compute the identical statistic per dimension.
	//
	// Nil when Status is "insufficient_data" -- a park where nothing was weighed twice must never
	// render as "0 g/day", which would read as a real, static herd.
	AverageADGGPerDay *float64 `json:"average_adg_g_per_day"`
	// PreviousAverageADGGPerDay is the same statistic for the immediately preceding period of
	// equal length, for the delta comparison. Nil when the previous period itself has nothing to
	// measure (PreviousStatus is "insufficient_data") -- there is nothing to compare to.
	PreviousAverageADGGPerDay *float64 `json:"previous_average_adg_g_per_day"`
	// PreviousStatus is the same Status marker as above, computed for the previous-period query.
	PreviousStatus string `json:"previous_status"`
	// DeltaGPerDay is AverageADGGPerDay - PreviousAverageADGGPerDay. Nil whenever EITHER side is
	// nil: a period with no current-period pairs, or one with no comparable previous period, has
	// no honest delta to report. A delta must never be synthesized from a 0 standing in for
	// "unknown" -- that reads as an invented improvement or decline.
	DeltaGPerDay *float64 `json:"delta_g_per_day"`
	// PositiveADGPercent is the % of qualifying SCANNED pairs with ADG > 0. Nil when Status is
	// "insufficient_data" (see AverageADGGPerDay). Individual-only on purpose: a whole-shed average
	// has no per-animal sign, so a pen cannot contribute to a "% of animals gaining" figure.
	PositiveADGPercent *float64 `json:"positive_adg_percent"`
	// NegativeADGCount is the count of qualifying PAIRS with ADG < 0 across the whole period --
	// a statistic about measurements, not about animals. One animal that dipped and recovered
	// contributes a negative pair while not currently losing weight.
	NegativeADGCount int `json:"negative_adg_count"`
	// LosingAnimalCount is the count of ANIMALS whose MOST RECENT pair is negative: the ones
	// actually losing weight right now, and the number that matches LosingAnimals.
	//
	// Both are reported because they answer different questions, and because showing the pair
	// count on a tile that drills into the animal list made the screen contradict itself -- "15
	// losing" leading to a list of 2.
	LosingAnimalCount int `json:"losing_animal_count"`
	// PairCount is the total number of qualifying (non-rejected, non-zero-elapsed)
	// consecutive-weigh SCANNED pairs in the period. It is the denominator of the pair-based
	// statistics above, and no longer the denominator of the headline itself -- see HeadlineAnimals.
	PairCount int `json:"pair_count"`
	// HeadlineAnimals is how many kids AverageADGGPerDay actually speaks for: the scanned pairs plus
	// every animal in the whole-shed pens that moved. This is the number a card should print beside
	// the gain, because printing PairCount there described the herd by the minority of it that
	// happens to be scanned one by one.
	HeadlineAnimals int `json:"headline_animals"`
	// RejectedObservationCount is the count of weighing_observations rows in the
	// period with verification_status='rejected'. These are EXCLUDED from every
	// ADG number above; this field exists purely as a data-quality signal.
	RejectedObservationCount int `json:"rejected_observation_count"`
	// UnverifiedObservationCount is the count of observations feeding the ADG
	// pairs above that are still verification_status='pending' (not yet
	// verified, not rejected). Pending observations ARE included in the ADG
	// math -- see weight_history.go's comment on why pending reads are not
	// hidden from leadership -- but this count lets a client build a
	// "verified only" toggle later.
	UnverifiedObservationCount int `json:"unverified_observation_count"`
}

// GrowthEligibility reports how many animals have enough weighs in the period
// to even produce an ADG number, versus how many were touched at all.
type GrowthEligibility struct {
	// AnimalsWithTwoPlusWeighs is the count of distinct animals (goat_id when
	// resolved, else the normalized scanned identifier) with >= 2 accepted
	// (non-rejected) weighs in the period.
	AnimalsWithTwoPlusWeighs int `json:"animals_with_two_plus_weighs"`
	// TotalAnimalsWeighed is the count of distinct animals with >= 1 accepted
	// (non-rejected) weigh in the period, regardless of ADG eligibility.
	TotalAnimalsWeighed int `json:"total_animals_weighed"`
}

// GrowthTrendPoint groups the ADG pairs that exist into a calendar week PURELY for display --
// this is a bucketing choice for charting whatever dates the data actually has, not an assumed
// weighing cadence. A week with no pairs is simply absent from the array: it is never
// fabricated with an interpolated value, and it is never reported as "missed" or "overdue".
type GrowthTrendPoint struct {
	// WeekStart is the Monday (ISO week) of the calendar week this point's pairs were bucketed
	// into (display grouping only), YYYY-MM-DD, in Asia/Kolkata.
	WeekStart string `json:"week_start"`
	// MedianADGGPerDay is the median ADG of pairs whose later weigh falls in this calendar week.
	MedianADGGPerDay float64 `json:"median_adg_g_per_day"`
	// PairCount is how many pairs contributed to this week's median.
	PairCount int `json:"pair_count"`
}

// GrowthShedLeaderboardRow is one shed's ADG/weight summary for the period.
type GrowthShedLeaderboardRow struct {
	LocationID                 string `json:"location_id"`
	DisplayName                string `json:"display_name"`
	PartitionLabel             string `json:"partition_label,omitempty"`
	OperationalLocationDisplay string `json:"operational_location_display"`
	// ParkName is the park's SHORT CODE (CBE, CPT) when it has one, falling back to its full
	// name -- the same convention ShedWeightsRow uses, so the two series sharing the gain chart
	// name a park identically. REQUIRED, not decorative: 39 shed names exist in BOTH parks, so a
	// row without it names two different sheds at once.
	ParkName         string  `json:"park_name"`
	AnimalCount      int     `json:"n"`
	MedianWeightKg   float64 `json:"median_weight_kg"`
	MedianADGGPerDay float64 `json:"median_adg_g_per_day"`
	// ADGPairCount is how many qualifying ADG pairs this shed's median is based on. Can be
	// less than AnimalCount -- a shed can have animals weighed once (no pair yet).
	ADGPairCount int `json:"adg_pair_count"`
}

// GrowthDistributionBucket is one bin of the ADG histogram. Bins are FIXED at
// 25 g/day width so parks and periods are visually comparable, plus one
// explicit bucket for negative ADG (weight loss) rather than folding it into
// the first positive bin.
type GrowthDistributionBucket struct {
	// Label is a human-readable bucket name, e.g. "negative", "0-25", "25-50", "300+".
	Label string `json:"label"`
	// MinGPerDay is the inclusive lower edge (null/omitted for the negative bucket, which has no floor).
	MinGPerDay *float64 `json:"min_g_per_day,omitempty"`
	// MaxGPerDay is the exclusive upper edge (null/omitted for the top overflow bucket).
	MaxGPerDay *float64 `json:"max_g_per_day,omitempty"`
	Count      int      `json:"count"`
}

// GrowthSaleReadiness counts animals against fixed weight thresholds using
// each animal's LATEST weight EVER recorded (not bounded to the requested
// period) -- see the package comment on GrowthADG for why.
type GrowthSaleReadiness struct {
	AtOrAbove30Kg int `json:"at_or_above_30kg"`
	AtOrAbove35Kg int `json:"at_or_above_35kg"`
	// AnimalsConsidered is the count of distinct resolved animals with at
	// least one accepted (non-rejected) weigh ever recorded, i.e. the
	// denominator these two counts are drawn from.
	AnimalsConsidered int `json:"animals_considered"`
}

// GrowthLumpSumShedTrendPoint is one shed's one week of population-level
// (not per-animal) average weight from weighing_shed_observations. This is
// KEPT ENTIRELY SEPARATE from the individual ADG numbers above: shed
// population changes between weighs (animals move sheds, are sold, etc.), so
// deriving a per-animal ADG from the delta between two lump-sum averages
// would attribute population turnover to individual growth. This section
// answers a different, valid question: "is this shed's average getting
// heavier" -- not "are these specific animals growing".
type GrowthLumpSumShedTrendPoint struct {
	LocationID                 string  `json:"location_id"`
	DisplayName                string  `json:"display_name"`
	PartitionLabel             string  `json:"partition_label,omitempty"`
	OperationalLocationDisplay string  `json:"operational_location_display"`
	WeekStart                  string  `json:"week_start"`
	AverageWeightKg            float64 `json:"average_weight_kg"`
	HeadCount                  int     `json:"head_count"`
}

// GrowthLumpSum is the lump-sum (per-shed-partition) aggregate, reported
// entirely separately from the individual-animal ADG sections above.
type GrowthLumpSum struct {
	ShedWeekTrend []GrowthLumpSumShedTrendPoint `json:"shed_week_trend"`
}

// GrowthADG is the full leadership ADG / growth read-model response for a
// park and period.
//
// BUSINESS RULES (see individual field docs for the "why"):
//   - ADG is computed per animal between CONSECUTIVE accepted weighs, resolved
//     to a goat_id via goat_identifiers where possible; an unresolved
//     identifier still counts (grouped by its own normalized tag) because a
//     real weight was recorded even if identity resolution has not caught up.
//   - verification_status='rejected' observations are excluded from ADG math
//     entirely (they are not real weights); pending/unverified observations
//     ARE included, matching the standing decision in weight_history.go.
//   - There is NO weighing cadence rule at Mesha: weighing is free-flow, animals get weighed
//     whenever they get weighed, and no code here assumes or enforces a schedule (no "weekly",
//     no "monthly", no minimum-interval, no "overdue"/"missed weigh day"/"expected next weigh").
//     The ONLY pair excluded from ADG math is a same-timestamp pair, because it has no elapsed
//     time and the division is undefined -- that is arithmetic, not a judgment about frequency.
//     Every other pair, however close or far apart, contributes its ADG, and its days_between is
//     carried alongside it as plain data rather than silently dropped or scored for "confidence".
//   - No target/benchmark ADG value is included anywhere in this response;
//     none exists in product docs, and inventing one would misrepresent it
//     as an authoritative target. The only comparisons offered are the
//     population's own median/percentiles.
type GrowthADG struct {
	// ParkID is the single requested park, or "" when the caller omitted park_id and this
	// response aggregates across every park the caller is authorized to monitor -- see ParkIDs.
	ParkID string `json:"park_id,omitempty"`
	// ParkIDs lists every park this response aggregates across. Populated whenever the caller
	// omitted park_id (herd-wide view): the set is exactly the caller's own authorized-park scope
	// (reusing the same scoping helper other leadership reads use), never widened. When a single
	// park_id was requested, this is that one park's id, for a consistent contract either way.
	ParkIDs []string `json:"park_ids"`
	// Parks names the ids in ParkIDs so a client can offer a park selector without inventing
	// labels. Read from `locations`, which is an allowlisted org table -- NOT herd data.
	Parks []GrowthPark `json:"parks"`
	// LosingAnimals names the animals whose latest pair shows a LOSS. Returned so the headline
	// count is drillable: a tappable "15 losing" that leads nowhere specific is a dead end, and
	// the whole point of surfacing it is to let someone go and look at those animals.
	LosingAnimals   []GrowthLosingAnimal       `json:"losing_animals"`
	PeriodStart     string                     `json:"period_start"`
	PeriodEnd       string                     `json:"period_end"`
	Headline        GrowthADGHeadline          `json:"headline"`
	Eligibility     GrowthEligibility          `json:"eligibility"`
	Trend           []GrowthTrendPoint         `json:"trend"`
	ShedLeaderboard []GrowthShedLeaderboardRow `json:"shed_leaderboard"`
	Distribution    []GrowthDistributionBucket `json:"distribution"`
	SaleReadiness   GrowthSaleReadiness        `json:"sale_readiness"`
	LumpSum         GrowthLumpSum              `json:"lump_sum"`
}

// GrowthPark is the id/name pair behind a park scope option.
type GrowthPark struct {
	ParkID string `json:"park_id"`
	Name   string `json:"name"`
}

// GrowthLosingAnimal is one animal that lost weight between its two most recent weighs.
//
// Identified by its RAW SCANNED TAG only -- weighing never resolves a tag to a goat.
type GrowthLosingAnimal struct {
	ScannedIdentifier          string  `json:"scanned_identifier"`
	ShedDisplayName            string  `json:"shed_display_name"`
	OperationalLocationDisplay string  `json:"operational_location_display"`
	PreviousWeightKg           float64 `json:"previous_weight_kg"`
	LatestWeightKg             float64 `json:"latest_weight_kg"`
	ADGGPerDay                 float64 `json:"adg_g_per_day"`
	DaysBetween                float64 `json:"days_between"`
	LatestWeighDate            string  `json:"latest_weigh_date"`
}
