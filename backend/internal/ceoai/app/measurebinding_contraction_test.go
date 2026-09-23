package app

import (
	"strings"
	"testing"
)

// TestAContractionNeverBecomesASubject is the live defect, as a test. Asked
// "who hasn't paid us" against the real reads, the assistant answered:
//
//	I don't have a source for hasn paid in the reads I can reach…
//
// `contentWord` cut the token at the apostrophe, which is right for "what's"
// (leaving the stop word "what") and wrong for every NEGATIVE contraction: it
// keeps "hasn", four letters and in no stop list, so the compound-subject pass
// read it as a thing the farm might record and named it back to the leader.
//
// Fixing the coverage tokenizer alone was not enough and that is the general
// shape worth remembering: a question is tokenized in more than one place, and
// closing the contraction in one of them moved the defect from "nominates
// nothing" to "nominates the right view and then refuses it by a made-up
// name". Both readings of an apostrophe have to agree.
func TestAContractionNeverBecomesASubject(t *testing.T) {
	for _, raw := range []string{
		"hasn't", "haven't", "isn't", "aren't", "wasn't", "weren't",
		"don't", "doesn't", "didn't", "won't", "can't", "couldn't",
		"what's", "who's", "we're", "we've", "they'll", "i'd", "i'm",
	} {
		if got := contentWord(raw); got != "" {
			t.Errorf("%q was read as the subject %q; a contraction names nothing the farm records", raw, got)
		}
	}
}

// TestTheContractedQuestionNamesNoInventedSubject walks the whole compound pass
// the refusal sentence is composed from, so the assertion is about what the
// leader would be TOLD rather than about one helper.
func TestTheContractedQuestionNamesNoInventedSubject(t *testing.T) {
	for _, q := range []string{
		"who hasn't paid us",
		"which buyers haven't paid",
		"what's our revenue",
		"which sheds aren't full",
		"who didn't submit their proof",
	} {
		for _, pair := range compoundSubjects(q) {
			for _, w := range pair {
				if strings.ContainsAny(w, "'’") {
					t.Errorf("%q: subject word %q still carries an apostrophe", q, w)
				}
				// The fragments a cut-at-the-apostrophe tokenizer leaves behind.
				switch w {
				case "hasn", "haven", "isn", "aren", "wasn", "weren", "didn", "doesn", "couldn", "wouldn", "shouldn":
					t.Errorf("%q: the refusal would name the invented subject %q", q, w)
				}
			}
		}
	}
}
