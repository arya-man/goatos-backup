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
import "time"

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

	// DeclaredCount is what the LOAD ITSELF says it brought in (procurement_loads.expected_count,
	// the load sheet's procured figure for a seeded load). It is the denominator whenever it is
	// known, which is what makes Unaccounted a real discrepancy rather than an arithmetic
	// identity: animals the load declares but the register cannot account for stay visible.
	DeclaredCount int
	// Purchased is the load's size as reported: DeclaredCount when the load declares one, else
	// the animals actually attributed to it (tracked + prior outcomes).
	Purchased  int
	Sold       int
	Mortality  int // exit_reason = died
	OtherExits int // culled / transferred / lost — real outcomes, not discrepancies
	Remaining  int // still alive on farm
	// Unaccounted is Purchased minus every outcome above. Non-zero means the load and the register
	// disagree — animals the load declares that nothing accounts for (positive), or more animals
	// attributed than the load declares (negative). Either way the row shows it in red; it is
	// never absorbed into another bucket.
	Unaccounted int

	AnimalCost    *float64
	TransportCost *float64
	OtherCost     *float64
	// PurchaseValue is derived from the three parts; nil when no cost is recorded.
	PurchaseValue *float64
	// PurchaseWeightKg is the load's total LIVE weight at purchase. nil = not weighed, which is a
	// different fact from zero and is why LandedPricePerKg can be absent on a fully costed load.
	PurchaseWeightKg *float64
	// The SALE side of the same comparison, and the clock between them. All imported facts: the
	// legacy sales were never tagged to their animals, so nothing in GoatOS can derive them.
	//
	// SoldWeighedAnimals is the denominator that keeps AvgSaleWeightKg honest -- some legacy sales
	// recorded no weight, so it counts only the animals that actually carry one. Dividing by the
	// full sold count would invent a lighter animal than the farm ever sold.
	SoldWeightKg       *float64
	SoldWeighedAnimals *int
	// SoldWeighedValue is the revenue on those SAME weighed sales, so SalePricePerKg divides a
	// value and a weight drawn from one set of rows. It is NOT the load's sold value and must
	// never be shown as one -- that figure comes from a different source the maintainer chose to
	// keep (2026-09-01).
	SoldWeighedValue *float64
	// DaysSincePurchase is how long ago the load was BOUGHT, in whole days at the current business
	// date. It is the load's age, and it is deliberately a different clock from FatteningDays:
	// that one starts on ARRIVAL and stops at SALE, this one starts at PURCHASE and keeps running.
	// A load past LoadAgeAlertDays with animals still on farm is capital sitting in a shed, which
	// is what the daily CXO alert is about.
	DaysSincePurchase *int

	// ArrivedOn is the day the animals REACHED THE FARM -- not the purchase date, because the farm
	// warms animals up at the source and buys them a day or more before they land here.
	ArrivedOn string
	// FatteningDays is arrival to sale, animal-weighted across the load's sales.
	FatteningDays *int

	// AvgPurchaseWeightKg and AvgSaleWeightKg are how heavy one animal was coming in and going
	// out. The pair is the farm's growth read on a load; each is absent rather than zero when its
	// half was never weighed.
	AvgPurchaseWeightKg *float64
	AvgSaleWeightKg     *float64
	// SalePricePerKg is what one kilogram fetched, against LandedPricePerKg's what one cost.
	SalePricePerKg *float64

	// LandedPricePerKg is what one live kilogram of this load cost to land on the farm --
	// PurchaseValue / PurchaseWeightKg. The farm's own comparison figure between vendors, and the
	// reason PurchaseValue had to become landed cost first: per-kg on the ex-farm price alone
	// understated every deal by the transport nobody had recorded.
	LandedPricePerKg *float64

	// CostLines itemise what those three buckets are MADE OF -- the farm's own cost events for
	// this load (transport, booking, labour, transit, transition feed), newest kind order fixed by
	// CostLineKinds. Empty for a load costed by hand before the itemisation existed, which is not
	// the same as a load with no cost: the buckets above still answer that.
	//
	// The LIST renders the three buckets and this stays for the opened load (maintainer decision
	// 2026-09-01), so a reader sees one number per column and the breakdown only when they ask.
	CostLines []LoadCostLine

	SoldValue float64
	// SoldPriced is how many of Sold carry an attributed deal share; Sold - SoldPriced animals
	// were exited as sold without a tagged deal and carry no value here.
	SoldPriced int

	AvgSoldPrice   *float64
	PriceBasis     string
	RemainingValue *float64
	// ProfitLoss is what the load is worth against what it cost: sold value PLUS the animals
	// still on farm at the estimated sold price, MINUS the recorded landed cost (maintainer
	// decision 2026-08-31). Nil when the cost is not recorded — a load whose cost nobody entered
	// has no profit to state, and treating the missing cost as zero would report the whole sale
	// as profit. Part of it is UNREALISED whenever Remaining > 0, which is why the row also
	// carries the price basis that valued the stock.
	ProfitLoss *float64

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
	// ProfitLoss sums only the loads that HAVE a profit figure (i.e. a recorded cost), over the
	// same key set as CostedLoads, so the total never mixes priced and unpriced loads.
	ProfitLoss float64
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
// LoadAgeAlertDays is the age at which a load still holding animals becomes a daily CXO alert
// (maintainer decision 2026-09-01). Counted from the PURCHASE date, not arrival: the money left
// the business when the load was bought, so that is when the clock on it starts.
const LoadAgeAlertDays = 90

