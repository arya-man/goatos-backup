package domain

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Feed by weight band (maintainer request 2026-09-18): the LATEST locked feed
// direction for every pen, set beside the pen's latest weight evidence, so a
// reader can ask "what are the 25-30 kg animals eating" without leaving the
// ADG Analytics screen.
//
// This is a Growth Director read for the same reason feed-vs-growth is: it
// joins the feed-direction sheet to weighing tables, and the weighing module
// may read only weighing-owned tables. Read-only, reporting only, and never a
// gate on any capture.
//
// WEIGHT EVIDENCE PRECEDENCE, per park + pen: the latest non-withdrawn
// whole-pen (lump-sum) weigh wins and produces ONE row banded on the pen
// average. Only a pen with no lump-sum weigh falls back to per-animal
// evidence: the latest weigh of every scanned tag across the park, attributed
// to the pen of THAT latest weigh, then grouped into 5 kg bands -- one row per
// band. A feed rollup whose pen has neither is EXCLUDED, and the count of
// excluded rollups is reported so the gap is visible rather than read as a
// pen nobody feeds.

// Weight source keys on a feed-band row. Stable keys, never copy.
const (
	FeedBandSourcePenAverage = "pen_average"
	FeedBandSourcePerAnimal  = "per_animal"
)

// FeedBandKeys are the six 5 kg bands in ascending order. Same thresholds the
// Weight-wise chart bands on, and the same keys its page copy names.
var FeedBandKeys = []string{"under_15", "15_20", "20_25", "25_30", "30_35", "35_plus"}

// FeedWeightBandRow is one feed rollup (park, pen, shed tag, ration group,
// experiment arm, breed, workflow) matched to one piece of weight evidence.
type FeedWeightBandRow struct {
	ParkID   string `json:"park_id"`
	ParkName string `json:"park_name"`
	// WeightSource is pen_average or per_animal.
	WeightSource string `json:"weight_source"`
	// Band is the stable band key: under_15, 15_20, 20_25, 25_30, 30_35, 35_plus.
	Band string `json:"band"`
	// Pen is the operator-facing pen label ("Godel 1 - Part 3", "Castro 1").
	Pen string `json:"pen"`
	// ShedTag is the feed sheet's stage tag as stored ("F2-Male", "K3 + K1").
	ShedTag string `json:"shed_tag"`
	// Group is the DISPLAY group the shed tag reads as (F2-Male -> Fattening), suffixed with
	// the shed tag when two rollups in one pen would otherwise read identically.
	Group string `json:"group"`
	// Gender is read off the ANIMALS behind the weight, never off the shed tag: "Male",
	// "Female", "Mixed 12F·10M", or empty when no animal resolves in the herd register.
	Gender string `json:"gender"`
	// Breed is the display breed: " x " rendered as " cross ".
	Breed string `json:"breed"`
	// FeedType is the workflow: normal or experiment.
	FeedType      string `json:"feed_type"`
	RationGroup   string `json:"ration_group"`
	ExperimentArm string `json:"experiment_arm,omitempty"`
	// FeedGiven is the items joined with " + ", each as "<item> <g>g/head".
	FeedGiven string `json:"feed_given"`
	// PenKgPerDay is the pen's authored kg/day across items and sessions.
	PenKgPerDay float64 `json:"pen_kg_per_day"`
	// WeightAnimals is the ON-FARM head count behind AverageWeightKg: the lump-sum count for a
	// pen-average row, the on-farm scanned animals in the band for a per-animal row. Zero on a
	// band row that exists only because of animals that have since left.
	WeightAnimals   int     `json:"weight_animals"`
	AverageWeightKg float64 `json:"average_weight_kg"`
	// WeightAnimalsAll / AverageWeightKgAll / GenderAll are the same figures counting every
	// weighed animal including those that have since exited, so the screen's Animals toggle needs
	// no second read. Equal to the on-farm figures on a pen-average row.
	WeightAnimalsAll   int     `json:"weight_animals_all"`
	AverageWeightKgAll float64 `json:"average_weight_kg_all"`
	GenderAll          string  `json:"gender_all"`
	// ExitedAnimals is how many weighed animals of this band have since EXITED the register
	// (goats.exited_at set: sold, died, or any other exit): outside WeightAnimals, inside
	// WeightAnimalsAll; always 0 on a pen-average row, whose head count is a frozen census.
	ExitedAnimals int `json:"exited_animals"`
	// ExitedSold + ExitedDied + ExitedOther = ExitedAnimals, bucketed by FeedExitBucket.
	ExitedSold  int `json:"exited_sold"`
	ExitedDied  int `json:"exited_died"`
	ExitedOther int `json:"exited_other"`
}

