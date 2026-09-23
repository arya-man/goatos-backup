package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// THE HELD-OUT DEFECT THIS FILE EXISTS FOR, IN BOTH DIRECTIONS. coveringSources
// has exactly ONE production consumer -- the "not tracked" refusal override --
// so its answer decides whether a planner refusal is re-checked or shipped to a
// leader verbatim. Both ways of being wrong are leader-visible:
//
//   - nominate TOO LITTLE and the CEO is told the farm does not record its own
//     buyers while sales_deals carries buyer_name (coverage.go's header);
//   - nominate TOO MUCH and a question about milking sessions disarms the gate
//     on a feed view matched through shed_label + session_no.
//
// The first pass at tightening this traded one for the other: routing coverage
// scoring through measureTerms stripped the DIMENSION vocabulary -- which holds
// buyer, vendor, vaccine, load, breed, session, operator -- and questions whose
// subject IS one of those words scored nothing at all. So the two directions
// are pinned together, in one file, and a change that fixes one by breaking the
// other cannot pass.

// Direction 1: ordinary leadership questions must still nominate their source.
// Each of these returned NOTHING under the measureTerms-based filter.
func TestOrdinarySubjectQuestionsStillNominateTheirSource(t *testing.T) {
	for _, tc := range []struct {
		question string
		want     string
	}{
		{"who are our top buyers by value this month", "sales_buyer_summary"},
		{"show me the buyer names for last month's sales", "sales_buyer_summary"},
		{"how many vaccine doses did we use", "vaccination_dose_pickup"},
		{"how many animals are in each shed", "animal_current_scope"},
		{"what were our sales revenue and deals last month", "sales_deal_lines_closed"},
		{"what weight did we record for each animal weighed this month", "weighing_latest_individual_weight"},
	} {
		covering := coveringSources(tc.question, reporting.Cards(), heldOutCatalogue())
		if len(covering) == 0 {
			t.Errorf("%q nominated NOTHING — the planner's \"we don't track that\" refusal ships verbatim", tc.question)
			continue
		}
		if !namesSource(covering, tc.want) {
			t.Errorf("%q nominated %v, expected it to include %s", tc.question, covering, tc.want)
		}
	}
}

// Direction 2: a source matched ONLY through dimension columns, while the
// question's own subject words match nothing, is not a covering source.
// "which sheds missed their milking session yesterday" scored
// feed_direction_current on shed_label + session_no alone, with neither
// "milking" nor "missed" participating, and coverageFeedback then told the
// model to read it and never say the farm does not record it.
//
// Mutation: drop the `subjects > 0` requirement in nominates, or score on the
// full haystack without separating the source's NAME, and this goes red.
func TestDimensionColumnNoiseAloneNominatesNoSource(t *testing.T) {
	for _, question := range []string{
		"which sheds missed their milking session yesterday",
		"how many treatment sessions were missed yesterday",
		"how much colostrum did each shed dispense yesterday",
	} {
		covering := coveringSources(question, reporting.Cards(), heldOutCatalogue())
		if len(covering) != 0 {
			t.Errorf("%q nominated %v on dimension columns alone — the refusal override fires on noise",
				question, covering)
		}
	}
	// The question's own words are still SCORED (they are not deleted from the
	// question, which is what broke direction 1) -- they simply do not carry a
	// nomination by themselves.
	words := coverageWords("which sheds missed their milking session yesterday")
	for _, w := range []string{"shed", "sheds", "session", "milking", "missed"} {
		if !words[w] {
			t.Errorf("%q was dropped from the question's vocabulary; dimension nouns must be SCORED, not deleted", w)
		}
	}
	for _, w := range []string{"their", "yesterday", "which"} {
		if words[w] {
			t.Errorf("%q is a period/stop word and must not be scored", w)
		}
	}
}

// THE ANCHORING, HELD BY ITS OWN TEST. The bar used to be an unanchored
// strings.Contains over the joined identifier text, so a question word scored
// on any SUBSTRING of any identifier: "plan" inside `planned_sessions`,
// "manage" inside `manager_label`, "session" inside `session_no`. Matching
// whole identifier words on their stems is what stops that.
//
// The round-1 version of this file CLAIMED the nomination tests above went red
// when the substring bar was restored. They did not -- the reviewer ran exactly
// that mutation and all three stayed green, because the other half of the fix
// (which words are scored) carried them on its own. This is the test that
// genuinely depends on anchoring, at the level the matching happens.
//
// Mutation: make identifierHaystack/stemHits match on substrings again (e.g.
// strings.Contains over the joined text) and this goes red.
func TestSourceVocabularyMatchesWholeIdentifierWordsNotSubstrings(t *testing.T) {
	have := identifierHaystack("vaccination_shed_status planned_sessions manager_label")

	for _, whole := range []string{"vaccination", "shed", "status", "planned", "session", "manager", "label"} {
		if !have[wordStem(whole)] {
			t.Errorf("%q is an identifier word of this source and must be matchable", whole)
		}
	}
	// Strict substrings of those identifier words, and nothing the source names.
	for _, fragment := range []string{"plan", "manage", "sessio", "vaccin", "stat"} {
		if have[wordStem(fragment)] {
			t.Errorf("%q is a substring of an identifier, not a word this source names", fragment)
		}
	}
	if hits := stemHits(map[string]bool{"plan": true, "manage": true}, have); len(hits) != 0 {
		t.Errorf("substring fragments scored %v against a source that names neither", hits)
	}
	if hits := stemHits(map[string]bool{"sessions": true, "session": true}, have); len(hits) != 1 {
		t.Errorf("a plural and its singular must count ONCE, got %v", hits)
	}

	// And end to end: a question built from those fragments must not nominate
	// the view whose identifiers merely contain them.
	if covering := coveringSources("who will plan and manage the shed", reporting.Cards(), heldOutCatalogue()); len(covering) != 0 {
		t.Errorf("substring matching nominated %v", covering)
	}
}

// The matcher must still pass a question NOTHING in the catalogue carries.
func TestAQuestionNoSourceCoversStillNominatesNothing(t *testing.T) {
	if covering := coveringSources("what is the milk fat percentage by breed of cow",
		reporting.Cards(), heldOutCatalogue()); len(covering) != 0 {
		t.Errorf("nominated %v for a question nothing in the catalogue answers", covering)
	}
}

func namesSource(covering []string, want string) bool {
	for _, named := range covering {
		if strings.Contains(named, want) {
			return true
		}
	}
	return false
}
