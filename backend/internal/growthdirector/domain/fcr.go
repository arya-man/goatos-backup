package domain

import (
	"math"
	"sort"
	"strings"
	"time"
)

// FEED CONVERSION RATIO (maintainer request 2026-09-07): "under weighing add a tab called FCR --
// we know feed consumption and weight gain".
//
// FCR is kilograms of feed per kilogram of live weight gained, over the SAME animals and the SAME
// days. Lower is better. On this farm feed is directed to a PEN and weighing happens per pen
// (whole-shed) or per scanned kid inside a pen, so the base grain of the whole report is the pen,
// and every cut -- breed, sex, weight band, park, origin, week -- is a roll-up of pens.
//
// THE UNIT OF MEASUREMENT IS THE SEGMENT: two consecutive weighing rounds of one pen. Feed is what
// the sheet directed to the pen on the days between the two rounds; gain is the pen's daily gain
// across the round multiplied by the head-days the sheet actually fed on those days. Multiplying
// ADG by fed head-days keeps the numerator and denominator on the identical population -- a pen's
// TOTAL weight moves when animals enter or leave, which is not growth, and the feed sheet's head
// count on each day is the population that actually ate. It also works for whole-shed pens and
// scanned pens without a special case: a whole-shed round has an average and a head count, a
// scanned round has per-animal pairs whose mean is the pen's daily gain.
//
// Everything below is pure arithmetic over segment rows the repository returns. It is kept out of
// SQL so the roll-up rules (agree-or-neither cohorts, sum-over-sum never mean-of-means, absence
// never rendered as zero) are unit-tested in Go, and so the same rows feed the pen table, the
// group charts and the weekly series without three queries that could drift.

// FCRBasisDirectedFeed discloses that the feed side is the DIRECTED sheet quantity (what the crew
// was told to give), not a measured intake. The only measured quantity in the feed chain is the
// verifier's packed weight, which is sparse; version 1 uses the sheet (maintainer decision
// 2026-09-07) and says so.
const FCRBasisDirectedFeed = "directed_feed"

// Cohort labels a pen carries when its residents do not agree. A pen holding two breeds is filed
// as Mixed, never split and never filed under the majority -- one pen's feed cannot be divided
// between two cohorts, and the majority would flip as animals move.
const (
	CohortMixed   = "mixed"
	CohortUnknown = "unknown"
)

// Origin labels, matching the weighing origin filter vocabulary.
const (
	OriginFarmBorn  = "farm_born"
	OriginPurchased = "purchased"
)

// Pen statuses. Exactly one per pen; a pen with an FCR is "ok" even when it also carries blocked
// feed cells (BlockedCells discloses those separately, because the FCR is then UNDERSTATED rather
// than absent).
const (
	FCRPenOK          = "ok"
	FCRPenWeighedOnce = "weighed_once" // fewer than two rounds in the window: no gain exists yet
	FCRPenNoFeed      = "no_feed"      // rounds exist but the feed sheet has no rows between them
	FCRPenNoGain      = "no_gain"      // feed exists but the pen did not gain: a ratio would be infinite or negative
)

// SalePrice is one species' assumed live-weight sale price, the newest row effective on or before
// the reported period's last day.
type SalePrice struct {
	Species       string  `json:"species"`
	PricePerKgINR float64 `json:"price_per_kg_inr"`
	EffectiveFrom string  `json:"effective_from"`
	SetBy         string  `json:"set_by"`
}

// SalePrices is the whole vocabulary the tab (and the Comparison tab) prices against.
type SalePrices struct {
	Prices []SalePrice `json:"prices"`
}

// PenPrice is the sale price a pen's gain is valued at: the HEAD-WEIGHTED price of its live
// residents. A pen holding 14 sheep and 3 goats is priced at (14 × sheep + 3 × goat) / 17, so a
// mixed pen is valued rather than left blank, and a single-species pen collapses to that species'
// price. It is absent only when a resident species has no configured price (or the pen has no
// live residents), because guessing the missing half would value gain nobody priced.
func (p SalePrices) PenPrice(goats, sheep int) (float64, bool) {
	total := goats + sheep
	if total <= 0 {
		return 0, false
	}
	var sum float64
	for species, n := range map[string]int{"goat": goats, "sheep": sheep} {
		if n == 0 {
			continue
		}
		price, ok := p.PriceFor(species)
		if !ok {
			return 0, false
		}
		sum += float64(n) * price
	}
	return sum / float64(total), true
}