// Exit buckets: the register's lifecycle status / exit reason folded to the three words the
// screen shows. "sold" and "died" are the same buckets Herd Analytics reports as Sold and
// Deaths; everything else with an exited_at (inactive, transferred, lost, culled, blank) is
// "other" -- shown, never hidden, never mislabelled as dead.
const (
	FeedExitSold  = "sold"
	FeedExitDied  = "died"
	FeedExitOther = "other"
)

// FeedExitBucket folds goats.lifecycle_status / goats.exit_reason to one of the three exit
// buckets. Mirrored by the sold / died flags in feedWeightBandSQL (animal_sex CTE).
func FeedExitBucket(lifecycleStatus, exitReason string) string {
	ls := strings.ToLower(strings.TrimSpace(lifecycleStatus))
	er := strings.ToLower(strings.TrimSpace(exitReason))
	switch {
	case ls == "sold" || er == "sold":
		return FeedExitSold
	case ls == "dead" || ls == "died" || er == "dead" || er == "died":
		return FeedExitDied
	default:
		return FeedExitOther
	}
}

// FeedWeightBandUnmatched is one feed rollup on the latest sheet whose pen has no qualifying
// weighing in the period -- the "Not shown" list: on the sheet, counted, never banded.
type FeedWeightBandUnmatched struct {
	ParkID        string  `json:"park_id"`
	ParkName      string  `json:"park_name"`
	Pen           string  `json:"pen"`
	ShedTag       string  `json:"shed_tag"`
	Group         string  `json:"group"`
	RationGroup   string  `json:"ration_group"`
	ExperimentArm string  `json:"experiment_arm,omitempty"`
	Breed         string  `json:"breed"`
	FeedType      string  `json:"feed_type"`
	FeedGiven     string  `json:"feed_given"`
	PenKgPerDay   float64 `json:"pen_kg_per_day"`
}

// FeedWeightBandExit is one animal that exited the register inside the period (sold, died or
// other), with its last weigh in the period when it has one -- the "Exited in period" panel.
type FeedWeightBandExit struct {
	ParkID   string `json:"park_id"`
	ParkName string `json:"park_name"`
	Tag      string `json:"tag"`
	// Pen is the pen of the animal's last weigh in the period, or, for an animal not weighed in
	// it, the register's current placement; empty when neither is known.
	Pen string `json:"pen"`
	// WeighedInPeriod says whether the animal has a weigh inside the period (and so may sit on a
	// band row); the last-weigh fields are present exactly when it is true.
	WeighedInPeriod bool `json:"weighed_in_period"`
	// Gender is the register's sex as display copy ("Male" / "Female"), empty when unknown.
	Gender string `json:"gender"`
	// Reason is the register's exit reason as stored, with the lifecycle status beside it for
	// the cases where the reason is blank; Bucket is FeedExitBucket of the two (sold / died /
	// other), the pill the panel shows beside the stored text.
	Reason          string `json:"reason"`
	LifecycleStatus string `json:"lifecycle_status"`
	Bucket          string `json:"bucket"`
	// ExitedAt is the exit business date, YYYY-MM-DD.
	ExitedAt string `json:"exited_at"`
	// LastWeighedAt is the business date of the last weigh in the period, or empty.
	LastWeighedAt string `json:"last_weighed_at,omitempty"`
	// LastBand and LastWeightKg describe that weigh; both absent when never weighed.
	LastBand     string   `json:"last_band,omitempty"`
	LastWeightKg *float64 `json:"last_weight_kg,omitempty"`
	// FeedType and FeedGiven are what the animal's last pen is fed on the latest sheet (its
	// first rollup); both empty when that pen has no feed row today.
	FeedType  string `json:"feed_type,omitempty"`
	FeedGiven string `json:"feed_given,omitempty"`
}

