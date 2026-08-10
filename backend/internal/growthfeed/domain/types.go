// Package growthfeed answers ONE management question the Weights screen cannot
// answer today: given two pens holding the same kind of animal, why is one
// growing faster than the other, and what is each one being fed to get there.
//
// WHY THIS IS ITS OWN MODULE, AND NOT PART OF WEIGHING.
//
// Weighing is ISOLATED by maintainer lock (AGENTS.md -> "Weighing Is ISOLATED").
// It knows a scanned string and a weight; it reads no herd table and no other
// module's schema, with exactly one file-scoped exemption for the breed/sex/stage
// reporting read. Feed config is squarely another module's schema, so the answer
// to "what is this pen eating" can never live inside weighing.
//
// The join therefore lives OUTSIDE both modules, in this read-only reporting
// composition. Nothing here is on a write path: no scan is gated, no capture is
// validated, no feed sheet is issued, and no weighing or feed state is mutated.
// Weighing still does not read feed, and feed still does not read weighing —
// a third reader reads both and correlates them for leadership.
//
// WHAT THIS MODULE MAY READ, and nothing else:
//   - weighing_observations / weighing_shed_observations / weighing_campaign_sheds
//     / weighing_campaigns  (READ-ONLY, for the gain figures)
//   - goats                 (READ-ONLY, for the pen's breed / sex / stage cohort)
//   - locations             (park + shed + partition catalogue)
//   - feed_ration_groups / feed_shed_tags / feed_ration_rates / feed_shed_factors
//     / feed_item_catalog / feed_experiment_config  (READ-ONLY, authored config)
//
// Widening that list is a MAINTAINER decision, exactly as it is for weighing.
//
// CROSS-SURFACE PARITY. The gain numbers here are NOT a third opinion. They are
// computed from the same two definitions the Weights screen already renders —
// the per-animal pair median from weighing's growth read, and the four-week
// shed-average movement from its shed-weights read — so a pen reads the same
// g/day here as it does in the chart above it. See adapters/postgres/pens.go,
// which names the sibling query each arm mirrors.
package domain

// FeedPlanStatus explains what happened when this pen's ration was resolved. It
// exists because a missing number and a zero number mean opposite things in feed
// config: an authored 0 means "this pen genuinely eats none of that item" (milk-fed
// kids), while a MISSING rate row means "nobody ever configured it" and must never
// be read as zero. See migration 000001's CONFIGURED ZERO block.
type FeedPlanStatus string

const (
	// FeedPlanResolved means the pen's cohort matched an authored ration and at
	// least one feed item carries a rate. Only this status may produce a
	// feed-conversion ratio. It does NOT mean every catalog item is covered — the
	// grid is sparse by design; see ResolveFeedPlan.
	FeedPlanResolved FeedPlanStatus = "resolved"
	// FeedPlanExperiment means the pen is hand-configured in feed_experiment_config,
	// whose absolute_kg is a SHED TOTAL and never a per-head rate. A per-head figure
	// exists only when the pen's live head count is known to divide by.
	FeedPlanExperiment FeedPlanStatus = "experiment"
	// FeedPlanUnknownCohort means the pen's stage or breed could not be resolved to
	// one value, so there is no (ration group, shed tag) to look a ration up by. This
	// is the agree-or-go-bare rule: a pen holding several cohorts is reported as
	// unresolved rather than attributed to whichever one happens to sort first.
	FeedPlanUnknownCohort FeedPlanStatus = "unknown_cohort"
	// FeedPlanNoConfig means the cohort resolved but the park has no ration grid for
	// it at all.
	FeedPlanNoConfig FeedPlanStatus = "no_config"
)

// ADG basis labels. A per-animal median and a shed-average movement are DIFFERENT
// MEASUREMENTS and are never averaged together or compared with one another: the
// first is animals growing, the second also moves when animals join or leave the
// shed. They are kept apart here and, critically, in the peer group below.
const (
	ADGBasisPerAnimalMedian     = "per_animal_median"
	ADGBasisShedAverageMovement = "shed_average_movement"
)

// MinPeerPens is the smallest group that can produce a benchmark. A "peer median"
// over a single pen is that pen's own number, which would render every such pen at
// exactly 0% off the pace and read as confirmation that it is doing fine.
const MinPeerPens = 2

