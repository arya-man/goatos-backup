package app

import (
	"sort"
	"strings"

	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// "No rows" is not "not modelled", and the difference matters more as the farm
// fills in: on the held-out run the assistant told the CEO that buyer names,
// price per kg and payment state are not tracked, while `sales_deals` carries
// buyer_name, total_weight_kg and payment_received — the table was simply
// EMPTY. That answer is wrong today and stays wrong the day the first sale is
// recorded, because nothing about it depends on the data.
//
// The schema cards already say what the farm records. Before a planner refusal
// that claims the records do not exist is passed to a leader, the cards are
// asked the same question: if a view's own name or columns carry the words the
// question used, the refusal is not accepted at face value.

// notTrackedRefusal reports a refusal whose CLAIM is that the data is not
// recorded, as opposed to one about scope, safety or an unanswerable question.
// Only this shape is re-checked — a tenant-scope refusal must stand.
func notTrackedRefusal(refusal string) bool {
	low := strings.ToLower(refusal)
	for _, claim := range []string{
		"not tracked", "doesn't track", "does not track", "isn't tracked", "is not tracked",
		"not recorded", "doesn't record", "does not record", "not captured", "not stored",
		"no data on", "not available in", "doesn't have", "does not have", "not modelled",
		"not modeled", "cannot be answered from", "can't be answered from",
	} {
		if strings.Contains(low, claim) {
			return true
		}
	}
	return false
}

// coveringSources names every READ the catalogue offers whose own name, columns
// or description carry the question's words — the ceo_ai views AND the curated
// tools, because a subject the farm records through a read API (sales is one)
// has no schema card at all and would otherwise look unmodelled.
func coveringSources(questionText string, cards []reporting.SchemaCard, catalog []ports.ToolSpec) []string {
	var named []string
	for _, card := range coveringViews(questionText, cards) {
		named = append(named, "ceo_ai."+card.Name+" ("+strings.Join(columnNames(card), ", ")+")")
	}
	named = append(named, coveringTools(questionText, catalog)...)
	const maxNamed = 4
	if len(named) > maxNamed {
		named = named[:maxNamed]
	}
	return named
}

// coveringTools scores catalogue tools the same way the views are scored.
func coveringTools(questionText string, catalog []ports.ToolSpec) []string {
	words := coverageWords(questionText)
	if len(words) == 0 {
		return nil
	}
	type scored struct {
		name  string
		score int
	}
	var hits []scored
	for _, spec := range catalog {
		// A tool's NAME is what it is about; its description and params are the
		// rest of its vocabulary. They are scored separately for the same reason
		// a view's name is scored separately from its columns.
		if score, ok := nominates(words,
			identifierHaystack(spec.Name),
			identifierHaystack(spec.Name+" "+spec.Description+" "+strings.Join(spec.Params, " "))); ok {
			hits = append(hits, scored{name: spec.Name, score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].name < hits[j].name
	})
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, "the "+h.name+" read")
	}
	return out
}

// coveringViews returns the ceo_ai views whose own name or columns carry the
// question's words, best first. It is a CAPABILITY check over the catalog, not
// a per-topic list: a view added tomorrow participates without a code change.
func coveringViews(questionText string, cards []reporting.SchemaCard) []reporting.SchemaCard {
	words := coverageWords(questionText)
	if len(words) == 0 {
		return nil
	}
	type scored struct {
		card  reporting.SchemaCard
		score int
	}
	var hits []scored
	for _, card := range cards {
		if score, ok := nominates(words,
			identifierHaystack(card.Name),
			identifierHaystack(card.Name+" "+strings.Join(columnNames(card), " "))); ok {
			hits = append(hits, scored{card: card, score: score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].card.Name < hits[j].card.Name
	})
	const maxNamed = 3
	if len(hits) > maxNamed {
		hits = hits[:maxNamed]
	}
	out := make([]reporting.SchemaCard, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.card)
	}
	return out
}

// distinctStemHits counts how many INDEPENDENT subject words of the question a
// source carries. questionWords deliberately adds a crude singular beside every
// plural, so "sessions" arrives as both "sessions" and "session" -- and a source
// with one incidental `planned_sessions` column then scored 2 and satisfied the
// "two independent words" rule on its own. That is how "how many treatment
// sessions were missed yesterday" nominated the vaccination-shed view and
// disarmed the refusal for a subject the farm's read models do not carry at all.
// Counting by stem restores what the rule always meant.
// stemHits are the question stems a source's vocabulary carries, deduplicated.
// questionWords deliberately adds a crude singular beside every plural, so
// "sessions" arrives as both "sessions" and "session" -- and a source with one
// incidental `planned_sessions` column then counted 2 and satisfied the "two
// independent words" rule on its own. Counting by stem restores what that rule
// always meant.
func stemHits(words map[string]bool, have map[string]bool) []string {
	seen := map[string]bool{}
	for w := range words {
		if have[wordStem(w)] {
			seen[wordStem(w)] = true
		}
	}
	out := make([]string, 0, len(seen))
	for stem := range seen {
		out = append(out, stem)
	}
	sort.Strings(out)
	return out
}

