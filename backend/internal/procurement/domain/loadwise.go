package domain

// LOAD-WISE SALES — the Sales page's per-procurement-load reconciliation (maintainer decision
// 2026-08-31, docs/decisions/sales-loadwise.md).
//
// One row per procurement load answers: how many animals were purchased (accepted at herd
// intake), how many of those are sold, dead, otherwise exited, and still on farm — and what the
// load cost against what its animals brought in. Counts must reconcile: purchased = sold +
// mortality + other exits + remaining + unaccounted, and a non-zero unaccounted is shown loudly,
// never absorbed.
//
// Money rules, each locked with the maintainer:
//   - Purchase value = animal_cost + transport_cost + other_cost, recorded on the load itself.
//     A load with no recorded cost reports NO purchase value ("cost not recorded"), never zero.
//   - Sold value is attributed per animal: a sales deal's value split evenly across the animals
//     tagged to it (goat_sale_allocations), summed by the load those animals came from. A sold
//     animal with no tagged deal contributes nothing and is counted as unpriced — the row says
//     how much of its sold count is actually priced rather than pretending full attribution.
//   - Remaining stock value = remaining count x the load's own average sold price; a load with no
//     priced sales yet falls back to the overall average sold price across every tagged sale, and
//     with no basis at all the estimate is absent, never invented.

// Price bases for the remaining-stock estimate. The basis travels with the number so the screen
// can say WHICH average priced it.
const (
	LoadwisePriceBasisLoad    = "load"
	LoadwisePriceBasisOverall = "overall"
	LoadwisePriceBasisNone    = "none"
)

// LoadwiseLoad is one procurement load's reconciliation row.
type LoadwiseLoad struct {
	LoadID string
	// LoadRef is the farm's own load NUMBER ("131"), carried in procurement_loads.context ->
	// 'load_ref' for loads seeded from the legacy load sheet; "" for loads the app created
	// before a number convention exists. Display only, never a key.
	LoadRef      string
	VendorName   string
	PurchaseDate string // YYYY-MM-DD business date, "" when the load has none
	Status       string
	// Farm is the park code (CBE/CPT) every accepted animal of the load resolves to, or "" when
	// the load's animals span parks or none is known — agree-or-go-bare, never a majority pick.
	Farm string

	Purchased  int
	Sold       int
	Mortality  int // exit_reason = died
	OtherExits int // culled / transferred / lost — real outcomes, not discrepancies
	Remaining  int // still alive on farm
	// Unaccounted is purchased minus everything above. Non-zero means the herd register and the
	// load disagree (merged/inactive edge cases, data gaps) and the row must show it in red.
	Unaccounted int

	AnimalCost    *float64
	TransportCost *float64
	OtherCost     *float64
	// PurchaseValue is derived from the three parts; nil when no cost is recorded.
	PurchaseValue *float64

	SoldValue float64
	// SoldPriced is how many of Sold carry an attributed deal share; Sold - SoldPriced animals
	// were exited as sold without a tagged deal and carry no value here.
	SoldPriced int

	AvgSoldPrice   *float64
	PriceBasis     string
	RemainingValue *float64

	// PriorSold / PriorDead are the load's PRE-GOATOS outcomes (procurement_load_prior_outcomes):
	// animals already sold or already dead before the load's remaining animals were tracked here,
	// seeded from the legacy records with the dates they span. FinalizeLoadwise FOLDS them into
	// Purchased/Sold/Mortality and the money, so the reconciliation covers the whole load; the
	// raw blocks stay on the row so the screen can show the history and its dates.
	PriorSold LoadwisePriorOutcome
	PriorDead LoadwisePriorOutcome

	RowVersion int
}

// LoadwisePriorOutcome is one pre-GoatOS outcome block: how many animals, the revenue where the
// records carry it (sold only), and the date range the events span ("" when unknown).
type LoadwisePriorOutcome struct {
	Count   int
	Value   *float64
	FirstOn string
	LastOn  string
}

// LoadwiseSummary aggregates the served rows — same grain, same predicate, summed once here so no
// client re-derives its own totals.
type LoadwiseSummary struct {
	Purchased   int
	Sold        int
	Mortality   int
	OtherExits  int
	Remaining   int
	Unaccounted int

	// PurchaseValue sums only RECORDED costs; CostedLoads says how many of the served loads carry
	// one, so the total is never read as covering loads whose cost is missing.
	PurchaseValue float64
	CostedLoads   int
	SoldValue     float64
	// RemainingValue sums the per-load estimates that have a price basis.
	RemainingValue float64
}

// LoadwiseSales is the whole load-wise read: the served rows (newest purchase first), whole-filter
// totals, and the overall average sold price used as the fallback basis.
type LoadwiseSales struct {
	Loads []LoadwiseLoad
	// TotalLoads is the whole-tenant load count; len(Loads) is the served window.
	TotalLoads          int
	OverallAvgSoldPrice *float64
	Summary             LoadwiseSummary
}

