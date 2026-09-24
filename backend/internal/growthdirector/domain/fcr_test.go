package domain

import (
	"math"
	"strings"
	"testing"
)

func f(v float64) *float64 { return &v }

func near(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: got nil, want %.4f", name, want)
	}
	if math.Abs(*got-want) > 0.0005 {
		t.Fatalf("%s: got %.4f, want %.4f", name, *got, want)
	}
}

// One whole-shed pen weighed three times: two segments. Feed and head-days come from the sheet,
// gain is ADG x head-days. FCR is the SUM over segments, never a mean of segment ratios.
func TestFCRPenSumsSegmentsAndValuesGainAtTheSpeciesPrice(t *testing.T) {
	pens := []FCRPenRow{{
		PenKey: "shed|1", LocationID: "shed", ParkID: "p", ParkName: "Coimbatore", ShedName: "Castro", PartitionLabel: "1",
		Modes: []string{"per_shed_partition"}, Rounds: 3, FirstWeighDate: "2026-08-03", LastWeighDate: "2026-08-17",
		FirstAverageKg: f(18.0), LatestAnimals: 100, Residents: 100, Breeds: 1, Breed: "Anantapur Sheep", Sexes: 1, Sex: "male",
		SpeciesCount: 1, Species: "sheep", ResidentMix: []HeadMix{{Species: "sheep", Animals: 100}}, BoughtResidents: 100,
	}}
	segments := []FCRSegmentRow{
		// 7 days x 100 heads = 700 head-days, 100 g/day -> 70 kg gain; 420 kg feed -> FCR 6
		{PenKey: "shed|1", StartDate: "2026-08-03", EndDate: "2026-08-10", Animals: 100, ADGGPerDay: 100, Mode: "per_shed_partition", FeedKg: f(420), FeedCostINR: f(8400), HeadDays: f(700)},
		// 7 days x 100 heads, 50 g/day -> 35 kg gain; 420 kg feed -> FCR 12
		{PenKey: "shed|1", StartDate: "2026-08-10", EndDate: "2026-08-17", Animals: 100, ADGGPerDay: 50, Mode: "per_shed_partition", FeedKg: f(420), FeedCostINR: f(8400), HeadDays: f(700), BlockedCells: 2},
	}
	prices := SalePrices{Prices: []SalePrice{{Species: "sheep", PricePerKgINR: 425}, {Species: "goat", PricePerKgINR: 450}}}

	got := BuildFCRReport(pens, segments, prices, FCRFilters{})
	if len(got.Pens) != 1 {
		t.Fatalf("pens = %d", len(got.Pens))
	}
	pen := got.Pens[0]
	// Sum over sum: 840 / 105 = 8, NOT the mean of 6 and 12 (= 9).
	near(t, "fcr", pen.FCR, 8)
	near(t, "gain", pen.GainKg, 105)
	near(t, "feed", pen.FeedKg, 840)
	near(t, "adg", pen.ADGGPerDay, 75) // 105 kg over 1400 head-days
	near(t, "gain value", pen.GainValueINR, 105*425)
	near(t, "cost per kg gain", pen.FeedCostPerKgGainINR, 16800.0/105)
	near(t, "pen margin", pen.MarginINR, 105*425-16800)
	near(t, "breed margin", got.ByBreed[0].MarginINR, 105*425-16800)
	if pen.Status != FCRPenOK || pen.BlockedCells != 2 {
		t.Fatalf("status=%s blocked=%d", pen.Status, pen.BlockedCells)
	}
	if pen.Origin != OriginPurchased || pen.Breed != "anantapur sheep" || pen.WeightBand != "15-20" {
		t.Fatalf("cohort: origin=%s breed=%s band=%s", pen.Origin, pen.Breed, pen.WeightBand)
	}
	near(t, "summary fcr", got.Summary.FCR, 8)
	// Feed ₹20/kg, sale ₹425/kg -> break-even 21.25.
	near(t, "break-even", got.Summary.BreakEvenFCR, 21.25)
	near(t, "margin", got.Summary.MarginINR, 105*425-16800)
	if len(got.Weekly) != 2 || got.Weekly[0].WeekStart != "2026-08-10" || got.Weekly[1].WeekStart != "2026-08-17" {
		t.Fatalf("weekly = %+v", got.Weekly)
	}
	near(t, "week 1 fcr", got.Weekly[0].FCR, 6)
	near(t, "week 2 fcr", got.Weekly[1].FCR, 12)
	if got.Summary.PensWithBlockedCells != 1 {
		t.Fatalf("blocked pens = %d", got.Summary.PensWithBlockedCells)
	}
}

