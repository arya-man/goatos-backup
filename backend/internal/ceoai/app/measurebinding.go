package app

import (
	"regexp"
	"sort"
	"strings"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// A NUMBER FROM A NEIGHBOURING SOURCE, CONFIDENTLY LABELLED, is the one failure
// a leader cannot catch. Asked how much milk the farm produced today, the
// assistant answered "CBE 24, CPT 24" -- two real ANIMAL counts read from the
// animal-scope view, wearing the milk question's words. Asked how many
// treatment sessions were missed, it counted workforce task rows. Asked how
// many vaccination obligations were COMPLETED, it reported the count of
// obligations DUE. Each answer is grounded (the number is really in the
// evidence), really scoped to the tenant, and really wrong.
//
// Grounding cannot see this: the number IS in the rows. The label cannot be
// trusted either, because the model writes it -- an alias reading "Missed
// treatment sessions" proves only that the model typed those words. The one
// thing the model does not author is WHICH COLUMNS THE READ TOUCHED, so that is
// what the measure is bound to: a figure may be presented as the answer to a
// question only when the read it came from actually read a column that models
// what the question asked about.
//
// Fail closed, in two directions:
//
//   - Nothing in the catalogue models the asked measure -> REFUSE and say the
//     farm does not track it, instead of answering from whatever is nearest.
//   - Something models it but the read that ran did not touch it -> a fit issue,
//     which takes the ordinary one re-plan and, if it still does not bind, is
//     flagged and downgraded to partial rather than presented as the answer.

// measureBindingIssues reports that no executed read touched a column modelling
// the measure the question asked about, while the catalogue does model it.
func measureBindingIssues(q domain.Question, subs []domain.SubQuestion, results []domain.ToolResult, cards []reporting.SchemaCard, catalog []ports.ToolSpec) []FitIssue {
	terms := measureTerms(q.Text)
	if len(terms) == 0 {
		return nil
	}
	// Only a measure the farm DOES model can be mis-bound; one it does not
	// model at all is the refusal case below, not a re-plan.
	modelled := modelledTerms(terms, cards, catalog)
	if len(modelled) == 0 {
		return nil
	}
	// ONLY a read whose VIEW is known can be judged. A curated Cube or API tool
	// reports what its own implementation reports, and neither its name nor its
	// catalogue sentence is a column list -- guessing from them would flag a
	// correct read as a misfit. A SQL read over a schema card is the case where
	// the columns touched are knowable, and it is the case the held-out failures
	// came from.
	// THE SHARPEST FORM OF THE CHECK, and the only one that does not guess: a
	// term is held against a read ONLY when the card that ran HAS a column for
	// it. "How many vaccination obligations were COMPLETED" ran over the
	// vaccination-obligations view -- which carries completed_business_day --
	// and aggregated the DUE column instead, so the word the card itself models
	// went untouched. A word the card has no column for says nothing about that
	// read: the answer may still be wrong, but this evidence cannot show it,
	// and flagging on it would fail every question carrying an adjective.
	// A BLUNTER RULE WAS TRIED HERE AND MEASURED, AND IT IS RECORDED BECAUSE IT
	// LOOKS RIGHT: flag the read when the card that ran models NOTHING the
	// question asked about, which is exactly the shape of the milk question
	// answered "CBE 24, CPT 24" from the animal-scope view. On the held-out set
	// it also flagged three answers that were CORRECT -- "what's the current
	// headcount per park?" and "how many kids vs adults are there in each park",
	// both answered rightly by counting rows of that same animal-scope view.
	//
	// The reason is worth keeping: a COUNT(*) has no column to bind to. The
	// measure IS the row count, and the subject is carried by a filter VALUE
	// ('K2') rather than by any column name, so a correct read of that shape can
	// never satisfy a column-level binding test. "Kids on milk feeding per park"
	// and "kids vs adults per park" produce almost the same SQL over the same
	// view; the only thing separating them is the word "milk", which nothing in
	// the catalogue models -- so the discriminator lives in the REFUSAL path,
	// in subjectSubstitution below, and not here.
	//
	// A VALUE VOCABULARY on the cards (management_stage 'K2' = "kids") was
	// proposed as the way to make that per-term refusal safe, and MEASURED
	// before it was built: it is not what separates the two questions. "kids"
	// and "adults" are already modelled -- the counts_breakdown tool's own
	// catalogue sentence names them -- so ct-05 was never at risk from a
	// per-term rule; what put every other held-out question at risk was that
	// ~25 of 43 carry SOME word no schema models ("percentage", "biggest",
	// "cases", "rounds"). The shape that separates them is the compound
	// SUBJECT, not the value dictionary, so the cards were left alone.
	// THE FLAG SET IS NOT A GLOBAL BAG. It used to be one map for the whole
	// plan with a `delete` on every satisfied term, so a SECOND sub-question
	// over a DIFFERENT card could clear a flag the first one raised -- the same
	// pooled-evidence defect review.go just fixed for grounding. A read binds
	// the measure for the question IT answered; another read over another card
	// is no evidence about it, and must not clear it. A term is now held
	// per (card that ran, term), and any card that carries the column and did
	// not read it raises the flag for good.
	judged := false
	unmet := map[string]bool{}
	for i := range results {
		var sub domain.SubQuestion
		if i < len(subs) {
			sub = subs[i]
		}
		card, ok := cardForResult(results[i], sub)
		if !ok {
			continue
		}
		judged = true
		for _, t := range modelled {
			if !cardHasColumnFor(card, t) {
				continue
			}
			if readTouchesAny(results[i], sub, []string{t}, card) {
				continue
			}
			unmet[t] = true
		}
	}
	if !judged || len(unmet) == 0 {
		return nil
	}
	missing := make([]string, 0, len(unmet))
	for t := range unmet {
		missing = append(missing, t)
	}
	sort.Strings(missing)
	return []FitIssue{{
		Kind:   "measure_binding",
		Detail: "the " + strings.Join(missing, " / ") + " you asked about (the read that ran does not report it)",
	}}
}

// measureUnmodelled reports that NOTHING in the catalogue -- no view column, no
// tool -- models any word of what the question asks about. The honest answer is
// that the farm does not record it; answering from the nearest source is how a
// milk question is answered with an animal count.
//
// It requires EVERY term to be unmodelled, never merely one: a question is full
// of words ("split", "right now") that no schema models and whose absence says
// nothing. One modelled term is enough to make the question answerable.
func measureUnmodelled(questionText string, cards []reporting.SchemaCard, catalog []ports.ToolSpec) (bool, []string) {
	terms := measureTerms(questionText)
	if len(terms) == 0 {
		return false, nil
	}
	if len(modelledTerms(terms, cards, catalog)) > 0 {
		return false, nil
	}
	// With no tool catalogue in hand this cannot be judged: the views are only
	// half of what the farm records, and refusing on half the evidence would
	// call a read API's own subject untracked.
	if len(catalog) == 0 {
		return false, nil
	}
	// THE COVERAGE MATCHER IS DELIBERATELY NOT CONSULTED HERE, and removing it
	// broke a circle. The refusal override in the orchestrator fires only when
	// coveringSources(...) is NON-EMPTY, and this check returned "not
	// unmodelled" on that SAME predicate over the SAME inputs -- so on every
	// path where a planner refusal was overridden, this gate was switched off by
	// construction, leaving only subjectSubstitution, which itself no-ops on a
	// read with no SourceView and on a compound whose both words are unmodelled.
	// A gate the caller disables by calling it is not a gate.
	//
	// Dropping it FAILS CLOSED, and adds nothing the checks above did not
	// already do: they require that NOTHING in the catalogue -- no view name, no
	// view column, no tool name, description or param -- models ANY of the
	// question's measure terms, and coveringSources scores over those same
	// strings. A source that genuinely covered the measure would have made
	// modelledTerms non-empty and returned above. What this call added was a
	// nomination scored on dimension-column noise, which is exactly the
	// laundering path the override was being used for.
	return true, terms
}

// subjectSubstitution catches the failure measureUnmodelled and the binding gate
// structurally cannot: "how many kids are on MILK FEEDING, by park", answered
// "CBE 24, CPT 24" from the animal-scope view -- the 48-goat park split of a
// COUNT(*), wearing the milk question's words.
//
// Why neither existing check sees it. The binding gate needs a COLUMN to bind
// the measure to, and a COUNT(*) has none: the measure IS the row count and the
// subject rides a FILTER VALUE (management_stage = 'K2'), so "kids per park"
// and "kids on milk feeding per park" produce almost the same SQL over the same
// view. measureUnmodelled needs EVERY term unmodelled, and "kids"/"feeding" are
// modelled elsewhere in the catalogue, so one unmodelled word never reaches it.
//
// The discriminator is the COMPOUND SUBJECT. "milk feeding" is two adjacent
// content words: nothing in the catalogue models "milk", while "feeding" is
// modelled -- by the FEED views, not by the animal-scope card that answered.
// So the question names a subject that (a) no source models and (b) the source
// that ran does not report either. That is a substituted number, and it is
// refused. "kids vs adults per park" has no such pair (both words are modelled,
// and the card that ran is the one that models them), so it still answers.
//
// Four narrowings, each measured against the held-out set and each load-bearing:
//
//   - ROWS WERE RETURNED. An empty read answered "No records found" is honest
//     whatever the question named -- nothing was substituted, so there is
//     nothing to refuse. This is what keeps the health-case and milk-session
//     questions, whose sources really are empty, answering as they do today.
//   - THE PARTNER IS MODELLED SOMEWHERE BUT NOT BY THE CARD THAT RAN. A card
//     that models the partner IS on the question's subject ("crowded sheds"
//     over the shed-capacity view, "preventive care tasks" over a task view),
//     so an unmodelled adjective beside it proves nothing.
//   - THE UNMODELLED WORD MUST NAME A SUBJECT. Comparatives and computations
//     ("biggest", "highest", "percentage", "shortfall", "compared") are how a
//     question asks for arithmetic, never what it asks about, and no schema
//     models them.
//   - A TOOL CATALOGUE IS IN HAND. With none, the views are half the evidence
//     and "modelled by nothing" cannot be judged (same reasoning as
//     measureUnmodelled).
func subjectSubstitution(questionText string, subs []domain.SubQuestion, results []domain.ToolResult, cards []reporting.SchemaCard, catalog []ports.ToolSpec) (bool, string, string) {
	if len(catalog) == 0 {
		return false, "", ""
	}
	pairs := compoundSubjects(questionText)
	if len(pairs) == 0 {
		return false, "", ""
	}
	for i := range results {
		var sub domain.SubQuestion
		if i < len(subs) {
			sub = subs[i]
		}
		if results[i].Err != nil || len(results[i].Facts) == 0 {
			continue
		}
		card, ok := cardForResult(results[i], sub)
		if !ok {
			continue
		}
		for _, p := range pairs {
			head, tail := p[0], p[1]
			var unmodelled, partner string
			switch {
			case !termModelled(head, cards, catalog) && termModelled(tail, cards, catalog):
				unmodelled, partner = head, tail
			case !termModelled(tail, cards, catalog) && termModelled(head, cards, catalog):
				unmodelled, partner = tail, head
			default:
				continue
			}
			if !namesASubject(unmodelled) {
				continue
			}
			if cardModels(card, unmodelled) || cardModels(card, partner) {
				continue
			}
			return true, head + " " + tail, card.Name
		}
	}
	return false, "", ""
}

// compoundSubjects are the question's adjacent content-word pairs, in the order
// the question said them. A subject the farm does not record is usually named by
// two words together ("milk feeding", "treatment sessions"), and it is the PAIR
// that makes one unmodelled word meaningful.
func compoundSubjects(questionText string) [][2]string {
	fields := strings.Fields(strings.ToLower(questionText))
	var out [][2]string
	for i := 0; i+1 < len(fields); i++ {
		a, b := contentWord(fields[i]), contentWord(fields[i+1])
		if a == "" || b == "" {
			continue
		}
		out = append(out, [2]string{a, b})
	}
	return out
}

// contentWord strips a question word down to the noun it carries, or returns ""
// when it carries none.
//
// CUTTING AT THE APOSTROPHE WAS WRONG, and it was wrong live. It was written
// for "what's" — cut at the quote and what is left is "what", a stop word — and
// it happens to be right for exactly that shape. On a NEGATIVE contraction it
// keeps the wrong half: "hasn't" becomes "hasn", which is four letters long and
// is not in any stop list, so it reads as a noun the farm might record. "who
// hasn't paid us" then produced the compound subject `hasn paid` and the leader
// was told, in those words, that there is no source for "hasn paid".
//
// The contraction is expanded rather than cut, through the same closed set of
// English clitics the coverage tokenizer uses, so "hasn't" arrives here as the
// ordinary stop word "has" and carries no subject at all. One expansion, two
// callers: a leader typing an apostrophe must not reach two different answers
// depending on which gate reads the word.
func contentWord(raw string) string {
	w := expandContraction(strings.Trim(raw, ".,;:?!()\"'“”"))
	if len(w) < 4 || questionStopWords[w] {
		return ""
	}
	return w
}

// namesASubject rejects the words a question uses to ask for ARITHMETIC or to
// point at a period, rather than to name what it is about. They are unmodelled
// by every schema, for the same reason: they are not things the farm records.
func namesASubject(term string) bool {
	if term == "" || questionStopWords[term] || nonMeasureWords[term] {
		return false
	}
	if _, isDimension := dimensionNouns[term]; isDimension {
		return false
	}
	return !derivationWords[wordStem(term)]
}

// derivationWords name HOW MUCH or HOW COMPARED, never WHAT.
var derivationWords = map[string]bool{
	"actually": true, "biggest": true, "largest": true, "smallest": true,
	"highest": true, "lowest": true, "most": true, "least": true, "best": true,
	"worst": true, "better": true, "worse": true, "more": true, "less": true,
	"compared": true, "comparison": true, "versus": true, "split": true,
	"share": true, "percentage": true, "percent": true, "ratio": true,
	"relative": true, "breakdown": true, "shortfall": true, "variance": true,
	"difference": true, "between": true, "still": true, "came": true,
	"need": true, "bought": true, "realised": true, "realized": true,
	"record": true, "recorded": true, "down": true, "have": true, "were": true,
	"been": true, "there": true, "they": true, "them": true, "then": true,
	"than": true, "other": true, "olde": true, "older": true, "print": true,
	"delete": true, "update": true, "select": true, "ignore": true,
	// Observed reducing a whole question to junk and then refusing ON the junk:
	// "which diseases are most common" -> ["common"] and "how many loads
	// arrived yesterday" -> ["arrived"] (disease and load are dimension nouns,
	// so the subject itself is stripped by measureTerms), and the leader was
	// told "We don't track common, most in Goat OS". Same family as "came"
	// and "recorded" above: a quality or an event verb, never a thing the farm
	// records under that name.
	"common": true, "arrive": true, "arrived": true, "arriving": true,
}

// cardModels reports that the card the answer came from carries this word in
// its own repo-owned description -- its name, a column, or its purpose
// sentence. The purpose is included deliberately: it is written in this repo,
// not by the model, and it says in farm language what the view is FOR, which is
// what a row count of it means.
func cardModels(card reporting.SchemaCard, term string) bool {
	if matchesTerm(term, card.Name) || matchesTerm(term, card.Purpose) {
		return true
	}
	// THE LEADER FOLD IS DELIBERATELY *NOT* APPLIED HERE, and that asymmetry
	// with termModelled is the decision rather than an omission.
	//
	// The live defect the fold fixes is in the MEASURE gate: "who owes us
	// money" nominated sales_buyer_summary, the planner read it, and the
	// answer died on `measureUnmodelled`. cardModels serves
	// `subjectSubstitution`, a different gate answering a different question --
	// does the read THAT RAN report the subject the question named. Folding
	// here would widen the one gate that catches a real number wearing another
	// subject's words ("kids on MILK FEEDING" answered from the animal-scope
	// view), for no measured benefit: a first attempt did exactly that and its
	// mutant SURVIVED, because nothing reaching this function needed it.
	//
	// If a leader-worded subject is ever measured dying here, close it with a
	// test that reproduces it first.
	return cardHasColumnFor(card, term)
}

// leaderFold returns the catalogue's word for a leader's word, or "" when the
// term is already the catalogue's own vocabulary (or nothing folds it).
//
// It is coverageStem, the fold `nominates` already runs, and nothing more.
// Returning "" for an unchanged word is what keeps every caller a RETRY rather
// than a replacement: the literal term is always tested first, so this can only
// ever add a match, never remove one.
func leaderFold(term string) string {
	folded := coverageStem(term)
	if folded == "" || folded == term || folded == wordStem(term) {
		return ""
	}
	return folded
}

// noSourceFor is the one sentence both refusals open with, and its exact claim
// matters. It used to read "We don't track <subject> in Goat OS" -- a statement
// about the PRODUCT, made from evidence that only supports a statement about
// the READS THIS ASSISTANT CAN REACH. The two are not the same, and the gap is
// not hypothetical: `health_cases` holds real rows, `/health/analytics` reports
// them, and no ceo_ai view or tool exposes them to the assistant (a recorded
// coverage decision, docs/ceo-ai/coverage-matrix.md), so a leader asking about
// open health cases was told the farm does not track them. That is a false
// statement about the product, dressed as an honest refusal.
//
// The refusal itself is unchanged and stays exactly as strict: the assistant
// still will not answer from a neighbouring measure. It now says the true
// thing about WHY.
func noSourceFor(subject string) string {
	return "I don't have a source for " + subject + " in the reads I can reach, so I can't answer that."
}

// substitutionIsRefused is the promise that follows, on both refusals: the
// honesty gate this PR exists for, worded once.
const substitutionIsRefused = " I won't answer it from a different measure that happens to be nearby — " +
	"if it is recorded somewhere else, tell me where and I'll read that."

// substitutedSubjectRefusal is what a leader is told instead of a neighbouring
// view's number wearing their question's words.
func substitutedSubjectRefusal(subject string, sourceView string) string {
	from := ""
	if sourceView != "" {
		from = " I read " + sourceView + ", which does not report it."
	}
	return noSourceFor(subject) + from + substitutionIsRefused
}

// unmodelledRefusal is what a leader is told instead of a neighbour's number,
// with no question text in hand to read the subject's own word order from.
func unmodelledRefusal(terms []string) string {
	return unmodelledRefusalFor("", terms)
}

// unmodelledRefusalFor is the same refusal written from the QUESTION, so the
// subject reads as the reader said it.
//
// "How many treatment sessions were missed yesterday?" produced terms sorted
// alphabetically -- `missed`, `treatment` -- and the sentence "I don't have a
// source for missed and treatment", which is not English. The terms are the
// right SET; what was lost is that the question says them as ONE PHRASE. Read
// back off the question, the same refusal says "missed treatment sessions".
func unmodelledRefusalFor(questionText string, terms []string) string {
	return noSourceFor(subjectPhrase(questionText, terms)) + substitutionIsRefused
}

// subjectPhraseWindow is how far apart the unmodelled terms may sit and still
// be one thing the question is ABOUT. Three words covers a compound subject
// and the noun it hangs on ("treatment sessions were missed"); beyond that the
// terms are separate ideas the question happens to carry, and welding them
// into a phrase would invent a subject nobody named. "how many litres of milk
// did we produce today" spans five, so it keeps the list join.
const subjectPhraseWindow = 3

// subjectPhrase renders the unmodelled terms as the noun phrase the question
// used, falling back to the comma-and-"and" list whenever it cannot read one
// off the question with confidence.
func subjectPhrase(questionText string, terms []string) string {
	if len(terms) < 2 || questionText == "" {
		return joinSubjectTerms(terms)
	}
	words := phraseWords(questionText)
	lo, hi := -1, -1
	for _, t := range terms {
		i := indexOfTerm(words, t)
		if i < 0 {
			return joinSubjectTerms(terms)
		}
		if lo < 0 || i < lo {
			lo = i
		}
		if i > hi {
			hi = i
		}
	}
	if hi-lo > subjectPhraseWindow {
		return joinSubjectTerms(terms)
	}
	// The span's own content words, in the question's order, minus the
	// auxiliaries ("were", "did") that carry no subject. The dimension noun
	// BETWEEN the terms is kept deliberately -- "sessions" is what "treatment"
	// and "missed" are both about, and it is the word that makes the phrase a
	// thing rather than two adjectives.
	var lead, head []string
	for i := lo; i <= hi; i++ {
		w := words[i]
		if w == "" || questionStopWords[w] || nonMeasureWords[w] {
			continue
		}
		// A past participle said AFTER the noun ("sessions were missed") reads
		// before it ("missed sessions"), which is how a person names it.
		if len(head) > 0 && strings.HasSuffix(w, "ed") {
			lead = append(lead, w)
			continue
		}
		head = append(head, w)
	}
	phrase := strings.Join(append(lead, head...), " ")
	if phrase == "" {
		return joinSubjectTerms(terms)
	}
	return phrase
}

// phraseWords are the question's words, lower-cased and stripped of the
// punctuation around them, IN ORDER and with every position kept -- a dropped
// word would shift the span the terms are measured across.
func phraseWords(questionText string) []string {
	fields := strings.Fields(strings.ToLower(questionText))
	out := make([]string, 0, len(fields))
	for _, raw := range fields {
		out = append(out, strings.Trim(raw, ".,;:?!()'\"“”"))
	}
	return out
}

// indexOfTerm finds where the question says this term. questionWords supplies
// a crude singular beside each plural, so a term may be the singular of the
// word actually written; both spellings resolve to the written one.
func indexOfTerm(words []string, term string) int {
	for i, w := range words {
		if w == term || strings.TrimSuffix(w, "s") == term {
			return i
		}
	}
	return -1
}

// joinSubjectTerms renders the unmodelled terms as something a person reads.
// A bare comma join produced "We don't track missed, treatment in Goat OS" --
// the terms are a SET the sentence has to carry, and a list of two joined with
// a comma reads as a typo rather than as two words.
func joinSubjectTerms(terms []string) string {
	switch len(terms) {
	case 0:
		return "that"
	case 1:
		return terms[0]
	case 2:
		return terms[0] + " and " + terms[1]
	default:
		return strings.Join(terms[:len(terms)-1], ", ") + " and " + terms[len(terms)-1]
	}
}

// modelledTerms keeps the question's terms that some view column or some tool
// actually models.
func modelledTerms(terms []string, cards []reporting.SchemaCard, catalog []ports.ToolSpec) []string {
	var out []string
	for _, t := range terms {
		if termModelled(t, cards, catalog) {
			out = append(out, t)
		}
	}
	return out
}

func termModelled(term string, cards []reporting.SchemaCard, catalog []ports.ToolSpec) bool {
	if termNamedInCatalogue(term, cards, catalog) {
		return true
	}
	// THE FARM'S WORDS REACH THIS GATE TOO, or closing the coverage gate on
	// them bought nothing. Measured live on this branch AFTER leaderNouns
	// landed: "who owes us money" nominated sales_buyer_summary correctly, the
	// planner read it, and the answer was then refused HERE -- "I don't have a
	// source for owes us money in the reads I can reach" -- while
	// outstanding_rupees on that very card held 325,930. One gate had been
	// taught the leader's spelling and the next had not, so the question died
	// one step later than it used to.
	//
	// It is the SAME fold, not a second synonym list: coverageStem runs both
	// sides and TestTheLeaderFoldOnlyEverMeetsAWordTheCatalogueNames already
	// pins every leaderNouns entry to a word the live catalogue really
	// carries. So this can only let a leader's word meet a column that exists.
	// A subject the catalogue names nothing for -- "runway", "rent",
	// "attrition" -- folds onto nothing and stays refused exactly as strictly
	// as before, which is what the direction-B sweep measures.
	if folded := leaderFold(term); folded != "" {
		return termNamedInCatalogue(folded, cards, catalog)
	}
	return false
}

// termNamedInCatalogue is the literal match: a card's name, a card's column, or
// a tool's name, description or parameter.
func termNamedInCatalogue(term string, cards []reporting.SchemaCard, catalog []ports.ToolSpec) bool {
	for _, card := range cards {
		if matchesTerm(term, card.Name) {
			return true
		}
		for _, col := range card.Columns {
			if matchesTerm(term, col.Name) {
				return true
			}
		}
	}
	for _, spec := range catalog {
		if matchesTerm(term, spec.Name) || matchesTerm(term, spec.Description) {
			return true
		}
		for _, p := range spec.Params {
			if matchesTerm(term, p) {
				return true
			}
		}
	}
	return false
}

// readTouchesAny reports whether the read behind this result actually read a
// column (or ran over a view) modelling one of the terms. It reads the SQL and
// the tool NAME, never the model-authored labels.
func readTouchesAny(r domain.ToolResult, sub domain.SubQuestion, terms []string, card reporting.SchemaCard) bool {
	if r.Err != nil {
		return false
	}
	haystacks := []string{card.Name}
	// Only the card's OWN columns that the SQL names: an arbitrary word in the
	// SQL text (an alias the model invented) proves nothing about what was read.
	if sql, _ := sub.Params["sql"].(string); sql != "" {
		for _, col := range card.Columns {
			if sqlNamesColumn(sql, col.Name) {
				haystacks = append(haystacks, col.Name)
			}
		}
	}
	for _, h := range haystacks {
		for _, t := range terms {
			if matchesTerm(t, h) {
				return true
			}
		}
	}
	return false
}

// sqlNamesColumn reports the column appearing in the SQL as an identifier of
// its own rather than inside a longer word.
func sqlNamesColumn(sql, col string) bool {
	if col == "" {
		return false
	}
	re, err := regexp.Compile(`(?i)\b` + regexp.QuoteMeta(col) + `\b`)
	if err != nil {
		return false
	}
	return re.MatchString(sql)
}

// matchesTerm compares a question word with a schema word on their shared stem,
// so "completed" finds completed_business_day and "vaccinations" finds
// vaccination_obligations without a synonym list to maintain.
func matchesTerm(term, text string) bool {
	if term == "" || text == "" {
		return false
	}
	hay := strings.ReplaceAll(strings.ToLower(text), "_", " ")
	for _, w := range strings.Fields(hay) {
		if w == term || strings.HasPrefix(w, term) || strings.HasPrefix(term, w) && len(w) >= 4 {
			return true
		}
	}
	return false
}

// measureTerms are the question's content words with the DIMENSION vocabulary
// removed: a question is answered BY a measure and broken down BY a dimension,
// and "park" appearing in every view must not make a milk question look
// modelled.
func measureTerms(questionText string) []string {
	words := questionWords(questionText)
	if len(words) == 0 {
		return nil
	}
	var out []string
	for w := range words {
		if _, isDimension := dimensionNouns[w]; isDimension {
			continue
		}
		if nonMeasureWords[w] {
			continue
		}
		// DERIVATION WORDS ARE NOT MEASURES either, for the same reason
		// namesASubject already rejects them: "most", "biggest", "percentage",
		// "arrived" are how a question asks for arithmetic or states a fact
		// about a period, never what it asks ABOUT, and no schema models them.
		// Leaving them in reduced a question to junk and then refused it on the
		// junk: "which diseases are most common" (disease is a dimension) came
		// out as terms ["common","most"] and a leader was told "We don't track
		// common, most in Goat OS".
		if derivationWords[wordStem(w)] || derivationWords[w] {
			continue
		}
		out = append(out, w)
	}
	sort.Strings(out)
	return out
}

// nonMeasureWords name WHEN or WHOSE, never WHAT. A period word is already
// handled by the window contract, and a question is not unanswerable because no
// column is called "yesterday".
var nonMeasureWords = map[string]bool{
	"yesterday": true, "today": true, "tomorrow": true, "week": true, "weeks": true,
	"month": true, "months": true, "quarter": true, "quarters": true, "year": true, "years": true,
	"date": true, "dates": true, "period": true, "window": true, "time": true, "times": true,
	"recent": true, "recently": true, "latest": true, "current": true, "currently": true,
	"their": true, "ours": true, "yours": true, "mine": true, "theirs": true,
	"both": true, "each": true, "every": true, "across": true, "acros": true,
	"please": true, "right": true, "total": true, "totals": true, "overall": true,
	"average": true, "averages": true, "number": true, "count": true, "counts": true,
	"much": true, "many": true, "show": true, "give": true, "tell": true, "list": true,
	"drafted": true, "model": true, "value": true, "values": true,
}

// cardForResult resolves the schema card a result was read from: the view the
// executor recorded, or the card the model's SQL named.
func cardForResult(r domain.ToolResult, sub domain.SubQuestion) (reporting.SchemaCard, bool) {
	if r.Err != nil {
		return reporting.SchemaCard{}, false
	}
	if sql, _ := sub.Params["sql"].(string); sql != "" {
		if card, ok := reporting.CardForSQL(sql); ok {
			return card, true
		}
	}
	if r.SourceView != "" {
		if card, ok := reporting.CardByName(r.SourceView); ok {
			return card, true
		}
	}
	return reporting.SchemaCard{}, false
}

// someReadNamesACard reports whether any executed read can be judged at all.
func someReadNamesACard(subs []domain.SubQuestion, results []domain.ToolResult) bool {
	for i := range results {
		var sub domain.SubQuestion
		if i < len(subs) {
			sub = subs[i]
		}
		if _, ok := cardForResult(results[i], sub); ok {
			return true
		}
	}
	return false
}

// cardHasColumnFor reports that the card the read ran over carries a column
// modelling this word -- which is what makes its absence from the read
// meaningful rather than merely unproven.
func cardHasColumnFor(card reporting.SchemaCard, term string) bool {
	for _, col := range card.Columns {
		if matchesTerm(term, col.Name) {
			return true
		}
	}
	return false
}