// PriceFor returns the price for a species, or false when none is configured.
func (p SalePrices) PriceFor(species string) (float64, bool) {
	for _, row := range p.Prices {
		if row.Species == species {
			return row.PricePerKgINR, true
		}
	}
	return 0, false
}

// FCRSegmentRow is one (pen, consecutive-round pair) as the repository returns it. Nullable
// figures are pointers: nil means the fact does not exist for this segment (no feed rows between
// the two rounds, no head count), never zero.
type FCRSegmentRow struct {
	PenKey       string // stable pen identity: shed uuid + "|" + scrubbed partition key
	StartDate    string // business date of the earlier round (inclusive feed day)
	EndDate      string // business date of the later round (exclusive feed day)
	Animals      int    // head count at the later round
	ADGGPerDay   float64
	Mode         string // individual_animal | per_shed_partition
	PairedKids   int    // scanned segments only: kids weighed in both rounds
	FeedKg       *float64
	FeedCostINR  *float64 // priced portion only
	UnpricedKg   float64  // directed kg with no purchase price on or before the day
	BlockedCells int
	HeadDays     *float64
}

// FCRPenRow is one pen as the repository returns it: identity, cohort and its rounds.
type FCRPenRow struct {
	PenKey          string
	LocationID      string // the PHYSICAL shed the feed sheet keys on
	ParkID          string
	ParkName        string
	ParkCode        string // the park's CODE (CBE, CPT): the All-parks cluster order, never the name
	ShedName        string
	PartitionLabel  string
	Display         string // operational display, composed by the adapter through oploc
	Modes           []string
	Rounds          int
	FirstWeighDate  string
	LastWeighDate   string
	FirstAverageKg  *float64
	LatestAnimals   int
	Residents       int
	Breeds          int
	Breed           string
	Sexes           int
	Sex             string
	SpeciesCount    int
	Species         string
	GoatResidents   int // live residents per species, for the head-weighted sale price
	SheepResidents  int
	BoughtResidents int
	// Weighed* describe the animals actually SCANNED in this pen during the window, resolved
	// through the register regardless of where they are now or whether they are still alive. They
	// are the fallback cohort for a pen that has NO live residents today -- kids sold or moved after
	// the last round -- so a pen that was fed and weighed all month is not rendered as "unknown" and
	// unvalued because it happens to be empty on the day the tab is read.
	WeighedAnimals     int
	WeighedBreeds      int
	WeighedBreed       string
	WeighedSexes       int
	WeighedSex         string
	WeighedSpeciesN    int
	WeighedSpecies     string
	WeighedGoats       int
	WeighedSheep       int
	WeighedBought      int
	IndividualScanned  bool
	WholeShedWeighed   bool
	WindowFeedKg       *float64 // directed kg over the WHOLE window, for pens with no segment
	WindowBlockedCells int
}

// FCRPen is one pen on the tab.
type FCRPen struct {
	LocationID                 string   `json:"location_id"`
	PartitionLabel             string   `json:"partition_label"`
	OperationalLocationDisplay string   `json:"operational_location_display"`
	ParkID                     string   `json:"park_id"`
	ParkName                   string   `json:"park_name"`
	WeighingModes              []string `json:"weighing_modes"`
	Breed                      string   `json:"breed"`
	Sex                        string   `json:"sex"`
	Species                    string   `json:"species"`
	Origin                     string   `json:"origin"`
	Animals                    int      `json:"animals"`
	Rounds                     int      `json:"rounds"`
	FirstWeighDate             string   `json:"first_weigh_date"`
	LastWeighDate              string   `json:"last_weigh_date"`
	StartWeightKg              *float64 `json:"start_weight_kg"`
	WeightBand                 string   `json:"weight_band"`
	HeadDays                   *float64 `json:"head_days"`
	ADGGPerDay                 *float64 `json:"adg_g_per_day"`
	GainKg                     *float64 `json:"gain_kg"`
	FeedKg                     *float64 `json:"feed_kg"`
	FCR                        *float64 `json:"fcr"`
	FeedCostINR                *float64 `json:"feed_cost_inr"`
	FeedCostPerKgGainINR       *float64 `json:"feed_cost_per_kg_gain_inr"`
	GainValueINR               *float64 `json:"gain_value_inr"`
	// MarginINR is gain value minus feed cost: the money the pen made (or lost) over what it ate in
	// the period. Absent unless BOTH sides exist -- a margin over an unpriced feed bill is a guess.
	MarginINR      *float64 `json:"margin_inr"`
	UnpricedFeedKg float64  `json:"unpriced_feed_kg"`
	BlockedCells   int      `json:"blocked_cells"`
	Status         string   `json:"status"`
}

