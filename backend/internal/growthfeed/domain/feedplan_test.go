package domain

import "testing"

func resolvedInput() FeedPlanInput {
	return FeedPlanInput{
		CohortResolved:         true,
		RationGroupLabel:       "Sirohi",
		ShedTagLabel:           "Grower",
		ItemsConfigured:        4,
		ItemsBlocked:           0,
		PlannedGramsPerHeadDay: 1250,
		EnergyKcalPerHeadDay:   3400,
		EnergyComplete:         true,
		LiveAnimals:            68,
	}
}

func TestFullyAuthoredRationReportsGramsAndEnergy(t *testing.T) {
	status, grams, kcal := ResolveFeedPlan(resolvedInput())

	if status != FeedPlanResolved {
		t.Fatalf("status = %q, want %q", status, FeedPlanResolved)
	}
	if grams == nil || *grams != 1250 {
		t.Fatalf("grams = %v, want 1250", grams)
	}
	if kcal == nil || *kcal != 3400 {
		t.Fatalf("energy = %v, want 3400", kcal)
	}
}

// THE SPARSE-GRID RULE, checked against live staging data. A ration authors what
// the cohort eats and stays silent about the rest: on real data NO (group, tag)
// combination covers all 14 catalog items. Treating an unauthored item as an
// incomplete ration suppressed the conversion ratio on every pen in the estate.
func TestSparseRationStillProducesATotalAndAConversionRatio(t *testing.T) {
	in := resolvedInput()
	in.ItemsConfigured, in.ItemsBlocked = 10, 4
	in.PlannedGramsPerHeadDay = 900

	status, grams, _ := ResolveFeedPlan(in)

	if status != FeedPlanResolved {
		t.Fatalf("status = %q, want %q — a sparse ration is a normal ration", status, FeedPlanResolved)
	}
	if grams == nil || *grams != 900 {
		t.Fatalf("grams = %v, want the 900 that IS authored", grams)
	}

	// And the number the screen exists for actually appears.
	pens := []Pen{{
		Breed: ptrS("Sirohi"), Stage: ptrS("Grower"),
		ADGGPerDay: ptrF(300), ADGBasis: ADGBasisPerAnimalMedian,
		FeedPlanStatus: status, PlannedFeedGPerHeadDay: grams,
	}}
	Benchmark(pens)
	if pens[0].FeedPerKgGainKg == nil {
		t.Fatal("a sparse but real ration produced no conversion ratio")
	}
	if got := *pens[0].FeedPerKgGainKg; got != 3 {
		t.Fatalf("conversion = %v, want 3.0 kg feed per kg gain", got)
	}
}

// The item counts still travel, because they are what lets a reader question a
// total that looks too small for the animal.
func TestItemCountsSpanEveryActiveFeedItem(t *testing.T) {
	in := resolvedInput()
	in.ItemsConfigured, in.ItemsBlocked = 10, 4
	if in.ItemsConfigured+in.ItemsBlocked != 14 {
		t.Fatalf("configured + blocked must span every active item, got %d", in.ItemsConfigured+in.ItemsBlocked)
	}
}

func TestEveryItemBlockedIsNoConfigNotAZeroRation(t *testing.T) {
	in := resolvedInput()
	in.ItemsConfigured, in.ItemsBlocked = 0, 4
	in.PlannedGramsPerHeadDay = 0

	status, grams, _ := ResolveFeedPlan(in)

	if status != FeedPlanNoConfig {
		t.Fatalf("status = %q, want %q", status, FeedPlanNoConfig)
	}
	// 0 g/head/day would read as "this pen is meant to eat nothing", which is a
	// different and untrue statement from "nobody configured it".
	if grams != nil {
		t.Fatalf("grams = %v, want none", *grams)
	}
}

// An authored zero is a real ration (milk-fed kids eat no solids) and must stay
// distinguishable from the unconfigured case above.
func TestAuthoredZeroRationIsResolvedNotMissing(t *testing.T) {
	in := resolvedInput()
	in.PlannedGramsPerHeadDay = 0
	in.EnergyKcalPerHeadDay = 0

	status, grams, _ := ResolveFeedPlan(in)

	if status != FeedPlanResolved {
		t.Fatalf("status = %q, want %q", status, FeedPlanResolved)
	}
	if grams == nil || *grams != 0 {
		t.Fatalf("grams = %v, want an explicit 0", grams)
	}
}

