package domain

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Assumptions is everything the Weighing area reads that is a FIGURE SOMEONE DECIDED rather
// than a measurement: the assumed live-weight sale price per species, the sale-ready weight
// line, and the age at which a load still holding animals raises an alert. Maintainer decision
// 2026-09-19: all of these are edited from ONE drawer on ADG Analytics, by people holding
// weighing.assumptions.write, and every read that uses them re-reads the row on each request so
// a change shows up on the next load of the page.
type Assumptions struct {
	SalePrices []SalePrice       `json:"sale_prices"`
	Values     []AssumptionValue `json:"values"`
}

// AssumptionValue is one keyed figure. Label is NOT here: the label is page copy owned by the
// admin-web contract (adminui), keyed on Key, because a farm word belongs with the other farm
// words and not in a domain struct.
type AssumptionValue struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
	// Exactly one of Value / Values / Date carries the figure, by Kind.
	Value      float64   `json:"value"`
	Values     []float64 `json:"values"`
	Date       string    `json:"date"`
	Unit       string    `json:"unit"`
	SetBy      string    `json:"set_by"`
	UpdatedAt  string    `json:"updated_at"`
	RowVersion int       `json:"row_version"`
}

// Kinds of assumption. A number is one figure with a band; a number_list is an ascending list
// (the band edges); a date is a business date (no date key is seeded today -- the Weights pages'
// landing date and picker floor live on the Weighing SOP's weights_pages block, not here).
const (
	AssumptionKindNumber     = "number"
	AssumptionKindNumberList = "number_list"
	AssumptionKindDate       = "date"
)

// AssumptionKey names one keyed figure and the band a write must land in. The band is the
// business band, checked on the write path: a value outside it is REJECTED, never clamped or
// defaulted, because a 3 kg sale line or a 2-day load alert is a typo and not a decision.
type AssumptionKey struct {
	Key   string
	Kind  string
	Unit  string
	Min   float64
	Max   float64
	Whole bool // days are whole numbers; a weight line may carry decimals
	// For a number_list: how many edges the list must hold, inclusive.
	MinItems, MaxItems int
}

const (
	// AssumptionSaleReadyThresholdKg is the sale-ready line ("Over 35 kg"). Read by the
	// shed-weights count through its sale_threshold_kg parameter and by the Farm value card.
	AssumptionSaleReadyThresholdKg = "sale_ready_threshold_kg"
	// AssumptionLoadAgeAlertDays is the age past which a load still holding animals is a daily
	// CXO alert. Read by the load-wise read model and the load-age notifier.
	AssumptionLoadAgeAlertDays = "load_age_alert_days"

	// The second batch (same day, 000253): the rest of what ADG Analytics carried as constants.
	AssumptionSaleReadyLowerKg     = "sale_ready_lower_kg"          // the "Over 30 kg" line
	AssumptionSlowGrowthTargetGDay = "slow_growth_target_g_per_day" // the slow-growth rule of thumb
	AssumptionBadScanLossGDay      = "bad_scan_loss_g_per_day"      // a pair losing MORE than this per day is a bad scan
	AssumptionDefaultPeriodDays    = "default_period_days"          // the Growth Director window when no dates are given
	AssumptionWeightBandEdgesKg    = "weight_band_edges_kg"         // the band board / Weight-wise / FCR-by-band edges

	// Defaults, used ONLY when a tenant has no row (a tenant seeded before the migrations and
	// never backfilled). They are the figures the constants carried.
	DefaultSaleReadyThresholdKg = 35.0
	DefaultSaleReadyLowerKg     = 30.0
	DefaultLoadAgeAlertDays     = 90
	DefaultSlowGrowthTargetGDay = 200
	DefaultBadScanLossGDay      = 300
	DefaultPeriodDaysAssumption = 15
)

// DefaultWeightBandEdgesKg are the band edges the constants carried.
var DefaultWeightBandEdgesKg = []float64{15, 20, 25, 30, 35}

// AssumptionKeys is the catalog of keyed figures. Adding a figure means adding a row here, a
// seed in a migration, a copy label in adminui, and the consumer that stops reading a constant.
var AssumptionKeys = []AssumptionKey{
	{Key: AssumptionSaleReadyThresholdKg, Kind: AssumptionKindNumber, Unit: "kg", Min: 10, Max: 80},
	{Key: AssumptionSaleReadyLowerKg, Kind: AssumptionKindNumber, Unit: "kg", Min: 5, Max: 80},
	{Key: AssumptionLoadAgeAlertDays, Kind: AssumptionKindNumber, Unit: "days", Min: 7, Max: 730, Whole: true},
	{Key: AssumptionSlowGrowthTargetGDay, Kind: AssumptionKindNumber, Unit: "g/day", Min: 20, Max: 1000, Whole: true},
	{Key: AssumptionBadScanLossGDay, Kind: AssumptionKindNumber, Unit: "g/day", Min: 50, Max: 5000, Whole: true},
	{Key: AssumptionDefaultPeriodDays, Kind: AssumptionKindNumber, Unit: "days", Min: 1, Max: 365, Whole: true},
	{Key: AssumptionWeightBandEdgesKg, Kind: AssumptionKindNumberList, Unit: "kg", Min: 1, Max: 200, MinItems: 2, MaxItems: 8},
}

