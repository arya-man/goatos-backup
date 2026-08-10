package domain

import (
	"math"
	"testing"
)

func ptrF(v float64) *float64 { return &v }
func ptrS(v string) *string   { return &v }

// pen builds a fully-resolved comparable pen so each test can vary ONE thing and
// have the failure name that thing.
func pen(name, breed, stage string, adg float64, basis string) Pen {
	return Pen{
		LocationID:                 name,
		ShedName:                   name,
		OperationalLocationDisplay: name,
		Breed:                      ptrS(breed),
		Stage:                      ptrS(stage),
		ADGGPerDay:                 ptrF(adg),
		ADGBasis:                   basis,
		FeedPlanStatus:             FeedPlanResolved,
		PlannedFeedGPerHeadDay:     ptrF(1000),
	}
}

// The headline case the screen exists for: same breed, same stage, same way of
// weighing, wildly different gain. The laggard must be reported as behind.
func TestLikeForLikePensAreRankedAgainstTheirPeerMedian(t *testing.T) {
	pens := []Pen{
		pen("Castro 2", "Sirohi", "Grower", 400, ADGBasisPerAnimalMedian),
		pen("Mandela 1", "Sirohi", "Grower", 200, ADGBasisPerAnimalMedian),
		pen("Godel 1", "Sirohi", "Grower", 300, ADGBasisPerAnimalMedian),
	}
	_, _, _, comparable := Benchmark(pens)

	if comparable != 3 {
		t.Fatalf("comparable = %d, want 3", comparable)
	}
	for _, p := range pens {
		if p.PeerMedianADGGPerDay == nil || *p.PeerMedianADGGPerDay != 300 {
			t.Fatalf("%s peer median = %v, want 300", p.ShedName, p.PeerMedianADGGPerDay)
		}
		if p.PeerPenCount != 3 {
			t.Fatalf("%s peer count = %d, want 3", p.ShedName, p.PeerPenCount)
		}
	}
	// 200 against a median of 300 is a third off the pace — the number that sends
	// somebody to that shed tomorrow morning.
	if got := *pens[1].ADGVsPeerPct; math.Abs(got-(-33.333333)) > 0.001 {
		t.Fatalf("laggard vs peers = %.4f%%, want -33.3333%%", got)
	}
	if got := *pens[0].ADGVsPeerPct; math.Abs(got-33.333333) > 0.001 {
		t.Fatalf("leader vs peers = %.4f%%, want +33.3333%%", got)
	}
}

// RULE 1. A per-animal median and a shed-average movement are different
// measurements. If basis dropped out of the peer key these four pens would form
// one group and each pen would be ranked against a number it was never measured
// the same way as.
func TestPensWeighedDifferentlyAreNeverPeers(t *testing.T) {
	pens := []Pen{
		pen("A", "Sirohi", "Grower", 400, ADGBasisPerAnimalMedian),
		pen("B", "Sirohi", "Grower", 380, ADGBasisPerAnimalMedian),
		pen("C", "Sirohi", "Grower", 90, ADGBasisShedAverageMovement),
		pen("D", "Sirohi", "Grower", 110, ADGBasisShedAverageMovement),
	}
	Benchmark(pens)

	if pens[0].PeerGroupKey == pens[2].PeerGroupKey {
		t.Fatalf("per-animal and shed-average pens share peer key %q", pens[0].PeerGroupKey)
	}
	if *pens[0].PeerMedianADGGPerDay != 390 {
		t.Fatalf("per-animal median = %v, want 390", *pens[0].PeerMedianADGGPerDay)
	}
	if *pens[2].PeerMedianADGGPerDay != 100 {
		t.Fatalf("shed-average median = %v, want 100", *pens[2].PeerMedianADGGPerDay)
	}
	// The specific harm being prevented: pooled, the median would be 245 and the
	// perfectly healthy shed-average pens would each read ~60% behind.
	if pens[2].ADGVsPeerPct == nil || math.Abs(*pens[2].ADGVsPeerPct-(-10)) > 0.001 {
		t.Fatalf("shed-average pen vs peers = %v, want -10%%", pens[2].ADGVsPeerPct)
	}
}

