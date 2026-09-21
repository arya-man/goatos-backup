package domain

import (
	"reflect"
	"testing"
)

func kgPtr(v float64) *float64 { return &v }

// bandOf pulls one band out of the result so a test can name the band it means.
func bandOf(t *testing.T, b SoldWeightBands, key string) SoldWeightBand {
	t.Helper()
	for _, got := range b.Bands {
		if got.Band == key {
			return got
		}
	}
	t.Fatalf("band %q missing from %+v", key, b.Bands)
	return SoldWeightBand{}
}

// liveDeal builds a single-line live deal of n animals with the given recorded weight.
func liveDeal(id string, animals float64, weightKg *float64) Deal {
	return Deal{
		DealID: id,
		Lines: []DealLine{{
			LineNo: 1, ProductType: ProductGoat, Breed: "Malai",
			AnimalCount: &animals, TotalWeightKg: weightKg, SalesValue: 1,
		}},
	}
}

// TestSoldWeightBandFilesEachWeightOnTheMaintainersEdges pins the 2026-09-08 edges: 40+, 35-40,
// 20-35, below 20, each edge belonging to the band ABOVE it.
func TestSoldWeightBandFilesEachWeightOnTheMaintainersEdges(t *testing.T) {
	for _, tc := range []struct {
		kg   float64
		band string
	}{
		{19.99, SoldBandUnder20},
		{20, SoldBandFrom20To35},
		{34.99, SoldBandFrom20To35},
		{35, SoldBandFrom35To40},
		{39.99, SoldBandFrom35To40},
		{40, SoldBandAtOrAbove40},
		{52, SoldBandAtOrAbove40},
	} {
		if got := SoldWeightBandFor(tc.kg); got != tc.band {
			t.Fatalf("%.2f kg -> %s, want %s", tc.kg, got, tc.band)
		}
	}
}

// TestSoldWeightBandsCountEveryAnimalOfEveryClosedDealExactlyOnce is the whole contract of this
// card in one assertion: the banded TOTAL is the animals sold, the same figure the page's
// headline reports, whichever way each animal's weight was arrived at. A card whose tiles sum to
// less than the headline above it -- which is exactly what this change was raised to fix -- is
// the failure mode being pinned.
func TestSoldWeightBandsCountEveryAnimalOfEveryClosedDealExactlyOnce(t *testing.T) {
	deals := []Deal{
		liveDeal("measured", 3, kgPtr(120)), // all three tagged and weighed
		liveDeal("load", 10, kgPtr(310)),    // none tagged: 31.0 kg each from the load
		liveDeal("estimated-kg", 42, nil),   // migration 000381's sales id 28
		liveDeal("estimated-band", 4, nil),  // migration 000381's sales ids 20-23
		liveDeal("nothing-known", 5, nil),   // no weight of any kind
	}
	deals[2].Lines[0].EstimatedWeightKg = kgPtr(1050)
	deals[2].Lines[0].WeightEstimateBasis = "price-derived"
	deals[3].Lines[0].EstimatedWeightBand = SoldBandUnder20
	deals[3].Lines[0].WeightEstimateBasis = "sold by the head"

	got := BuildSoldWeightBands(deals, map[string][]float64{
		"measured": {19, 37, 41},
	})

	if got.Total != 3+10+42+4+5 {
		t.Fatalf("total = %d, want every animal of every deal (64)", got.Total)
	}
	sum := got.Unweighed
	for _, b := range got.Bands {
		sum += b.Total
		if b.Measured+b.LoadAverage+b.Estimated != b.Total {
			t.Fatalf("band %s: sources %d+%d+%d do not make its total %d",
				b.Band, b.Measured, b.LoadAverage, b.Estimated, b.Total)
		}
	}
	if sum != got.Total {
		t.Fatalf("bands plus unweighed = %d, total = %d", sum, got.Total)
	}
	if got.Measured != 3 || got.LoadAverage != 10 || got.Estimated != 46 || got.Unweighed != 5 {
		t.Fatalf("provenance split = measured %d / load %d / estimated %d / unweighed %d",
			got.Measured, got.LoadAverage, got.Estimated, got.Unweighed)
	}
	// 19 kg measured + the four band-only animals.
	if b := bandOf(t, got, SoldBandUnder20); b.Total != 5 || b.Measured != 1 || b.Estimated != 4 {
		t.Fatalf("below 20 = %+v", b)
	}
	// 31.0 kg load average x10, and the 42 goats estimated at 25.0 kg each.
	if b := bandOf(t, got, SoldBandFrom20To35); b.Total != 52 || b.LoadAverage != 10 || b.Estimated != 42 {
		t.Fatalf("20-35 = %+v", b)
	}
	if b := bandOf(t, got, SoldBandFrom35To40); b.Total != 1 || b.Measured != 1 {
		t.Fatalf("35-40 = %+v", b)
	}
	if b := bandOf(t, got, SoldBandAtOrAbove40); b.Total != 1 || b.Measured != 1 {
		t.Fatalf("40+ = %+v", b)
	}
}

