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

func TestResolveShiftingDestinationStageKeepsCurrentForFlushing(t *testing.T) {
	// The maintainer's explicit exception. Asserted with Flushing PRESENT in the writable
	// vocabulary, so this proves the named rule rather than the accident that Flushing happens to
	// be missing from animal_stage_lookup today.
	vocabWithFlushing := append(append([]string{}, writableVocabulary...), FlushingStageName)
	if got := ResolveShiftingDestinationStage([]string{"Flushing"}, vocabWithFlushing); got != "" {
		t.Fatalf("flushing destination resolved to %q, want keep-current", got)
	}
	if got := ResolveShiftingDestinationStage([]string{"flushing"}, vocabWithFlushing); got != "" {
		t.Fatalf("lowercase flushing resolved to %q, want keep-current", got)
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
