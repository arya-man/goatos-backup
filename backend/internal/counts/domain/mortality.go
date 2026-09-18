package domain

import (
	"strings"
	"time"
)

// Mortality is the Counts leadership read that answers ONE question from every angle the
// farm can ask it: which animals died, and what did they have in common.
//
// It introduces no fact of its own. A death is the animal's own exit row (`exit_reason =
// 'died'`, dated the IST day of `exited_at`), exactly as Herd Analytics counts it, so the
// Deaths tile here and the Deaths tile there can never disagree. Every attribute a death is
// sliced by -- breed, sex, pen tag, pen, farm, date of birth, purchase load -- is read off the
// animal's own row as it stood WHEN IT DIED, because the exit UPDATE touches only the
// lifecycle columns and leaves everything else frozen. The cause is Health's
// `health_death_causes` row where the death form recorded one.
//
// TWO KINDS OF SERIES, and the payload names which is which:
//
//   RATE series carry a denominator. `at_risk` is the animals that were on the farm at any
//   point in the window -- alive today with an entry date inside or before the window, or
//   exited during it -- grouped by the SAME attribute the deaths are. A breed with 3 deaths
//   out of 40 at risk and one with 3 out of 400 read very differently, and the rate is what
//   separates them. `rate_pct` is deaths / at_risk and is omitted when at_risk is zero.
//
//   COUNT series carry deaths alone. Age at death, season, cause, days since arrival and
//   days since the last vaccination are facts ABOUT a death and have no living counterpart
//   to divide by; a rate there would be an invented number.
//
// TIME GRAIN IS THE INDIA BUSINESS DAY, and every derived calendar fact -- month, season --
// is taken from the death's IST date, never a UTC instant.

// MortalityBucket is one slice of the deaths in the window.
//
// Key is the raw stored value (empty for an unrecorded one) or a fixed band key the domain
// labels; Label is what a reader sees. AtRisk and RatePct are set only on RATE series; on a
// COUNT series both are zero and the client must not draw a rate.
type MortalityBucket struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Deaths int64  `json:"deaths"`
	// AtRisk is the denominator for a rate series: animals of this bucket that were on the
	// farm during the window (live today, or exited inside the window). Zero on count series.
	AtRisk int64 `json:"at_risk"`
	// RatePct is deaths as a percentage of at_risk, nil when there is no denominator.
	RatePct *float64 `json:"rate_pct,omitempty"`
	// Basis is set on the CAUSE series only: "recorded" when the death form named the cause,
	// "inferred" when an older death is attributed from a case that was open when the animal
	// died, "none" for a death with no cause established. Empty elsewhere.
	Basis string `json:"basis,omitempty"`
}

// MortalityMonth is one India-calendar month of deaths, kids and adults kept apart because
// their causes differ enough that one line hides both.
type MortalityMonth struct {
	Month  string `json:"month"`
	Label  string `json:"label"`
	Deaths int64  `json:"deaths"`
	Kids   int64  `json:"kids"`
	Adults int64  `json:"adults"`
}

// MortalityCrossCell is one cell of a two-dimension cross tab (season x stage, load x
// cause, breed x cause). Only cells with at least one death are returned; the client draws
// the empty ones.
type MortalityCrossCell struct {
	RowKey   string `json:"row_key"`
	RowLabel string `json:"row_label"`
	ColKey   string `json:"col_key"`
	ColLabel string `json:"col_label"`
	Deaths   int64  `json:"deaths"`
}

// MortalityDeath is one animal in the bounded most-recent list under the charts. Every
// label is backend-resolved so the client renders no id.
type MortalityDeath struct {
	GoatID       string `json:"goat_id"`
	DisplayID    string `json:"display_id"`
	Tag          string `json:"tag"`
	DiedOn       string `json:"died_on"`
	Breed        string `json:"breed"`
	Sex          string `json:"sex"`
	Stage        string `json:"stage"`
	AgeDays      *int64 `json:"age_days,omitempty"`
	AgeBandKey   string `json:"age_band_key"`
	AgeBandLabel string `json:"age_band_label"`
	Park         string `json:"park"`
	// Pen is the operational location display, "Castro 1" or "Godel 1 - Part 3".
	Pen     string `json:"pen"`
	LoadRef string `json:"load_ref"`
	// CauseKey is the raw recorded key (empty unless CauseBasis is "recorded"); CauseLabel is
	// the farm word a reader sees.
	CauseKey   string `json:"cause_key"`
	CauseLabel string `json:"cause_label"`
	CauseBasis string `json:"cause_basis"`
	Season     string `json:"season"`
}

