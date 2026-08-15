package domain

import "testing"

// writableVocabulary mirrors the active animal_stage_lookup rows the seed installs
// (backend/cmd/seed-vaccination-real/main.go seedAnimalStageLookup), minus the clinical states the
// destination catalog already filters out before this function ever sees them.
var writableVocabulary = []string{
	"K0", "K1", "K2", "K3", "F2", "F2-Male", "F2-Female", "Buck", "Mother",
	"Milking", "M0", "Warmup", "Pregnant", "Non-Pregnant",
}

func TestResolveShiftingDestinationStageAdoptsTheDestinationCohort(t *testing.T) {
	// The rule's whole point: a clean single-cohort shed hands its tag to the arriving animal with
	// nothing asked of the operator. These are real single-tag units from the CPT source.
	for _, tc := range []struct {
		name  string
		stage string
	}{
		{"cpt gandhi 1", "Non-Pregnant"},
		{"cpt old yashoda 5", "Buck"},
		{"kid shed", "K2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveShiftingDestinationStage([]string{tc.stage}, writableVocabulary)
			if got != tc.stage {
				t.Fatalf("ResolveShiftingDestinationStage([%q]) = %q, want %q", tc.stage, got, tc.stage)
			}
		})
	}
}

func TestResolveShiftingDestinationStageReturnsVocabularyCasing(t *testing.T) {
	// goats.management_stage is free text, so a resident row may carry casing the vocabulary does
	// not. The snapshot must be the canonical spelling the relocation will look up, otherwise the
	// stored target and the lookup disagree on a case-sensitive read.
	got := ResolveShiftingDestinationStage([]string{"non-pregnant"}, writableVocabulary)
	if got != "Non-Pregnant" {
		t.Fatalf("got %q, want canonical %q", got, "Non-Pregnant")
	}
}

func TestResolveShiftingDestinationStageTreatsCaseVariantsAsOneCohort(t *testing.T) {
	// "K2", "k2" and " K2 " are one cohort. Counting them as three would make a clean shed look
	// mixed and silently downgrade it to keep-current.
	got := ResolveShiftingDestinationStage([]string{"K2", "k2", " K2 "}, writableVocabulary)
	if got != "K2" {
		t.Fatalf("got %q, want %q", got, "K2")
	}
}

func TestResolveShiftingDestinationStageAdoptsFlushing(t *testing.T) {
	// REVERSAL, maintainer decision 2026-08-15. Flushing used to be the one named exception a
	// movement could never adopt, on the grounds that a placement decision must not silently become
	// a feeding decision. The maintainer was shown that exact consequence -- flushing ration plus a
	// re-keyed vaccination schedule -- and chose to adopt it anyway.
	//
	// Asserted with Flushing PRESENT in the writable vocabulary (migration 000169 lists it), so this
	// proves the rule rather than the accident of what the seed happens to carry. Case-insensitively,
	// and returning the vocabulary's canonical casing, like every other cohort.
	vocabWithFlushing := append(append([]string{}, writableVocabulary...), FlushingStageName)
	for _, resident := range []string{"Flushing", "flushing", " FLUSHING "} {
		if got := ResolveShiftingDestinationStage([]string{resident}, vocabWithFlushing); got != FlushingStageName {
			t.Fatalf("flushing destination %q resolved to %q, want %q", resident, got, FlushingStageName)
		}
	}
	// Still keep-current when the tenant has NOT listed Flushing as writable: the vocabulary check
	// is what governs the tag now that nothing special-cases the string.
	if got := ResolveShiftingDestinationStage([]string{"Flushing"}, writableVocabulary); got != "" {
		t.Fatalf("unlisted flushing resolved to %q, want keep-current", got)
	}
}

// TestResolveShiftingDestinationStageRefusesClinicalResidents pins that a shed whose residents all
// carry a bare clinical STATE never stamps it, even when the tenant lists that state as writable.
//
// Without this the vocabulary check alone would resolve it, and the movement would then be rejected
// by identity/adapters/postgres.resolveDestinationTag at the SECOND GATE -- after the operator has
// shot the completion video and the park head has approved.
func TestResolveShiftingDestinationStageRefusesClinicalResidents(t *testing.T) {
	vocabWithClinical := append(append([]string{}, writableVocabulary...), "ICU", "Quarantine")
	for _, clinical := range []string{"ICU", "icu", "Quarantine", "Under Treatment"} {
		got := ResolveShiftingDestinationStageDetailed([]string{clinical}, vocabWithClinical)
		if got.Stage != "" {
			t.Fatalf("clinical resident cohort %q resolved to %q, want keep-current", clinical, got.Stage)
		}
		if got.Reason != StageReasonNotApplicable {
			t.Fatalf("clinical resident cohort %q reason = %q, want %q", clinical, got.Reason, StageReasonNotApplicable)
		}
	}
}

func TestResolveShiftingDestinationStageKeepsCurrentForMixedShed(t *testing.T) {
	// Real mixed units from the committed source data. None of these has a single answer, and a
	// majority pick would stamp a cohort on thin, unstable evidence.
	for _, tc := range []struct {
		name   string
		stages []string
	}{
		{"cbe gandhi 2", []string{"F2-Female", "Non-Pregnant", "Mother", "Pregnant"}},
		{"cbe godel 2", []string{"F2", "Non-Pregnant", "Warmup", "Mother", "F2-Male", "K0", "M0", "Milking"}},
		{"cpt yashoda 4", []string{"K2", "K0"}},
		{"two animals two cohorts", []string{"Pregnant", "Mother"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveShiftingDestinationStage(tc.stages, writableVocabulary); got != "" {
				t.Fatalf("mixed shed resolved to %q, want keep-current", got)
			}
		})
	}
}

func TestResolveShiftingDestinationStageKeepsCurrentForEmptyShed(t *testing.T) {
	// A brand-new or fully vacated shed. Nothing to adopt, and inventing a cohort here would be
	// pure fabrication.
	for _, stages := range [][]string{nil, {}, {""}, {"   "}} {
		if got := ResolveShiftingDestinationStage(stages, writableVocabulary); got != "" {
			t.Fatalf("empty shed %#v resolved to %q, want keep-current", stages, got)
		}
	}
}

func TestResolveShiftingDestinationStageKeepsCurrentForUnwritableCohort(t *testing.T) {
	// These are REAL single-tag sheds (CBE Yashoda 10 is 23 animals of ICU-Kid; Sumathi 2 - Part 8
	// is 15 of ICU-Non-Pregnant) whose cohort is absent from animal_stage_lookup. Adopting the tag
	// would pass the raise and then fail the relocation at the second gate, AFTER the operator's
	// completion video and the park head's approval. Keep-current is what keeps the movement alive.
	for _, stage := range []string{"ICU-Kid", "ICU-Non-Pregnant", "Quarantine kids"} {
		if got := ResolveShiftingDestinationStage([]string{stage}, writableVocabulary); got != "" {
			t.Fatalf("unwritable cohort %q resolved to %q, want keep-current", stage, got)
		}
	}
}

func TestResolveShiftingDestinationStageKeepsCurrentWhenVocabularyIsUnavailable(t *testing.T) {
	// Fail-safe, not fail-open: if the writable vocabulary comes back empty for any reason, the
	// resolver must not stamp an unvalidated tag it cannot prove is writable.
	if got := ResolveShiftingDestinationStage([]string{"K2"}, nil); got != "" {
		t.Fatalf("got %q with no writable vocabulary, want keep-current", got)
	}
}