// RULE 2. A pen with NO authored ration has no conversion ratio. Its planned total
// would be 0, and 0 grams over a real gain is an infinitely efficient pen that
// sorts straight to the top of the ranking.
func TestPenWithNoAuthoredRationProducesNoConversionRatio(t *testing.T) {
	full := pen("full", "Sirohi", "Grower", 400, ADGBasisPerAnimalMedian)
	unconfigured := pen("unconfigured", "Sirohi", "Grower", 400, ADGBasisPerAnimalMedian)
	unconfigured.FeedPlanStatus = FeedPlanNoConfig
	unconfigured.PlannedFeedGPerHeadDay = nil
	// Same gain on both, so the ONLY difference is whether a ration exists.
	pens := []Pen{full, unconfigured}
	Benchmark(pens)

	if pens[0].FeedPerKgGainKg == nil {
		t.Fatal("fully resolved ration produced no conversion ratio")
	}
	if got := *pens[0].FeedPerKgGainKg; math.Abs(got-2.5) > 1e-9 {
		t.Fatalf("conversion = %v, want 2.5 kg feed per kg gain", got)
	}
	if pens[1].FeedPerKgGainKg != nil {
		t.Fatalf("unconfigured pen produced ratio %v, want none", *pens[1].FeedPerKgGainKg)
	}
}

// A milk-fed kid pen is authored at 0 g of solid feed and still gains weight. The
// arithmetic ratio is 0.0 kg of feed per kg of gain, which would sort every such
// pen to the top of a "most efficient pens" ranking on the strength of milk this
// grid cannot see. Found on live staging: 0 g planned against 139 g/day of gain.
func TestZeroGramRationProducesNoConversionRatio(t *testing.T) {
	p := pen("K1 kids", "Anantapur Sheep", "K1", 139, ADGBasisPerAnimalMedian)
	p.PlannedFeedGPerHeadDay = ptrF(0)
	pens := []Pen{p}
	Benchmark(pens)

	if pens[0].FeedPerKgGainKg != nil {
		t.Fatalf("a 0 g ration produced conversion %v, want none", *pens[0].FeedPerKgGainKg)
	}
	// The pen is still ranked on GROWTH — only the feed number is withheld.
	if pens[0].PeerGroupKey == "" {
		t.Fatal("a milk-fed pen must still be comparable on gain")
	}
}

// A pen that is flat or shrinking has no conversion ratio. A negative one sorts
// to the top of a "most efficient" ranking; a near-zero denominator produces a
// headline number in the thousands.
func TestFlatOrLosingPenProducesNoConversionRatio(t *testing.T) {
	for _, adg := range []float64{0, -50} {
		p := pen("x", "Sirohi", "Grower", adg, ADGBasisPerAnimalMedian)
		pens := []Pen{p}
		Benchmark(pens)
		if pens[0].FeedPerKgGainKg != nil {
			t.Fatalf("adg %v produced conversion %v, want none", adg, *pens[0].FeedPerKgGainKg)
		}
	}
}

// RULE 3. A group of one has no benchmark. Its median is its own number, which
// renders as exactly 0% off the pace and reads as reassurance.
func TestSinglePenGroupIsNotBenchmarkedAgainstItself(t *testing.T) {
	pens := []Pen{pen("lonely", "Osmanabadi", "Fattening", 250, ADGBasisPerAnimalMedian)}
	_, _, _, comparable := Benchmark(pens)

	if comparable != 0 {
		t.Fatalf("comparable = %d, want 0", comparable)
	}
	if pens[0].PeerMedianADGGPerDay != nil || pens[0].ADGVsPeerPct != nil {
		t.Fatalf("single pen was benchmarked: median=%v pct=%v",
			pens[0].PeerMedianADGGPerDay, pens[0].ADGVsPeerPct)
	}
	// The key still travels, so the screen can say WHY there is no comparison
	// rather than showing a blank cell.
	if pens[0].PeerGroupKey == "" {
		t.Fatal("peer key was dropped; the reader cannot tell why there is no comparison")
	}
}

