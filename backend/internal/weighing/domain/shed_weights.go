package domain

// Shed-weights is the admin-web "Kids — Weights" read model: what each shed
// weighed, most recently, across both capture modes.
//
// WHY THIS EXISTS AS ITS OWN READ. The columns it serves already exist in
// ceo_ai.weighing_capture_activity, but a core operator screen may not read the
// ceo_ai reporting schema (docs/decisions/ceo-ai-reporting-boundary.md, gated by
// `make ceo-ai-boundary-guard`), and LeadershipShedPage is an EVIDENCE gallery —
// it carries paged observation rows, so summing those in the client to build a
// KPI would be the capped read-time rollup anti-pattern
// (docs/decisions/scale-anti-patterns.md). Neither is a substitute for a read at
// the grain this screen renders.
//
// WEIGHING ISOLATION. Every field below comes from weighing's own tables plus
// `locations` for park/shed labels — the same allowlisted org-table join every
// other weighing read uses. Nothing here touches goats, goat_identifiers,
// herd_* or any vaccination table, so breed, sex, age and management stage are
// deliberately ABSENT: weighing knows a scanned string and a weight, and must
// not ask what animal that is.

// ShedWeightsRow is ONE shed's most recent weigh.
//
// GRAIN: one row per (park_id, location_id) — the physical shed, NOT the
// campaign bucket. A shed weighed in two consecutive campaigns owns two buckets;
// this screen answers "what does this shed weigh now", so the newest bucket
// carrying data wins and older ones are not emitted. Emitting bucket grain is
// what made the same shed appear twice in design review.
type ShedWeightsRow struct {
	LocationID string `json:"location_id"`
	ParkID     string `json:"park_id"`
	ParkName   string `json:"park_name"`
	// ShedDisplayName is resolved from locations.name, never from the bucket's own
	// display_name: that column is free text typed at planning time and has held
	// "M1P5", "C1" and "Mandela 2 Part 6" for sheds whose canonical names are
	// "Mandela 1 - Part 5", "Castro 1" and "Mandela 2 - Part 6".
	ShedDisplayName string `json:"shed_display_name"`
	// WeighingCategory is 'individual_animal' or 'per_shed_partition', fixed when
	// the bucket is planned. It decides which of the two observation tables holds
	// this shed's data; the write path never populates both for one bucket, so the
	// two sources are disjoint and are selected between, never summed.
	WeighingCategory string `json:"weighing_category"`
	// AnimalsWeighed counts ANIMALS, not captures. For an individual shed it is the
	// number of DISTINCT scanned tags — a reopened bucket re-scans tags that already
	// have a row, so count(*) over-reports. For a lump-sum shed it is the
	// operator-declared animal_count.
	AnimalsWeighed  int     `json:"animals_weighed"`
	AverageWeightKg float64 `json:"average_weight_kg"`
	// TotalWeightKg is the weight of the animals actually weighed. It is NOT the
	// shed's total weight: free-flow weighing has no roster, so nothing knows which
	// animals were missed.
	TotalWeightKg float64 `json:"total_weight_kg"`
	// LastWeighedDate is the Asia/Kolkata business DATE (YYYY-MM-DD) of the most
	// recent accepted weigh. Empty when the shed is planned but never weighed —
	// which is a fact, not a fault: weighing has no cadence, so a shed with no
	// recent weigh is never "overdue".
	LastWeighedDate string `json:"last_weighed_date,omitempty"`
	// BucketStatus is the weighing_campaign_sheds status of the bucket this row came
	// from: pending, in_progress, completed or canceled.
	BucketStatus string `json:"bucket_status"`
	// ShedAverageGainGPerDay is how fast this shed's AVERAGE weight is moving, for
	// WHOLE-SHED sheds that were weighed more than once in the window. Nil otherwise.
	//
	// IT IS NOT PER-ANIMAL GROWTH, and must never be labelled as such. A shed's
	// population changes between weighs — Castro 3 went 67 head to 64 — so if the
	// lightest animals leave, the average rises while no animal gained a gram. It
	// answers "is this shed getting heavier", which is a real and different question.
	//
	// Measured across the FULL span (first weigh to last) rather than between
	// consecutive weighs. Consecutive pairs are unusably noisy at this grain: the same
	// shed produced 45 g/day one week and 391 the next, and a two-day gap produced
	// +1,532 g/day — 1.5 kg per animal per day, which is impossible and is really the
	// short span amplifying a small change in composition.
	ShedAverageGainGPerDay *float64 `json:"shed_average_gain_g_per_day,omitempty"`
	// GainSpanDays is the span that gain was measured over, so a reader can discount a
	// figure drawn from two days against one drawn from a month. A short span is not
	// hidden or filtered — it is reported with its span attached.
	GainSpanDays int `json:"gain_span_days,omitempty"`
}

