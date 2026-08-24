// Package domain holds the Growth Director read-model types.
//
// Growth Director is a READ-ONLY reporting module. It lives OUTSIDE
// backend/internal/weighing on purpose: weighing is isolated from the herd
// (it knows a scanned string and a weight, never an animal), while these
// widgets need breed and sex from `goats` via `goat_identifiers` and the
// feed-direction sheet. Putting this read in the weighing module would trip
// the free-flow isolation guard and violate the maintainer boundary, so it is
// its own module that CONSUMES weighing tables read-only and never gates any
// weighing behaviour.
package domain

// Actor is the authenticated caller, resolved from auth grants by the HTTP layer.
type Actor struct {
	TenantID string
	UserID   string
	Roles    []string
}

// DefaultPeriodDays is the reporting window when the caller names no dates:
// the last 7 inclusive Asia/Kolkata business days ending today, matching the
// Weights screen this section renders under.
const DefaultPeriodDays = 7

// MaxParks caps the unpaged park vocabulary, mirroring the weighing module's
// planner cap. Parks are physical farms; blowing this cap is a broken
// assumption, not an operating condition.
const MaxParks = 100

// SlowGrowthTargetGPerDay is the ops rule-of-thumb daily gain target. It is a
// CONSTANT deliberately disclosed in the response (`slow_growth.target_g_per_day`)
// rather than buried in SQL, so the screen can label it as a rule of thumb and a
// future authored target can replace it without a contract change.
const SlowGrowthTargetGPerDay = 200

// PeriodResolutionCampaignWeek discloses the window semantics: weighing data is
// selected by CAMPAIGN-WEEK OVERLAP (campaigns are week-grain, so a requested
// window pulls in every overlapping week in full), while feed rows use the exact
// feed_day range. One resolution string covers both because the difference is a
// property of the whole response, not of one widget.
const PeriodResolutionCampaignWeek = "campaign_week"

// Weight band labels, in ascending order. Thresholds are 15/20/25/30/35 kg;
// the two top bands are the sale-ready ones.
var BandLabels = []string{"<15", "15-20", "20-25", "25-30", "30-35", "35+"}

// Slow-growth statuses.
const (
	SlowGrowthStatusOnTrack     = "on_track"
	SlowGrowthStatusBelowTarget = "below_target"
	SlowGrowthStatusLosing      = "losing"
)

// Feed-vs-growth ADG bases.
const (
	FeedGrowthBasisPerAnimal = "per_animal"
	FeedGrowthBasisWholeShed = "whole_shed"
)

// Park is the id/name pair behind a park scope option.
type Park struct {
	ParkID string `json:"park_id"`
	Name   string `json:"name"`
}

// Period is the resolved reporting window. Start/End are INCLUSIVE Asia/Kolkata
// business dates; Resolution discloses that weighing snaps to overlapping
// campaign weeks while feed uses the exact day range.
type Period struct {
	Start      string `json:"start"`
	End        string `json:"end"`
	Resolution string `json:"resolution"`
}

// GrowthDirectorWeights is the whole Growth Director section: six widgets plus
// the envelope. Every widget carries its own denominators — there is NO
// expected-animal roster anywhere (the table was dropped), so every count is an
// actual-scan/identity count, never "of expected".
type GrowthDirectorWeights struct {
	Period       Period       `json:"period"`
	Parks        []Park       `json:"parks"`
	RoadToSale   RoadToSale   `json:"road_to_sale"`
	FairFight    FairFight    `json:"fair_fight"`
	SlowGrowth   SlowGrowth   `json:"slow_growth"`
	FeedVsGrowth FeedVsGrowth `json:"feed_vs_growth"`
	FeedProblems FeedProblems `json:"feed_problems"`
	Trust        Trust        `json:"trust"`
}

// RoadToSale places every identity's LATEST weight into a band on the way to
// sale weight. Identity = lower(btrim(scanned_identifier)); matched = the tag
// resolves through goat_identifiers (lifetime-unique per tenant, so 0..1).
// Unmatched identities stay in the bands — a scale reading is a scale reading —
// but are counted separately.
type RoadToSale struct {
	TotalIdentities     int          `json:"total_identities"`
	MatchedIdentities   int          `json:"matched_identities"`
	UnmatchedIdentities int          `json:"unmatched_identities"`
	Bands               []WeightBand `json:"bands"`
	Movement            BandMovement `json:"movement"`
}

// WeightBand is one weight band and the identities whose latest weight sits in it.
type WeightBand struct {
	Band          string `json:"band"`
	IdentityCount int    `json:"identity_count"`
}

// BandMovement compares each pair-eligible identity's previous-round band to its
// latest-round band. PairIdentities is the denominator: identities weighed in at
// least two campaign rounds (two captures inside ONE round dedupe to one and are
// not movement-eligible).
type BandMovement struct {
	PairIdentities int `json:"pair_identities"`
	MovedUp        int `json:"moved_up"`
	Held           int `json:"held"`
	MovedDown      int `json:"moved_down"`
}

// FairFight compares the SAME breed and sex across DIFFERENT sheds. A cohort
// only renders when at least two sheds each field at least three pair-identities,
// so a median never rests on a coin flip.
type FairFight struct {
	Cohorts []FairFightCohort `json:"cohorts"`
}

// FairFightCohort is one (breed, sex) cohort with its per-shed medians,
// strongest first. Breed and sex come ONLY from the herd register (goats via
// goat_identifiers, one-hop merge redirect) — never from shed names or feed
// sheet breed strings.
type FairFightCohort struct {
	Breed string          `json:"breed"`
	Sex   string          `json:"sex"`
	Sheds []FairFightShed `json:"sheds"`
}