// Absence is never zero: a pen weighed once, a pen with rounds but no feed rows, and a pen that
// lost weight each report their own status and stay out of every ratio.
func TestFCRAbsenceStatusesStayOutOfRatios(t *testing.T) {
	pens := []FCRPenRow{
		{PenKey: "once", LocationID: "a", ParkID: "p", ParkName: "P", ShedName: "A", Rounds: 1, LatestAnimals: 10, Residents: 10, Breeds: 1, Breed: "Beetal", Sexes: 1, Sex: "male", SpeciesCount: 1, Species: "goat", WindowFeedKg: f(120)},
		{PenKey: "nofeed", LocationID: "b", ParkID: "p", ParkName: "P", ShedName: "B", Rounds: 2, LatestAnimals: 10, Residents: 10, Breeds: 1, Breed: "Beetal", Sexes: 1, Sex: "male", SpeciesCount: 1, Species: "goat"},
		{PenKey: "shrank", LocationID: "c", ParkID: "p", ParkName: "P", ShedName: "C", Rounds: 2, LatestAnimals: 10, Residents: 10, Breeds: 1, Breed: "Beetal", Sexes: 1, Sex: "male", SpeciesCount: 1, Species: "goat"},
		{PenKey: "ok", LocationID: "d", ParkID: "p", ParkName: "P", ShedName: "D", Rounds: 2, LatestAnimals: 10, Residents: 10, Breeds: 1, Breed: "Beetal", Sexes: 1, Sex: "male", SpeciesCount: 1, Species: "goat"},
	}
	segments := []FCRSegmentRow{
		{PenKey: "nofeed", StartDate: "2026-08-03", EndDate: "2026-08-10", ADGGPerDay: 100, HeadDays: f(70)},
		{PenKey: "shrank", StartDate: "2026-08-03", EndDate: "2026-08-10", ADGGPerDay: -40, FeedKg: f(50), HeadDays: f(70)},
		{PenKey: "ok", StartDate: "2026-08-03", EndDate: "2026-08-10", ADGGPerDay: 100, FeedKg: f(49), HeadDays: f(70)},
	}
	got := BuildFCRReport(pens, segments, SalePrices{}, FCRFilters{})
	byKey := map[string]FCRPen{}
	for _, p := range got.Pens {
		byKey[p.LocationID] = p
	}
	if byKey["a"].Status != FCRPenWeighedOnce || byKey["a"].FCR != nil || byKey["a"].FeedKg == nil {
		t.Fatalf("weighed once: %+v", byKey["a"])
	}
	if byKey["b"].Status != FCRPenNoFeed || byKey["b"].FCR != nil {
		t.Fatalf("no feed: %+v", byKey["b"])
	}
	if byKey["c"].Status != FCRPenNoGain || byKey["c"].FCR != nil || byKey["c"].GainKg == nil || *byKey["c"].GainKg >= 0 {
		t.Fatalf("no gain: %+v", byKey["c"])
	}
	near(t, "ok fcr", byKey["d"].FCR, 7)
	// No price configured: gain has no value, and the strip says so rather than inventing ₹0.
	if byKey["d"].GainValueINR != nil || got.Summary.GainValueINR != nil || got.Summary.BreakEvenFCR != nil {
		t.Fatalf("unpriced gain must stay absent: %+v", got.Summary)
	}
	s := got.Summary
	if s.PensInScope != 4 || s.PensWithFCR != 1 || s.PensWeighedOnce != 1 || s.PensWithoutFeed != 1 || s.PensWithoutGain != 1 {
		t.Fatalf("summary = %+v", s)
	}
	near(t, "summary fcr excludes the shrinking pen", s.FCR, 7)
	// Park cluster then A→Z, never worst first: A, B, C, D in one park.
	if got.Pens[0].LocationID != "a" || got.Pens[3].LocationID != "d" {
		t.Fatalf("order = %v", got.Pens)
	}
}