// An unknown breed or stage must not pool into one giant "unknown" group where
// a Sirohi grower would be ranked against an Osmanabadi kid.
func TestUnresolvedCohortPensAreNeverPooledIntoOneGroup(t *testing.T) {
	a := pen("a", "Sirohi", "Grower", 400, ADGBasisPerAnimalMedian)
	a.Breed = nil
	b := pen("b", "Osmanabadi", "Kid", 100, ADGBasisPerAnimalMedian)
	b.Stage = nil
	pens := []Pen{a, b}
	withoutCohort, _, _, comparable := Benchmark(pens)

	if withoutCohort != 2 {
		t.Fatalf("pens without cohort = %d, want 2", withoutCohort)
	}
	if comparable != 0 {
		t.Fatalf("comparable = %d, want 0", comparable)
	}
	for _, p := range pens {
		if p.PeerGroupKey != "" {
			t.Fatalf("%s got peer key %q, want empty", p.ShedName, p.PeerGroupKey)
		}
		if p.PeerMedianADGGPerDay != nil {
			t.Fatalf("%s was benchmarked despite an unresolved cohort", p.ShedName)
		}
	}
}

// A pen with no second weigh has no gain. It stays in the table — it is a real
// pen and its absence would misrepresent coverage — but it is counted as a gap
// and never ranked.
func TestPenWithNoSecondWeighIsCountedNotDropped(t *testing.T) {
	a := pen("a", "Sirohi", "Grower", 400, ADGBasisPerAnimalMedian)
	b := pen("b", "Sirohi", "Grower", 300, ADGBasisPerAnimalMedian)
	c := pen("c", "Sirohi", "Grower", 0, ADGBasisPerAnimalMedian)
	c.ADGGPerDay = nil
	pens := []Pen{a, b, c}
	_, _, withoutGain, comparable := Benchmark(pens)

	if withoutGain != 1 {
		t.Fatalf("pens without gain = %d, want 1", withoutGain)
	}
	if comparable != 2 {
		t.Fatalf("comparable = %d, want 2 (the ungained pen must not be ranked)", comparable)
	}
	// It must also not have joined the group and pulled the median toward zero.
	if *pens[0].PeerMedianADGGPerDay != 350 {
		t.Fatalf("peer median = %v, want 350", *pens[0].PeerMedianADGGPerDay)
	}
	if pens[2].ADGVsPeerPct != nil {
		t.Fatal("a pen with no gain was given a percentage against its peers")
	}
}

// A group whose median gain is negative (a shrinking cohort) must still report
// the worse pen as worse. Without the absolute value on the denominator the sign
// flips and the worst pen reads as the best.
func TestNegativePeerMedianKeepsTheSignHonest(t *testing.T) {
	pens := []Pen{
		pen("worse", "Sirohi", "Grower", -300, ADGBasisPerAnimalMedian),
		pen("better", "Sirohi", "Grower", -100, ADGBasisPerAnimalMedian),
	}
	Benchmark(pens)

	if *pens[0].PeerMedianADGGPerDay != -200 {
		t.Fatalf("peer median = %v, want -200", *pens[0].PeerMedianADGGPerDay)
	}
	if got := *pens[0].ADGVsPeerPct; got >= 0 {
		t.Fatalf("the worse pen reported %+.1f%%, want a negative value", got)
	}
	if got := *pens[1].ADGVsPeerPct; got <= 0 {
		t.Fatalf("the better pen reported %+.1f%%, want a positive value", got)
	}
}