// Pen is one operational location — a partition if the shed is subdivided, the
// shed itself if not — carrying its growth, its cohort, and its authored ration.
//
// Every field a client renders as a location string is composed by the backend
// through platform/oploc; a renderer must never join shed name and partition
// itself (AGENTS.md -> Operational Location, Rule 5).
type Pen struct {
	ParkID   string `json:"park_id"`
	ParkName string `json:"park_name"`
	// LocationID + PartitionLabel is the pen's stable key. Shed NAME is never a key:
	// two parks both hold a "Castro", and name-keying merges them (OL-1).
	LocationID                 string  `json:"location_id"`
	ShedName                   string  `json:"shed_name"`
	PartitionLabel             *string `json:"partition_label"`
	OperationalLocationDisplay string  `json:"operational_location_display"`

	// ---- growth, from weighing ----

	// WeighingCategory is how this pen was weighed: individual_animal or
	// per_shed_partition. It decides which gain arm can apply and is shown on the
	// row, because the two rows do not answer the same questions.
	WeighingCategory string `json:"weighing_category"`
	AnimalsWeighed   int    `json:"animals_weighed"`
	// AverageWeightKg is nil when nothing was weighed in the window. It is NOT 0:
	// a herd that weighs nothing is a different, untrue statement.
	AverageWeightKg *float64 `json:"average_weight_kg"`
	// ADGGPerDay is grams per head per day. Nil means the pen has no second weigh
	// to measure movement against, which is a real and common state early on.
	ADGGPerDay *float64 `json:"adg_g_per_day"`
	ADGBasis   string   `json:"adg_basis"`
	// ADGSampleCount is the number of animal pairs behind a per-animal median, or
	// the animals behind a whole-shed weigh. It is the reader's cue for how much
	// weight to put on the figure.
	ADGSampleCount int  `json:"adg_sample_count"`
	ADGSpanDays    *int `json:"adg_span_days"`

	// ---- cohort, from the herd register ----

	// Breed / Sex / Stage are AGREE-OR-GO-BARE: populated only when every live
	// animal resolving to this pen shares one value. A pen holding nine breeds is
	// reported as unknown rather than attributed to one of them.
	Breed *string `json:"breed"`
	Sex   *string `json:"sex"`
	Stage *string `json:"stage"`
	// LiveAnimals is the pen's current cohort size from the herd register. It is a
	// DIFFERENT number from AnimalsWeighed (who turned up on the scale) and the two
	// are shown side by side rather than one standing in for the other.
	LiveAnimals int `json:"live_animals"`

	// ---- ration, from feed config ----

	RationGroupLabel *string        `json:"ration_group_label"`
	ShedTagLabel     *string        `json:"shed_tag_label"`
	FeedPlanStatus   FeedPlanStatus `json:"feed_plan_status"`
	// PlannedFeedGPerHeadDay is the authored ration: sum over feed items of
	// grams_per_head x shed factor. It is what the pen is MEANT to eat, not what it
	// was issued or what it consumed — this module reads config, never a feed sheet
	// or a distribution completion.
	PlannedFeedGPerHeadDay *float64 `json:"planned_feed_g_per_head_day"`
	// PlannedEnergyKcalPerHeadDay is nil whenever any contributing item has no
	// authored energy value, rather than silently omitting that item's energy and
	// reporting a smaller number as if it were complete.
	PlannedEnergyKcalPerHeadDay *float64 `json:"planned_energy_kcal_per_head_day"`
	// FeedItemsConfigured / FeedItemsBlocked are how many active feed items did and
	// did not carry a rate for this pen. Blocked is NOT an error: a ration covers
	// what the cohort eats and stays silent about the rest. They travel so a reader
	// can question a total that looks too small.
	FeedItemsConfigured int `json:"feed_items_configured"`
	FeedItemsBlocked    int `json:"feed_items_blocked"`

	// ---- derived comparison, computed by Benchmark ----

	// FeedPerKgGainKg is kilograms of feed per kilogram of gain: planned grams per
	// head per day divided by grams gained per head per day. Both terms are g/head/day
	// so the ratio is dimensionless, and it is the single most commercially useful
	// number on the row. Nil unless the ration is fully resolved AND the pen is
	// actually gaining — a pen that is losing weight has no meaningful conversion,
	// and a negative one would rank as "best".
	FeedPerKgGainKg *float64 `json:"feed_per_kg_gain_kg"`
	// PeerGroupKey is breed + stage + gain basis. Basis is IN the key deliberately:
	// ranking a per-animal median against a shed-average movement compares two
	// different measurements and would put a pen "behind" pens it was never
	// measured the same way as.
	PeerGroupKey string `json:"peer_group_key"`
	// PeerMedianADGGPerDay is the median across every pen in the group INCLUDING
	// this one. Median, not mean: one 8-head pen with a freak number must not drag
	// the line every other pen is judged against.
	PeerMedianADGGPerDay *float64 `json:"peer_median_adg_g_per_day"`
	// ADGVsPeerPct is this pen's gain against its peer median, as a percentage
	// difference. Nil when the group is too small to have a median, or when the
	// median is 0 (dividing by it manufactures an infinity).
	ADGVsPeerPct *float64 `json:"adg_vs_peer_pct"`
	PeerPenCount int      `json:"peer_pen_count"`
}

// PenGrowthFeed is the whole response. Rows are the pens; the counters explain
// what could NOT be answered, because a comparison screen that silently drops the
// pens it could not resolve reads as an estate where everything is comparable.
type PenGrowthFeed struct {
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
	Rows        []Pen  `json:"rows"`
	// PensWithoutCohort / PensWithoutRation / PensWithoutGain are the honesty
	// counters. Each is a count of rows PRESENT in Rows that could not answer that
	// part of the question — never a count of rows that were dropped.
	PensWithoutCohort int `json:"pens_without_cohort"`
	PensWithoutRation int `json:"pens_without_ration"`
	PensWithoutGain   int `json:"pens_without_gain"`
	// ComparablePens is how many rows landed in a peer group big enough to rank.
	ComparablePens int `json:"comparable_pens"`
}
