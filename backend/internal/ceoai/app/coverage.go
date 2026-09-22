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
	words := questionWords(questionText)
	if len(words) == 0 {
		return nil
	}
	type scored struct {
		name  string
		score int
	}
	var hits []scored
	for _, spec := range catalog {
		haystack := strings.ToLower(spec.Name + " " + spec.Description + " " + strings.Join(spec.Params, " "))
		haystack = strings.ReplaceAll(haystack, "_", " ")
		score := 0
		for w := range words {
			if strings.Contains(haystack, w) {
				score++
			}
		}
		if score >= 2 {
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
	words := questionWords(questionText)
	if len(words) == 0 {
		return nil
	}
	type scored struct {
		card  reporting.SchemaCard
		score int
	}
	var hits []scored
	for _, card := range cards {
		haystack := strings.ToLower(card.Name + " " + strings.Join(columnNames(card), " "))
		haystack = strings.ReplaceAll(haystack, "_", " ")
		score := 0
		for w := range words {
			if strings.Contains(haystack, w) {
				score++
			}
		}
		// Two independent words keep a single incidental match ("date", "label")
		// from nominating every view in the catalog.
		if score >= 2 {
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
