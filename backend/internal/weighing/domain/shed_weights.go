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
// GRAIN: one row per (park_id, location_id, partition_label) — the physical operational shed, NOT the
// campaign bucket. A shed weighed in two consecutive campaigns owns two buckets;
// this screen answers "what does this shed weigh now", so the newest bucket
// carrying data wins and older ones are not emitted. Emitting bucket grain is
// what made the same shed appear twice in design review.
type ShedWeightsRow struct {
	LocationID string `json:"location_id"`
	ParkID     string `json:"park_id"`
	// ParkName is the park's SHORT CODE (CBE, CPT) when it has one, falling back to
	// its full name. The farm calls them CBE and CPT, and the full names cost a
	// column's width on every row for a word nobody uses.
	ParkName string `json:"park_name"`
	// ShedDisplayName is resolved from locations.name, never from the bucket's own
	// display_name: that column is free text typed at planning time and has held
	// "M1P5", "C1" and "Mandela 2 Part 6" for sheds whose canonical names are
	// "Mandela 1 - Part 5", "Castro 1" and "Mandela 2 - Part 6".
	ShedDisplayName            string `json:"shed_display_name"`
	PartitionLabel             string `json:"partition_label,omitempty"`
	OperationalLocationDisplay string `json:"operational_location_display"`
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
	// Measured from the first accepted weigh date inside the selected window to the
	// latest accepted weigh date in that same window. If the shed has only one
	// weighed date in the selected window, the value stays nil.
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
	// AnimalsWeighed and TotalWeightKg use the same backend denominator as the
	// daily-gain cards. Individual animals count when a tag has a prior weigh and a
	// selected-window endpoint; lump-sum pens count when the pen has prior/latest
	// points. Rows remain the latest shed snapshot, so clients must render this
	// summary verbatim instead of re-summing rows.
	AnimalsWeighed           int     `json:"animals_weighed"`
	IndividualAnimalsWeighed int     `json:"individual_animals_weighed"`
	LumpSumAnimalsWeighed    int     `json:"lump_sum_animals_weighed"`
	TotalWeightKg            float64 `json:"total_weight_kg"`
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

// LoadGainBucket is one PROCUREMENT LOAD's growth, blended across the sheds that
// load was placed into.
//
// The load a shed's animals came from is authored in weighing_shed_load_tags, a
// weighing-owned mapping — weighing never reads procurement's tables, so the
// isolation rule is untouched. See that migration for why the mapping is
// shed-level and what it therefore cannot do.
//
// GRAIN: one row per load_ref that has at least one weighed, unambiguously tagged
// shed. A shed carrying TWO loads is excluded from every load in this list and
// counted in LoadUnattributedSheds instead: one shed average cannot be split
// between two suppliers, and apportioning it by head count would invent a
// distribution nobody measured.
type LoadGainBucket struct {
	// LoadRef is the farm's own load number, rendered verbatim.
	LoadRef string `json:"load_ref"`
	// OwnerName is the supplier the load was bought from. Empty when unrecorded —
	// clients show the load alone rather than inventing a label.
	OwnerName string `json:"owner_name,omitempty"`
	// Sheds counts the tagged sheds behind this load that carry a weigh.
	Sheds int `json:"sheds"`
	// Animals is the head count at each shed's LATEST weigh, summed. It is the
	// denominator both figures below are weighted by.
	Animals int `json:"animals"`
	// AverageWeightKg is a weighted mean over ANIMALS across the load's sheds, never
	// a mean of per-shed averages, which would let a 10-head shed pull as hard as a
	// 73-head one.
	AverageWeightKg float64 `json:"average_weight_kg"`
	// GainGPerDay blends each shed's own selected-window movement, weighted by head
	// count. Nil when no shed in the load was weighed twice inside the selected
	// window — a load with only one weigh has a weight but no growth, and reporting
	// 0 would read as "flat".
	//
	// IT IS SHED-AVERAGE MOVEMENT, NOT PER-ANIMAL GROWTH, and carries every caveat
	// ShedWeightsRow.ShedAverageGainGPerDay does: a shed's population changes between
	// weighs, so if the lightest animals leave the average rises while no animal
	// gained a gram.
	GainGPerDay *float64 `json:"gain_g_per_day,omitempty"`
	// GainSpanDays is the widest span any contributing shed was measured over, so a
	// figure drawn from two days can be discounted on sight rather than hidden.
	GainSpanDays int `json:"gain_span_days,omitempty"`
	// Placements names WHERE this load's weighed animals actually are: one entry per
	// contributing operational shed row, park included. Without it the chart reports
	// that a supplier's stock grew without saying which park or shed grew it, and a
	// reader cannot walk from the load bar to the shed table below.
	//
	// It is the SAME key set the figures above are computed over -- the rows of
	// shed_latest that carry this load's tag -- so sum(Placements.Animals) equals
	// Animals and len(Placements) equals Sheds. Ordered park, then shed.
	Placements []LoadPlacement `json:"placements"`
}

// LoadPlacement is ONE operational shed a load's weighed animals sit in.
//
// GRAIN: one row per (location_id, partition_label) measured row behind the load,
// which is the grain the load's own averages blend. The load TAG itself is
// authored at physical-shed grain (weighing_shed_load_tags keys on location_id),
// so a partition shown here says where the weighed animals are, never that the
// tag was authored per pen.
type LoadPlacement struct {
	// ParkName is the park's SHORT CODE (CBE, CPT) when it has one, falling back to
	// its full name -- the same rule ShedWeightsRow.ParkName follows, so the two
	// surfaces cannot disagree about what a park is called.
	ParkName string `json:"park_name"`
	// ShedDisplayName comes from locations.name, never from the weighing bucket's
	// own display_name: that column is free text typed at planning time.
	ShedDisplayName            string `json:"shed_display_name"`
	PartitionLabel             string `json:"partition_label,omitempty"`
	OperationalLocationDisplay string `json:"operational_location_display"`
	// Animals is this shed row's head count at its LATEST weigh -- the same figure
	// that weights it inside the load's blended average.
	Animals int `json:"animals"`
}

// ShedWeights is the full response.
type ShedWeights struct {
	// Parks is the park vocabulary for the filter, restricted to the caller's own
	// authorized scope. Clients must not invent park labels.
	Parks   []GrowthPark       `json:"parks"`
	Summary ShedWeightsSummary `json:"summary"`
	Rows    []ShedWeightsRow   `json:"rows"`
	// ByLoad is the procurement-load breakdown, strongest grower first. Empty when no
	// shed in scope carries a load tag, which is the normal state until the mapping
	// is authored for a tenant.
	ByLoad []LoadGainBucket `json:"by_load"`
	// LoadUnattributedSheds counts weighed sheds that carry NO load tag or MORE THAN
	// ONE. Returned so the gap between the load chart and the shed table is legible
	// as unmapped rather than looking like missing weighing data.
	LoadUnattributedSheds int `json:"load_unattributed_sheds"`
	// LumpWeighingDates is the distinct set of Asia/Kolkata business dates inside
	// the resolved window where at least one live lump-sum weighing was accepted.
	// The Weights page calendar renders these as day markers, so the marker's grain
	// is the same park/window filter the reader is already using.
	LumpWeighingDates []string `json:"lump_weighing_dates"`
	// LatestWeighingDate is the Asia/Kolkata business DATE of the most recent weigh of ANY kind --
	// individual or whole-shed -- inside the queried range. Empty when nothing was weighed.
	//
	// It exists because the Weights page opens on "the last two whole-shed weigh dates", and a
	// window whose END came from that same lump-only set silently dropped every kid weighed since:
	// 199 kids scanned across 17 sheds on 25 Aug fell outside a window that closed on the 24th,
	// purely because the 25th had no whole-shed weigh to put it on that map. The START still comes
	// from the lump dates -- two of them are what make a shed-average movement measurable -- but the
	// END is the last day the farm weighed anything at all.
	LatestWeighingDate string `json:"latest_weighing_date,omitempty"`
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
// neither bound: the last 15 inclusive days, matching the screen's default filter.
const ShedWeightsDefaultPeriodDays = 15

// MaxShedWeightsRows bounds the shed list. Both parks together hold well over a
// hundred sheds, so this is a hard ceiling on one response rather than a page
// size — the screen renders every shed in scope and pages client-side, and this
// stops a tenant-wide read from returning an unbounded estate.
const MaxShedWeightsRows = 300