// identifierHaystack is a source's own vocabulary, one stem per identifier word.
//
// THE MATCH IS ANCHORED, and that is the point of this function. The bar was an
// unanchored strings.Contains over `name + column names`, so a question word
// scored on any SUBSTRING of any identifier: "session" landed inside
// `session_no`, "plan" inside `planned_sessions`, "manage" inside
// `manager_label`. Matching whole identifier words on their stems nominates a
// source only for words it actually names.
func identifierHaystack(text string) map[string]bool {
	stems := map[string]bool{}
	flat := strings.Map(func(r rune) rune {
		switch r {
		case '_', '/', '(', ')', ',', ';', ':', '-', '.', '"', '\'':
			return ' '
		}
		return r
	}, strings.ToLower(text))
	for _, w := range strings.Fields(flat) {
		if len(w) < 4 {
			continue
		}
		stems[wordStem(w)] = true
	}
	return stems
}

// coverageWords is the question's own vocabulary, scored against a source.
//
// IT DOES NOT DROP THE DIMENSION NOUNS, and an earlier version of this file
// did -- which broke the very thing coverage.go exists for. `dimensionNouns`
// holds buyer, vendor, vaccine, disease, load, session, breed, operator,
// species and status, so routing coverage scoring through measureTerms left
// "who are our top buyers this month", "how many vaccine doses did we use" and
// "how many animals are in each shed" with NO words at all: nothing was
// nominated, the "not tracked" override could not fire, and the CEO was told
// the farm does not record its own buyers -- the held-out defect described at
// the top of this file, re-opened.
//
// A dimension noun IS the subject of plenty of real questions. What it must not
// do is nominate a source ON ITS OWN, beside another dimension noun, while the
// question's real subject participates in nothing: that is how "which sheds
// missed their milking session yesterday" reached feed_direction_current on
// `shed_label` + `session_no` with neither "milking" nor "missed" scoring.
// `nominates` below draws that line by POSITION IN THE MATCH rather than by
// deleting the word from the question.
//
// What is dropped here is only what says nothing about subject anywhere: the
// period/quantifier vocabulary (nonMeasureWords) and the structural column
// nouns (coverageNoiseWords).
func coverageWords(questionText string) map[string]bool {
	out := map[string]bool{}
	for w := range questionWords(questionText) {
		if nonMeasureWords[w] || coverageNoiseWords[w] {
			continue
		}
		out[w] = true
	}
	return out
}

// axisDimensions are the dimension words that are ONLY ever a breakdown axis:
// a question is answered BY a measure and sliced BY a park, a pen or a period.
// The farm always has pens, so "shed" naming a view proves nothing about
// whether that view answers the question -- vaccination_shed_status and
// shed_capacity_current both carry it.
//
// Every OTHER dimension noun (buyer, vendor, load, vaccine, disease, item,
// breed, operator, stage, session, status) is a thing the farm RECORDS, and a
// question can be entirely about one: "who are our top buyers", "how many loads
// arrived yesterday". For those, a view whose own NAME carries the word IS the
// source, and refusing to nominate it is how the CEO got told the farm does not
// record its own buyers.
var axisDimensions = map[string]bool{
	"park": true, "pen": true,
	"day": true, "week": true, "month": true, "year": true, "quarter": true,
}

// isAxisWord reports that a question word is a pure breakdown axis.
func isAxisWord(stem string) bool {
	canonical, isDimension := dimensionNouns[stem]
	if !isDimension {
		return false
	}
	return axisDimensions[canonical]
}

// isSubjectWord reports that a word can be what a question is ABOUT, rather
// than only how it is sliced.
func isSubjectWord(stem string) bool {
	_, isDimension := dimensionNouns[stem]
	return !isDimension
}