// TestSoldWeightBandsPreferMeasuredAndSpreadOnlyWhatIsLeft: a PARTLY tagged deal must not count
// its weighed animals twice, and the animals it spreads the load over must be only the ones
// nobody put on a scale -- at the weight that is actually left after the measured ones are taken
// out of the load total, never at the whole load's average.
func TestSoldWeightBandsPreferMeasuredAndSpreadOnlyWhatIsLeft(t *testing.T) {
	// 5 animals, 200 kg recorded. Two were weighed at 45 kg each (90 kg), so the other three
	// share the remaining 110 kg -- 36.7 kg each, which is 35-40. The whole-load average would
	// have been 40 kg and put them in the band above.
	got := BuildSoldWeightBands(
		[]Deal{liveDeal("partly", 5, kgPtr(200))},
		map[string][]float64{"partly": {45, 45}},
	)
	if got.Total != 5 || got.Measured != 2 || got.LoadAverage != 3 {
		t.Fatalf("counts = %+v", got)
	}
	if b := bandOf(t, got, SoldBandAtOrAbove40); b.Total != 2 || b.Measured != 2 {
		t.Fatalf("the two weighed animals belong in 40+, got %+v", b)
	}
	if b := bandOf(t, got, SoldBandFrom35To40); b.Total != 3 || b.LoadAverage != 3 {
		t.Fatalf("the remaining three belong at the LEFTOVER average, got %+v", b)
	}
}

// TestSoldWeightBandsFallBackToTheLineAverageWhenTheLeftoverIsImpossible: when the animals
// already weighed account for more than the whole recorded load, the two figures disagree and
// the leftover is zero or negative. Banding on a negative weight would file real animals under
// 20 kg; the plain line average is the honest fallback.
func TestSoldWeightBandsFallBackToTheLineAverageWhenTheLeftoverIsImpossible(t *testing.T) {
	got := BuildSoldWeightBands(
		[]Deal{liveDeal("contradiction", 4, kgPtr(80))},
		map[string][]float64{"contradiction": {45, 45}},
	)
	// 80 kg for four, minus 90 kg already weighed, is impossible; fall back to 80/4 = 20 kg.
	if b := bandOf(t, got, SoldBandFrom20To35); b.Total != 2 || b.LoadAverage != 2 {
		t.Fatalf("the unweighed two belong at the line average of 20 kg, got %+v", b)
	}
	if got.Total != 4 {
		t.Fatalf("total = %d, want 4", got.Total)
	}
}

// TestSoldWeightBandsNeverCountManureAndNeverDropAMixedDealsAnimals: a mixed sale's manure line
// is weight and money, never animals, while its live lines are banded at their own weights. The
// deal's tagged animals fill the live lines in line order.
func TestSoldWeightBandsNeverCountManureAndNeverDropAMixedDealsAnimals(t *testing.T) {
	two, three, one := 2.0, 3.0, 1.0
	deal := Deal{DealID: "mixed", Lines: []DealLine{
		{LineNo: 1, ProductType: ProductSheep, Breed: "Anantapur", AnimalCount: &two, TotalWeightKg: kgPtr(84)},
		{LineNo: 2, ProductType: ProductManure, Breed: "Manure", AnimalCount: &one, TotalWeightKg: kgPtr(1340)},
		{LineNo: 3, ProductType: ProductGoat, Breed: "Malai", AnimalCount: &three, TotalWeightKg: kgPtr(75)},
	}}
	got := BuildSoldWeightBands([]Deal{deal}, nil)
	if got.Total != 5 {
		t.Fatalf("total = %d, want the 5 animals and none of the manure", got.Total)
	}
	if b := bandOf(t, got, SoldBandAtOrAbove40); b.Total != 2 {
		t.Fatalf("the two sheep at 42 kg belong in 40+, got %+v", b)
	}
	if b := bandOf(t, got, SoldBandFrom20To35); b.Total != 3 {
		t.Fatalf("the three goats at 25 kg belong in 20-35, got %+v", b)
	}
	// 1,340 kg of manure over one "animal" would have landed in 40+ and is the mistake this
	// test exists to catch.
	if got.Total != got.Measured+got.LoadAverage+got.Estimated+got.Unweighed {
		t.Fatalf("provenance does not partition the total: %+v", got)
	}
}

