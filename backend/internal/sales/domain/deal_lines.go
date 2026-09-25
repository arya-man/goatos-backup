package domain

import (
	"fmt"
	"math"
	"strings"
)

// ProductMixed is the deal-level ROLLUP word for a sale whose lines name more than one product
// (or more than one breed, on the breed column). It is a summary, never something sold: a LINE
// may not carry it, and IsProductType still refuses it on a write.
const ProductMixed = "Mixed"

// MaxDealLines bounds one sale. A real mixed sale is two or three lines; the cap stops a client
// loop from turning one deal into a table.
const MaxDealLines = 20

// DealLine is ONE product/breed slice of a sale (maintainer decision 2026-09-12, migration
// 000296): what was sold, in the order the desk entered it. The deal owns the buyer, the date,
// the advance, the status and the receipts; the line owns product, breed, counts, weight and its
// share of the value.
type DealLine struct {
	LineID string
	LineNo int

	ProductType string
	// ProductCode and ProductKind are the registry row this line was sold under, stamped at write
	// time (migration 000422). The kind is what every read below asks -- never the name, which the
	// farm may since have renamed.
	ProductCode string
	ProductKind string
	// Breed is the line's VARIANT: an animal line's breed, a feed line's feed item, 'Manure' for
	// manure. The column keeps the older word so every (product, breed) band and chart still keys
	// on the fact it always keyed on.
	Breed string

	// Quantity at RatePerUnit, for a line priced by the unit rather than as a lump: feed by the
	// kilogram, and manure when the desk records it that way. SalesValue stays the authoritative
	// money figure and is Quantity * RatePerUnit when both are recorded.
	Quantity    *float64
	Unit        string
	RatePerUnit *float64

	AnimalCount   *float64
	MaleCount     *float64
	FemaleCount   *float64
	TotalWeightKg *float64
	SalesValue    float64

	// The recorded assumption for a line the desk could not weigh (migration 000381). It sits
	// BESIDE TotalWeightKg and never replaces it: a line with a recorded weight is read from the
	// recording, and only a line with none falls through to these. Exactly one of the two is
	// ever set -- kilograms for the whole line, or a band where no kilogram is honestly knowable
	// -- and WeightEstimateBasis says in words how the figure was arrived at, so the assumption
	// can be audited and argued with rather than only obeyed.
	//
	// They are read by the sold-weight bands ALONE. Realized price per kg and the price bands
	// deliberately do not see them: two of these estimates were derived FROM price, and feeding
	// them back into a price average would only re-assert the rate they were derived from.
	EstimatedWeightKg   *float64
	EstimatedWeightBand string
	WeightEstimateBasis string
}

// Kind is what this line's product does, from the line itself. A line recorded before migration
// 000422 stamped one -- or a synthetic line built from a deal read without its lines -- falls back
// to the built-in vocabulary, which is the only thing those rows can be: until 000422 nothing else
// could be stored.
func (l DealLine) Kind() string {
	if l.ProductKind != "" {
		return l.ProductKind
	}
	return BuiltinKind(l.ProductType)
}

// IsLive reports whether this line sold live animals.
func (l DealLine) IsLive() bool { return l.Kind() == KindAnimal }

// Code is the registry row this line was sold under. A line recorded before migration 000422, or
// a synthetic line from a deal read without lines, falls back to the built-in whose name it
// carries -- and to nothing at all for a rollup of 'Mixed', which names no single product.
func (l DealLine) Code() string {
	if l.ProductCode != "" {
		return l.ProductCode
	}
	switch l.ProductType {
	case ProductSheep:
		return ProductCodeSheep
	case ProductGoat:
		return ProductCodeGoat
	case ProductManure:
		return ProductCodeManure
	}
	return ""
}

// QuantityKg is the line's recorded quantity, 0 when none was recorded. It is what a feed line
// SOLD -- kilograms off the store -- and is a different fact from TotalWeightKg, which is live
// weight on the hoof.
func (l DealLine) QuantityKg() float64 {
	if l.Quantity == nil {
		return 0
	}
	return *l.Quantity
}

// Animals resolves how many animals this line moved: animal_count when recorded, otherwise the
// male+female split, and always zero for manure.
func (l DealLine) Animals() float64 {
	if !l.IsLive() {
		return 0
	}
	if l.AnimalCount != nil {
		return *l.AnimalCount
	}
	total := 0.0
	if l.MaleCount != nil {
		total += *l.MaleCount
	}
	if l.FemaleCount != nil {
		total += *l.FemaleCount
	}
	return total
}

// WeightKg is the line's recorded weight, 0 when none was recorded.
func (l DealLine) WeightKg() float64 {
	if l.TotalWeightKg == nil {
		return 0
	}
	return *l.TotalWeightKg
}