// All-parks order is the park CODE (CBE before CPT), not the name (Channapatna sorts ahead of
// Coimbatore), and inside a park pens read A→Z with digit runs as numbers, so Part 2 precedes
// Part 10. The by-park group follows the same cluster order.
func TestFCRPensClusterByParkCodeThenReadAToZ(t *testing.T) {
	base := func(key, park, code, parkName, shed, part string) FCRPenRow {
		return FCRPenRow{PenKey: key, LocationID: key, ParkID: park, ParkName: parkName, ParkCode: code, ShedName: shed, PartitionLabel: part,
			Display: parkName + " · " + shed + " - " + part, Rounds: 2, LatestAnimals: 10, Residents: 10, Breeds: 1, Breed: "Beetal", Sexes: 1, Sex: "male", SpeciesCount: 1, Species: "goat"}
	}
	pens := []FCRPenRow{
		base("cpt10", "cpt", "CPT", "Channapatna", "Mandela 1", "Part 10"),
		base("cbe2", "cbe", "CBE", "Coimbatore", "Godel 1", "Part 2"),
		base("cpt2", "cpt", "CPT", "Channapatna", "Mandela 1", "Part 2"),
		base("cbe10", "cbe", "CBE", "Coimbatore", "Godel 1", "Part 10"),
	}
	seg := func(key string, feedKg float64) FCRSegmentRow {
		return FCRSegmentRow{PenKey: key, StartDate: "2026-08-03", EndDate: "2026-08-10", Animals: 10, ADGGPerDay: 100, Mode: "per_shed_partition", FeedKg: f(feedKg), HeadDays: f(70)}
	}
	// The worst FCR sits in CPT so a worst-first sort would surface it: it must not.
	segments := []FCRSegmentRow{seg("cpt10", 900), seg("cbe2", 50), seg("cpt2", 60), seg("cbe10", 70)}
	got := BuildFCRReport(pens, segments, SalePrices{}, FCRFilters{})
	var order []string
	for _, p := range got.Pens {
		order = append(order, p.LocationID)
	}
	if strings.Join(order, ",") != "cbe2,cbe10,cpt2,cpt10" {
		t.Fatalf("pen order = %v", order)
	}
	if len(got.ByPark) != 2 || got.ByPark[0].Label != "Coimbatore" || got.ByPark[1].Label != "Channapatna" {
		t.Fatalf("by park = %+v", got.ByPark)
	}
}

// Agree-or-neither at PEN grain, because feed cannot be split: a mixed-breed pen is one Mixed
// bar, and under a Sex filter a mixed pen is dropped rather than half-counted.
func TestFCRCohortsAreAgreeOrNeitherAndFiltersApplyPerPen(t *testing.T) {
	pens := []FCRPenRow{
		{PenKey: "m", LocationID: "m", ParkID: "p", ParkName: "P", ShedName: "M", Rounds: 2, LatestAnimals: 12, Residents: 12, Breeds: 2, Breed: "Beetal", Sexes: 2, Sex: "male", SpeciesCount: 1, Species: "goat", BoughtResidents: 4, FirstAverageKg: f(31)},
		{PenKey: "g", LocationID: "g", ParkID: "p", ParkName: "P", ShedName: "G", Rounds: 2, LatestAnimals: 8, Residents: 8, Breeds: 1, Breed: "Sirohi", Sexes: 1, Sex: "male", SpeciesCount: 1, Species: "goat", BoughtResidents: 0, FirstAverageKg: f(14.9)},
	}
	segments := []FCRSegmentRow{
		{PenKey: "m", StartDate: "2026-08-03", EndDate: "2026-08-10", ADGGPerDay: 100, FeedKg: f(84), HeadDays: f(84)},
		{PenKey: "g", StartDate: "2026-08-03", EndDate: "2026-08-10", ADGGPerDay: 200, FeedKg: f(56), HeadDays: f(56)},
	}
	got := BuildFCRReport(pens, segments, SalePrices{}, FCRFilters{})
	m := got.Pens[0]
	if m.LocationID != "m" {
		m = got.Pens[1]
	}
	if m.Breed != CohortMixed || m.Sex != CohortMixed || m.Origin != CohortMixed || m.WeightBand != "30-35" {
		t.Fatalf("mixed pen cohort = %+v", m)
	}
	if len(got.ByBreed) != 2 || got.ByBreed[0].Key != CohortMixed || got.ByBreed[1].Key != "sirohi" {
		t.Fatalf("by breed = %+v", got.ByBreed) // worst FCR first: mixed 10, sirohi 5
	}
	if len(got.ByBand) != 2 || got.ByBand[0].Key != "<15" || got.ByBand[1].Key != "30-35" {
		t.Fatalf("by band = %+v", got.ByBand)
	}
	if len(got.ByOrigin) != 2 {
		t.Fatalf("by origin = %+v", got.ByOrigin)
	}

	filtered := BuildFCRReport(pens, segments, SalePrices{}, FCRFilters{Sex: "male"})
	if len(filtered.Pens) != 1 || filtered.Pens[0].LocationID != "g" {
		t.Fatalf("sex filter must drop the mixed pen whole, got %+v", filtered.Pens)
	}
	near(t, "filtered fcr", filtered.Summary.FCR, 5)
	byOrigin := BuildFCRReport(pens, segments, SalePrices{}, FCRFilters{Origin: OriginPurchased})
	if len(byOrigin.Pens) != 0 {
		t.Fatalf("a pen with 4 of 12 bought is claimed by neither origin, got %+v", byOrigin.Pens)
	}
}

