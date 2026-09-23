package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// A DATE IS NOT A SUBJECT. Every probe in this file is a question whose ONLY
// unusual word is a calendar word -- a month name, a weekday, a year, a day
// number -- and every one of them nominated NOTHING before the period-word
// rule landed, while the same question asked with "last month" or "yesterday"
// nominated two or four sources. The cause was not that anybody disliked
// August: a month name is a noun, it sits in a noun position ("in August",
// "for September", "on 18 September 2026"), no card and no tool is named after
// it, and the polarity-inversion rule therefore read it as evidence the
// question was about ANOTHER company's records and closed both arms of
// nominates.
//
// The month phrasings are GENERATED from templates crossed with the whole
// calendar rather than hand-copied, so the thirteenth phrasing nobody typed is
// covered by construction -- and the foreign-scope probes at the bottom run in
// the same test, because "make August work" is trivially achieved by deleting
// the strictness rule and that must stay impossible.

// periodTemplates are the shapes a leader puts a date into. %s takes a
// calendar word.
var periodTemplates = []string{
	"what was our revenue in %s",
	"what was our revenue for %s",
	"what was our revenue during %s",
	"how much revenue did we make in %s",
	"how many animals did we sell in %s",
	"how many goats did we weigh in %s",
	"revenue for %s please",
}

// calendarWords are the spellings the templates are crossed with: full month
// names, the abbreviations a leader types, weekday names, quarters and a bare
// year. The list is the CLOSED calendar of English, not a sample of it.
var calendarWords = []string{
	"January", "February", "March", "April", "May", "June",
	"July", "August", "September", "October", "November", "December",
	"jan", "feb", "mar", "apr", "jun", "jul", "aug", "sep", "sept", "oct", "nov", "dec",
	"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday",
	"Q1", "Q2", "Q3", "Q4", "2026",
}

// namedPeriodQuestions are the phrasings a template cannot express: a month
// with a year beside it, a date range, a bare numeric day, and the branch's
// own golden entry `pf-feed-directed-cbe-named-day`, which nominated NOTHING.
var namedPeriodQuestions = []string{
	"what was our revenue in August 2026",
	"what was our revenue in 2026",
	"what was our revenue on Monday",
	"what was our revenue between 1 August and 31 August",
	"what was our revenue on 18 September 2026",
	"what was our revenue on 18th September",
	"August revenue",
	"How much feed was directed to Coimbatore on 18 September 2026, in kg?",
}

// relativePeriodQuestions already worked, because `yesterday` and `last month`
// happen to sit in questionStopWords. They are asserted beside the named ones
// so a regression on either side is one failure, not two files apart.
var relativePeriodQuestions = []string{
	"what was our revenue last month",
	"how many animals did we sell last month",
	"what was our revenue yesterday",
	"what was our revenue since 1 August 2026",
}

// participlePhraseQuestions are the noun phrases where a PAST PARTICIPLE sits
// between a determiner and a modelled head noun. "list the roles that have a
// named backup" nominated NOTHING though both `roles` and `backup` are
// modelled, because `named` was taken to be the head noun of "a named backup"
// and no card is called `named`.
var participlePhraseQuestions = []string{
	"list the roles that have a named backup",
	"which roles have a named backup",
}

// foreignScopeQuestions are the direction-B half and they are the reason this
// test cannot be satisfied by relaxing strictness: each names a subject this
// farm records NOTHING about, and each must keep nominating nothing at all.
var foreignScopeQuestions = []string{
	"what is the weather forecast in August",
	"how many people work in our Bangalore office",
	"how did the stock market do in August",
	"is the wifi in the office working",
	"what is the price of bitcoin",
	"who won the cricket match on Monday",
	"what did our competitor charge in August",
	"what did our lawyer say about the contract",
	"please book me a flight to Delhi in August",
}

func TestNamedPeriodQuestionsNominate(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()

	generated := 0
	for _, tmpl := range periodTemplates {
		for _, word := range calendarWords {
			q := fmt.Sprintf(tmpl, word)
			generated++
			if covering := coveringSources(q, cards, catalog); len(covering) == 0 {
				t.Errorf("A DATE IS NOT A SUBJECT: %q nominated NOTHING; the same question with `last month` nominates sources", q)
			}
		}
	}
	if generated < 200 {
		t.Fatalf("only %d period phrasings were generated; the pin IS the generation", generated)
	}

	for _, q := range append(append([]string(nil), namedPeriodQuestions...), relativePeriodQuestions...) {
		if covering := coveringSources(q, cards, catalog); len(covering) == 0 {
			t.Errorf("%q nominated NOTHING", q)
		}
	}
}

