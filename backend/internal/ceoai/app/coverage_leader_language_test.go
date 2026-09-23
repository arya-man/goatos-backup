package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// THE GENERATED LEADER-LANGUAGE SWEEP, and it exists because every hand-written
// table in this package was written by the same author who had just written the
// matcher, in the same English, on the same afternoon. Six review rounds passed
// while 26 of 76 ordinary invented leader questions nominated NOTHING, and the
// reason is visible in one grep: the 127-question golden set contained ZERO
// `n't` contractions. Nobody was hiding the defect; nobody had typed the word.
//
// So this file does not hold a list of the questions that failed. A hand-copy
// of those 26 would pin exactly the 26 and nothing else, and the 27th would
// ship the same way. Every probe below is GENERATED -- from the golden set, from
// the sweep tables, from the leader fold itself, and from templates crossed with
// nouns -- so a phrasing nobody thought of is covered by construction.
//
// Both directions are asserted, always together, because a fix for either one
// alone is exactly how this function has oscillated across four review rounds.

// contractions are the mechanical rewrites that turn an expanded English
// question into the one a leader actually types. The list is GRAMMAR -- it
// names no farm word -- and it is applied to questions written by other people
// (the golden set, the sweep tables), never to a question written here.
var contractions = [][2]string{
	{"is not", "isn't"}, {"are not", "aren't"}, {"was not", "wasn't"}, {"were not", "weren't"},
	{"do not", "don't"}, {"does not", "doesn't"}, {"did not", "didn't"},
	{"have not", "haven't"}, {"has not", "hasn't"}, {"had not", "hadn't"},
	{"will not", "won't"}, {"can not", "can't"}, {"cannot", "can't"},
	{"what is", "what's"}, {"who is", "who's"}, {"that is", "that's"},
	{"there is", "there's"}, {"it is", "it's"}, {"how is", "how's"},
	{"we are", "we're"}, {"they are", "they're"}, {"we have", "we've"},
	{"we will", "we'll"}, {"we would", "we'd"},
}

// contract returns the contracted form of a question, and whether contracting
// changed anything.
func contract(question string) (string, bool) {
	out := strings.ToLower(question)
	changed := false
	for _, c := range contractions {
		if strings.Contains(out, c[0]) {
			out = strings.ReplaceAll(out, c[0], c[1])
			changed = true
		}
	}
	return out, changed
}

func sortedSources(covering []string) []string {
	out := append([]string(nil), covering...)
	sort.Strings(out)
	return out
}