func mix(pairs ...any) []HeadMix {
	out := []HeadMix{}
	for i := 0; i < len(pairs); i += 4 {
		out = append(out, HeadMix{Species: pairs[i].(string), ManagementStage: pairs[i+1].(string), Sex: pairs[i+2].(string), Animals: pairs[i+3].(int)})
	}
	return out
}

// A mixed-species pen is valued at the head-weighted price of its residents, never left blank; a
// pen whose resident species has no price stays unvalued.
func TestPenPriceIsHeadWeightedAcrossSpecies(t *testing.T) {
	prices := SalePrices{Prices: []SalePrice{{Species: "goat", PricePerKgINR: 450}, {Species: "sheep", PricePerKgINR: 425}}}
	got, ok := prices.PenPrice(mix("goat", "", "", 3, "sheep", "", "", 14))
	if !ok || math.Abs(got-(3*450+14*425)/17.0) > 0.0001 {
		t.Fatalf("mixed pen price = %v %v", got, ok)
	}
	if got, ok := prices.PenPrice(mix("sheep", "", "", 10)); !ok || got != 425 {
		t.Fatalf("single-species pen must collapse to that price, got %v %v", got, ok)
	}
	if _, ok := (SalePrices{Prices: []SalePrice{{Species: "goat", PricePerKgINR: 450}}}).PenPrice(mix("goat", "", "", 2, "sheep", "", "", 5)); ok {
		t.Fatalf("a pen with an unpriced species must stay unvalued")
	}
	if _, ok := prices.PenPrice(nil); ok {
		t.Fatalf("a pen with no live residents has no price")
	}
}

// Maintainer decision 2026-09-24: every animal is valued at its own (species, stage, sex) price,
// and a combination nobody priced falls back to its species default rather than going blank.
func TestPenPriceValuesEachAnimalAtItsStageAndSex(t *testing.T) {
	prices := SalePrices{Prices: []SalePrice{
		{Species: "goat", PricePerKgINR: 425},
		{Species: "sheep", PricePerKgINR: 400},
		{Species: "goat", ManagementStage: "K3", Sex: "male", PricePerKgINR: 500},
		{Species: "goat", ManagementStage: "K3", Sex: "female", PricePerKgINR: 460},
		{Species: "sheep", ManagementStage: "K3", Sex: "male", PricePerKgINR: 380},
	}}
	// 10 goat K3 males at 500, 5 goat K3 females at 460, 5 goat F2 males at the goat default 425.
	got, ok := prices.PenPrice(mix("goat", "K3", "male", 10, "goat", "K3", "female", 5, "goat", "F2", "male", 5))
	if want := (10*500 + 5*460 + 5*425) / 20.0; !ok || math.Abs(got-want) > 0.0001 {
		t.Fatalf("stage x sex pen price = %v %v, want %v", got, ok, want)
	}
	// The override is per species: a sheep K3 female has none, so it takes the SHEEP default.
	if got, ok := prices.PriceForAnimal("sheep", "K3", "female"); !ok || got != 400 {
		t.Fatalf("sheep K3 female = %v %v, want the sheep default 400", got, ok)
	}
	if got, ok := prices.PriceForAnimal("Sheep", "k3", "MALE"); !ok || got != 380 {
		t.Fatalf("matching must ignore case, got %v %v", got, ok)
	}
	// An animal with no stage or sex on the register is valued at its species default.
	if got, ok := prices.PriceForAnimal("goat", "", "male"); !ok || got != 425 {
		t.Fatalf("stageless goat = %v %v, want 425", got, ok)
	}
	// PriceFor is the DEFAULT only; an override never answers it.
	if got, _ := prices.PriceFor("goat"); got != 425 {
		t.Fatalf("species default = %v, want 425", got)
	}
	// A species with overrides but no default: unpriced animals of it stay unvalued.
	noDefault := SalePrices{Prices: []SalePrice{{Species: "goat", ManagementStage: "K3", Sex: "male", PricePerKgINR: 500}}}
	if _, ok := noDefault.PenPrice(mix("goat", "K3", "male", 2, "goat", "F2", "female", 1)); ok {
		t.Fatalf("an animal with no override and no default must leave the pen unvalued")
	}
}

