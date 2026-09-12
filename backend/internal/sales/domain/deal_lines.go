package domain

import (
	"fmt"
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
	Breed       string

	AnimalCount   *float64
	MaleCount     *float64
	FemaleCount   *float64
	TotalWeightKg *float64
	SalesValue    float64
}

// Animals resolves how many animals this line moved: animal_count when recorded, otherwise the
// male+female split, and always zero for manure.
func (l DealLine) Animals() float64 {
	if !IsLiveProduct(l.ProductType) {
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

// DealLineWrite is one line of a record-sale body, before normalization.
type DealLineWrite struct {
	ProductType   string
	Breed         string
	AnimalCount   *float64
	MaleCount     *float64
	FemaleCount   *float64
	TotalWeightKg *float64
	SalesValue    float64
}

func (l DealLineWrite) normalize() DealLineWrite {
	out := l
	out.ProductType = strings.TrimSpace(l.ProductType)
	out.Breed = strings.Join(strings.Fields(l.Breed), " ")
	return out
}

// validate checks one line; field names carry the line's 1-based position so the desk can see
// WHICH row of the form was refused. lineNo 0 means the line came from the legacy single-product
// fields, whose names are reported bare.
func (l DealLineWrite) validate(lineNo int) error {
	field := func(name string) string {
		if lineNo == 0 {
			return name
		}
		return fmt.Sprintf("lines[%d].%s", lineNo, name)
	}
	if !IsProductType(l.ProductType) {
		return ErrDealValidation{Field: field("product_type"), Reason: "must be Sheep, Goat or Manure"}
	}
	if l.Breed == "" {
		return ErrDealValidation{Field: field("breed"), Reason: "required"}
	}
	if len(l.Breed) > maxDealShortField {
		return ErrDealValidation{Field: field("breed"), Reason: "too long"}
	}
	if l.SalesValue <= 0 {
		return ErrDealValidation{Field: field("sales_value"), Reason: "must be more than zero"}
	}
	for name, v := range map[string]*float64{
		"animal_count":    l.AnimalCount,
		"male_count":      l.MaleCount,
		"female_count":    l.FemaleCount,
		"total_weight_kg": l.TotalWeightKg,
	} {
		if v != nil && *v < 0 {
			return ErrDealValidation{Field: field(name), Reason: "must not be negative"}
		}
	}
	return nil
}

// linesFromLegacy turns a pre-000296 single-product body (product_type/breed/counts/value on the
// deal itself) into its one line, so an older client keeps recording exactly what it did before.
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
		if IsLiveProduct(l.ProductType) {
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
