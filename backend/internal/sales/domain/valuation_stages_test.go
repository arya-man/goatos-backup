package domain

import (
	"strings"
	"testing"
)

// withStage is the shape a write arrives in: a stage list plus two bucket rows per stage.
func withStage(stages []ValuationStage) ValuationAssumptions {
	v := ValuationAssumptions{Stages: stages}
	kg := 10.0
	for _, s := range stages {
		for _, g := range ValuationGenders {
			v.Buckets = append(v.Buckets, ValuationBucketRate{Bucket: ValuationBucketKey(s.Stage, g.Key), Label: BucketLabel(s.Label, g.Label), FixedWeightKg: &kg, PricePerKg: 500})
		}
	}
	return v
}

// THE FARM CAN ADD A STAGE (maintainer instruction 2026-09-24). Warmup is the case that prompted
// it: a stage the herd register has always carried, with 58 live kids standing in it, that the
// valuation could not price without a deploy. Adding it is a stage row and two figures.
func TestTheFarmCanAddAStageAndPriceIt(t *testing.T) {
	v := DefaultValuationAssumptions()
	v.Stages = append(v.Stages, ValuationStage{Label: "Warmup", Matches: []string{"Warmup"}})
	kg := 12.0
	for _, g := range ValuationGenders {
		v.Buckets = append(v.Buckets, ValuationBucketRate{Bucket: "warmup_" + g.Key, FixedWeightKg: &kg, PricePerKg: 520})
	}
	NormalizeValuationAssumptions(&v)
	if err := ValidateValuationAssumptions(v); err != nil {
		t.Fatalf("a farm must be able to add a stage: %v", err)
	}
	// The key is derived from the label ONCE, and the cards are named from the stage.
	if v.Stages[len(v.Stages)-1].Stage != "warmup" {
		t.Fatalf("stage key must be derived from the label, got %q", v.Stages[len(v.Stages)-1].Stage)
	}
	var labels []string
	for _, b := range v.Buckets {
		if strings.HasPrefix(b.Bucket, "warmup_") {
			labels = append(labels, b.Label)
		}
	}
	if len(labels) != 2 || labels[0] != "Warmup · Female" || labels[1] != "Warmup · Male" {
		t.Fatalf("the cards are named from the stage, got %v", labels)
	}
}

// A RENAME MOVES NO FIGURES. The key is derived once and never again, because the stored figures,
// the bucket keys and every audit row are keyed on it.
func TestRenamingAStageKeepsItsKeyAndItsFigures(t *testing.T) {
	v := DefaultValuationAssumptions()
	v.Stages[0].Label = "Fattening (farm)"
	NormalizeValuationAssumptions(&v)
	if v.Stages[0].Stage != "fattening" {
		t.Fatalf("a rename must not move the key, got %q", v.Stages[0].Stage)
	}
	if err := ValidateValuationAssumptions(v); err != nil {
		t.Fatalf("a rename must stay valid: %v", err)
	}
	if v.Buckets[0].Label != "Fattening (farm) · Female" {
		t.Fatalf("the card follows the stage's words, got %q", v.Buckets[0].Label)
	}
}

// ONE REGISTER ENTRY BELONGS TO ONE VALUATION STAGE. Two stages claiming it would value an animal
// by whichever row happens to sort first -- a farm value that moves when somebody reorders the
// screen. It is refused at the write rather than resolved at the read.
func TestTwoStagesMayNotClaimOneRegisterEntry(t *testing.T) {
	v := withStage([]ValuationStage{
		{Stage: "adult", Label: "Adult", Matches: []string{"Buck", "Mother"}},
		{Stage: "bucks", Label: "Bucks", Matches: []string{"Buck"}},
	})
	err := ValidateValuationAssumptions(v)
	if err == nil || !strings.Contains(err.Error(), "already valued as") {
		t.Fatalf("a register entry claimed twice must be refused, got %v", err)
	}
	// And the spelling does not get you past it: the write compares the same way the read files.
	v.Stages[1].Matches = []string{"buck "}
	if err := ValidateValuationAssumptions(v); err == nil {
		t.Fatal("a differently spelled duplicate must be refused too; the read would file it once")
	}
}

// A STAGE THAT MATCHES NOTHING IS REFUSED. It would sit on the screen with a price and never value
// an animal, while the animals it was meant for stand in the not-valued list.
func TestAStageMustCoverAtLeastOneRegisterEntry(t *testing.T) {
	v := withStage([]ValuationStage{{Stage: "warmup", Label: "Warmup", Matches: nil}})
	err := ValidateValuationAssumptions(v)
	if err == nil || !strings.Contains(err.Error(), "matches") {
		t.Fatalf("a stage covering nothing must be refused, got %v", err)
	}
}