// FCRGroup is one bar of a cut: sum-over-sum across its member pens, never a mean of pen ratios,
// so a large pen weighs as much as it should.
type FCRGroup struct {
	Key                  string   `json:"key"`
	Label                string   `json:"label"`
	Pens                 int      `json:"pens"`
	Animals              int      `json:"animals"`
	FeedKg               float64  `json:"feed_kg"`
	GainKg               float64  `json:"gain_kg"`
	HeadDays             float64  `json:"head_days"`
	FCR                  *float64 `json:"fcr"`
	ADGGPerDay           *float64 `json:"adg_g_per_day"`
	FeedCostINR          *float64 `json:"feed_cost_inr"`
	FeedCostPerKgGainINR *float64 `json:"feed_cost_per_kg_gain_inr"`
	GainValueINR         *float64 `json:"gain_value_inr"`
	MarginINR            *float64 `json:"margin_inr"`
}

// FCRWeek is one point of the weekly series: every segment whose LATER round fell in that week.
type FCRWeek struct {
	WeekStart            string   `json:"week_start"`
	Pens                 int      `json:"pens"`
	FeedKg               float64  `json:"feed_kg"`
	GainKg               float64  `json:"gain_kg"`
	FCR                  *float64 `json:"fcr"`
	FeedCostPerKgGainINR *float64 `json:"feed_cost_per_kg_gain_inr"`
}

// FCRSummary is the whole-filter KPI strip. Every figure is computed over the same pen set the
// table lists, so the strip and the table cannot disagree.
type FCRSummary struct {
	PensInScope          int      `json:"pens_in_scope"`
	PensWithFCR          int      `json:"pens_with_fcr"`
	PensWeighedOnce      int      `json:"pens_weighed_once"`
	PensWithoutFeed      int      `json:"pens_without_feed"`
	PensWithoutGain      int      `json:"pens_without_gain"`
	PensWithBlockedCells int      `json:"pens_with_blocked_cells"`
	Animals              int      `json:"animals"`
	FeedKg               float64  `json:"feed_kg"`
	GainKg               float64  `json:"gain_kg"`
	HeadDays             float64  `json:"head_days"`
	FCR                  *float64 `json:"fcr"`
	ADGGPerDay           *float64 `json:"adg_g_per_day"`
	FeedCostINR          *float64 `json:"feed_cost_inr"`
	FeedCostPerKgINR     *float64 `json:"feed_cost_per_kg_inr"`
	FeedCostPerKgGainINR *float64 `json:"feed_cost_per_kg_gain_inr"`
	GainValueINR         *float64 `json:"gain_value_inr"`
	ValuedGainKg         float64  `json:"valued_gain_kg"`
	ValuedPens           int      `json:"valued_pens"`
	MarginINR            *float64 `json:"margin_inr"`
	BreakEvenFCR         *float64 `json:"break_even_fcr"`
	UnpricedFeedKg       float64  `json:"unpriced_feed_kg"`
}

// FCRReport is the whole tab.
type FCRReport struct {
	Period     Period      `json:"period"`
	Parks      []Park      `json:"parks"`
	Basis      string      `json:"basis"`
	SalePrices []SalePrice `json:"sale_prices"`
	Summary    FCRSummary  `json:"summary"`
	Pens       []FCRPen    `json:"pens"`
	ByBreed    []FCRGroup  `json:"by_breed"`
	BySex      []FCRGroup  `json:"by_sex"`
	ByBand     []FCRGroup  `json:"by_weight_band"`
	ByPark     []FCRGroup  `json:"by_park"`
	ByOrigin   []FCRGroup  `json:"by_origin"`
	Weekly     []FCRWeek   `json:"weekly"`
}

// FCRFilters are the page filters applied at PEN grain. Feed is directed to a whole pen and cannot
// be split, so under a Sex or Origin filter a pen counts only when EVERY resident is that sex /
// that origin -- the same agree-or-neither rule the Weights page applies to a whole-shed weigh, now
// applied to scanned pens too, because the feed side of this ratio has no per-animal grain.
type FCRFilters struct {
	Sex    string
	Origin string
	// BandEdgesKg are the tenant's band edges (growth_assumptions); empty means the defaults.
	BandEdgesKg []float64
}

