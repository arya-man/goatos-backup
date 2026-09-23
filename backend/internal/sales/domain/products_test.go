package domain

import (
	"errors"
	"testing"
)

// builtinCatalog is the registry every tenant starts with (migration 000393): the exact three
// products that used to be constants in this package. Tests written before the registry existed
// pass it, so they keep asserting what they always asserted.
func builtinCatalog() ProductCatalog {
	return NewProductCatalog([]Product{
		{Code: ProductCodeSheep, Name: ProductSheep, Kind: KindAnimal, Unit: "head", SpeciesCode: "sheep", SortOrder: 10},
		{Code: ProductCodeGoat, Name: ProductGoat, Kind: KindAnimal, Unit: "head", SpeciesCode: "goat", SortOrder: 20},
		{Code: ProductCodeManure, Name: ProductManure, Kind: KindOther, Unit: "kg", SortOrder: 30},
	})
}

// farmCatalog is the built-ins plus the two things a farm adds for itself: feed off its own store,
// and another non-animal product. Neither needs a line of code to exist, which is the point.
func farmCatalog() ProductCatalog {
	return NewProductCatalog(append(builtinCatalog().Products(),
		Product{Code: "feed", Name: "Feed", Kind: KindFeed, Unit: "kg", SortOrder: 40},
		Product{Code: "hay", Name: "Hay", Kind: KindOther, Unit: "kg", SortOrder: 50},
	))
}

func f(v float64) *float64 { return &v }

func feedSale() DealWrite {
	return DealWrite{
		SaleDate: "2026-09-23", Farm: "CPT",
		BuyerName: "Ramesh Traders", BuyerVendorID: "8f2f0d1e-1a2b-4c3d-9e8f-0a1b2c3d4e5f",
		Lines: []DealLineWrite{{ProductType: "Feed", Breed: "Maize", Quantity: f(2000), RatePerUnit: f(21)}},
	}
}

// A farm that adds feed to its registry can sell feed, with no code naming it.
func TestFeedIsSellableBecauseTheRegistrySaysSo(t *testing.T) {
	w := feedSale().Normalize(farmCatalog())
	if err := w.Validate(farmCatalog()); err != nil {
		t.Fatalf("a feed sale must record: %v", err)
	}
	line := w.Lines[0]
	code, kind, unit := line.ResolvedProduct()
	if code != "feed" || kind != KindFeed || unit != "kg" {
		t.Fatalf("line must be stamped with the registry row, got %q/%q/%q", code, kind, unit)
	}
	// The money follows the kilograms, computed once rather than taken from the caller.
	if line.SalesValue != 42000 {
		t.Fatalf("2000kg at 21 must be 42000, got %v", line.SalesValue)
	}
	if w.SalesValue != 42000 {
		t.Fatalf("the deal rolls up to the same money, got %v", w.SalesValue)
	}
	// A feed sale moved no animals, so the deal must not claim a live target.
	if w.AnimalCount != nil {
		t.Fatalf("a feed sale has no animal count, got %v", *w.AnimalCount)
	}
}

// The same body against a farm that has NOT added feed is refused, and the refusal points at the
// registry rather than reciting a vocabulary this package no longer owns.
func TestAProductTheFarmDoesNotSellIsRefused(t *testing.T) {
	w := feedSale().Normalize(builtinCatalog())
	err := w.Validate(builtinCatalog())
	if err == nil {
		t.Fatal("selling feed with no feed product in the registry must be refused")
	}
	var v ErrDealValidation
	if !errors.As(err, &v) || v.Field != "lines[1].product_type" {
		t.Fatalf("must name the line's product field, got %#v", err)
	}
}

// The kilograms ARE the feed sale: a feed line with no quantity would take money without taking
// anything off the shelf, and the store's reported stock would drift from the store.
func TestAFeedLineWithoutKilogramsIsRefused(t *testing.T) {
	// The FIELD is the assertion, not merely that something was refused. A feed line's value is
	// computed from its quantity, so a missing quantity also makes the value zero -- and an
	// earlier version of this test passed on the generic sales_value rule while the quantity rule
	// it claimed to pin was never reached. Naming the field is what makes it bite.
	for name, mutate := range map[string]func(*DealWrite){
		"no quantity at all": func(w *DealWrite) { w.Lines[0].Quantity = nil },
		"zero kilograms":     func(w *DealWrite) { w.Lines[0].Quantity = f(0) },
	} {
		w := feedSale()
		mutate(&w)
		w = w.Normalize(farmCatalog())
		err := w.Validate(farmCatalog())
		var v ErrDealValidation
		if !errors.As(err, &v) || v.Field != "lines[1].quantity" {
			t.Fatalf("%s: must be refused on the quantity field, got %#v", name, err)
		}
	}
}

// And the rate, for the same reason: feed is sold at a price per kilogram, and a line with none
// records money against kilograms at no stated price.
func TestAFeedLineWithoutARateIsRefused(t *testing.T) {
	w := feedSale()
	w.Lines[0].RatePerUnit = nil
	w = w.Normalize(farmCatalog())
	err := w.Validate(farmCatalog())
	var v ErrDealValidation
	if !errors.As(err, &v) || v.Field != "lines[1].rate_per_unit" {
		t.Fatalf("must be refused on the rate field, got %#v", err)
	}
}

// Refused rather than quietly dropped: a line carrying both feed kilograms and an animal count is
// two sales in one, and keeping the money while losing what it was for is the worse answer.
func TestAFeedLineMayNotCarryAnimals(t *testing.T) {
	w := feedSale()
	w.Lines[0].AnimalCount = f(12)
	w = w.Normalize(farmCatalog())
	err := w.Validate(farmCatalog())
	if err == nil {
		t.Fatal("a feed line naming animals must be refused")
	}
	var v ErrDealValidation
	if !errors.As(err, &v) || v.Field != "lines[1].animal_count" {
		t.Fatalf("must name the count field, got %#v", err)
	}
}

// A sale records under the word the FARM keeps, not the spelling that arrived on the wire.
func TestTheRegistrySpellingWins(t *testing.T) {
	w := feedSale()
	w.Lines[0].ProductType = "  feed  "
	w = w.Normalize(farmCatalog())
	if w.Lines[0].ProductType != "Feed" {
		t.Fatalf("must record the registry's spelling, got %q", w.Lines[0].ProductType)
	}
}

// An empty registry is not a licence to guess. The service refuses; the catalog says so plainly.
func TestAnEmptyRegistrySellsNothing(t *testing.T) {
	if !NewProductCatalog(nil).IsEmpty() {
		t.Fatal("a tenant with no products must read as empty, never as the built-ins")
	}
}