// nominates decides whether one source covers the question, and returns the
// score used to rank it. nameHay is the source's own name; fullHay is the name
// plus its columns (a view) or its description and params (a tool).
//
// Two ways in:
//
//   - ONE of the question's SUBJECT words -- anything outside the dimension
//     vocabulary -- appears anywhere on the source. One is enough: "what was our
//     revenue last month" carries exactly one such word, and `revenue_rupees` is
//     the column that answers it. Requiring two made every single-subject
//     question structurally unreachable, which is most of them.
//   - the source's NAME carries a dimension word that is NOT a pure axis --
//     sales_buyer_summary for a buyer question, procurement_loads_base for a
//     load question. The source is named after the thing being asked about.
//
// What is deliberately NOT a way in: a match that is only axis/dimension words
// on COLUMNS. Every view carries `shed_label` and half carry `session_no`, so
// "which sheds missed their milking session yesterday" matched feed views on
// nothing but its breakdown, with neither "milking" nor "missed" participating,
// and coverageFeedback then told the model never to say the farm does not
// record it. That question still nominates nothing.
func nominates(words map[string]bool, nameHay, fullHay map[string]bool) (int, bool) {
	all := stemHits(words, fullHay)
	name := stemHits(words, nameHay)
	score := len(all) + len(name)
	for _, stem := range all {
		if isSubjectWord(stem) {
			return score, true
		}
	}
	for _, stem := range name {
		if !isAxisWord(stem) {
			return score, true
		}
	}
	return 0, false
}

// coverageNoiseWords are column-shaped words that say nothing about WHAT a
// question is about. They are deliberately a COVERAGE-ONLY list: the honesty
// gates still read these words, because "treatment sessions" is a compound
// subject even where "session" may not nominate a view on its own.
var coverageNoiseWords = map[string]bool{
	"label": true, "labels": true, "name": true, "names": true,
	"workflow": true, "workflows": true, "blocked": true, "block": true,
	"reason": true, "reasons": true, "note": true, "notes": true,
	"scope": true, "scopes": true, "detail": true, "details": true,
	"entry": true, "entries": true, "record": true, "records": true,
	"row": true, "rows": true, "data": true, "info": true, "information": true,
}

// wordStem folds a plural onto its singular so the two forms of one word count
// once. It mirrors the singularization questionWords applies.
func wordStem(w string) string {
	// FOUR-LETTER PLURALS FOLD TOO. `kids` is the word a leader uses and `kid`
	// is the word the schema uses (mortality_base.kid_deaths, the counts tool's
	// "kids/adults"); with the old `len(w) > 4` bar the two never met and "how
	// many kids do we have" nominated nothing. Both sides of every comparison
	// run through this function, so folding one letter earlier keeps them
	// agreeing with each other.
	if strings.HasSuffix(w, "s") && len(w) >= 4 {
		return strings.TrimSuffix(w, "s")
	}
	// A GERUND FOLDS ONTO ITS VERB, but only when a real word is left: a leader
	// asks which items need "reordering" and the column is `reorder_flag`. The
	// five-character floor is what keeps it honest -- "milking" would fold to
	// "milk" and start matching a milk column that does not exist, so it stays
	// whole. Both sides of every comparison run through this function.
	if strings.HasSuffix(w, "ing") && len(w)-3 >= 5 {
		return strings.TrimSuffix(w, "ing")
	}
	return w
}

// coverageFeedback is the re-plan instruction: it names the sources and says
// plainly that an EMPTY source is still an answerable question.
func coverageFeedback(sources []string) string {
	var b strings.Builder
	b.WriteString("You refused because the data is not recorded, but these sources carry it: ")
	b.WriteString(strings.Join(sources, "; "))
	b.WriteString(". Plan a read over the one that fits. A source with no rows yet is still the right answer — report that nothing is recorded for the period, never that the farm does not record it.")
	return b.String()
}

// columnNames is the card's column names, which is what a question's words are
// matched against.
func columnNames(card reporting.SchemaCard) []string {
	names := make([]string, 0, len(card.Columns))
	for _, c := range card.Columns {
		names = append(names, c.Name)
	}
	return names
}

// questionStopWords are words that carry no subject, so they must not nominate
// a view on their own.
var questionStopWords = map[string]bool{
	"what": true, "which": true, "have": true, "this": true, "that": true, "with": true,
	"from": true, "they": true, "them": true, "there": true, "many": true, "much": true,
	"show": true, "give": true, "tell": true, "does": true, "doing": true, "about": true,
	"into": true, "over": true, "been": true, "were": true, "will": true, "when": true,
	"where": true, "your": true, "ours": true, "each": true, "every": true, "last": true,
	"month": true, "week": true, "today": true, "date": true, "days": true, "year": true,
	"farm": true, "park": true, "please": true, "current": true, "total": true,
}

// questionWords is the set of subject-bearing words a question used.
func questionWords(text string) map[string]bool {
	words := map[string]bool{}
	for _, raw := range strings.Fields(strings.ToLower(text)) {
		w := strings.Trim(raw, ".,;:?!()'\"")
		if len(w) < 4 || questionStopWords[w] {
			continue
		}
		words[w] = true
		// Crude singular so "sales"/"sale", "buyers"/"buyer" meet the column
		// names, which are singular about as often as they are plural.
		if strings.HasSuffix(w, "s") && len(w) > 4 {
			words[strings.TrimSuffix(w, "s")] = true
		}
	}
	return words
}