// FeedWeightBandReconciliation is the count at every stage from sheet rows to
// output rows, so a reader can see where rows went instead of guessing.
type FeedWeightBandReconciliation struct {
	// FeedDay is the sheet day the rows were read from (YYYY-MM-DD); empty when there is
	// no locked/amended sheet at all.
	FeedDay string `json:"feed_day"`
	// PositiveRows is every sheet row with quantity_kg > 0 on the latest issue per park and
	// workflow.
	PositiveRows int `json:"positive_rows"`
	// CollapsedItems is those rows with session duplicates summed per feed item.
	CollapsedItems int `json:"collapsed_items"`
	// Rollups is the items rolled up per (park, pen, shed tag, ration group, arm, breed,
	// workflow). The feed side: never narrowed by the period or the sex filter.
	Rollups int `json:"rollups"`
	// MatchedRollups is the rollups with at least one weight row in the period whose on-farm
	// head count is above zero; MatchedRollupsAll counts a rollup whose only weighed animals
	// have since left as matched too (the screen's "Include exited" reading).
	MatchedRollups    int `json:"matched_rollups"`
	MatchedRollupsAll int `json:"matched_rollups_all"`
	// ExcludedRollups is Rollups - MatchedRollups (and the *All twin): fed pens with no such
	// weighing.
	ExcludedRollups    int `json:"excluded_rollups"`
	ExcludedRollupsAll int `json:"excluded_rollups_all"`
	// OutputRows is the rows served, every variant included: one per matched rollup for a
	// pen-average pen, one per band for a per-animal pen (a band with only exited animals is a
	// row with weight_animals = 0).
	OutputRows int `json:"output_rows"`
	// IndividualAnimalsWeighed and LumpSumAnimalsWeighed are the weighing side's own totals
	// BEFORE any feed match, at the General tab's grain -- the figures that tab's Individual /
	// Lump sum split reports for the same filters. Individual counts exited animals too, as
	// that tab does.
	IndividualAnimalsWeighed int `json:"individual_animals_weighed"`
	LumpSumAnimalsWeighed    int `json:"lump_sum_animals_weighed"`
	// ExitedAnimals is how many animals exited the register inside the period under the page's
	// park / sex / origin filters -- the same population Herd Analytics' exits count (the
	// panel's list); ExitedSold + ExitedDied + ExitedOther = ExitedAnimals (FeedExitBucket), and
	// ExitedWeighed + ExitedNotWeighed = ExitedAnimals (weighed inside the period or not; only
	// the weighed ones can sit on a band row).
	ExitedAnimals    int `json:"exited_animals"`
	ExitedSold       int `json:"exited_sold"`
	ExitedDied       int `json:"exited_died"`
	ExitedOther      int `json:"exited_other"`
	ExitedWeighed    int `json:"exited_weighed"`
	ExitedNotWeighed int `json:"exited_not_weighed"`
}

type FeedWeightBand struct {
	Reconciliation FeedWeightBandReconciliation `json:"reconciliation"`
	// Rows are ordered park, then pen-average rows before per-animal rows, then pen,
	// band, shed tag.
	Rows []FeedWeightBandRow `json:"rows"`
	// Unmatched are the feed rollups with no qualifying weighing in the period, ordered park,
	// pen, shed tag.
	Unmatched []FeedWeightBandUnmatched `json:"unmatched"`
	// Exited are the animals sold or dead inside the period, newest exit first.
	Exited []FeedWeightBandExit `json:"exited"`
}