func sameSources(a, b []string) bool {
	x, y := sortedSources(a), sortedSources(b)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// TestContractedAndExpandedQuestionsAgree is the pin for the contraction
// defect, and it is generated from questions written elsewhere: every
// affirmative question in the golden set and in both nominate-direction sweep
// tables is contracted mechanically and must nominate EXACTLY what its expanded
// form nominates.
//
// It would have caught the shipped defect with no new question typed: "who has
// not paid us" nominated two sources and "who hasn't paid us" nominated none.
func TestContractedAndExpandedQuestionsAgree(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()

	var questions []string
	for _, tc := range sweepMustNominate {
		questions = append(questions, tc.question)
	}
	for _, tc := range sweepMustNominateInLeaderEnglish {
		questions = append(questions, tc.question)
	}
	questions = append(questions, goldenQuestions(t)...)

	generated := 0
	for _, q := range questions {
		contracted, changed := contract(q)
		if !changed {
			continue
		}
		generated++
		want := coveringSources(q, cards, catalog)
		got := coveringSources(contracted, cards, catalog)
		if !sameSources(want, got) {
			t.Errorf("CONTRACTION CHANGED THE ANSWER: %q nominated %v but %q nominated %v",
				q, sortedSources(want), contracted, sortedSources(got))
		}
	}
	if generated < 20 {
		t.Fatalf("only %d contracted variants were generated; the pin is the generation, so a source set that stops producing them is the failure", generated)
	}
}

// TestEveryContractionExpandsToAStopWord is the unit-level half: the clitic
// stripper is grammar, so `hasn't` must arrive as the ordinary word `has` and
// never as a token the catalogue can never name.
func TestEveryContractionExpandsToAStopWord(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"hasn't", "has"}, {"haven't", "have"}, {"aren't", "are"}, {"isn't", "is"},
		{"didn't", "did"}, {"doesn't", "does"}, {"what's", "what"}, {"who's", "who"},
		{"we're", "we"}, {"we've", "we"}, {"we'll", "we"}, {"we'd", "we"},
		{"won't", "will"}, {"can't", "can"},
	} {
		if got := expandContraction(tc.in); got != tc.want {
			t.Errorf("expandContraction(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// A word with no clitic is untouched -- the stripper must never eat a farm
	// word that happens to end in one of these letters.
	for _, w := range []string{"animals", "buyers", "sheds", "revenue", "loads", "vaccines"} {
		if got := expandContraction(w); got != w {
			t.Errorf("expandContraction(%q) = %q; a plural is not a contraction", w, got)
		}
	}
}

// TestEveryLeaderWordNominatesOnItsOwn is generated from the fold itself. Each
// entry of leaderNouns is a word a leader really types for something the farm
// really records, so asked on its own it must reach a source. This is what
// catches the class where a fold exists but the word never survives to be
// scored -- `buy` was dropped by the four-letter floor, so "what did we buy
// this week" produced an EMPTY word set and bailed before it was scored.
func TestEveryLeaderWordNominatesOnItsOwn(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()
	for leader := range leaderNouns {
		if covering := coveringSources(leader, cards, catalog); len(covering) == 0 {
			t.Errorf("the fold carries %q but the word on its own nominates NOTHING -- it is folded and then dropped before it can score",
				leader)
		}
	}
}

// TestEveryRecordedDimensionNominatesOnItsOwn is the single-noun half, and it
// is the one the axis rule broke: "sheds" and "parks" nominated NOTHING while
// shed_capacity_current and vaccination_shed_status sit in the catalogue,
// because axisDimensions suppresses a breakdown-only match. That suppression is
// right when the question HAS another subject and wrong when the breakdown IS
// the subject.
func TestEveryRecordedDimensionNominatesOnItsOwn(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()
	vocab := catalogueVocabulary(cards, catalog)
	for noun := range dimensionNouns {
		// Period words (day/week/month/year/date) and `farm` carry no subject
		// and are dropped before scoring on purpose; a dimension the catalogue
		// does not name under this exact spelling cannot be asserted either.
		if len(noun) < 4 || questionStopWords[noun] || nonMeasureWords[noun] || coverageNoiseWords[noun] {
			continue
		}
		if vocab[coverageStem(noun)] == 0 {
			continue
		}
		if covering := coveringSources(noun, cards, catalog); len(covering) == 0 {
			t.Errorf("%q is a dimension the farm records and asked on its own it nominates NOTHING", noun)
		}
	}
}

// foreignSubjects are subjects a goat farm could plausibly have and THIS farm
// records nothing about. None of them is a word the catalogue names.
var foreignSubjects = []string{
	"chicken", "chickens", "loan", "mortgage", "payroll", "salary", "tax", "insurance",
	"rainfall", "borewell", "electricity", "diesel", "wifi", "website", "instagram",
	"tractor", "fencing", "solar", "warehouse", "trademark", "lawsuit", "investor",
	"landlord", "auditor", "broker", "colostrum", "embryo", "semen", "cheese", "wool",
}

// foreignTemplates are the shapes a leader asks a question in. Crossed with the
// subjects above they generate a large out-of-sample direction-B sweep that no
// tune for direction A can have been written against.
var foreignTemplates = []string{
	"how many %s do we have",
	"what is our %s cost",
	"how much did we spend on %s",
	"what is the %s rate on our %s",
	"who handles our %s",
	"what is the status of our %s",
	"how much %s did we get last month",
	"show me the %s report",
}

// TestGeneratedForeignScopeQuestionsNominateNothing is direction B, generated.
// The seventeen foreign-scope probes the review measured (weather, the
// Bangalore office, the stock market, wifi, Instagram, rainfall, cash runway,
// land rent, payroll, the board meeting) all nominate nothing today, and the
// fix must not buy its coverage by reopening them -- restoring the name arm
// under `strict` is exactly what caused round 5's false nominations.
func TestGeneratedForeignScopeQuestionsNominateNothing(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()
	probes := 0
	for _, tmpl := range foreignTemplates {
		for _, subject := range foreignSubjects {
			q := strings.ReplaceAll(tmpl, "%s", subject)
			probes++
			if covering := coveringSources(q, cards, catalog); len(covering) != 0 {
				t.Errorf("FALSE NOMINATION: %q nominated %v -- the farm records nothing about %q, so the planner's refusal is the honest answer",
					q, covering, subject)
			}
		}
	}
	if probes < 200 {
		t.Fatalf("only %d generated foreign probes; the pin is the cross product, not a sample", probes)
	}
}

// TestTypoedQuestionsDoNotBecomeForeignScope is the third generated shape. A
// leader types fast, and a misspelling is a word the catalogue does not name --
// which under the old rule was POSITIVE PROOF that the question was about
// another company, so one slip killed a fully covered question. A typo must
// degrade the answer at worst; it must never flip the question's scope.
func TestTypoedQuestionsDoNotBecomeForeignScope(t *testing.T) {
	cards, catalog := reporting.Cards(), liveCatalogue()
	for _, tc := range []struct{ typoed, want string }{
		{"how many animals are sik", "animal"},
		{"how many animals are hevy", "animal"},
		{"which buyers still owe us monie", "buyer"},
		{"which loads arrivd yesterday", "load"},
		{"how many vaccines did we giv", "vaccin"},
		{"how many animals do we hav", "animal"},
	} {
		covering := coveringSources(tc.typoed, cards, catalog)
		if len(covering) == 0 {
			t.Errorf("A TYPO FLIPPED THE SCOPE: %q nominated nothing; a misspelled word is not evidence that the question is about another company",
				tc.typoed)
			continue
		}
		if !namesSource(covering, tc.want) {
			t.Errorf("%q nominated %v, expected a source naming %q", tc.typoed, covering, tc.want)
		}
	}
}

// goldenQuestions reads every question out of the committed golden set. It is
// the largest body of leader English in the repo that was NOT written against
// this matcher, which is exactly what makes it a good generator.
func goldenQuestions(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "..", "tools", "ceo-ai", "eval", "golden")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read golden dir: %v", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		var rows []struct {
			Class    string `json:"class"`
			Question string `json:"question"`
		}
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, r := range rows {
			// Refusal questions are deliberately about things the farm does not
			// do; contracting one asserts nothing about coverage.
			if strings.HasPrefix(r.Class, "refusal") {
				continue
			}
			out = append(out, r.Question)
		}
	}
	if len(out) < 80 {
		t.Fatalf("only %d golden questions found at %s; the generator must read the real set", len(out), dir)
	}
	return out
}

// TestTheGoldenSetCarriesContractedAndVerbOnlyPhrasings is the guard on the
// generator's input. The contraction defect survived six rounds because the
// golden set contained no `n't` at all: a matcher is only tested by the English
// it is shown.
func TestTheGoldenSetCarriesContractedAndVerbOnlyPhrasings(t *testing.T) {
	contracted := 0
	for _, q := range goldenQuestions(t) {
		// `n't` ONLY. A possessive apostrophe-s ("Castro 1's vaccination") is
		// not a contraction and was already in the set while the defect shipped.
		if strings.Contains(strings.ToLower(q), "n't") {
			contracted++
		}
	}
	if contracted < 3 {
		t.Errorf("the golden set carries only %d contracted questions; it carried ZERO when this defect shipped, and a set with none cannot see it", contracted)
	}
}