// DaysSincePurchase is the load's age in whole days at asOf, both read as Asia/Kolkata BUSINESS
// DATES. Day-grain on purpose: a load is bought on a day, not at an instant, and hour arithmetic
// here would make the same load read 89 or 90 depending on the time the page was opened.
//
// Absent when the purchase date is unknown or lies in the future — a negative age is not a fact
// about the load, it is a fact about a bad date, and rendering it as a bar would assert the
// former.
func DaysSincePurchase(purchaseDate, asOf string) *int {
	if purchaseDate == "" || asOf == "" {
		return nil
	}
	bought, err := time.Parse("2006-01-02", purchaseDate)
	if err != nil {
		return nil
	}
	today, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		return nil
	}
	days := int(today.Sub(bought).Hours() / 24)
	if days < 0 {
		return nil
	}
	return &days
}

// FinalizeLoadwise takes asOf as the current Asia/Kolkata business date so the age clock is
// derived, never stored: a stored age is wrong the next morning.
func FinalizeLoadwise(loads []LoadwiseLoad, totalLoads int, overallAvg *float64, asOf string) LoadwiseSales {
	out := LoadwiseSales{Loads: loads, TotalLoads: totalLoads, OverallAvgSoldPrice: overallAvg}
	for i := range loads {
		row := &loads[i]
		// Fold the pre-GoatOS history in FIRST: those animals were purchased on this load and
		// their outcome is known, so every count and the money must range over the WHOLE load.
		attributed := row.Purchased + row.PriorSold.Count + row.PriorDead.Count
		row.Sold += row.PriorSold.Count
		row.Mortality += row.PriorDead.Count
		if row.PriorSold.Value != nil {
			row.SoldValue += *row.PriorSold.Value
			row.SoldPriced += row.PriorSold.Count
		}
		// The DECLARED size wins as the denominator when the load states one. Deriving Purchased
		// from its own parts instead would make Unaccounted zero by construction and hide exactly
		// the difference this column exists to show.
		row.Purchased = attributed
		if row.DeclaredCount > 0 {
			row.Purchased = row.DeclaredCount
		}
		row.Unaccounted = row.Purchased - row.Sold - row.Mortality - row.OtherExits - row.Remaining
		row.PurchaseValue = loadPurchaseValue(row.AnimalCost, row.TransportCost, row.OtherCost)
		row.AvgSoldPrice, row.PriceBasis = loadAvgSoldPrice(row.SoldValue, row.SoldPriced, overallAvg)
		row.RemainingValue = remainingValue(row.Remaining, row.AvgSoldPrice)
		row.LandedPricePerKg = landedPricePerKg(row.PurchaseValue, row.PurchaseWeightKg)
		// Per-animal weights: the purchase side over the animals the load BROUGHT IN, the sale
		// side over only the animals that were actually weighed on the way out.
		row.AvgPurchaseWeightKg = perAnimal(row.PurchaseWeightKg, row.Purchased)
		if row.SoldWeighedAnimals != nil {
			row.AvgSaleWeightKg = perAnimal(row.SoldWeightKg, *row.SoldWeighedAnimals)
		}
		// Price per kg on the SAME rows the weight came from -- numerator and denominator must
		// range over one key set, or the ratio describes no real set of sales.
		row.SalePricePerKg = landedPricePerKg(row.SoldWeighedValue, row.SoldWeightKg)
		row.DaysSincePurchase = DaysSincePurchase(row.PurchaseDate, asOf)
		row.ProfitLoss = profitLoss(row.PurchaseValue, row.SoldValue, row.RemainingValue)

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
		if row.ProfitLoss != nil {
			out.Summary.ProfitLoss += *row.ProfitLoss
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

// landedPricePerKg is what one live kilogram of a load cost to land: the LANDED cost (animals plus
// transport plus everything else) over the live weight bought.
//
// Absent whenever either half is missing, and never a fabricated zero: a load nobody costed and a
// load nobody weighed both have no per-kg price to state, and printing 0 would read as "these
// animals were free". The zero-weight branch is belt-and-braces -- the schema already refuses a
// non-positive weight -- because a divide-by-zero here would render as +Inf on a farm screen.
func landedPricePerKg(purchaseValue, weightKg *float64) *float64 {
	if purchaseValue == nil || weightKg == nil || *weightKg <= 0 {
		return nil
	}
	price := *purchaseValue / *weightKg
	return &price
}

// perAnimal divides a load-level weight by a head count. Absent whenever either half is missing or
// the count is not positive: a load nobody weighed and a load with no animals both have no
// per-animal weight to state, and 0 kg would read as a fact about the animals rather than about
// the record.
func perAnimal(total *float64, count int) *float64 {
	if total == nil || count <= 0 {
		return nil
	}
	value := *total / float64(count)
	return &value
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

// profitLoss states what the load is worth against what it cost: realised sales PLUS the stock
// still on farm at its estimated price, MINUS the landed cost.
//
// Absent when the cost is NOT RECORDED. That is the whole reason this returns a pointer: with no
// cost, "profit" would be the entire sale value, which reads as a spectacular margin on a load
// nobody has priced. Remaining stock counts as zero when it cannot be valued (no price basis),
// which understates rather than invents.
func profitLoss(purchaseValue *float64, soldValue float64, remainingValue *float64) *float64 {
	if purchaseValue == nil {
		return nil
	}
	total := soldValue - *purchaseValue
	if remainingValue != nil {
		total += *remainingValue
	}
	return &total
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

// Cost-line kinds -- the farm's own vocabulary, taken from the Procurement DB sheet's Record Type
// column so an imported line and a hand-entered one are the same kind of thing. 'animal' is the
// sheet's "Purchase" row, renamed to match the bucket it rolls into.
const (
	CostKindAnimal         = "animal"
	CostKindTransport      = "transport"
	CostKindBooking        = "booking"
	CostKindLabour         = "labour"
	CostKindTransit        = "transit"
	CostKindTransitionFeed = "transition_feed"
	CostKindOther          = "other"
)

// CostLineKinds is the display order of the itemisation, animal first and the movement costs in
// the order they happen on the ground. Also the closed vocabulary the write path validates
// against, so this list and the schema's kind CHECK are the same set stated twice deliberately --
// one gives a farm-worded refusal, the other makes a bad row impossible.
var CostLineKinds = []string{
	CostKindAnimal,
	CostKindTransport,
	CostKindBooking,
	CostKindLabour,
	CostKindTransit,
	CostKindTransitionFeed,
	CostKindOther,
}

// LoadCostLine is one recorded cost event on a load.
type LoadCostLine struct {
	LineID string
	Kind   string
	Amount float64
	Note   string
	// Source is "sheet_import" for a figure lifted from the farm's Procurement DB sheet and "app"
	// for one a person typed. Carried so an imported number is never mistaken for entered work.
	Source string
}

// CostBucketForKind maps a cost kind onto the column it rolls into. Everything that is neither the
// animals themselves nor their transport is "other" -- the maintainer kept the list at three
// columns on purpose (2026-09-01), so booking, labour, transit and transition feed share one.
//
// An UNKNOWN kind also answers "other" rather than being dropped. A cost nobody has classified is
// still money the farm spent, and silently excluding it would understate purchase value in exactly
// the way this whole change exists to fix.
func CostBucketForKind(kind string) string {
	switch kind {
	case CostKindAnimal:
		return CostKindAnimal
	case CostKindTransport:
		return CostKindTransport
	default:
		return CostKindOther
	}
}

// RollUpCostLines reduces an itemisation to the three bucket figures the list renders.
//
// A bucket with NO lines comes back nil, not zero, and the distinction is the whole point: nil is
// "not recorded" and renders as such, while zero asserts the farm spent nothing. A load with no
// lines at all therefore rolls up to three nils, and the caller keeps whatever the columns already
// hold rather than erasing a hand-entered cost.
func RollUpCostLines(lines []LoadCostLine) (animal, transport, other *float64) {
	sums := map[string]float64{}
	seen := map[string]bool{}
	for _, line := range lines {
		bucket := CostBucketForKind(line.Kind)
		sums[bucket] += line.Amount
		seen[bucket] = true
	}
	pick := func(bucket string) *float64 {
		if !seen[bucket] {
			return nil
		}
		value := sums[bucket]
		return &value
	}
	return pick(CostKindAnimal), pick(CostKindTransport), pick(CostKindOther)
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

// OverdueLoad is one load past LoadAgeAlertDays that still holds animals — the subject of the
// daily CXO alert (maintainer decision 2026-09-01).
//
// It carries what the message must SAY, not an id to look up later: which load, whose vendor,
// which park, how old, how many animals are still standing in the shed and what the load cost.
// An alert that says "1 load is overdue" is the abstract-notification defect the specificity rule
// bans.
type OverdueLoad struct {
	LoadID     string
	LoadRef    string
	VendorName string
	Farm       string
	// PurchaseDate is the ISO business date the load was bought; the message renders it in farm
	// words, never as the wire string.
	PurchaseDate      string
	DaysSincePurchase int
	Remaining         int
	PurchaseValue     *float64
}

// OverdueLoads selects the loads a daily alert must name: older than LoadAgeAlertDays AND still
// holding animals.
//
// BOTH conditions, and the second is the one that makes the alert worth reading. A load bought a
// year ago that sold out is history — alerting on it every morning forever would train the reader
// to ignore the alert, which costs more than the alert gains. Only capital still standing in a
// shed is actionable.
//
// Strictly greater than the threshold: "exceeds 90 days" is the maintainer's wording, so day 90
// is not yet overdue and day 91 is.
func OverdueLoads(loads []LoadwiseLoad) []OverdueLoad {
	var out []OverdueLoad
	for _, load := range loads {
		if load.DaysSincePurchase == nil || *load.DaysSincePurchase <= LoadAgeAlertDays {
			continue
		}
		if load.Remaining <= 0 {
			continue
		}
		out = append(out, OverdueLoad{
			LoadID:            load.LoadID,
			LoadRef:           load.LoadRef,
			VendorName:        load.VendorName,
			Farm:              load.Farm,
			PurchaseDate:      load.PurchaseDate,
			DaysSincePurchase: *load.DaysSincePurchase,
			Remaining:         load.Remaining,
			PurchaseValue:     load.PurchaseValue,
		})
	}
	return out
}