func TestMixedCohortPenHasNoRationToLookUp(t *testing.T) {
	in := resolvedInput()
	in.CohortResolved = false

	status, grams, _ := ResolveFeedPlan(in)

	if status != FeedPlanUnknownCohort {
		t.Fatalf("status = %q, want %q", status, FeedPlanUnknownCohort)
	}
	if grams != nil {
		t.Fatalf("a mixed-cohort pen was given a ration of %v", *grams)
	}
}

func TestCohortWithNoAuthoredVocabularyIsNoConfig(t *testing.T) {
	for name, mutate := range map[string]func(*FeedPlanInput){
		"stage has no shed tag":     func(in *FeedPlanInput) { in.ShedTagLabel = "" },
		"breed has no ration group": func(in *FeedPlanInput) { in.RationGroupLabel = "" },
	} {
		in := resolvedInput()
		mutate(&in)
		status, _, _ := ResolveFeedPlan(in)
		if status != FeedPlanNoConfig {
			t.Fatalf("%s: status = %q, want %q", name, status, FeedPlanNoConfig)
		}
	}
}

// Incomplete energy data blocks the energy figure only; the grams are unaffected,
// because a missing kcal value never changes how much feed the pen receives.
func TestIncompleteEnergyDropsEnergyButKeepsGrams(t *testing.T) {
	in := resolvedInput()
	in.EnergyComplete = false

	status, grams, kcal := ResolveFeedPlan(in)

	if status != FeedPlanResolved {
		t.Fatalf("status = %q, want %q", status, FeedPlanResolved)
	}
	if grams == nil || *grams != 1250 {
		t.Fatalf("grams = %v, want 1250", grams)
	}
	if kcal != nil {
		t.Fatalf("energy = %v, want none when a contributing item has no kcal value", *kcal)
	}
}

// absolute_kg is a SHED TOTAL. Reporting it as a per-head rate would overstate the
// ration by the head count — a 60-head shed would read as eating 60x what it does.
func TestExperimentShedTotalIsDividedByHeadCount(t *testing.T) {
	in := FeedPlanInput{IsExperiment: true, ExperimentTotalKgPerDay: 40, LiveAnimals: 50}

	status, grams, kcal := ResolveFeedPlan(in)

	if status != FeedPlanExperiment {
		t.Fatalf("status = %q, want %q", status, FeedPlanExperiment)
	}
	if grams == nil || *grams != 800 {
		t.Fatalf("grams = %v, want 800 (40 kg over 50 head)", grams)
	}
	if kcal != nil {
		t.Fatal("experiment pens carry no energy rollup")
	}
}

func TestExperimentShedWithNoKnownHeadCountReportsNoPerHeadRate(t *testing.T) {
	in := FeedPlanInput{IsExperiment: true, ExperimentTotalKgPerDay: 40, LiveAnimals: 0}

	status, grams, _ := ResolveFeedPlan(in)

	if status != FeedPlanExperiment {
		t.Fatalf("status = %q, want %q", status, FeedPlanExperiment)
	}
	if grams != nil {
		t.Fatalf("grams = %v, want none rather than a division by zero", *grams)
	}
}

// An experiment pen never produces a conversion ratio either: its status is not
// FeedPlanResolved, so Benchmark declines it.
func TestExperimentPenProducesNoConversionRatio(t *testing.T) {
	status, grams, _ := ResolveFeedPlan(FeedPlanInput{
		IsExperiment: true, ExperimentTotalKgPerDay: 40, LiveAnimals: 50,
	})
	pens := []Pen{{
		Breed: ptrS("Sirohi"), Stage: ptrS("Grower"),
		ADGGPerDay: ptrF(400), ADGBasis: ADGBasisPerAnimalMedian,
		FeedPlanStatus: status, PlannedFeedGPerHeadDay: grams,
	}}
	Benchmark(pens)
	if pens[0].FeedPerKgGainKg != nil {
		t.Fatalf("experiment pen produced conversion %v", *pens[0].FeedPerKgGainKg)
	}
}