// SoldWeightKg uses a kilogram quantity when present, otherwise the explicitly
// recorded weight. A quantity counted by number is never a weight. Older rows
// without quantity/unit retain their measured weight; a recorded zero stays zero.
func (l DealLine) SoldWeightKg() float64 {
	if l.Quantity != nil && l.Unit == UnitKg {
		return *l.Quantity
	}
	return l.WeightKg()
}

// DealLineWrite is one line of a record-sale body, before normalization.
type DealLineWrite struct {
	ProductType   string
	Breed         string
	Quantity      *float64
	RatePerUnit   *float64
	AnimalCount   *float64
	MaleCount     *float64
	FemaleCount   *float64
	TotalWeightKg *float64
	SalesValue    float64

	// Resolved from the registry by normalize. Unexported because a CLIENT may not name them: a
	// body that could send its own kind could sell a goat as feed and draw it out of the store.
	productCode string
	productKind string
	unit        string
}

// ResolvedProduct is what normalize stamped: the registry row this line resolved to. The
// repository writes these onto the line and reads the kind to decide whether the sale depletes
// feed stock.
func (l DealLineWrite) ResolvedProduct() (code, kind, unit string) {
	return l.productCode, l.productKind, l.unit
}

// QuantityKg is the line's recorded quantity, 0 when none was recorded.
func (l DealLineWrite) QuantityKg() float64 {
	if l.Quantity == nil {
		return 0
	}
	return *l.Quantity
}

// Kind is the resolved kind, or the built-in fallback for a line whose product did not resolve --
// which validate then refuses. Reading it before that refusal must not panic or lie.
func (l DealLineWrite) Kind() string {
	if l.productKind != "" {
		return l.productKind
	}
	return BuiltinKind(l.ProductType)
}

// normalize trims the line and resolves its product against the ACTIVE registry, stamping the
// code, kind and unit the sale is recorded under.
//
// A line priced by the unit has its value COMPUTED here, once: quantity * rate. The desk enters
// two tonnes at twenty-one rupees and the money follows, so a stored value can never disagree with
// the arithmetic printed beside it. A line that names no rate keeps the lump value it was sent.
func (l DealLineWrite) normalize(cat ProductCatalog) DealLineWrite {
	out := l
	out.ProductType = strings.TrimSpace(l.ProductType)
	out.Breed = strings.Join(strings.Fields(l.Breed), " ")
	if p, ok := cat.Lookup(out.ProductType); ok {
		// The registry's spelling wins over the caller's, so a sale is recorded under the word the
		// farm actually keeps -- not 'sheep' because that is how it arrived on the wire.
		out.ProductType = p.Name
		out.productCode, out.productKind, out.unit = p.Code, p.Kind, p.Unit
	}
	if out.Quantity != nil && out.RatePerUnit != nil {
		out.SalesValue = *out.Quantity * *out.RatePerUnit
	}
	return out
}

