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
				delete(unmet, t)
				continue
			}
			if _, met := unmet[t]; !met {
				unmet[t] = true
			}
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
	// Last check, and the one that keeps this narrow: the coverage matcher
	// scores a whole question against every view and tool, so a subject the
	// farm records under words the question did not use is still nominated.
	// Only a question NOTHING nominates is refused.
	if len(coveringSources(questionText, cards, catalog)) > 0 {
		return false, nil
	}
	return true, terms
}

// unmodelledRefusal is what a leader is told instead of a neighbour's number.
func unmodelledRefusal(terms []string) string {
	subject := strings.Join(terms, ", ")
	return "We don't track " + subject + " in Goat OS, so I have no source for that. " +
		"I won't answer it from a different measure that happens to be nearby — " +
		"if it is recorded somewhere else, tell me where and I'll read that."
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