// BuildFCRReport turns repository rows into the tab. Pens and segments are joined on PenKey.
func BuildFCRReport(pens []FCRPenRow, segments []FCRSegmentRow, prices SalePrices, filters FCRFilters) FCRReport {
	out := FCRReport{
		Basis:      FCRBasisDirectedFeed,
		SalePrices: append([]SalePrice{}, prices.Prices...),
		Pens:       []FCRPen{},
		ByBreed:    []FCRGroup{},
		BySex:      []FCRGroup{},
		ByBand:     []FCRGroup{},
		ByPark:     []FCRGroup{},
		ByOrigin:   []FCRGroup{},
		Weekly:     []FCRWeek{},
	}
	if out.SalePrices == nil {
		out.SalePrices = []SalePrice{}
	}
	segByPen := map[string][]FCRSegmentRow{}
	for _, seg := range segments {
		segByPen[seg.PenKey] = append(segByPen[seg.PenKey], seg)
	}

	type penAgg struct {
		pen      FCRPen
		segs     []FCRSegmentRow
		parkCode string
	}
	var aggs []penAgg
	for _, row := range pens {
		row = cohortSource(row)
		pen := penFromRow(row, filters.BandEdgesKg)
		if !penPassesFilters(pen, filters) {
			continue
		}
		segs := segByPen[row.PenKey]
		applySegments(&pen, row, segs, prices)
		aggs = append(aggs, penAgg{pen: pen, segs: segs, parkCode: row.ParkCode})
	}

	// Pens: clustered by park in CODE order (CBE, then CPT — maintainer decision 2026-09-16 for
	// every All-parks analytics surface; the NAME order puts Channapatna first), and A→Z inside a
	// park the way a person reads pen names, so "Part 2" precedes "Part 10". The same shape the
	// Weights shed table uses. A pen's FCR no longer decides its place: the bars carry the number.
	sort.SliceStable(aggs, func(i, j int) bool {
		a, b := aggs[i].pen, aggs[j].pen
		if ac, bc := aggs[i].parkCode, aggs[j].parkCode; ac != bc {
			return ac < bc
		}
		return naturalLess(a.OperationalLocationDisplay, b.OperationalLocationDisplay)
	})
	parkCodes := map[string]string{}
	for _, agg := range aggs {
		parkCodes[agg.pen.ParkID] = agg.parkCode
	}

	groups := map[string]map[string]*FCRGroup{"breed": {}, "sex": {}, "band": {}, "park": {}, "origin": {}}
	weeks := map[string]*FCRWeek{}
	weekPens := map[string]map[string]struct{}{}
	var summary FCRSummary
	var pricedFeedKg float64
	for _, agg := range aggs {
		pen := agg.pen
		out.Pens = append(out.Pens, pen)
		summary.PensInScope++
		summary.Animals += pen.Animals
		if pen.BlockedCells > 0 {
			summary.PensWithBlockedCells++
		}
		switch pen.Status {
		case FCRPenWeighedOnce:
			summary.PensWeighedOnce++
		case FCRPenNoFeed:
			summary.PensWithoutFeed++
		case FCRPenNoGain:
			summary.PensWithoutGain++
		}
		if pen.FCR == nil {
			continue
		}
		summary.PensWithFCR++
		summary.FeedKg += *pen.FeedKg
		summary.GainKg += *pen.GainKg
		summary.HeadDays += *pen.HeadDays
		summary.UnpricedFeedKg += pen.UnpricedFeedKg
		if pen.FeedCostINR != nil {
			summary.FeedCostINR = addPtr(summary.FeedCostINR, *pen.FeedCostINR)
			pricedFeedKg += *pen.FeedKg - pen.UnpricedFeedKg
		}
		if pen.GainValueINR != nil {
			summary.GainValueINR = addPtr(summary.GainValueINR, *pen.GainValueINR)
			summary.ValuedGainKg += *pen.GainKg
			summary.ValuedPens++
		}

		addGroup(groups["breed"], pen.Breed, cohortLabel(pen.Breed), pen)
		addGroup(groups["sex"], pen.Sex, cohortLabel(pen.Sex), pen)
		addGroup(groups["band"], pen.WeightBand, pen.WeightBand, pen)
		addGroup(groups["park"], pen.ParkID, pen.ParkName, pen)
		addGroup(groups["origin"], pen.Origin, pen.Origin, pen)

		for _, seg := range agg.segs {
			gain, feed, ok := segmentGainAndFeed(seg)
			if !ok {
				continue
			}
			week := weekStartOf(seg.EndDate)
			w := weeks[week]
			if w == nil {
				w = &FCRWeek{WeekStart: week}
				weeks[week] = w
				weekPens[week] = map[string]struct{}{}
			}
			weekPens[week][seg.PenKey] = struct{}{}
			w.FeedKg += feed
			w.GainKg += gain
			if seg.FeedCostINR != nil {
				w.FeedCostPerKgGainINR = addPtr(w.FeedCostPerKgGainINR, *seg.FeedCostINR) // cost total for now; divided below
			}
		}
	}

	if summary.GainKg > 0 && summary.PensWithFCR > 0 {
		summary.FCR = ptr(summary.FeedKg / summary.GainKg)
		if summary.FeedCostINR != nil {
			summary.FeedCostPerKgGainINR = ptr(*summary.FeedCostINR / summary.GainKg)
		}
	}
	if summary.HeadDays > 0 {
		summary.ADGGPerDay = ptr(summary.GainKg * 1000 / summary.HeadDays)
	}
	if summary.FeedCostINR != nil && pricedFeedKg > 0 {
		summary.FeedCostPerKgINR = ptr(*summary.FeedCostINR / pricedFeedKg)
	}
	if summary.GainValueINR != nil && summary.FeedCostINR != nil {
		summary.MarginINR = ptr(*summary.GainValueINR - *summary.FeedCostINR)
	}
	// Break-even FCR: the sale price a kilogram of gain fetches divided by what a kilogram of feed
	// costs. Above it a pen eats more value than it puts on. The sale side is the average price
	// over the gain actually valued, so a mixed goat/sheep farm gets one honest line.
	if summary.GainValueINR != nil && summary.ValuedGainKg > 0 && summary.FeedCostPerKgINR != nil && *summary.FeedCostPerKgINR > 0 {
		summary.BreakEvenFCR = ptr((*summary.GainValueINR / summary.ValuedGainKg) / *summary.FeedCostPerKgINR)
	}
	out.Summary = summary

	out.ByBreed = finishGroups(groups["breed"], sortByFCRDesc)
	out.BySex = finishGroups(groups["sex"], sortByFCRDesc)
	out.ByBand = finishGroups(groups["band"], sortByBandFor(filters.BandEdgesKg))
	out.ByPark = finishGroups(groups["park"], func(a, b FCRGroup) bool {
		if ac, bc := parkCodes[a.Key], parkCodes[b.Key]; ac != bc {
			return ac < bc
		}
		return a.Label < b.Label
	})
	out.ByOrigin = finishGroups(groups["origin"], sortByLabel)

	weekKeys := make([]string, 0, len(weeks))
	for k := range weeks {
		weekKeys = append(weekKeys, k)
	}
	sort.Strings(weekKeys)
	for _, k := range weekKeys {
		w := *weeks[k]
		w.Pens = len(weekPens[k])
		if w.GainKg > 0 {
			w.FCR = ptr(w.FeedKg / w.GainKg)
			if w.FeedCostPerKgGainINR != nil {
				w.FeedCostPerKgGainINR = ptr(*w.FeedCostPerKgGainINR / w.GainKg)
			}
		} else {
			w.FCR = nil
			w.FeedCostPerKgGainINR = nil
		}
		out.Weekly = append(out.Weekly, w)
	}
	return out
}

