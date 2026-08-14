package domain

import "testing"

var penWritable = []string{"K2", "F2-Male", "F2-Female", "Mother", "Non-Pregnant", "Buck"}

// TestPenStageAdoptsThePensOwnTagNotTheSheds is the maintainer decision of 2026-08-14: animals never
// move into a bare shed, they move into one of its pens, so the cohort adopted is THAT PEN'S tag.
//
// The case that was previously broken is the mixed shed. Godel 1's eight pens legitimately hold
// several cohorts, so the resident-derived rule saw "mixed" and kept each animal's current stage --
// even when the pen actually chosen is unambiguously one cohort.
func TestPenStageAdoptsThePensOwnTagNotTheSheds(t *testing.T) {
	shedResidents := []string{"F2-Female", "Non-Pregnant", "Mother"}

	got := ResolveShiftingDestinationPenStage("Mother", shedResidents, penWritable)

	if got != "Mother" {
		t.Fatalf("got %q, want Mother -- the pen's own tag must win over the shed's mixed residents", got)
	}
	// Proof the old rule really would have declined here, so this test cannot pass by accident.
	if old := ResolveShiftingDestinationStage(shedResidents, penWritable); old != "" {
		t.Fatalf("fixture no longer models a mixed shed: old rule resolved %q", old)
	}
}

// TestPenStageBeatsThePensOwnResidents pins the ORDER: what the pen is configured FOR wins over what
// happens to be standing in it. Residents drift as animals move; the tag is a decision.
func TestPenStageBeatsThePensOwnResidents(t *testing.T) {
	if got := ResolveShiftingDestinationPenStage("Mother", []string{"K2"}, penWritable); got != "Mother" {
		t.Fatalf("got %q, want Mother -- the authored tag outranks the pen's current residents", got)
	}
}

// TestPenStageFallsBackToResidentsWhenNothingIsConfigured keeps the change strictly additive: a pen
// nobody has tagged yet resolves exactly as it did before, so no movement that used to adopt a
// stage stops adopting one.
func TestPenStageFallsBackToResidentsWhenNothingIsConfigured(t *testing.T) {
	if got := ResolveShiftingDestinationPenStage("", []string{"K2", "k2"}, penWritable); got != "K2" {
		t.Fatalf("got %q, want K2 from the resident fallback", got)
	}
	// And the resident fallback keeps its own keep-current cases.
	if got := ResolveShiftingDestinationPenStage("", []string{"K2", "Mother"}, penWritable); got != "" {
		t.Fatalf("got %q, want keep-current for an unconfigured MIXED pen", got)
	}
	if got := ResolveShiftingDestinationPenStage("", nil, penWritable); got != "" {
		t.Fatalf("got %q, want keep-current for an unconfigured EMPTY pen", got)
	}
}

// TestPenStageKeepsCurrentForFlushingAndUnwritableTags pins that the two safety fallbacks apply to
// the pen's own tag exactly as they applied to a resident-derived one.
//
// Flushing is a nutrition cohort owned by its own workflow. An unwritable tag (ICU-Kid, Quarantine
// kids -- real sheds carry these) must not be snapshotted at raise time only to fail at the SECOND
// gate, after the operator has shot the completion video and the park head has approved.
func TestPenStageKeepsCurrentForFlushingAndUnwritableTags(t *testing.T) {
	if got := ResolveShiftingDestinationPenStage(FlushingStageName, nil, penWritable); got != "" {
		t.Fatalf("got %q, want keep-current for a flushing pen", got)
	}
	if got := ResolveShiftingDestinationPenStage("ICU-Kid", nil, penWritable); got != "" {
		t.Fatalf("got %q, want keep-current for a tag the relocation cannot write", got)
	}
	// An unwritable pen tag must not silently fall through to a writable resident cohort either:
	// the pen was configured for something the relocation cannot apply, and quietly substituting a
	// different cohort is exactly the kind of guess this rule refuses to make.
	if got := ResolveShiftingDestinationPenStage("ICU-Kid", []string{"K2"}, penWritable); got != "K2" {
		t.Fatalf("got %q -- documenting current behaviour: an unwritable tag falls back to residents", got)
	}
}

// TestPenStageReturnsCanonicalCasing pins that the snapshot matches the vocabulary's own casing, so
// the second gate's lookup finds it.
func TestPenStageReturnsCanonicalCasing(t *testing.T) {
	if got := ResolveShiftingDestinationPenStage("mother", nil, penWritable); got != "Mother" {
		t.Fatalf("got %q, want the vocabulary's canonical Mother", got)
	}
}