// FeedBandForKg bands a weight the same way the Weight-wise chart does:
// lower-inclusive, upper-exclusive, so every weight lands in exactly one band.
func FeedBandForKg(kg float64) string {
	switch {
	case kg < 15:
		return "under_15"
	case kg < 20:
		return "15_20"
	case kg < 25:
		return "20_25"
	case kg < 30:
		return "25_30"
	case kg < 35:
		return "30_35"
	default:
		return "35_plus"
	}
}

// FeedBandIndex orders band keys ascending; an unknown key sorts last.
func FeedBandIndex(band string) int {
	for i, key := range FeedBandKeys {
		if key == band {
			return i
		}
	}
	return len(FeedBandKeys)
}

// feedItemDisplayPrefixes are stripped from feed item labels for display: every
// item on the farm's sheet is Mesha's, so the brand adds width and no meaning.
var feedItemDisplayPrefixes = []string{"Dry Masoor ", "Mesha "}

// FeedItemDisplay strips the sheet's brand prefixes from a feed item label.
func FeedItemDisplay(label string) string {
	out := strings.TrimSpace(label)
	for _, prefix := range feedItemDisplayPrefixes {
		out = strings.TrimPrefix(out, prefix)
	}
	return out
}

// BreedDisplay renders the sheet's cross notation the way the farm says it:
// "Beetal x Sojat" reads "Beetal cross Sojat".
func BreedDisplay(breed string) string {
	return strings.ReplaceAll(strings.TrimSpace(breed), " x ", " cross ")
}

// ShedTagGroup is the display group a shed tag reads as. The mapping is the one
// the rest of the admin surfaces use for stage tags (the command board's cohort
// ladder and the counts summary cards): F2-* is the fattening kid cohort, K0-K3
// and ICU-Kid are kids, Buck / Mother / Non-Pregnant are the adult herd, Warmup
// is the arrival state. A composite tag ("F2-Male + K3") joins its distinct
// groups with " + ". The tag says NOTHING about gender here: that is read off the
// animals (GenderDisplay), because a pen tagged F2-Male can hold whatever the
// register says it holds.
func ShedTagGroup(shedTag string) string {
	parts := strings.Split(shedTag, "+")
	var groups []string
	seen := map[string]bool{}
	for _, part := range parts {
		g := shedTagPartGroup(strings.TrimSpace(part))
		if g == "" || seen[g] {
			continue
		}
		seen[g] = true
		groups = append(groups, g)
	}
	return strings.Join(groups, " + ")
}

func shedTagPartGroup(part string) string {
	if part == "" {
		return ""
	}
	switch strings.ToUpper(part) {
	case "F2-MALE", "FATTENING MALE", "F2-FEMALE", "FATTENING FEMALE", "F2", "FATTENING":
		return "Fattening"
	case "K0", "K1", "K2", "K3", "K4", "KID", "KIDS", "ICU-KID", "ICU-KIDS", "QUARANTINE KIDS":
		return "Kid"
	case "BUCK", "BUCKS":
		return "Buck"
	case "MOTHER", "MOTHERS", "MILKING":
		return "Mother"
	case "PREGNANT":
		return "Pregnant"
	case "NON-PREGNANT", "NON PREGNANT", "ICU-NON-PREGNANT":
		return "Non-Pregnant"
	case "ADULT", "ADULTS":
		return "Adult"
	case "WARMUP", "WARM-UP":
		return "Warmup"
	}
	// An unmapped tag renders as itself rather than vanishing: a new stage the farm
	// introduces must stay visible, never be absorbed into a guess.
	return part
}

// SexDisplay is one animal's register sex as display copy; empty when unknown.
func SexDisplay(sex string) string {
	switch strings.ToLower(strings.TrimSpace(sex)) {
	case "male":
		return "Male"
	case "female":
		return "Female"
	}
	return ""
}

