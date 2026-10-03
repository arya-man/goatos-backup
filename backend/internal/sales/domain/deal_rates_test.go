package domain

import "testing"

func TestDealRatesDivideTheLiveLinesOnly(t *testing.T) {
	// The 29/09 Pappi sale: 2 goats + 5 sheep, 264.2 kg, ₹1,17,000, plus a manure line that must
	// move none of the three figures.
	d := Deal{Lines: []DealLine{
		{ProductType: "Goat", AnimalCount: f(2), TotalWeightKg: f(80), SalesValue: 36000},
		{ProductType: "Sheep", AnimalCount: f(5), TotalWeightKg: f(184.2), SalesValue: 81000},
		{ProductType: "Manure", Breed: "Manure", TotalWeightKg: f(500), SalesValue: 5000},
	}}
	r := d.Rates()
	if r.PricePerKg == nil || int(*r.PricePerKg) != 442 { // 117000 / 264.2 = 442.85
		t.Fatalf("price per kg = %v, want 442.85", r.PricePerKg)
	}
	if r.WeightPerAnimal == nil || int(*r.WeightPerAnimal*10) != 377 { // 264.2 / 7
		t.Fatalf("weight per animal = %v, want 37.7", r.WeightPerAnimal)
	}
	if r.PricePerAnimal == nil || int(*r.PricePerAnimal) != 16714 { // 117000 / 7
		t.Fatalf("price per animal = %v, want 16714", r.PricePerAnimal)
	}
}

func TestDealRatesAreAbsentWhenNothingDivides(t *testing.T) {
	manure := Deal{ProductType: "Manure", Breed: "Manure", SalesValue: 54520}
	if r := manure.Rates(); r.PricePerKg != nil || r.WeightPerAnimal != nil || r.PricePerAnimal != nil {
		t.Fatalf("a manure sale has no live rate, got %+v", r)
	}
	// Counted but never weighed: a price per animal, and no price per kg.
	counted := Deal{ProductType: "Sheep", AnimalCount: f(4), SalesValue: 40000}
	r := counted.Rates()
	if r.PricePerKg != nil || r.WeightPerAnimal != nil || r.PricePerAnimal == nil || *r.PricePerAnimal != 10000 {
		t.Fatalf("unweighed sale rates = %+v", r)
	}
}