// TestSoldWeightBandsAlwaysCarryAllFourBandsHeaviestFirst: the page renders the backend's order
// verbatim and shows every band including an empty one, so the contract must always carry four
// in that order even for an empty register.
func TestSoldWeightBandsAlwaysCarryAllFourBandsHeaviestFirst(t *testing.T) {
	want := []string{SoldBandAtOrAbove40, SoldBandFrom35To40, SoldBandFrom20To35, SoldBandUnder20}
	for _, b := range []SoldWeightBands{
		BuildSoldWeightBands(nil, nil),
		BuildSoldWeightBands([]Deal{liveDeal("one", 1, kgPtr(30))}, nil),
	} {
		got := make([]string, 0, len(b.Bands))
		for _, band := range b.Bands {
			got = append(got, band.Band)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("band order = %v, want %v", got, want)
		}
	}
}

// TestSoldWeightBandsIgnoreAnEstimateWhereAWeightWasRecorded: the estimate is a fallback, never
// an override. A line the desk actually weighed is read from that weight even if an estimate is
// sitting beside it -- which is what keeps migration 000381's assumption from quietly displacing
// a real recording if one is added later.
func TestSoldWeightBandsIgnoreAnEstimateWhereAWeightWasRecorded(t *testing.T) {
	deal := liveDeal("both", 2, kgPtr(90))
	deal.Lines[0].EstimatedWeightKg = kgPtr(20)
	deal.Lines[0].WeightEstimateBasis = "stale assumption"
	got := BuildSoldWeightBands([]Deal{deal}, nil)
	if b := bandOf(t, got, SoldBandAtOrAbove40); b.Total != 2 || b.LoadAverage != 2 {
		t.Fatalf("the recorded 45 kg each must win over the estimate, got %+v", got.Bands)
	}
	if got.Estimated != 0 {
		t.Fatalf("estimated = %d, want none: the line was weighed", got.Estimated)
	}
}

// TestSoldWeightBandsRejectAnUnknownStoredBand: a band string the code does not know is DROPPED
// to unweighed rather than rendered, because the page has no label for it and a tile with no
// label is worse than an honest gap.
func TestSoldWeightBandsRejectAnUnknownStoredBand(t *testing.T) {
	deal := liveDeal("odd", 3, nil)
	deal.Lines[0].EstimatedWeightBand = "from_60_to_70"
	deal.Lines[0].WeightEstimateBasis = "a band nobody declared"
	got := BuildSoldWeightBands([]Deal{deal}, nil)
	if got.Unweighed != 3 || got.Estimated != 0 || got.Total != 3 {
		t.Fatalf("an unknown band must fall through to unweighed, got %+v", got)
	}
}

// TestSoldWeightBandsBandEveryTaggedAnimalEvenBeyondTheRecordedCount: a deal carrying more
// tagged animals than its recorded animal_count still weighed them. Counting the register's
// number and dropping the surplus would hide an animal that was demonstrably put on a scale.
func TestSoldWeightBandsBandEveryTaggedAnimalEvenBeyondTheRecordedCount(t *testing.T) {
	got := BuildSoldWeightBands(
		[]Deal{liveDeal("undercounted", 2, nil)},
		map[string][]float64{"undercounted": {21, 22, 23}},
	)
	if got.Total != 3 || got.Measured != 3 || got.Unweighed != 0 {
		t.Fatalf("all three weighed animals must be counted, got %+v", got)
	}
}