// ShedWeightsSummary is the whole-filter rollup behind the KPI cards.
//
// It is a WHOLE-RESULT aggregate, never a page rollup: pagination changes which
// rows are returned, never these numbers (docs/architecture/operational-read-model-contract.md
// rule 3). Clients render it verbatim and must not re-derive it from Rows.
type ShedWeightsSummary struct {
	// ShedsWeighed counts sheds with at least one accepted weigh in the window;
	// ShedsInScope counts every shed the filters select, weighed or not, so a client
	// can say "20 of 27" without a second call.
	ShedsWeighed int `json:"sheds_weighed"`
	ShedsInScope int `json:"sheds_in_scope"`
	// AnimalsWeighed and TotalWeightKg sum the same per-shed figures Rows carries, so
	// the cards and the table always reconcile.
	AnimalsWeighed int     `json:"animals_weighed"`
	TotalWeightKg  float64 `json:"total_weight_kg"`
	// AverageWeightKg is TotalWeightKg / AnimalsWeighed — a weighted mean over
	// ANIMALS, not the mean of the per-shed averages, which would let a 10-animal
	// shed pull as hard as a 73-animal one. Nil when nothing was weighed, so a client
	// never renders "0.0 kg" where it means "no data".
	AverageWeightKg *float64 `json:"average_weight_kg"`
	// AtOrAbove30Kg / AtOrAbove35Kg count ANIMALS over the sale thresholds, using each
	// tag's latest weight inside the window.
	//
	// THEY COVER PER-ANIMAL SHEDS ONLY, and ThresholdBasisAnimals is their real
	// denominator. A lump-sum shed reports one average for the whole shed, so it
	// cannot say how many of its animals cleared 30 kg — averaging is not counting,
	// and splitting the count by the average would invent a distribution nobody
	// measured. Showing these against AnimalsWeighed (which includes lump-sum
	// animals) would silently understate the share, so the basis travels with them.
	AtOrAbove30Kg         int `json:"at_or_above_30kg"`
	AtOrAbove35Kg         int `json:"at_or_above_35kg"`
	ThresholdBasisAnimals int `json:"threshold_basis_animals"`
}

// ShedWeights is the full response.
type ShedWeights struct {
	// Parks is the park vocabulary for the filter, restricted to the caller's own
	// authorized scope. Clients must not invent park labels.
	Parks   []GrowthPark       `json:"parks"`
	Summary ShedWeightsSummary `json:"summary"`
	Rows    []ShedWeightsRow   `json:"rows"`
	// PeriodStart / PeriodEnd echo the RESOLVED window (YYYY-MM-DD, Asia/Kolkata) so
	// the screen labels what it is actually showing rather than what it asked for.
	PeriodStart string `json:"period_start"`
	PeriodEnd   string `json:"period_end"`
}

// Sale-readiness thresholds, shared with GrowthSaleReadiness so the two reads
// cannot drift into two different definitions of "sale ready".
const (
	SaleThresholdLowerKg = 30.0
	SaleThresholdUpperKg = 35.0
)

// ShedWeightsDefaultPeriodDays is the default window when the caller names
// neither bound: four weeks, matching the screen's default filter.
const ShedWeightsDefaultPeriodDays = 28

// MaxShedWeightsRows bounds the shed list. Both parks together hold well over a
// hundred sheds, so this is a hard ceiling on one response rather than a page
// size — the screen renders every shed in scope and pages client-side, and this
// stops a tenant-wide read from returning an unbounded estate.
const MaxShedWeightsRows = 300
