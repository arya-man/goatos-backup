package domain

import (
	"errors"
	"testing"
)

// builtinCatalog is the registry every tenant starts with (migration 000402): the exact three
// products that used to be constants in this package. Tests written before the registry existed
// pass it, so they keep asserting what they always asserted.
func builtinCatalog() ProductCatalog {
	return NewProductCatalog([]Product{
		{Code: ProductCodeSheep, Name: ProductSheep, Kind: KindAnimal, Unit: UnitNumber, SpeciesCode: "sheep", SortOrder: 10},
		{Code: ProductCodeGoat, Name: ProductGoat, Kind: KindAnimal, Unit: UnitNumber, SpeciesCode: "goat", SortOrder: 20},
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

// AN ITEM THE FARM ADDS ITSELF SELLS LIKE ANY OTHER (maintainer instruction 2026-09-23). Sheep
// tags are a row somebody types on Sales Config -- no code names them -- and selling them asks
// how many and at what rate, exactly as feed does.
func TestAnItemTheFarmAddedSellsByNumberAtARate(t *testing.T) {
	catalog := NewProductCatalog(append(builtinCatalog().Products(),
		Product{Code: "sheep_tags", Name: "Sheep tags", Kind: KindOther, Unit: UnitNumber, SortOrder: 60},
	))
	w := DealWrite{
		SaleDate: "2026-09-23", Farm: "CBE",
		BuyerName: "Tag Buyer", BuyerVendorID: "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34",
		Lines: []DealLineWrite{{ProductType: "Sheep tags", Breed: "Sheep tags", Quantity: f(200), RatePerUnit: f(12)}},
	}.Normalize(catalog)
	if err := w.Validate(catalog); err != nil {
		t.Fatalf("selling 200 tags must record: %v", err)
	}
	if w.Lines[0].SalesValue != 2400 {
		t.Fatalf("200 tags at 12 must be 2400, got %v", w.Lines[0].SalesValue)
	}
	// It takes nothing off the feed store: only feed does that.
	if w.Lines[0].Kind() == KindFeed {
		t.Fatal("an item the farm added is not feed and must not draw on the feed store")
	}
	// And it has no animals, whatever a client sends.
	missing := w
	missing.Lines = []DealLineWrite{{ProductType: "Sheep tags", Breed: "Sheep tags", Quantity: f(200)}}
	missing = missing.Normalize(catalog)
	var v ErrDealValidation
	if err := missing.Validate(catalog); !errors.As(err, &v) || v.Field != "lines[1].rate_per_unit" {
		t.Fatalf("a counted item with no rate must be refused on the rate field, got %#v", err)
	}
}

// ANIMALS ARE THE EXCEPTION, ON PURPOSE (maintainer decision 2026-09-23): a lot of goats is
// haggled as a lot, not at a fixed rate per head, so an animal line keeps its head count and its
// negotiated lump value and is NOT asked for a rate.
func TestAnAnimalLineIsStillPricedAsALot(t *testing.T) {
	w := DealWrite{
		SaleDate: "2026-09-23", Farm: "CBE",
		BuyerName: "Irshad Bhai", BuyerVendorID: "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34",
		Lines: []DealLineWrite{{ProductType: "Goat", Breed: "Sojat", AnimalCount: f(12), SalesValue: 96000}},
	}.Normalize(builtinCatalog())
	if err := w.Validate(builtinCatalog()); err != nil {
		t.Fatalf("an animal lot must record without a rate: %v", err)
	}
	if w.Lines[0].SalesValue != 96000 {
		t.Fatalf("the negotiated value must survive untouched, got %v", w.Lines[0].SalesValue)
	}
}

// The code is derived ONCE from the name, so a later rename cannot move yesterday's sales out of
// their card.
func TestAnItemCodeIsDerivedOnceAndSurvivesARename(t *testing.T) {
	for name, want := range map[string]string{
		"Sheep tags":     "sheep_tags",
		"  Ear  Tags  ":  "ear_tags",
		"Manure (dry)":   "manure_dry",
		"2026 calendars": "item_2026_calendars",
		"---":            "",
	} {
		if got := ProductCodeFromName(name); got != want {
			t.Fatalf("code for %q = %q, want %q", name, got, want)
		}
	}
	// Adding derives a code; editing keeps the one the row already has.
	added := ProductWrite{Name: "Sheep tags", Kind: KindOther, Unit: UnitNumber}.Normalize()
	if added.Code != "sheep_tags" {
		t.Fatalf("a new item derives its code, got %q", added.Code)
	}
	renamed := ProductWrite{Code: "sheep_tags", Name: "Ear tags for sheep", Kind: KindOther, Unit: UnitNumber}.Normalize()
	if renamed.Code != "sheep_tags" {
		t.Fatalf("a rename must keep the original code, got %q", renamed.Code)
	}
}

// A name with no letter or number in it derives no code, and is refused where a person can see it
// rather than at a database constraint.
func TestAnUnnameableItemIsRefusedOnItsName(t *testing.T) {
	err := ProductWrite{Name: "---", Kind: KindOther, Unit: UnitNumber}.Normalize().Validate()
	var v ErrProductValidation
	if !errors.As(err, &v) || v.Field != "name" {
		t.Fatalf("must be refused on the name, got %#v", err)
	}
}

// A species belongs to an animal item alone; the schema refuses it elsewhere, so it is dropped
// before it can reach that constraint.
func TestOnlyAnAnimalItemCarriesASpecies(t *testing.T) {
	w := ProductWrite{Name: "Sheep tags", Kind: KindOther, Unit: UnitNumber, SpeciesCode: "sheep"}.Normalize()
	if w.SpeciesCode != "" {
		t.Fatalf("a non-animal item must carry no species, got %q", w.SpeciesCode)
	}
	a := ProductWrite{Name: "Buffalo", Kind: KindAnimal, Unit: UnitNumber, SpeciesCode: "buffalo"}.Normalize()
	if a.SpeciesCode != "buffalo" {
		t.Fatalf("an animal item keeps its species, got %q", a.SpeciesCode)
	}
}

// ADDING AN ITEM MUST NEVER BE AN EDIT OF ANOTHER ONE. The code is derived from the name, so a
// new item called "Feed" derives the same code as an existing Feed -- and the repository's upsert,
// which is right for an edit, would rewrite that row instead. The browser run on 2026-09-23 did
// exactly this and flipped the farm's real feed item to something else; the flag below is what
// lets the write path tell the two apart.
func TestAddingIsDistinguishableFromEditing(t *testing.T) {
	adding := ProductWrite{Name: "Feed", Kind: KindOther, Unit: UnitKg}.Normalize()
	if !adding.Adding || adding.Code != "feed" {
		t.Fatalf("a write with no code is an ADD deriving its code, got adding=%v code=%q", adding.Adding, adding.Code)
	}
	editing := ProductWrite{Code: "feed", Name: "Feed pellets", Kind: KindFeed, Unit: UnitKg}.Normalize()
	if editing.Adding || editing.Code != "feed" {
		t.Fatalf("a write carrying a code is an EDIT of that row, got adding=%v code=%q", editing.Adding, editing.Code)
	}
}

// Feed's stock is kept in kilograms, so the item is refused where it is authored rather than at
// the sale that would spend the wrong unit.
func TestFeedItemMustBeSoldByTheKilogram(t *testing.T) {
	w := ProductWrite{Name: "Bagged feed", Code: "bagged_feed", Kind: KindFeed, Unit: UnitNumber, Status: StatusActive}
	var pe ErrProductValidation
	if err := w.Validate(); !errors.As(err, &pe) || pe.Field != "unit" {
		t.Fatalf("error = %v, want the unit refused", err)
	}
	w.Unit = UnitKg
	if err := w.Validate(); err != nil {
		t.Fatalf("feed by the kilogram was refused: %v", err)
	}
}

func TestQueuedSaleResolvesPreviousProductName(t *testing.T) {
	rows := farmCatalog().Products()
	for i := range rows {
		if rows[i].Code == "feed" {
			rows[i].Name = "Feed from store"
			rows[i].Aliases = []string{"feed"}
		}
	}
	catalog := NewProductCatalog(rows)
	write := feedSale().Normalize(catalog)
	if err := write.Validate(catalog); err != nil {
		t.Fatal(err)
	}
	code, kind, unit := write.Lines[0].ResolvedProduct()
	if code != "feed" || kind != KindFeed || unit != UnitKg || write.Lines[0].ProductType != "Feed from store" {
		t.Fatalf("queued name changed identity: %+v", write.Lines[0])
	}
	if len(catalog.Products()) != len(rows) {
		t.Fatal("aliases must not appear as separate products")
	}
}

func TestProductNameReservesMixedRollupForAddsAndRenames(t *testing.T) {
	for _, code := range []string{"", "feed"} {
		for _, name := range []string{"Mixed", " mixed ", "MIXED"} {
			w := (ProductWrite{Code: code, Name: name, Kind: KindOther, Unit: UnitKg}).Normalize()
			var bad ErrProductValidation
			if err := w.Validate(); !errors.As(err, &bad) || bad.Field != "name" {
				t.Fatalf("%q / %q: %v", code, name, err)
			}
		}
	}
	if err := (ProductWrite{Name: "Mixed feed", Kind: KindFeed, Unit: UnitKg}).Normalize().Validate(); err != nil {
		t.Fatal(err)
	}
}