// TestSoldWeightBandsAreIndependentOfDealOrder: the bands are a WHOLE-REGISTER fact, so reading
// the deals in any order yields the same counts. This is what lets the read be a single
// unordered whole-filter query rather than anything page-shaped.
func TestSoldWeightBandsAreIndependentOfDealOrder(t *testing.T) {
	deals := []Deal{
		liveDeal("a", 3, kgPtr(120)),
		liveDeal("b", 7, kgPtr(140)),
		liveDeal("c", 2, nil),
		liveDeal("d", 5, kgPtr(210)),
	}
	measured := map[string][]float64{"a": {19, 37, 41}}
	forward := BuildSoldWeightBands(deals, measured)
	reversed := BuildSoldWeightBands([]Deal{deals[3], deals[2], deals[1], deals[0]}, measured)
	if !reflect.DeepEqual(forward, reversed) {
		t.Fatalf("deal order changed the bands:\n%+v\n%+v", forward, reversed)
	}
}

// TestSoldWeightBandsOneToManyAllocationsCountEachAnimalOnce: a deal with MANY tagged animals is
// many sold animals, each banded once at its own weight -- never once per deal, and never
// multiplied by anything else hanging off the deal (its lines, its payments, its evidence rows).
// The 1:N side is the allocations, and this pins that the fold walks them individually while the
// deal is visited once.
func TestSoldWeightBandsOneToManyAllocationsCountEachAnimalOnce(t *testing.T) {
	got := BuildSoldWeightBands(
		[]Deal{liveDeal("one-deal", 5, kgPtr(160))},
		map[string][]float64{"one-deal": {18, 22, 36, 44, 44}},
	)
	if got.Total != 5 || got.Measured != 5 || got.LoadAverage != 0 {
		t.Fatalf("five tagged animals on one deal must be five measured animals, got %+v", got)
	}
	if b := bandOf(t, got, SoldBandUnder20); b.Total != 1 {
		t.Fatalf("below 20 = %+v", b)
	}
	if b := bandOf(t, got, SoldBandFrom20To35); b.Total != 1 {
		t.Fatalf("20-35 = %+v", b)
	}
	if b := bandOf(t, got, SoldBandFrom35To40); b.Total != 1 {
		t.Fatalf("35-40 = %+v", b)
	}
	if b := bandOf(t, got, SoldBandAtOrAbove40); b.Total != 2 {
		t.Fatalf("40+ = %+v", b)
	}
}

// TestSoldWeightBandsPageBoundaryIsMeaninglessHere: the bands are a WHOLE-REGISTER fact. Folding
// the same deals in any chunking gives the same counts as folding them at once, which is what
// makes it safe for the read to be one unordered whole-filter query -- and what makes a
// page-local rollup of this card wrong by construction, since a page could never see all of it.
func TestSoldWeightBandsPageBoundaryIsMeaninglessHere(t *testing.T) {
	deals := []Deal{
		liveDeal("p1", 3, kgPtr(120)),
		liveDeal("p2", 7, kgPtr(140)),
		liveDeal("p3", 2, nil),
		liveDeal("p4", 5, kgPtr(210)),
		liveDeal("p5", 11, kgPtr(418)),
	}
	measured := map[string][]float64{"p1": {19, 37, 41}, "p4": {45}}
	whole := BuildSoldWeightBands(deals, measured)

	merged := SoldWeightBands{}
	for _, chunk := range [][]Deal{deals[:2], deals[2:3], deals[3:]} {
		part := BuildSoldWeightBands(chunk, measured)
		if merged.Bands == nil {
			merged = part
			continue
		}
		merged.Total += part.Total
		merged.Measured += part.Measured
		merged.LoadAverage += part.LoadAverage
		merged.Estimated += part.Estimated
		merged.Unweighed += part.Unweighed
		for i := range merged.Bands {
			merged.Bands[i].Total += part.Bands[i].Total
			merged.Bands[i].Measured += part.Bands[i].Measured
			merged.Bands[i].LoadAverage += part.Bands[i].LoadAverage
			merged.Bands[i].Estimated += part.Bands[i].Estimated
		}
	}
	if !reflect.DeepEqual(whole, merged) {
		t.Fatalf("chunking changed the bands:\n whole  %+v\n merged %+v", whole, merged)
	}
	if whole.Total != 28 {
		t.Fatalf("total = %d, want 28", whole.Total)
	}
}