// FairFightShed is one shed's showing inside a cohort.
type FairFightShed struct {
	LocationID string `json:"location_id"`
	// OperationalKey is the stable grouping identity: parent shed uuid plus
	// normalized partition (partition IS the operational shed).
	OperationalKey   string  `json:"operational_key"`
	ShedDisplayName  string  `json:"shed_display_name"`
	PairIdentities   int     `json:"pair_identities"`
	MedianADGGPerDay float64 `json:"median_adg_g_per_day"`
}

// SlowGrowth lists every (shed, breed, sex) group with at least three plausible
// pair-identities and labels its status against the disclosed target. Weight
// changes within 3% of starting body weight are scored as flat (gut fill /
// scale noise); losses steeper than 0.30 kg/day are excluded as bad scans.
type SlowGrowth struct {
	TargetGPerDay float64           `json:"target_g_per_day"`
	Groups        []SlowGrowthGroup `json:"groups"`
}

// SlowGrowthGroup is one (shed, breed, sex) group's growth.
// WeekOverWeekDeltaG is nil until the group has consecutive-round pairs in two
// distinct campaign weeks — two weeks of weighing are needed before a trend
// exists.
type SlowGrowthGroup struct {
	LocationID string `json:"location_id"`
	// OperationalKey is the stable grouping identity: parent shed uuid plus
	// normalized partition (partition IS the operational shed).
	OperationalKey     string   `json:"operational_key"`
	ShedDisplayName    string   `json:"shed_display_name"`
	Breed              string   `json:"breed"`
	Sex                string   `json:"sex"`
	PairIdentities     int      `json:"pair_identities"`
	MedianADGGPerDay   float64  `json:"median_adg_g_per_day"`
	WeekOverWeekDeltaG *float64 `json:"week_over_week_delta_g"`
	Status             string   `json:"status"`
}

// FeedVsGrowth sets feed DIRECTED against growth measured, per shed. Estimate
// is ALWAYS true: the feed figure is what the sheet told the team to give, not
// what the kids finished — leftovers are not measured yet.
type FeedVsGrowth struct {
	Sheds    []FeedVsGrowthShed `json:"sheds"`
	Estimate bool               `json:"estimate"`
}

// FeedVsGrowthShed is one shed's feed-vs-growth row. All three figures are
// nullable and null NEVER means zero: feed is null when no per-head feed basis
// exists (head-days zero, or every row experiment/informational), ADG is null
// when no growth pair exists, and the ratio is null whenever either side is
// missing or gain is non-positive. Basis discloses whether the growth figure is
// a per-animal median (>=3 pairs) or the whole-shed average movement (thinner
// data). IsExperiment marks sheds carrying experiment / informational-headcount
// rows, whose authored kg is a shed total and is never divided by heads.
type FeedVsGrowthShed struct {
	LocationID string `json:"location_id"`
	// PartitionLabel is the PEN within LocationID, blank for an undivided shed. It is half of
	// this row's identity, not decoration: the row grain is one pen, so a partitioned shed
	// returns up to ten rows under ONE location_id and location_id alone identifies none of
	// them. Required by the operational-location convention on every location-bearing response,
	// and required in practice by any client that keys a list on this row.
	PartitionLabel     string   `json:"partition_label"`
	ShedDisplayName    string   `json:"shed_display_name"`
	FeedGPerHeadPerDay *float64 `json:"feed_g_per_head_per_day"`
	ADGGPerDay         *float64 `json:"adg_g_per_day"`
	KgFeedPerKgGain    *float64 `json:"kg_feed_per_kg_gain"`
	Basis              string   `json:"basis"`
	IsExperiment       bool     `json:"is_experiment"`
	PairIdentities     int      `json:"pair_identities"`
}

// FeedProblems reports feed sheet cells that could NOT be filled in. Blocked is
// structural: quantity_kg IS NULL if and only if a blocked reason exists (schema
// CHECK), so no heuristic is involved. An authored 0.000 is a real instruction
// (milk-fed kids) and is counted separately, never listed as a problem.
type FeedProblems struct {
	BlockedRowsLatestDay  int               `json:"blocked_rows_latest_day"`
	BlockedRowsHistory    int               `json:"blocked_rows_history"`
	AuthoredZeroLatestDay int               `json:"authored_zero_latest_day"`
	AuthoredZeroHistory   int               `json:"authored_zero_history"`
	Items                 []FeedProblemItem `json:"items"`
}

// FeedProblemItem is one (shed, feed item) group of blocked cells, most recently
// blocked first.
type FeedProblemItem struct {
	ShedLabel        string `json:"shed_label"`
	FeedItemLabel    string `json:"feed_item_label"`
	BlockedDays      int    `json:"blocked_days"`
	LatestReasonCode string `json:"latest_reason_code"`
}

// Trust is the honest-denominator panel behind every other widget. Unlike the
// growth widgets it INCLUDES rework captures — trust reports the raw stream —
// and breaks out ScansRework so the two views reconcile.
type Trust struct {
	ScansTotal               int `json:"scans_total"`
	ScansMatched             int `json:"scans_matched"`
	ScansUnmatched           int `json:"scans_unmatched"`
	IdentitiesTotal          int `json:"identities_total"`
	IdentitiesWithPair       int `json:"identities_with_pair"`
	IdentitiesOnceOnly       int `json:"identities_once_only"`
	WholeShedObservations    int `json:"whole_shed_observations"`
	ScansPendingVerification int `json:"scans_pending_verification"`
	ScansRework              int `json:"scans_rework"`
}