// cohortSource picks the residents the pen is described by: its live residents when it has any,
// else the animals scanned in it during the window. A pen with neither stays unknown.
func cohortSource(row FCRPenRow) FCRPenRow {
	if row.Residents > 0 || row.WeighedAnimals == 0 {
		return row
	}
	row.Residents = row.WeighedAnimals
	row.Breeds, row.Breed = row.WeighedBreeds, row.WeighedBreed
	row.Sexes, row.Sex = row.WeighedSexes, row.WeighedSex
	row.SpeciesCount, row.Species = row.WeighedSpeciesN, row.WeighedSpecies
	row.GoatResidents, row.SheepResidents = row.WeighedGoats, row.WeighedSheep
	row.BoughtResidents = row.WeighedBought
	return row
}

func penFromRow(row FCRPenRow, bandEdges []float64) FCRPen {
	row = cohortSource(row)
	pen := FCRPen{
		LocationID:                 row.LocationID,
		PartitionLabel:             row.PartitionLabel,
		OperationalLocationDisplay: row.Display,
		ParkID:                     row.ParkID,
		ParkName:                   row.ParkName,
		WeighingModes:              append([]string{}, row.Modes...),
		Animals:                    row.LatestAnimals,
		Rounds:                     row.Rounds,
		FirstWeighDate:             row.FirstWeighDate,
		LastWeighDate:              row.LastWeighDate,
		StartWeightKg:              row.FirstAverageKg,
		WeightBand:                 bandFor(row.FirstAverageKg, bandEdges),
		Breed:                      cohortValue(row.Residents, row.Breeds, row.Breed),
		Sex:                        cohortValue(row.Residents, row.Sexes, row.Sex),
		Species:                    cohortValue(row.Residents, row.SpeciesCount, row.Species),
		Origin:                     originFor(row.Residents, row.BoughtResidents),
		BlockedCells:               row.WindowBlockedCells,
		Status:                     FCRPenWeighedOnce,
	}
	if pen.WeighingModes == nil {
		pen.WeighingModes = []string{}
	}
	if row.Rounds < 2 {
		pen.FeedKg = row.WindowFeedKg
	}
	return pen
}