// validate checks one line; field names carry the line's 1-based position so the desk can see
// WHICH row of the form was refused. lineNo 0 means the line came from the legacy single-product
// fields, whose names are reported bare.
func (l DealLineWrite) validate(lineNo int, cat ProductCatalog) error {
	field := func(name string) string {
		if lineNo == 0 {
			return name
		}
		return fmt.Sprintf("lines[%d].%s", lineNo, name)
	}
	if l.ProductType == "" {
		return ErrDealValidation{Field: field("product_type"), Reason: "required"}
	}
	product, known := cat.Lookup(l.ProductType)
	if !known {
		// Named, and not something this farm sells. The message points at the registry rather than
		// listing a vocabulary, because the vocabulary is now the farm's and this package no
		// longer knows it.
		return ErrDealValidation{
			Field:  field("product_type"),
			Reason: "is not something this farm sells; add it under Configuration, Items and settings",
		}
	}
	if l.Breed == "" {
		reason := "required"
		if product.DrawsFeedStock() {
			reason = "pick the feed being sold"
		}
		return ErrDealValidation{Field: field("breed"), Reason: reason}
	}
	if len(l.Breed) > maxDealShortField {
		return ErrDealValidation{Field: field("breed"), Reason: "too long"}
	}
	// What the KIND requires. Only these three branches read the product at all; everything above
	// and below is true of any line whatever the farm decided to sell.
	if product.DrawsFeedStock() && product.Unit != UnitKg {
		// An item authored before feed was pinned to kilograms. Its quantity would be subtracted
		// from a balance kept in kilograms, so the sale is refused here rather than silently
		// spending the wrong unit, and the message names the setting that fixes it.
		return ErrDealValidation{
			Field:  field("product_type"),
			Reason: "is feed from the store but is not sold by the kilogram; set its unit to kilograms under Configuration, Items and settings",
		}
	}
	if product.PricedPerUnit() {
		// AN OLDER APP IS STILL A REAL SALE. Manure sold by the kilogram long before this line
		// learned to carry a quantity and a rate, and an installed phone posts it the old way:
		// a value, and a weight. Those sales are sitting in outboxes right now, and refusing them
		// here is terminal -- the row dead-letters and the sale is lost, not retried. A legacy
		// line is accepted on the value it carries; only the kilograms are unknown, and the
		// reports already read a quantity when there is one and the weight when there is not.
		//
		// A NEW client is held to the new contract: it sends a quantity, so a quantity of zero or
		// a missing rate is a real mistake rather than an old app, and is still refused.
		//
		// FEED IS NEVER ACCEPTED THIS WAY, whatever it carries. No installed app can have queued a
		// feed sale -- the item did not exist -- so there is no compatibility to keep, and a feed
		// line without kilograms would take the money while leaving the sacks on the shelf. The
		// store's balance is the thing this refusal protects.
		legacyPriced := !product.DrawsFeedStock() && l.Quantity == nil && l.RatePerUnit == nil && l.SalesValue > 0
		if !legacyPriced {
			// An item sold by the kilogram or by number IS its quantity: a line without one would
			// take the sale's money without saying how much left the farm -- and for feed, without
			// taking anything off the shelf, so the stock the store reports would drift from it.
			if l.Quantity == nil || *l.Quantity <= 0 {
				return ErrDealValidation{Field: field("quantity"), Reason: "must be more than zero"}
			}
			if l.RatePerUnit == nil {
				return ErrDealValidation{Field: field("rate_per_unit"), Reason: "required; " + strings.ToLower(product.Name) + " is sold at a rate per " + product.UnitWord()}
			}
		}
		// Refused rather than ignored. A body carrying both a quantity and an animal count is two
		// different sales in one line, and silently dropping half of it would record the money
		// while losing what it was for.
		for name, v := range map[string]*float64{
			"animal_count": l.AnimalCount,
			"male_count":   l.MaleCount,
			"female_count": l.FemaleCount,
		} {
			if v != nil {
				return ErrDealValidation{Field: field(name), Reason: "this is not an animal sale, so it carries no animals"}
			}
		}
	}
	// The money check comes AFTER the kind rules on purpose. A feed line's value is COMPUTED from
	// quantity * rate, so a missing quantity makes the value zero too -- and refusing it as
	// "sales_value must be more than zero" would name a field the feed form does not even show,
	// sending the desk to look for a box that is not there. The kind rule names the box that is.
	if l.SalesValue <= 0 {
		return ErrDealValidation{Field: field("sales_value"), Reason: "must be more than zero"}
	}
	for name, v := range map[string]*float64{
		"animal_count":    l.AnimalCount,
		"male_count":      l.MaleCount,
		"female_count":    l.FemaleCount,
		"total_weight_kg": l.TotalWeightKg,
		"quantity":        l.Quantity,
		"rate_per_unit":   l.RatePerUnit,
	} {
		if v != nil && *v < 0 {
			return ErrDealValidation{Field: field(name), Reason: "must not be negative"}
		}
	}

	return nil
}

// linesFromLegacy turns a pre-000296 single-product body (product_type/breed/counts/value on the
// deal itself) into its one line, so an older client keeps recording exactly what it did before.
//
// It carries NO quantity or rate, deliberately: selling feed is a capability that arrived with
// lines, and an older client that cannot send lines cannot sell feed either. A feed line reaching
// this path would fail validation for the missing quantity, which is the honest answer -- better
// than a legacy body half-describing a sale that moves stock.
// Returns nil when the body names no product, which Validate then refuses as "no lines".
func (w DealWrite) linesFromLegacy() []DealLineWrite {
	if w.ProductType == "" && w.Breed == "" {
		return nil
	}
	return []DealLineWrite{{
		ProductType: w.ProductType, Breed: w.Breed,
		AnimalCount: w.AnimalCount, MaleCount: w.MaleCount, FemaleCount: w.FemaleCount,
		TotalWeightKg: w.TotalWeightKg, SalesValue: w.SalesValue,
	}}
}

// DealRollup is what the deal-level columns hold for a set of lines: sums for the numbers, and
// the one shared value -- or ProductMixed -- for product and breed. Every existing reader of
// sales_deals (the ledger row, tagging animals to a sale, the load-wise revenue join, the
// sex-count sync) keeps reading the deal row and stays correct, because these are maintained in
// the same transaction as the lines.
type DealRollup struct {
	ProductType   string
	Breed         string
	AnimalCount   *float64
	MaleCount     *float64
	FemaleCount   *float64
	TotalWeightKg *float64
	SalesValue    float64
}