func TestParticipleBeforeModelledNounNominates(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()
	for _, q := range participlePhraseQuestions {
		if covering := coveringSources(q, cards, catalog); len(covering) == 0 {
			t.Errorf("A PARTICIPLE IS NOT THE HEAD NOUN: %q nominated NOTHING though its head noun is modelled", q)
		}
	}
}

// TestPeriodAndParticipleFixesKeepForeignScopeOut is the direction-B guard.
// It runs the SAME catalogue and must report zero sources for every question
// about something the farm does not record -- including the ones that carry a
// month name, which is the exact shape a lazy fix would let through.
func TestPeriodAndParticipleFixesKeepForeignScopeOut(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()
	for _, q := range foreignScopeQuestions {
		if covering := coveringSources(q, cards, catalog); len(covering) != 0 {
			t.Errorf("FOREIGN SCOPE LEAKED: %q nominated %v; the farm records nothing about it", q, sortedSources(covering))
		}
	}
}

// TestPeriodWordVocabularyIsTheWholeCalendar is the unit-level half: a month
// abbreviation, a weekday, a quarter, a bare year and a day number are period
// words, and a farm noun that merely looks like one is NOT.
func TestPeriodWordVocabularyIsTheWholeCalendar(t *testing.T) {
	for _, w := range []string{
		"january", "august", "december", "jan", "aug", "sept", "dec",
		"monday", "sunday", "q1", "q4", "2026", "1999", "18", "18th", "1st", "31",
	} {
		if !isPeriodWord(w) {
			t.Errorf("%q is a calendar word and isPeriodWord said it is a subject", w)
		}
	}
	for _, w := range []string{
		"revenue", "animals", "goats", "feed", "weighing", "backup", "roles",
		"march2026", "32", "99999", "may1", "augustus",
	} {
		if isPeriodWord(w) {
			t.Errorf("%q is not a calendar word and isPeriodWord swallowed it -- a subject read as a date can never be evidence again", w)
		}
	}
}

// TestParticipleIsSkippedOnlyBeforeAModelledNoun pins the NARROWNESS of the
// participle rule. "a named backup" hands back `backup` because `named` is a
// past participle and the catalogue names `backup`; "their milking session"
// still hands back `milking`, because an -ing word after a determiner is a
// GERUND and is itself the noun -- which is the case determinerHeadedNouns
// exists for and must not be relaxed by this fix.
func TestParticipleIsSkippedOnlyBeforeAModelledNoun(t *testing.T) {
	vocab := catalogueVocabulary(reporting.Cards(), liveCatalogue())
	if vocab[coverageStem("backup")] == 0 {
		t.Fatalf("the catalogue no longer names `backup`; this probe needs a modelled head noun")
	}
	if isParticipleForm("milking") {
		t.Errorf("an -ing gerund must not be skipped as a participle")
	}

	heads := determinerHeadedNouns("list the roles that have a named backup", vocab)
	if heads["named"] || !heads["backup"] {
		t.Errorf("`a named backup` should be headed by `backup`, got %v", heads)
	}
	heads = determinerHeadedNouns("which sheds missed their milking session yesterday", vocab)
	if !heads["milking"] {
		t.Errorf("`their milking session` has no modelled head, so `milking` must stay the head noun, got %v", heads)
	}
}

// TestPolitenessWordsAreNotSubjects: `please` was already a stop word and
// `kindly`/`pls` are the same word in a leader's English.
func TestPolitenessWordsAreNotSubjects(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()
	for _, tmpl := range []string{"%s show me our revenue", "%s tell me how many animals we have", "%s share our revenue for August"} {
		for _, polite := range []string{"please", "kindly", "pls"} {
			q := fmt.Sprintf(tmpl, polite)
			if covering := coveringSources(q, cards, catalog); len(covering) == 0 {
				t.Errorf("politeness is not a subject: %q nominated NOTHING", q)
			}
		}
	}
	for _, w := range []string{"kindly", "pls", "plz", "please"} {
		if !questionStopWords[w] {
			t.Errorf("%q must carry no subject", w)
		}
		if strings.TrimSpace(w) == "" {
			t.Fatal("empty politeness word")
		}
	}
}