// A pen emptied after its last round is described by the animals scanned in it, not left unknown.
func TestEmptiedPenFallsBackToTheWeighedCohort(t *testing.T) {
	pens := []FCRPenRow{{
		PenKey: "e", LocationID: "e", ParkID: "p", ParkName: "P", ShedName: "E", Rounds: 2, LatestAnimals: 9,
		Residents: 0, WeighedAnimals: 9, WeighedBreeds: 1, WeighedBreed: "Sirohi", WeighedSexes: 1, WeighedSex: "male",
		WeighedSpeciesN: 1, WeighedSpecies: "goat", WeighedMix: []HeadMix{{Species: "goat", Animals: 9}}, WeighedBought: 9,
	}}
	segments := []FCRSegmentRow{{PenKey: "e", StartDate: "2026-08-03", EndDate: "2026-08-10", ADGGPerDay: 100, FeedKg: f(63), FeedCostINR: f(1260), HeadDays: f(63)}}
	got := BuildFCRReport(pens, segments, SalePrices{Prices: []SalePrice{{Species: "goat", PricePerKgINR: 450}}}, FCRFilters{})
	pen := got.Pens[0]
	if pen.Breed != "sirohi" || pen.Sex != "male" || pen.Species != "goat" || pen.Origin != OriginPurchased {
		t.Fatalf("cohort = %+v", pen)
	}
	near(t, "gain value from the weighed cohort", pen.GainValueINR, 6.3*450)
	// The fallback also feeds the filters: an emptied all-male pen still counts under Male.
	if male := BuildFCRReport(pens, segments, SalePrices{}, FCRFilters{Sex: "male"}); len(male.Pens) != 1 {
		t.Fatalf("emptied pen must pass the sex filter through its weighed cohort")
	}
	// Live residents WIN when present.
	pens[0].Residents, pens[0].Breeds, pens[0].Breed, pens[0].Sexes, pens[0].Sex = 4, 1, "Beetal", 1, "female"
	if got := BuildFCRReport(pens, segments, SalePrices{}, FCRFilters{}); got.Pens[0].Breed != "beetal" {
		t.Fatalf("live residents must win over the weighed cohort, got %s", got.Pens[0].Breed)
	}
}

func TestWeekStartOfIsMonday(t *testing.T) {
	for in, want := range map[string]string{"2026-09-07": "2026-09-07", "2026-09-13": "2026-09-07", "2026-09-06": "2026-08-31"} {
		if got := weekStartOf(in); got != want {
			t.Fatalf("weekStartOf(%s) = %s, want %s", in, got, want)
		}
	}
}

// Maintainer decision 2026-09-24: the drawer lists "only those stages for which weighing done",
// plus any stage already carrying a price (so it can be read and cleared), in the vocabulary's order.
func TestPriceableStagesAreTheWeighedOnesPlusAnyAlreadyPriced(t *testing.T) {
	all := []StageOption{{Code: "K0"}, {Code: "K3"}, {Code: "F2-Male"}, {Code: "ICU"}, {Code: "Warmup"}}
	prices := []SalePrice{
		{Species: "goat", PricePerKgINR: 425},
		{Species: "sheep", ManagementStage: "warmup", Sex: "male", PricePerKgINR: 400},
	}
	got := PriceableStages(all, []string{"F2-Male", "k3"}, prices)
	var codes []string
	for _, s := range got {
		codes = append(codes, s.Code)
	}
	if strings.Join(codes, ",") != "K3,F2-Male,Warmup" {
		t.Fatalf("priceable stages = %v, want K3,F2-Male,Warmup in vocabulary order", codes)
	}
	if got := PriceableStages(all, nil, []SalePrice{{Species: "goat", PricePerKgINR: 425}}); len(got) != 0 {
		t.Fatalf("nothing weighed and nothing priced must list no stage, got %v", got)
	}
}