// RollupLines computes the deal-level rollup of validated lines.
//
// A count sums only LIVE-product lines that recorded one and stays nil when none did: "not
// recorded" is a different fact from 0, and a blank animal count is what keeps the tag-animals gate
// from asking for a target the desk never declared. Manure may carry kg and value, but never a live
// animal target.
func RollupLines(lines []DealLineWrite) DealRollup {
	var out DealRollup
	sum := func(acc **float64, v *float64) {
		if v == nil {
			return
		}
		if *acc == nil {
			zero := 0.0
			*acc = &zero
		}
		**acc += *v
	}
	products := map[string]struct{}{}
	breeds := map[string]struct{}{}
	for _, l := range lines {
		products[l.ProductType] = struct{}{}
		breeds[l.Breed] = struct{}{}
		if l.Kind() == KindAnimal {
			sum(&out.AnimalCount, l.AnimalCount)
			sum(&out.MaleCount, l.MaleCount)
			sum(&out.FemaleCount, l.FemaleCount)
		}
		sum(&out.TotalWeightKg, l.TotalWeightKg)
		out.SalesValue += l.SalesValue
		if len(products) == 1 {
			out.ProductType = l.ProductType
		} else {
			out.ProductType = ProductMixed
		}
		if len(breeds) == 1 {
			out.Breed = l.Breed
		} else {
			out.Breed = ProductMixed
		}
	}
	return out
}

// lineView is the deal's lines, or -- for a deal read without them (a test fixture, a caller that
// never attached lines) -- ONE synthetic line built from the deal's own columns, which for a
// single-product deal is exactly the backfilled 000296 line. Reporting code iterates this so it
// never has to branch on whether lines were loaded.
func (d Deal) lineView() []DealLine {
	if len(d.Lines) > 0 {
		return d.Lines
	}
	return []DealLine{{
		LineNo: 1, ProductType: d.ProductType, Breed: d.Breed,
		AnimalCount: d.AnimalCount, MaleCount: d.MaleCount, FemaleCount: d.FemaleCount,
		TotalWeightKg: d.TotalWeightKg, SalesValue: d.SalesValue,
	}}
}

// FeedDemand is what ONE feed takes off ONE farm's store, summed across every line of a sale that
// names it.
//
// It is summed per FEED and not per line deliberately: one sale may carry the same feed twice --
// two lots at two rates is an ordinary way to write a load -- and weighing each line on its own let
// two 9,000 kg lines through a 13,790 kg store because neither exceeded it alone. The store went to
// -4,289.9 kg with nobody warned, which is exactly what the confirmation exists to prevent.
type FeedDemand struct {
	// LineNo is the FIRST line naming this feed, 1-based, so the sentence the desk reads names the
	// row they would look at first. 0 when the demand came from a recorded deal rather than a form.
	LineNo   int
	FeedItem string
	Kg       float64
}

// AggregateFeedDemand sums a write's feed lines per feed, in the order the farm typed them.
func AggregateFeedDemand(lines []DealLineWrite) []FeedDemand {
	at := map[string]int{}
	out := []FeedDemand{}
	for i, l := range lines {
		if l.Kind() != KindFeed {
			continue
		}
		idx, seen := at[l.Breed]
		if !seen {
			out = append(out, FeedDemand{LineNo: i + 1, FeedItem: l.Breed})
			idx = len(out) - 1
			at[l.Breed] = idx
		}
		out[idx].Kg += l.QuantityKg()
	}
	return out
}

// ValidateFeedItems refuses stock movements that cannot resolve to the tenant's active feed catalog.
func ValidateFeedItems(lines []DealLineWrite, items []string) error {
	for i, line := range lines {
		if line.Kind() != KindFeed {
			continue
		}
		found := false
		for _, item := range items {
			if strings.EqualFold(strings.TrimSpace(item), line.Breed) {
				found = true
				break
			}
		}
		if !found {
			return ErrDealValidation{Field: fmt.Sprintf("lines[%d].breed", i+1), Reason: "pick an active feed from the feed catalog"}
		}
	}
	return nil
}

// HasLiveAnimals reports whether this sale sold animals that can be tagged: an animal-kind line
// whose head counts add up to at least one whole animal (maintainer decision 2026-09-25). It decides
// whether the sale's workflow carries its tag, loading-video and gate-pass steps.
//
// It is read from the LINES, never the deal-level animal_count -- legacy manure deals carry an
// animal_count of 1 -- and it counts exactly what the tagging confirm will accept: the confirm
// refuses a deal whose floored animal_count (the sum of its animal lines' counts, RollupLines) is
// not above zero, so a line recorded with only a male/female split and no head count owes no tag.
func (w DealWrite) HasLiveAnimals() bool {
	total := 0.0
	for _, l := range w.Lines {
		if l.Kind() == KindAnimal && l.AnimalCount != nil && *l.AnimalCount > 0 {
			total += *l.AnimalCount
		}
	}
	return math.Floor(total) >= 1
}