// MortalityTotals are whole-window rollups. Computed server-side and never re-derived from
// the series by a client.
type MortalityTotals struct {
	Deaths  int64    `json:"deaths"`
	AtRisk  int64    `json:"at_risk"`
	RatePct *float64 `json:"rate_pct,omitempty"`

	KidDeaths    int64    `json:"kid_deaths"`
	KidAtRisk    int64    `json:"kid_at_risk"`
	KidRatePct   *float64 `json:"kid_rate_pct,omitempty"`
	AdultDeaths  int64    `json:"adult_deaths"`
	AdultAtRisk  int64    `json:"adult_at_risk"`
	AdultRatePct *float64 `json:"adult_rate_pct,omitempty"`

	// FirstWeekDeaths is the 0-7 day band alone: the single highest-risk window on any
	// goat farm, so it earns its own tile beside the kid figure it is part of.
	FirstWeekDeaths int64 `json:"first_week_deaths"`

	// CauseRecorded / CauseInferred / CauseNone partition Deaths exactly. A high "none"
	// share is a finding in itself: the farm is not establishing why animals die.
	CauseRecorded int64 `json:"cause_recorded"`
	CauseInferred int64 `json:"cause_inferred"`
	CauseNone     int64 `json:"cause_none"`
}

// Mortality is the whole page payload, already scoped to the requested park and window.
type Mortality struct {
	WindowFrom string           `json:"window_from"`
	WindowTo   string           `json:"window_to"`
	Totals     MortalityTotals  `json:"totals"`
	Months     []MortalityMonth `json:"months"`

	// RATE series (deaths beside at-risk, same attribute on both sides).
	KidAdult []MortalityBucket `json:"kid_adult"`
	Stage    []MortalityBucket `json:"stage"`
	Breed    []MortalityBucket `json:"breed"`
	Sex      []MortalityBucket `json:"sex"`
	Species  []MortalityBucket `json:"species"`
	Park     []MortalityBucket `json:"park"`
	Pen      []MortalityBucket `json:"pen"`
	Load     []MortalityBucket `json:"load"`

	// COUNT series (facts about the death alone).
	AgeAtDeath       []MortalityBucket `json:"age_at_death"`
	Season           []MortalityBucket `json:"season"`
	Cause            []MortalityBucket `json:"cause"`
	DaysSinceArrival []MortalityBucket `json:"days_since_arrival"`
	DaysSinceVaccine []MortalityBucket `json:"days_since_vaccination"`

	// Cross tabs.
	SeasonByStage []MortalityCrossCell `json:"season_by_stage"`
	LoadByCause   []MortalityCrossCell `json:"load_by_cause"`
	BreedByCause  []MortalityCrossCell `json:"breed_by_cause"`

	// Deaths is the bounded most-recent list; RecentLimit says how many it was capped at.
	Deaths      []MortalityDeath `json:"deaths"`
	RecentLimit int              `json:"recent_limit"`

	GeneratedAt time.Time `json:"generated_at"`
}

// MortalityQuery scopes the read. The window is two inclusive IST business days and
// resolves through the SAME rules Herd Analytics uses (ResolveHerdAnalyticsWindow), so the
// two Counts leadership screens agree on what a default window is.
type MortalityQuery struct {
	TenantID string
	ParkID   *string
	FromDate string
	ToDate   string
}

// MortalityRecentLimit caps the per-animal list. The counts above it are whole-window
// figures computed by their own queries and do not move with this cap.
const MortalityRecentLimit = 50

// ---------------------------------------------------------------------------
// Band vocabularies
//
// The SQL emits a stable KEY per band; the label a reader sees is resolved here, in ONE
// place, so a Go test can pin the boundaries and a client never composes copy from a key.
// ---------------------------------------------------------------------------

// MortalitySeasonKey buckets an IST calendar month into the farm's four seasons. The farm
// is in Karnataka / Tamil Nadu: summer heat runs March-May, the south-west monsoon
// June-September, the retreating (north-east) monsoon October-November, and the cool dry
// season December-February. Parasite load, heat stress and cold stress each follow one of
// these, which is why the split is on the page at all.
func MortalitySeasonKey(month time.Month) string {
	switch month {
	case time.March, time.April, time.May:
		return "summer"
	case time.June, time.July, time.August, time.September:
		return "monsoon"
	case time.October, time.November:
		return "post_monsoon"
	default:
		return "winter"
	}
}

// MortalitySeasonOrder is the fixed display order; a quiet season is still returned as an
// explicit zero so a reader sees "none in summer" rather than a missing bar.
var MortalitySeasonOrder = []string{"summer", "monsoon", "post_monsoon", "winter"}

var mortalitySeasonLabels = map[string]string{
	"summer":       "Summer (Mar–May)",
	"monsoon":      "Monsoon (Jun–Sep)",
	"post_monsoon": "Post-monsoon (Oct–Nov)",
	"winter":       "Winter (Dec–Feb)",
}

