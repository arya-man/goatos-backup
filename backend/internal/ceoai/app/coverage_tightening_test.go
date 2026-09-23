package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/adapters/readtools"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
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
// Two earlier passes each fixed one direction by breaking the other, so they
// are pinned TOGETHER, in one file, against the REAL catalogue.
//
// liveCatalogue is the production 12-tool set, not a stub. An earlier version of
// this file scored direction 2 against a 2-tool fixture -- one sixth of the real
// vocabulary -- so the looseness direction was under-tested by construction, and
// direction 1 was pinned on exactly the phrasings the author had tuned against.
// Both are now out-of-sample: every question below was drawn from an independent
// sweep, not from the strings the rule was written against.
func liveCatalogue() []ports.ToolSpec {
	specs := make([]ports.ToolSpec, 0, 12)
	for _, e := range readtools.NewToolExecutors() {
		specs = append(specs, e.Spec())
	}
	return specs
}

// Direction 1: ordinary leadership questions must nominate their source. Every
// one of these returned NOTHING under one or other of the earlier rules, and
// every one is a question a CEO actually asks.
func TestOrdinarySubjectQuestionsStillNominateTheirSource(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()
	if len(catalog) < 10 {
		t.Fatalf("the live catalogue shrank to %d tools; this test must score against the real one", len(catalog))
	}
	for _, tc := range []struct {
		question string
		want     string
	}{
		// The three defects coverage.go's own header names.
		{"do we know the buyer names", "sales_buyer_summary"},
		{"what is our price per kg", "sales_deal_lines_closed"},
		{"how much have our buyers paid us", "sales_buyer_summary"},
		{"which buyers still owe us money", "sales_buyer_summary"},
		// One strong subject word, matching a COLUMN and not a view name.
		{"what was our revenue last month", "sales_buyer_summary"},
		{"how much revenue did we make", "sales_buyer_summary"},
		{"which items need reordering", "inventory_stock_position"},
		// A dimension noun that IS the subject, matching a view NAME.
		{"how many loads arrived yesterday", "procurement_loads_base"},
		{"who are our top buyers by value this month", "sales_buyer_summary"},
		{"how many vaccine doses did we use", "vaccination_dose_pickup"},
		{"how many animals are in each shed", "animal_current_scope"},
		{"how many kids do we have right now", "counts_breakdown"},
		{"what were our sales revenue and deals last month", "sales_deal_lines_closed"},
		{"what weight did we record for each animal weighed this month", "weighing_latest_individual_weight"},
	} {
		covering := coveringSources(tc.question, cards, catalog)
		if len(covering) == 0 {
			t.Errorf("%q nominated NOTHING — the planner's \"we don't track that\" refusal ships verbatim", tc.question)
			continue
		}
		if !namesSource(covering, tc.want) {
			t.Errorf("%q nominated %v, expected it to include %s", tc.question, covering, tc.want)
		}
	}
}

// Direction 2: a source matched ONLY through dimension words, while the
// question's own subject words match nothing, is not a covering source.
// "which sheds missed their milking session yesterday" scored
// feed_direction_current on shed_label + session_no alone, with neither
// "milking" nor "missed" participating, and coverageFeedback then told the model
// to read it and never say the farm does not record it.
//
// Mutation: drop the subject requirement in nominates (accept any name hit, or
// any two stems), and this goes red.
func TestDimensionColumnNoiseAloneNominatesNoSource(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()
	for _, question := range []string{
		"which sheds missed their milking session yesterday",
		"how many treatment sessions were missed yesterday",
		"how much colostrum did each shed dispense yesterday",
		"what is the milk fat percentage by breed of cow",
	} {
		if covering := coveringSources(question, cards, catalog); len(covering) != 0 {
			t.Errorf("%q nominated %v on dimension words alone — the refusal override fires on noise",
				question, covering)
		}
	}
	// The question's own words are still SCORED (deleting them from the question
	// is what broke direction 1) -- they simply do not carry a nomination alone.
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
// strings.Contains over the joined identifier text, so a question word scored on
// any SUBSTRING of any identifier: "plan" inside `planned_sessions`, "manage"
// inside `manager_label`, "session" inside `session_no`.
//
// An earlier version of this file CLAIMED the nomination tests above went red
// when the substring bar was restored. They did not -- a reviewer ran exactly
// that mutation and all three stayed green, because the other half of the fix
// carried them. This is the test that genuinely depends on anchoring, at the
// level the matching happens.
//
// Mutation: make identifierHaystack/stemHits match on substrings again and this
// goes red on both the unit assertions and the end-to-end nomination.
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

	// End to end: a question built from those fragments must not nominate the
	// view whose identifiers merely contain them.
	if covering := coveringSources("who will plan and manage the shed",
		reporting.Cards(), liveCatalogue()); len(covering) != 0 {
		t.Errorf("substring matching nominated %v", covering)
	}
}

// KNOWN GAP, RECORDED RATHER THAN HIDDEN. "how many goats died last month" is
// answered by mortality_base, whose columns say `deaths` and `cause_established`
// while the question says "died" -- and nothing here bridges an irregular verb
// to its noun. Stemming folds plurals and gerunds because both sides of the
// comparison run through the same function; a died -> death mapping is a
// SYNONYM LIST, which this file deliberately does not keep (matchesTerm's own
// comment says so).
//
// It is not the failure this file exists to prevent: the question DOES nominate
// sources, so the "we don't track deaths" refusal is still overridden and
// re-planned -- just against neighbouring animal views rather than the mortality
// one, and the feedback names them for the planner to pick from. If this is ever
// worth closing, close it by giving mortality_base's own CARD the farm's word,
// never by loosening the matcher.
func TestADeathQuestionStillOverridesTheRefusalEvenThoughItMissesMortalityBase(t *testing.T) {
	covering := coveringSources("how many goats died last month and from what cause",
		reporting.Cards(), liveCatalogue())
	if len(covering) == 0 {
		t.Fatal("a death question nominated nothing — the refusal would ship to the leader verbatim")
	}
	if namesSource(covering, "mortality_base") {
		t.Log("mortality_base is now nominated; the recorded synonym gap has been closed, " +
			"so fold this question back into the direction-1 table")
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