// TestSoldWeightBandsParkScopeIsTheCallersAndNeverInvented: the fold has no farm or park of its
// own -- it bands exactly the deals it is handed, which is how the page's farm toggle narrows
// this card by narrowing ONE read rather than by filtering anything here. Banding a CBE-only
// slice must give exactly the CBE half of the whole, and the two halves must sum to it.
func TestSoldWeightBandsParkScopeIsTheCallersAndNeverInvented(t *testing.T) {
	cbe := []Deal{liveDeal("cbe-1", 4, kgPtr(160)), liveDeal("cbe-2", 6, kgPtr(150))}
	cpt := []Deal{liveDeal("cpt-1", 3, kgPtr(60)), liveDeal("cpt-2", 2, nil)}
	measured := map[string][]float64{"cbe-1": {41, 42, 43, 44}}

	all := BuildSoldWeightBands(append(append([]Deal{}, cbe...), cpt...), measured)
	justCBE := BuildSoldWeightBands(cbe, measured)
	justCPT := BuildSoldWeightBands(cpt, measured)

	if all.Total != justCBE.Total+justCPT.Total {
		t.Fatalf("the two farm slices (%d + %d) must sum to the whole (%d)",
			justCBE.Total, justCPT.Total, all.Total)
	}
	for i := range all.Bands {
		if all.Bands[i].Total != justCBE.Bands[i].Total+justCPT.Bands[i].Total {
			t.Fatalf("band %s does not split across the farms: %d vs %d + %d", all.Bands[i].Band,
				all.Bands[i].Total, justCBE.Bands[i].Total, justCPT.Bands[i].Total)
		}
	}
	// A CPT deal's animals must never be claimed by a CBE measured weight: the allocations are
	// keyed by deal, so handing the same map to a narrower deal set simply leaves them unread.
	if justCPT.Measured != 0 {
		t.Fatalf("CPT has no tagged animals; measured = %d", justCPT.Measured)
	}
}

// TestSoldWeightBandsStatusBucketsAreExhaustiveAndDisjoint: every sold animal lands in EXACTLY
// one of the five buckets (four bands + unweighed), whichever evidence claimed it, so the
// buckets always sum to the total and no animal is double-filed or dropped. The deal-STATUS half
// of the matrix -- only closed deals, and only `tagged` allocations, never a `released` one --
// is enforced in the caller's WHERE and named in its projection-review note; this pins the
// bucket half over a spread of every shape the fold can meet.
func TestSoldWeightBandsStatusBucketsAreExhaustiveAndDisjoint(t *testing.T) {
	deals := make([]Deal, 0, 60)
	measured := map[string][]float64{}
	for i := 0; i < 60; i++ {
		id := string(rune('a'+i%26)) + string(rune('A'+i/26))
		animals := float64(i%7 + 1)
		switch i % 4 {
		case 0: // a weighed load
			deals = append(deals, liveDeal(id, animals, kgPtr(float64(i%60)+12)))
		case 1: // tagged and weighed one by one
			d := liveDeal(id, animals, nil)
			w := make([]float64, 0, int(animals))
			for j := 0; j < int(animals); j++ {
				w = append(w, float64((i+j)%60)+0.5)
			}
			measured[id] = w
			deals = append(deals, d)
		case 2: // an estimate, in kilograms or as a band
			d := liveDeal(id, animals, nil)
			if i%8 == 2 {
				d.Lines[0].EstimatedWeightKg = kgPtr(animals * 25)
			} else {
				d.Lines[0].EstimatedWeightBand = SoldBandUnder20
			}
			d.Lines[0].WeightEstimateBasis = "recorded assumption"
			deals = append(deals, d)
		default: // nothing known at all
			deals = append(deals, liveDeal(id, animals, nil))
		}
	}

	got := BuildSoldWeightBands(deals, measured)
	want := 0
	for _, d := range deals {
		want += int(d.Lines[0].Animals())
	}
	buckets := got.Unweighed
	for _, b := range got.Bands {
		buckets += b.Total
	}
	if buckets != got.Total || got.Total != want {
		t.Fatalf("buckets %d, total %d, animals sold %d -- all three must agree", buckets, got.Total, want)
	}
	if got.Measured+got.LoadAverage+got.Estimated+got.Unweighed != got.Total {
		t.Fatalf("provenance does not partition the total: %+v", got)
	}
	if got.Measured == 0 || got.LoadAverage == 0 || got.Estimated == 0 || got.Unweighed == 0 {
		t.Fatalf("the spread must exercise all four outcomes, got %+v", got)
	}
}