func penPassesFilters(pen FCRPen, filters FCRFilters) bool {
	if sex := strings.TrimSpace(filters.Sex); sex != "" && pen.Sex != strings.ToLower(sex) {
		return false
	}
	if origin := strings.TrimSpace(filters.Origin); origin != "" && pen.Origin != strings.ToLower(origin) {
		return false
	}
	return true
}

// applySegments sums a pen's segments. A segment counts toward the pen only when BOTH sides
// exist: a feed figure and a head-day figure. A segment with feed but no head count would put
// kilograms into the numerator with nothing in the denominator -- a lie about a pen whose sheet
// simply lacked a head count -- so it is counted as blocked coverage instead.
func applySegments(pen *FCRPen, row FCRPenRow, segs []FCRSegmentRow, prices SalePrices) {
	if row.Rounds < 2 || len(segs) == 0 {
		pen.Status = FCRPenWeighedOnce
		return
	}
	var feedKg, gainKg, headDays, unpriced float64
	var cost *float64
	blocked := 0
	any := false
	for _, seg := range segs {
		blocked += seg.BlockedCells
		gain, feed, ok := segmentGainAndFeed(seg)
		if !ok {
			continue
		}
		any = true
		feedKg += feed
		gainKg += gain
		headDays += *seg.HeadDays
		unpriced += seg.UnpricedKg
		if seg.FeedCostINR != nil {
			cost = addPtr(cost, *seg.FeedCostINR)
		}
	}
	pen.BlockedCells = blocked
	if !any {
		pen.Status = FCRPenNoFeed
		return
	}
	pen.FeedKg = ptr(feedKg)
	pen.GainKg = ptr(gainKg)
	pen.HeadDays = ptr(headDays)
	pen.UnpricedFeedKg = unpriced
	pen.FeedCostINR = cost
	if headDays > 0 {
		pen.ADGGPerDay = ptr(gainKg * 1000 / headDays)
	}
	if gainKg <= 0 {
		pen.Status = FCRPenNoGain
		return
	}
	pen.Status = FCRPenOK
	pen.FCR = ptr(feedKg / gainKg)
	if cost != nil {
		pen.FeedCostPerKgGainINR = ptr(*cost / gainKg)
	}
	if price, ok := prices.PenPrice(row.GoatResidents, row.SheepResidents); ok {
		pen.GainValueINR = ptr(gainKg * price)
		if cost != nil {
			pen.MarginINR = ptr(*pen.GainValueINR - *cost)
		}
	}
}

// segmentGainAndFeed returns the segment's gain and feed in kg, or ok=false when the segment has no
// feed rows or no head count and therefore cannot take part in a ratio.
func segmentGainAndFeed(seg FCRSegmentRow) (gainKg, feedKg float64, ok bool) {
	if seg.FeedKg == nil || seg.HeadDays == nil || *seg.HeadDays <= 0 {
		return 0, 0, false
	}
	return seg.ADGGPerDay * *seg.HeadDays / 1000, *seg.FeedKg, true
}

