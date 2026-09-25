package domain

import "testing"

// HAS LIVE ANIMALS (maintainer decision 2026-09-25) decides whether a recorded sale's workflow
// carries its tag / loading / gate-pass steps. It is read from the LINES -- an animal-kind line
// with a head count above zero -- never from the deal-level animal_count, because 18 legacy manure
// deals carry animal_count = 1.
func TestHasLiveAnimalsReadsTheAnimalLinesHeadCount(t *testing.T) {
	cat := farmCatalog()
	line := func(product string, count *float64) DealLineWrite {
		return DealLineWrite{ProductType: product, Breed: "Any", AnimalCount: count, Quantity: fp(100), SalesValue: 1000}
	}
	cases := []struct {
		name  string
		lines []DealLineWrite
		want  bool
	}{
		{"manure only", []DealLineWrite{line(ProductManure, nil)}, false},
		{"feed only", []DealLineWrite{line("Feed", nil)}, false},
		{"other item only", []DealLineWrite{line("Hay", nil)}, false},
		{"animal line with blank head count", []DealLineWrite{line(ProductSheep, nil)}, false},
		{"animal line with zero head count", []DealLineWrite{line(ProductGoat, fp(0))}, false},
		{"animal line with a fractional head count below one", []DealLineWrite{line(ProductGoat, fp(0.5))}, false},
		{"manure line that happens to carry a count", []DealLineWrite{line(ProductManure, fp(1))}, false},
		{"animal line with three animals", []DealLineWrite{line(ProductSheep, fp(3))}, true},
		{"manure plus two goats", []DealLineWrite{line(ProductManure, nil), line(ProductGoat, fp(2))}, true},
	}
	for _, c := range cases {
		w := DealWrite{Lines: c.lines}.Normalize(cat)
		if got := w.HasLiveAnimals(); got != c.want {
			t.Errorf("%s: HasLiveAnimals = %v, want %v", c.name, got, c.want)
		}
	}
}