// EVERY STAGE IS PRICED FOR BOTH GENDERS, and only stages that exist are priced. The bucket set is
// derived from the stage list, so adding a stage without its two rows -- or keeping the rows of a
// stage that was removed -- is refused rather than silently valuing half a stage at nothing.
func TestTheBucketSetFollowsTheAuthoredStages(t *testing.T) {
	v := DefaultValuationAssumptions()
	v.Stages = append(v.Stages, ValuationStage{Stage: "warmup", Label: "Warmup", Matches: []string{"Warmup"}})
	if err := ValidateValuationAssumptions(v); err == nil {
		t.Fatal("a stage with no rows must be refused; half its herd would be valued at nothing")
	}
	v = DefaultValuationAssumptions()
	v.Stages = v.Stages[:len(v.Stages)-1]
	if err := ValidateValuationAssumptions(v); err == nil {
		t.Fatal("rows for a stage that no longer exists must be refused; they can never be reached")
	}
}

// The seeded list IS the retired CASE. If this drifts, a farm that has never opened the screen
// stops valuing its herd the way it did yesterday.
func TestTheSeededStagesAreTheRetiredHardCodedRule(t *testing.T) {
	want := map[string][]string{
		"fattening": {"F2", "F2-Male", "F2-Female"},
		"adult":     {"Buck", "Mother", "Milking", "M0", "Pregnant", "Non-Pregnant", "ICU"},
		"K0":        {"K0"},
		"K1":        {"K1"},
		"K2":        {"K2", "ICU-Kid"},
		"K3":        {"K3"},
	}
	if len(SeededValuationStages) != len(want) {
		t.Fatalf("expected %d seeded stages, got %d", len(want), len(SeededValuationStages))
	}
	for _, s := range SeededValuationStages {
		got := strings.Join(s.Matches, ",")
		if exp := strings.Join(want[s.Stage], ","); got != exp {
			t.Fatalf("%s covers %q, want %q", s.Stage, got, exp)
		}
	}
	if err := ValidateValuationAssumptions(DefaultValuationAssumptions()); err != nil {
		t.Fatalf("the seeded row must be a valid one: %v", err)
	}
}

// The normalizer is one half of a comparison whose other half is in SQL. Changing it here without
// changing the read would let a write pass that the read then files somewhere else.
func TestStageMatchNormalizationMatchesTheRead(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"ICU-Kid", "ICUKID"}, {"ICU- kid", "ICUKID"}, {" Non-Pregnant ", "NONPREGNANT"},
		{"F2-Male", "F2MALE"}, {"Warmup", "WARMUP"},
	} {
		if got := NormalizeStageMatch(c.in); got != c.want {
			t.Fatalf("NormalizeStageMatch(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The default assumptions are a COPY. A caller that adds a stage, or appends a match to one, must
// not rewrite what the next farm with no authored row reads.
func TestDefaultAssumptionsDoNotShareTheSeededStages(t *testing.T) {
	first := DefaultValuationAssumptions()
	// The ROW is edited before anything is appended, on purpose: appending to a full slice re-seats
	// it onto a fresh array, so an edit made after the append would touch that copy and the test
	// would pass whether or not the rows are shared.
	first.Stages[0].Label = "Renamed"
	first.Stages[0].Matches = append(first.Stages[0].Matches, "F9")
	first.Stages = append(first.Stages, ValuationStage{Stage: "warmup", Label: "Warmup", DisplayOrder: 7})

	second := DefaultValuationAssumptions()
	if len(second.Stages) != len(SeededValuationStages) {
		t.Fatalf("stages = %d, want the seeded %d: one caller's edit reached the next", len(second.Stages), len(SeededValuationStages))
	}
	if second.Stages[0].Label != "Fattening" {
		t.Fatalf("label = %q, want Fattening", second.Stages[0].Label)
	}
	if len(second.Stages[0].Matches) != len(SeededValuationStages[0].Matches) {
		t.Fatalf("matches = %v, want the seeded set", second.Stages[0].Matches)
	}
	if len(SeededValuationStages[0].Matches) != 3 {
		t.Fatalf("the seeded rows themselves were rewritten: %v", SeededValuationStages[0].Matches)
	}
}