// GenderDisplay is the gender a set of animals reads as, from the register's sex
// counts: one sex when every resolved animal agrees, "Mixed 12F·10M" when they do
// not, and empty when no animal resolved at all -- absence, never a guess.
func GenderDisplay(femaleCount, maleCount int) string {
	switch {
	case femaleCount > 0 && maleCount == 0:
		return "Female"
	case maleCount > 0 && femaleCount == 0:
		return "Male"
	case femaleCount == 0 && maleCount == 0:
		return ""
	}
	return fmt.Sprintf("Mixed %dF·%dM", femaleCount, maleCount)
}

var whitespaceRun = regexp.MustCompile(`\s+`)

// NormalisePenLabel collapses whitespace so a feed pen and a weighing pen that
// differ only in spacing match.
func NormalisePenLabel(label string) string {
	return strings.TrimSpace(whitespaceRun.ReplaceAllString(label, " "))
}

// DisambiguateGroups suffixes Group with the shed tag wherever two rollups in one
// park + pen would otherwise read identically on every feed-side display column
// (group, breed, feed type, feed given): two kid rollups in one pen ("Kid
// (ICU-Kid)" against "Kid (K3)") are both real and both need a name. Gender is
// deliberately not in the key: it comes from the animals, so it is the same for
// every rollup of a pen and can never tell two of them apart.
func DisambiguateGroups(rows []FeedWeightBandRow) {
	type key struct{ park, pen, group, breed, feedType, feedGiven string }
	tags := map[key]map[string]bool{}
	for _, row := range rows {
		k := key{row.ParkID, row.Pen, row.Group, row.Breed, row.FeedType, row.FeedGiven}
		if tags[k] == nil {
			tags[k] = map[string]bool{}
		}
		tags[k][row.ShedTag] = true
	}
	for i := range rows {
		row := &rows[i]
		k := key{row.ParkID, row.Pen, row.Group, row.Breed, row.FeedType, row.FeedGiven}
		if len(tags[k]) > 1 && row.ShedTag != "" {
			row.Group = row.Group + " (" + row.ShedTag + ")"
		}
	}
}

// SortFeedWeightBandUnmatched orders the not-shown rollups park, pen (natural), shed tag.
func SortFeedWeightBandUnmatched(rows []FeedWeightBandUnmatched) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.ParkName != b.ParkName {
			return a.ParkName < b.ParkName
		}
		if a.Pen != b.Pen {
			return naturalLess(a.Pen, b.Pen)
		}
		return a.ShedTag < b.ShedTag
	})
}

// SortFeedWeightBandRows orders rows park, pen-average before per-animal, pen
// (natural order), band ascending, then shed tag -- the order the table renders.
func SortFeedWeightBandRows(rows []FeedWeightBandRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.ParkName != b.ParkName {
			return a.ParkName < b.ParkName
		}
		if a.WeightSource != b.WeightSource {
			return a.WeightSource == FeedBandSourcePenAverage
		}
		if a.Pen != b.Pen {
			return naturalLess(a.Pen, b.Pen)
		}
		if FeedBandIndex(a.Band) != FeedBandIndex(b.Band) {
			return FeedBandIndex(a.Band) < FeedBandIndex(b.Band)
		}
		if a.ShedTag != b.ShedTag {
			return a.ShedTag < b.ShedTag
		}
		return a.FeedType < b.FeedType
	})
}

// naturalLess orders "Part 2" before "Part 10": digit runs compare by value.
func naturalLess(a, b string) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ca, cb := a[i], b[j]
		if isDigit(ca) && isDigit(cb) {
			si := i
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			sj := j
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na := strings.TrimLeft(a[si:i], "0")
			nb := strings.TrimLeft(b[sj:j], "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		la, lb := lowerByte(ca), lowerByte(cb)
		if la != lb {
			return la < lb
		}
		i++
		j++
	}
	return len(a)-i < len(b)-j
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func lowerByte(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