// GrowthSettings is the resolved set of figures the Growth Director and FCR reads take from the
// assumptions, each defaulted when the tenant has no row. Consumers take THIS, never a constant.
type GrowthSettings struct {
	BandEdgesKg          []float64
	SlowGrowthTargetG    float64
	BadScanLossGPerDay   float64 // positive magnitude; a pair losing more than this per day is discarded
	DefaultPeriodDays    int
	SaleReadyLowerKg     float64
	SaleReadyThresholdKg float64
}

// SettingsFrom resolves the settings out of a served assumption set.
func SettingsFrom(values []AssumptionValue) GrowthSettings {
	out := GrowthSettings{
		BandEdgesKg:          append([]float64{}, DefaultWeightBandEdgesKg...),
		SlowGrowthTargetG:    DefaultSlowGrowthTargetGDay,
		BadScanLossGPerDay:   DefaultBadScanLossGDay,
		DefaultPeriodDays:    DefaultPeriodDaysAssumption,
		SaleReadyLowerKg:     DefaultSaleReadyLowerKg,
		SaleReadyThresholdKg: DefaultSaleReadyThresholdKg,
	}
	for _, v := range values {
		switch v.Key {
		case AssumptionWeightBandEdgesKg:
			if len(v.Values) >= 2 {
				out.BandEdgesKg = append([]float64{}, v.Values...)
			}
		case AssumptionSlowGrowthTargetGDay:
			out.SlowGrowthTargetG = v.Value
		case AssumptionBadScanLossGDay:
			out.BadScanLossGPerDay = v.Value
		case AssumptionDefaultPeriodDays:
			if v.Value >= 1 {
				out.DefaultPeriodDays = int(v.Value)
			}
		case AssumptionSaleReadyLowerKg:
			out.SaleReadyLowerKg = v.Value
		case AssumptionSaleReadyThresholdKg:
			out.SaleReadyThresholdKg = v.Value
		}
	}
	return out
}

// BandLabelsFor renders the band vocabulary for a set of edges: "<15", "15-20", ..., "35+". The
// labels are the wire keys of the band board and the FCR by-band group, derived here ONCE.
func BandLabelsFor(edges []float64) []string {
	if len(edges) == 0 {
		edges = DefaultWeightBandEdgesKg
	}
	out := make([]string, 0, len(edges)+1)
	out = append(out, "<"+trimFloat(edges[0]))
	for i := 1; i < len(edges); i++ {
		out = append(out, trimFloat(edges[i-1])+"-"+trimFloat(edges[i]))
	}
	return append(out, trimFloat(edges[len(edges)-1])+"+")
}

// BandIndexFor places a weight in the edges the way SQL width_bucket does: 0 below the first
// edge, len(edges) at or above the last.
func BandIndexFor(kg float64, edges []float64) int {
	idx := 0
	for _, e := range edges {
		if kg >= e {
			idx++
		}
	}
	return idx
}

// SalePriceMinINR / SalePriceMaxINR bound an assumed live-weight price per kg.
const (
	SalePriceMinINR = 50
	SalePriceMaxINR = 5000
)

// SalePriceSpecies is the vocabulary the price table accepts (CHECK constraint in 000249).
var SalePriceSpecies = []string{"goat", "sheep"}

// AssumptionsUpdate is the whole set a PUT carries. Every field is the value the caller wants
// to hold; a row absent from the request is left untouched, so a client that only knows the
// prices cannot blank the thresholds.
type AssumptionsUpdate struct {
	SalePrices []SalePriceUpdate `json:"sale_prices"`
	Values     []ValueUpdate     `json:"values"`
}

type SalePriceUpdate struct {
	Species       string  `json:"species"`
	PricePerKgINR float64 `json:"price_per_kg_inr"`
	// LoadedPricePerKgINR is the price the drawer SHOWED when it was opened -- the fence. The
	// price table is append-only and effective-dated, so it has no row_version; the
	// compare-and-set is on the figure the editor decided against. Nil means the drawer loaded
	// no price for the species (a tenant with no row yet), and is a conflict once one exists.
	LoadedPricePerKgINR *float64 `json:"loaded_price_per_kg_inr"`
}

type ValueUpdate struct {
	Key        string    `json:"key"`
	Value      float64   `json:"value"`
	Values     []float64 `json:"values"`
	Date       string    `json:"date"`
	RowVersion int       `json:"row_version"`
}

