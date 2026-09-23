package app

import (
	"strings"
	"testing"
)

// TestTheRefusalNamesItsSubjectInWordsAPersonReads pins the sentence hl-03
// produced: "We don't track missed, treatment in Goat OS". Two things were
// wrong with it and both are fixed here -- it claimed the PRODUCT does not
// record treatment sessions, which is false (health_treatment_sessions is a
// real table that happened to be empty), and it rendered a two-word subject
// with a comma, which reads as a typo rather than as two words.
func TestTheRefusalNamesItsSubjectInWordsAPersonReads(t *testing.T) {
	cases := []struct {
		terms []string
		want  string
	}{
		{[]string{"missed", "treatment"}, "missed and treatment"},
		{[]string{"milk"}, "milk"},
		{[]string{"litre", "litres", "milk"}, "litre, litres and milk"},
	}
	for _, c := range cases {
		got := unmodelledRefusal(c.terms)
		if !strings.Contains(got, "I don't have a source for "+c.want+" in the reads I can reach") {
			t.Errorf("refusal for %v reads %q, want the subject as %q", c.terms, got, c.want)
		}
		if strings.Contains(got, "in Goat OS") {
			t.Errorf("refusal for %v claims the product does not track it: %q", c.terms, got)
		}
		// The honesty gate itself must survive every wording change.
		if !strings.Contains(got, "I won't answer it from a different measure") {
			t.Errorf("refusal for %v dropped the no-substitution promise: %q", c.terms, got)
		}
	}
	// A refusal with no terms at all still reads as a sentence.
	if got := unmodelledRefusal(nil); !strings.Contains(got, "I don't have a source for that") {
		t.Errorf("empty-term refusal reads %q", got)
	}
}

// TestTheSubstitutedSubjectRefusalKeepsItsTwoFacts: the refusal that replaced a
// neighbouring view's number names WHAT it has no source for and WHICH read
// actually ran. Both are load-bearing -- without the view name a reader cannot
// tell the assistant what to read instead, which is what the sentence invites
// them to do.
func TestTheSubstitutedSubjectRefusalKeepsItsTwoFacts(t *testing.T) {
	got := substitutedSubjectRefusal("health cases", "ops_exception_queue")
	for _, want := range []string{
		"I don't have a source for health cases in the reads I can reach",
		"I read ops_exception_queue, which does not report it.",
		"I won't answer it from a different measure",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("refusal is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "in Goat OS") {
		t.Errorf("refusal claims the product does not track health cases: %s", got)
	}
	// With no view to name, the sentence still stands on its own.
	if bare := substitutedSubjectRefusal("health cases", ""); strings.Contains(bare, "I read ,") || strings.Contains(bare, "I read .") {
		t.Errorf("a refusal with no source view reads badly: %s", bare)
	}
}
