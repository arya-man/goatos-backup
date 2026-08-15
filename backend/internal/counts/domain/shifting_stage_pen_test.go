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

// TestPenStageAdoptsFlushingAndRefusesClinicalStates pins the 2026-08-15 split between the two tags
// that used to be treated alike.
//
// FLUSHING IS NOW ADOPTED. The maintainer reversed the nutrition-cohort carve-out having been shown
// the consequence: a move into a flushing pen puts that animal on flushing ration and re-keys her
// vaccination schedule. Asserted with Flushing in the writable vocabulary (migration 000169 lists
// it), so this pins the RULE rather than the accident of whether the seed happens to carry it.
//
// A CLINICAL STATE IS STILL REFUSED. An animal in ICU or Quarantine has her vaccinations postponed,
// so a placement action must never assert one. Refused at RAISE time here -- not only at the second
// gate in identity/adapters/postgres.resolveDestinationTag -- so the form can grey the option out
// instead of the movement dying after the operator has already shot the completion video.
func TestPenStageAdoptsFlushingAndRefusesClinicalStates(t *testing.T) {
	vocabWithFlushing := append(append([]string{}, penWritable...), FlushingStageName)
	if got := ResolveShiftingDestinationPenStage(FlushingStageName, nil, vocabWithFlushing); got != FlushingStageName {
		t.Fatalf("got %q, want a flushing pen to stamp %q", got, FlushingStageName)
	}
	// The clinical KID pens are PEN names, not states, and migration 000167 lists them as writable
	// -- so the vocabulary has to carry them here for this to model the real tenant.
	vocabWithKidPens := append(append([]string{}, penWritable...), "ICU-Kid", "Quarantine kids")
	for _, penTag := range []string{"ICU-Kid", "Quarantine kids"} {
		if got := ResolveShiftingDestinationPenStage(penTag, nil, vocabWithKidPens); got != penTag {
			t.Fatalf("got %q, want the %q pen tag to be stamped", got, penTag)
		}
	}
	// Bare ICU is a clinical STATE. Refused even when the tenant lists it as a writable stage --
	// which a tenant really can (migrations/postgres/stage_age_band_test.go seeds exactly ICU and
	// Quarantine), so the vocabulary check alone would have let it through to the second gate.
	vocabWithClinical := append(append([]string{}, penWritable...), "ICU", "Quarantine")
	for _, clinical := range []string{"ICU", "icu", "Quarantine", "Under Treatment"} {
		if got := ResolveShiftingDestinationPenStage(clinical, nil, vocabWithClinical); got != "" {
			t.Fatalf("clinical pen tag %q resolved to %q, want keep-current", clinical, got)
		}
	}
	// A refused pen tag must not silently fall through to a writable resident cohort: the pen was
	// configured for something the relocation cannot apply, and quietly substituting a different
	// cohort is exactly the kind of guess this rule refuses to make.
	if got := ResolveShiftingDestinationPenStage("ICU", []string{"K2"}, vocabWithClinical); got != "" {
		t.Fatalf("got %q, want a refused pen tag to keep current rather than adopt a resident cohort", got)
	}
}

// TestPenStageReasonsAreFarmWordedAndExclusive pins the contract the raise form's greyed-out toggle
// depends on: a keep-current answer always carries a reason, a resolved one never does, and the
// reason describes the PEN'S OWN tag rather than its residents.
func TestPenStageReasonsAreFarmWordedAndExclusive(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		residents  []string
		wantStage  string
		wantReason string
	}{
		{name: "authored tag resolves with no reason", configured: "Mother", wantStage: "Mother"},
		{name: "unconfigured empty pen", wantReason: StageReasonNoTag},
		{name: "unconfigured mixed pen", residents: []string{"K2", "Mother"}, wantReason: StageReasonMixed},
		{
			name:      "unconfigured single-cohort pen resolves from residents",
			residents: []string{"K2"}, wantStage: "K2",
		},
		{
			// The reason must be about the pen's OWN tag. Reporting the residents' reason here would
			// tell the operator "This destination has no tag set" about a pen that visibly has one.
			name:       "clinical authored tag reports the clinical reason, not the residents'",
			configured: "ICU", residents: nil, wantReason: StageReasonNotApplicable,
		},
	}
	vocab := append(append([]string{}, penWritable...), "ICU")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveShiftingDestinationPenStageDetailed(tc.configured, tc.residents, vocab)
			if got.Stage != tc.wantStage {
				t.Fatalf("stage = %q, want %q", got.Stage, tc.wantStage)
			}
			if got.Reason != tc.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tc.wantReason)
			}
			// The exclusivity invariant the client keys its greyed-out state on.
			if (got.Stage != "") == (got.Reason != "") {
				t.Fatalf("stage %q and reason %q must be mutually exclusive", got.Stage, got.Reason)
			}
			if got.Resolved() != (got.Stage != "") {
				t.Fatalf("Resolved() disagrees with Stage %q", got.Stage)
			}
		})
	}
}

// TestPenStageReturnsCanonicalCasing pins that the snapshot matches the vocabulary's own casing, so
// the second gate's lookup finds it.
func TestPenStageReturnsCanonicalCasing(t *testing.T) {
	if got := ResolveShiftingDestinationPenStage("mother", nil, penWritable); got != "Mother" {
		t.Fatalf("got %q, want the vocabulary's canonical Mother", got)
	}
}