// ValidateAssumptionsUpdate rejects an update whose figures are outside the business bands,
// name an unknown key or species, or repeat a key. It returns a farm-worded message for the
// first problem; the transport maps it to 400 invalid_assumption.
func ValidateAssumptionsUpdate(update AssumptionsUpdate) error {
	if len(update.SalePrices) == 0 && len(update.Values) == 0 {
		return fmt.Errorf("nothing to change")
	}
	seenSpecies := map[string]bool{}
	for _, p := range update.SalePrices {
		species := strings.ToLower(strings.TrimSpace(p.Species))
		if !contains(SalePriceSpecies, species) {
			return fmt.Errorf("unknown species %q", p.Species)
		}
		if seenSpecies[species] {
			return fmt.Errorf("species %q named twice", species)
		}
		seenSpecies[species] = true
		if math.IsNaN(p.PricePerKgINR) || p.PricePerKgINR < SalePriceMinINR || p.PricePerKgINR > SalePriceMaxINR {
			return fmt.Errorf("sale price for %s must be between ₹%d and ₹%d per kg", species, SalePriceMinINR, SalePriceMaxINR)
		}
		if p.LoadedPricePerKgINR != nil && math.IsNaN(*p.LoadedPricePerKgINR) {
			return fmt.Errorf("sale price for %s needs the price it was loaded with", species)
		}
	}
	seenKeys := map[string]bool{}
	var saleLower, saleThreshold *float64
	for _, v := range update.Values {
		key, ok := LookupAssumptionKey(v.Key)
		if !ok {
			return fmt.Errorf("unknown assumption %q", v.Key)
		}
		if seenKeys[key.Key] {
			return fmt.Errorf("assumption %q named twice", key.Key)
		}
		seenKeys[key.Key] = true
		if v.RowVersion < 1 {
			return fmt.Errorf("%s needs the row_version it was loaded with", key.Key)
		}
		switch key.Kind {
		case AssumptionKindNumberList:
			if len(v.Values) < key.MinItems || len(v.Values) > key.MaxItems {
				return fmt.Errorf("%s needs between %d and %d edges", key.Key, key.MinItems, key.MaxItems)
			}
			for i, e := range v.Values {
				if math.IsNaN(e) || e < key.Min || e > key.Max {
					return fmt.Errorf("%s edges must be between %s and %s %s", key.Key, trimFloat(key.Min), trimFloat(key.Max), key.Unit)
				}
				if i > 0 && e <= v.Values[i-1] {
					return fmt.Errorf("%s edges must rise from one to the next", key.Key)
				}
			}
		case AssumptionKindDate:
			if _, err := time.Parse("2006-01-02", strings.TrimSpace(v.Date)); err != nil {
				return fmt.Errorf("%s must be a date (YYYY-MM-DD)", key.Key)
			}
		default:
			if math.IsNaN(v.Value) || v.Value < key.Min || v.Value > key.Max {
				return fmt.Errorf("%s must be between %s and %s %s", key.Key, trimFloat(key.Min), trimFloat(key.Max), key.Unit)
			}
			if key.Whole && v.Value != math.Trunc(v.Value) {
				return fmt.Errorf("%s must be a whole number of %s", key.Key, key.Unit)
			}
			switch key.Key {
			case AssumptionSaleReadyLowerKg:
				val := v.Value
				saleLower = &val
			case AssumptionSaleReadyThresholdKg:
				val := v.Value
				saleThreshold = &val
			}
		}
	}
	if saleLower != nil && saleThreshold != nil && *saleLower >= *saleThreshold {
		return fmt.Errorf("%s must be below %s", AssumptionSaleReadyLowerKg, AssumptionSaleReadyThresholdKg)
	}
	return nil
}

// LookupAssumptionKey resolves a key from the catalog, case- and space-insensitively.
func LookupAssumptionKey(raw string) (AssumptionKey, bool) {
	key := strings.ToLower(strings.TrimSpace(raw))
	for _, k := range AssumptionKeys {
		if k.Key == key {
			return k, true
		}
	}
	return AssumptionKey{}, false
}

// AssumptionValueOr reads one keyed figure out of a served set, or the default when the tenant
// has no row for it. Consumers call this so a missing row degrades to the figure the old
// constant carried rather than to zero.
func AssumptionValueOr(values []AssumptionValue, key string, fallback float64) float64 {
	for _, v := range values {
		if v.Key == key {
			return v.Value
		}
	}
	return fallback
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func trimFloat(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%g", v)
}

// CacheKey renders the settings as one stable token, so a read cached under one set of figures is
// never served for another (a bands regroup, a new target or cut-off changes the answer).
func (g GrowthSettings) CacheKey() string {
	parts := make([]string, 0, len(g.BandEdgesKg)+5)
	for _, e := range g.BandEdgesKg {
		parts = append(parts, trimFloat(e))
	}
	return "bands=" + strings.Join(parts, ",") + ";target=" + trimFloat(g.SlowGrowthTargetG) + ";badscan=" + trimFloat(g.BadScanLossGPerDay) + ";period=" + fmt.Sprintf("%d", g.DefaultPeriodDays)
}
