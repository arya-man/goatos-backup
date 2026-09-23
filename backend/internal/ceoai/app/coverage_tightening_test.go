package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// THE OVERRIDE THAT DISARMED THE GATE. coveringSources decided two things at
// once: whether a "not tracked" refusal gets overridden into a re-plan, and
// (through measureUnmodelled's old last check) whether the unmodelled-measure
// refusal runs at all. The matcher feeding both was an unanchored
// strings.Contains over `view name + column names` with a bar of two distinct
// stems, and the dimension vocabulary was not filtered out — so "which sheds
// missed their milking session yesterday" scored feed_direction_current on
// `shed_label` + `session_no` alone, with neither "milking" nor "missed"
// participating. The planner was then told to read that view and never to say
// the farm does not record it.
//
// Mutation: score coveringViews on questionWords again, or restore the
// strings.Contains substring bar in distinctStemHits, and this goes red.
func TestDimensionColumnNoiseAloneNominatesNoSource(t *testing.T) {
	const question = "which sheds missed their milking session yesterday"
	if _, ok := reporting.CardByName("feed_direction_current"); !ok {
		t.Skip("the feed-direction card is not in the catalogue")
	}
	covering := coveringSources(question, reporting.Cards(), heldOutCatalogue())
	for _, named := range covering {
		if strings.Contains(named, "feed_direction_current") {
			t.Fatalf("a feed view was nominated for a milking question on its shed/session columns alone: %v", covering)
		}
	}
	// The words that actually carry the subject must be the ones scored.
	words := coverageWords(question)
	for _, noise := range []string{"shed", "sheds", "session", "sessions", "yesterday", "their"} {
		if words[noise] {
			t.Errorf("%q is dimension/period noise and must not score a source", noise)
		}
	}
	if !words["milking"] || !words["missed"] {
		t.Errorf("the question's own subject words were dropped: %v", words)
	}
}

// The matcher must still nominate a source that really carries the question's
// words — this is the behaviour coverage.go exists for ("no rows" is not "not
// modelled"), and tightening it must not cost it.
func TestTheTightenedMatcherStillNominatesARealCoveringView(t *testing.T) {
	covering := coveringSources(
		"what weight did we record for each animal weighed this month",
		reporting.Cards(), heldOutCatalogue())
	if len(covering) == 0 {
		t.Fatal("a weight question nominated nothing, so its refusal would stand unchecked")
	}
}

// THE CIRCLE, PINNED AS AN INVARIANT. measureUnmodelled used to end with
// `if len(coveringSources(...)) > 0 { return false, nil }` — the SAME predicate,
// on the SAME inputs, that the orchestrator's refusal override fires on. So the
// override could never run beside a live unmodelled-measure gate: enabling one
// switched the other off by construction.
//
// That tail is gone. This pins the property that makes its absence safe and
// stops the coupling creeping back through the matcher: a source may be
// nominated ONLY for words the catalogue genuinely models. While that holds,
// coveringSources can never contradict modelledTerms, so the deleted tail could
// only ever have re-admitted a nomination scored on dimension-column noise.
//
// Mutation: restore the strings.Contains substring bar in distinctStemHits, or
// score coveringViews/coveringTools on questionWords again, and this goes red —
// a source is nominated for a question none of whose terms is modelled, which is
// exactly the state the deleted tail turned into a silent fail-open.
func TestANominatedSourceAlwaysModelsOneOfTheQuestionsOwnTerms(t *testing.T) {
	cards := reporting.Cards()
	catalog := heldOutCatalogue()
	for _, question := range []string{
		"which sheds missed their milking session yesterday",
		"how much colostrum did each shed dispense yesterday",
		"Kids on milk feeding per park today (head count)",
		"how many treatment sessions were missed yesterday",
		"what is the milk fat percentage by breed of cow",
		"what were our sales revenue and deals last month",
		"what weight did we record for each animal weighed this month",
	} {
		covering := coveringSources(question, cards, catalog)
		if len(covering) == 0 {
			continue
		}
		if len(modelledTerms(measureTerms(question), cards, catalog)) == 0 {
			t.Errorf("%q nominated %v while the catalogue models none of its terms — "+
				"the refusal override would fire on a source scored from noise", question, covering)
		}
	}
}