func addGroup(into map[string]*FCRGroup, key, label string, pen FCRPen) {
	if key == "" {
		key = CohortUnknown
		label = CohortUnknown
	}
	g := into[key]
	if g == nil {
		g = &FCRGroup{Key: key, Label: label}
		into[key] = g
	}
	g.Pens++
	g.Animals += pen.Animals
	g.FeedKg += *pen.FeedKg
	g.GainKg += *pen.GainKg
	g.HeadDays += *pen.HeadDays
	if pen.FeedCostINR != nil {
		g.FeedCostINR = addPtr(g.FeedCostINR, *pen.FeedCostINR)
	}
	if pen.GainValueINR != nil {
		g.GainValueINR = addPtr(g.GainValueINR, *pen.GainValueINR)
	}
}

func finishGroups(m map[string]*FCRGroup, less func(a, b FCRGroup) bool) []FCRGroup {
	out := make([]FCRGroup, 0, len(m))
	for _, g := range m {
		row := *g
		if row.GainKg > 0 {
			row.FCR = ptr(row.FeedKg / row.GainKg)
			if row.FeedCostINR != nil {
				row.FeedCostPerKgGainINR = ptr(*row.FeedCostINR / row.GainKg)
			}
		}
		if row.HeadDays > 0 {
			row.ADGGPerDay = ptr(row.GainKg * 1000 / row.HeadDays)
		}
		if row.GainValueINR != nil && row.FeedCostINR != nil {
			row.MarginINR = ptr(*row.GainValueINR - *row.FeedCostINR)
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}

func sortByFCRDesc(a, b FCRGroup) bool {
	switch {
	case a.FCR != nil && b.FCR != nil && *a.FCR != *b.FCR:
		return *a.FCR > *b.FCR
	case a.FCR != nil && b.FCR == nil:
		return true
	case a.FCR == nil && b.FCR != nil:
		return false
	}
	return a.Label < b.Label
}

func sortByLabel(a, b FCRGroup) bool { return a.Label < b.Label }

func sortByBandFor(edges []float64) func(a, b FCRGroup) bool {
	labels := BandLabelsFor(edges)
	index := func(label string) int {
		for i, b := range labels {
			if b == label {
				return i
			}
		}
		return len(labels)
	}
	return func(a, b FCRGroup) bool { return index(a.Key) < index(b.Key) }
}

// bandFor places a pen's average weight at its FIRST round of the window into the Growth
// Director's bands (the tenant's edges, growth_assumptions weight_band_edges_kg), so this tab and
// the band board agree on what a band is.
func bandFor(avgKg *float64, edges []float64) string {
	if avgKg == nil || math.IsNaN(*avgKg) {
		return CohortUnknown
	}
	if len(edges) == 0 {
		edges = DefaultWeightBandEdgesKg
	}
	return BandLabelsFor(edges)[BandIndexFor(*avgKg, edges)]
}

// cohortValue applies agree-or-neither: the value only when every resident agrees.
func cohortValue(residents, distinct int, value string) string {
	switch {
	case residents == 0:
		return CohortUnknown
	case distinct == 1 && strings.TrimSpace(value) != "":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return CohortMixed
	}
}

func cohortLabel(value string) string {
	return value
}

// originFor: purchased when EVERY live resident came off a procurement load, farm born when NONE
// did, mixed otherwise -- the identical agree-or-neither shape the Weights page's Origin filter
// applies to a whole-shed pen.
func originFor(residents, bought int) string {
	switch {
	case residents == 0:
		return CohortUnknown
	case bought == residents:
		return OriginPurchased
	case bought == 0:
		return OriginFarmBorn
	default:
		return CohortMixed
	}
}

// weekStartOf returns the Monday (business calendar) of the week holding a YYYY-MM-DD date,
// matching the Weights page's weekly series (date_trunc('week') is Monday-based in Postgres).
func weekStartOf(date string) string {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	offset := (int(d.Weekday()) + 6) % 7
	return d.AddDate(0, 0, -offset).Format("2006-01-02")
}

func ptr(v float64) *float64 { return &v }

func addPtr(acc *float64, v float64) *float64 {
	if acc == nil {
		return ptr(v)
	}
	sum := *acc + v
	return &sum
}