// FinalizeLoadwise derives every per-row value the SQL read leaves to the domain — purchase value,
// average sold price with its basis, the remaining-stock estimate — and the summary. It mutates
// the rows in place and returns the assembled read.
func FinalizeLoadwise(loads []LoadwiseLoad, totalLoads int, overallAvg *float64) LoadwiseSales {
	out := LoadwiseSales{Loads: loads, TotalLoads: totalLoads, OverallAvgSoldPrice: overallAvg}
	for i := range loads {
		row := &loads[i]
		// Fold the pre-GoatOS history in FIRST: those animals were purchased on this load and
		// their outcome is known, so every count and the money must range over the WHOLE load.
		row.Purchased += row.PriorSold.Count + row.PriorDead.Count
		row.Sold += row.PriorSold.Count
		row.Mortality += row.PriorDead.Count
		if row.PriorSold.Value != nil {
			row.SoldValue += *row.PriorSold.Value
			row.SoldPriced += row.PriorSold.Count
		}
		// Unaccounted is the arithmetic gap over the folded counts — derived here, never counted.
		row.Unaccounted = row.Purchased - row.Sold - row.Mortality - row.OtherExits - row.Remaining
		row.PurchaseValue = loadPurchaseValue(row.AnimalCost, row.TransportCost, row.OtherCost)
		row.AvgSoldPrice, row.PriceBasis = loadAvgSoldPrice(row.SoldValue, row.SoldPriced, overallAvg)
		row.RemainingValue = remainingValue(row.Remaining, row.AvgSoldPrice)

		out.Summary.Purchased += row.Purchased
		out.Summary.Sold += row.Sold
		out.Summary.Mortality += row.Mortality
		out.Summary.OtherExits += row.OtherExits
		out.Summary.Remaining += row.Remaining
		out.Summary.Unaccounted += row.Unaccounted
		out.Summary.SoldValue += row.SoldValue
		if row.PurchaseValue != nil {
			out.Summary.PurchaseValue += *row.PurchaseValue
			out.Summary.CostedLoads++
		}
		if row.RemainingValue != nil {
			out.Summary.RemainingValue += *row.RemainingValue
		}
	}
	return out
}

// loadPurchaseValue derives the landed value of one load. animal_cost is the anchor: without it
// the cost is NOT RECORDED and the value is absent — transport/other alone cannot exist (schema
// constraint procurement_loads_cost_requires_animal_cost).
func loadPurchaseValue(animal, transport, other *float64) *float64 {
	if animal == nil {
		return nil
	}
	total := *animal
	if transport != nil {
		total += *transport
	}
	if other != nil {
		total += *other
	}
	return &total
}

// loadAvgSoldPrice resolves the average price one of this load's animals actually sold for, and
// the basis that produced it: the load's own priced sales first, the overall average as the
// fallback, absent when neither exists. A zero-value share never forms a basis — an average of
// zero would price remaining stock as worthless on no evidence.
func loadAvgSoldPrice(soldValue float64, soldPriced int, overallAvg *float64) (*float64, string) {
	if soldPriced > 0 && soldValue > 0 {
		avg := soldValue / float64(soldPriced)
		return &avg, LoadwisePriceBasisLoad
	}
	if overallAvg != nil && *overallAvg > 0 {
		avg := *overallAvg
		return &avg, LoadwisePriceBasisOverall
	}
	return nil, LoadwisePriceBasisNone
}

// remainingValue prices the animals still on farm at the resolved average. No remaining animals
// means a zero estimate only when a basis exists; no basis means no estimate at all.
func remainingValue(remaining int, avg *float64) *float64 {
	if avg == nil {
		return nil
	}
	value := float64(remaining) * *avg
	return &value
}

// LoadCostEdit is the recorded landed cost of one load, as entered by the procurement desk.
// A PUT of the full state: nil animal cost (with nil detail) clears the recorded cost.
type LoadCostEdit struct {
	AnimalCost    *float64
	TransportCost *float64
	OtherCost     *float64
}

// ErrLoadwiseValidation reports one cost-form field that failed, the same shape (and app-layer
// message composition) ErrFeedPurchaseValidation uses.
type ErrLoadwiseValidation struct {
	Field  string
	Reason string
}

func (e ErrLoadwiseValidation) Error() string {
	return "procurement: load cost " + e.Field + " " + e.Reason
}

// Validate applies validate-or-reject: a PRESENT value that is negative fails, and cost detail
// without the main animal-cost figure fails — the same rule the schema enforces, stated here so
// the entry form gets a farm-worded error instead of a constraint name.
func (e LoadCostEdit) Validate() error {
	for _, part := range []struct {
		field string
		value *float64
	}{
		{"animal_cost", e.AnimalCost},
		{"transport_cost", e.TransportCost},
		{"other_cost", e.OtherCost},
	} {
		if part.value != nil && *part.value < 0 {
			return ErrLoadwiseValidation{Field: part.field, Reason: "cannot be negative"}
		}
	}
	if e.AnimalCost == nil && (e.TransportCost != nil || e.OtherCost != nil) {
		return ErrLoadwiseValidation{Field: "animal_cost", Reason: "is required before transport or other costs"}
	}
	return nil
}