// MortalitySeasonLabel resolves a season key to farm copy.
func MortalitySeasonLabel(key string) string {
	if label, ok := mortalitySeasonLabels[key]; ok {
		return label
	}
	return key
}

// MortalityAgeBandKey buckets age at death in days. The 0-7 day band is the neonatal
// window and stands alone; 8-30 is the rest of the first month; 31-90 is pre-weaning; 91-180
// and 181-365 are the growing kid; over a year is an adult. A death with no date of birth
// is "unknown", never folded into a band.
func MortalityAgeBandKey(ageDays *int64) string {
	if ageDays == nil || *ageDays < 0 {
		return "unknown"
	}
	d := *ageDays
	switch {
	case d <= 7:
		return "d0_7"
	case d <= 30:
		return "d8_30"
	case d <= 90:
		return "d31_90"
	case d <= 180:
		return "d91_180"
	case d <= 365:
		return "d181_365"
	default:
		return "over_1y"
	}
}

// MortalityAgeBandOrder is the fixed display order, youngest first.
var MortalityAgeBandOrder = []string{"d0_7", "d8_30", "d31_90", "d91_180", "d181_365", "over_1y", "unknown"}

var mortalityAgeBandLabels = map[string]string{
	"d0_7":     "0–7 days",
	"d8_30":    "8–30 days",
	"d31_90":   "1–3 months",
	"d91_180":  "3–6 months",
	"d181_365": "6–12 months",
	"over_1y":  "Over 1 year",
	"unknown":  "Age not recorded",
}

// MortalityAgeBandLabel resolves an age band key to farm copy.
func MortalityAgeBandLabel(key string) string {
	if label, ok := mortalityAgeBandLabels[key]; ok {
		return label
	}
	return key
}

// MortalityDaysSinceKey buckets "days between an event and the death" -- arrival on the
// farm, or the last accepted vaccination. The first week after arrival is transport stress
// and incoming disease; the first week after a shot is the reaction window.
func MortalityDaysSinceKey(days *int64) string {
	if days == nil || *days < 0 {
		return "unknown"
	}
	d := *days
	switch {
	case d <= 7:
		return "d0_7"
	case d <= 30:
		return "d8_30"
	case d <= 90:
		return "d31_90"
	default:
		return "over_90"
	}
}

// MortalityDaysSinceOrder is the fixed display order for both days-since series.
var MortalityDaysSinceOrder = []string{"d0_7", "d8_30", "d31_90", "over_90"}

var mortalityDaysSinceLabels = map[string]string{
	"d0_7":    "Within 7 days",
	"d8_30":   "8–30 days",
	"d31_90":  "31–90 days",
	"over_90": "Over 90 days",
}

// MortalityDaysSinceArrivalLabel labels the days-since-arrival bands. "unknown" here means
// the animal was born on the farm or its arrival was never dated -- a complete answer.
func MortalityDaysSinceArrivalLabel(key string) string {
	if key == "unknown" {
		return "Farm born / no arrival date"
	}
	if label, ok := mortalityDaysSinceLabels[key]; ok {
		return label
	}
	return key
}

// MortalityDaysSinceVaccineLabel labels the days-since-last-vaccination bands. "unknown"
// means no accepted vaccination on record before the death.
func MortalityDaysSinceVaccineLabel(key string) string {
	if key == "unknown" {
		return "Never vaccinated"
	}
	if label, ok := mortalityDaysSinceLabels[key]; ok {
		return label
	}
	return key
}

// Cause basis keys, mirrored by the client for its chip tone only.
const (
	MortalityCauseRecorded = "recorded"
	MortalityCauseInferred = "inferred"
	MortalityCauseNone     = "none"
)

// MortalityCauseNoneLabel is the one cause label the domain owns: a death with no cause
// established. It is deliberately not "Unknown" -- the operator answered "normal death"
// or the death predates the field, and both are complete answers.
const MortalityCauseNoneLabel = "No cause recorded"

// MortalityRatePct returns deaths as a percentage of atRisk, nil when there is nothing to
// divide by. One decimal, because "7.5%" is the grain a farm reads a mortality rate at and
// "7.4999" reads as false precision.
func MortalityRatePct(deaths, atRisk int64) *float64 {
	if atRisk <= 0 {
		return nil
	}
	pct := float64(deaths) * 100 / float64(atRisk)
	rounded := float64(int64(pct*10+0.5)) / 10
	return &rounded
}

// MortalityKidAdultLabel labels the two age-band keys the kid/adult series carries.
func MortalityKidAdultLabel(key string) string {
	switch strings.ToLower(key) {
	case "kid":
		return "Kids"
	case "adult":
		return "Adults"
	}
	return key
}