// A zero median has no percentage distance from it; reporting one is a division
// by zero rendered as an arrow.
func TestZeroPeerMedianProducesNoPercentage(t *testing.T) {
	pens := []Pen{
		pen("a", "Sirohi", "Grower", -100, ADGBasisPerAnimalMedian),
		pen("b", "Sirohi", "Grower", 100, ADGBasisPerAnimalMedian),
	}
	Benchmark(pens)

	if *pens[0].PeerMedianADGGPerDay != 0 {
		t.Fatalf("peer median = %v, want 0", *pens[0].PeerMedianADGGPerDay)
	}
	for _, p := range pens {
		if p.ADGVsPeerPct != nil {
			t.Fatalf("%s got %v%% against a zero median", p.ShedName, *p.ADGVsPeerPct)
		}
	}
}

// Every pen in a group must see the SAME median regardless of its position in the
// slice — the group's values are shared, so an in-place sort would reorder another
// pen's view of its own group.
func TestMedianIsIndependentOfInputOrder(t *testing.T) {
	forward := []Pen{
		pen("a", "Sirohi", "Grower", 100, ADGBasisPerAnimalMedian),
		pen("b", "Sirohi", "Grower", 500, ADGBasisPerAnimalMedian),
		pen("c", "Sirohi", "Grower", 300, ADGBasisPerAnimalMedian),
	}
	reverse := []Pen{
		pen("c", "Sirohi", "Grower", 300, ADGBasisPerAnimalMedian),
		pen("b", "Sirohi", "Grower", 500, ADGBasisPerAnimalMedian),
		pen("a", "Sirohi", "Grower", 100, ADGBasisPerAnimalMedian),
	}
	Benchmark(forward)
	Benchmark(reverse)

	for i := range forward {
		if *forward[i].PeerMedianADGGPerDay != 300 {
			t.Fatalf("forward[%d] median = %v, want 300", i, *forward[i].PeerMedianADGGPerDay)
		}
		if *reverse[i].PeerMedianADGGPerDay != 300 {
			t.Fatalf("reverse[%d] median = %v, want 300", i, *reverse[i].PeerMedianADGGPerDay)
		}
	}
}

// An even-sized group averages the two middle values rather than picking one.
func TestEvenSizedGroupMedianAveragesTheMiddlePair(t *testing.T) {
	pens := []Pen{
		pen("a", "Sirohi", "Grower", 100, ADGBasisPerAnimalMedian),
		pen("b", "Sirohi", "Grower", 200, ADGBasisPerAnimalMedian),
		pen("c", "Sirohi", "Grower", 300, ADGBasisPerAnimalMedian),
		pen("d", "Sirohi", "Grower", 500, ADGBasisPerAnimalMedian),
	}
	Benchmark(pens)
	if got := *pens[0].PeerMedianADGGPerDay; got != 250 {
		t.Fatalf("median = %v, want 250", got)
	}
}

// The counters describe rows that are PRESENT but unanswerable, never rows that
// were dropped. A comparison screen that silently omits what it could not resolve
// reads as an estate where everything is comparable.
func TestCountersDescribePresentRowsNotDroppedOnes(t *testing.T) {
	a := pen("a", "Sirohi", "Grower", 400, ADGBasisPerAnimalMedian)
	b := pen("b", "Sirohi", "Grower", 300, ADGBasisPerAnimalMedian)
	b.FeedPlanStatus = FeedPlanUnknownCohort
	c := pen("c", "Sirohi", "Grower", 0, ADGBasisPerAnimalMedian)
	c.Breed, c.ADGGPerDay = nil, nil
	c.FeedPlanStatus = FeedPlanNoConfig

	pens := []Pen{a, b, c}
	withoutCohort, withoutRation, withoutGain, comparable := Benchmark(pens)

	if len(pens) != 3 {
		t.Fatalf("Benchmark changed row count to %d", len(pens))
	}
	if withoutCohort != 1 || withoutRation != 2 || withoutGain != 1 || comparable != 2 {
		t.Fatalf("counters = cohort:%d ration:%d gain:%d comparable:%d; want 1/2/1/2",
			withoutCohort, withoutRation, withoutGain, comparable)
	}
}
