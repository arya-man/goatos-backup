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
		"no data on", "not available in", "doesn't have", "does not have", "don't have",
		"do not have", "no source for", "not modelled",
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
	vocab := catalogueVocabulary(cards, catalog)
	strict := hasUnmodelledSubject(questionText, vocab)
	axisOnly := questionIsOnlyAboutItsBreakdown(questionText)
	var named []string
	for _, card := range coveringViews(questionText, cards, vocab, strict, axisOnly) {
		named = append(named, "ceo_ai."+card.Name+" ("+strings.Join(columnNames(card), ", ")+")")
	}
	named = append(named, coveringTools(questionText, catalog, vocab, strict, axisOnly)...)
	const maxNamed = 4
	if len(named) > maxNamed {
		named = named[:maxNamed]
	}
	return named
}

// coveringTools scores catalogue tools the same way the views are scored.
func coveringTools(questionText string, catalog []ports.ToolSpec, vocab map[string]int, strict, axisOnly bool) []string {
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
			identifierHaystack(spec.Name+" "+spec.Description+" "+strings.Join(spec.Params, " ")), vocab, strict, axisOnly); ok {
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
func coveringViews(questionText string, cards []reporting.SchemaCard, vocab map[string]int, strict, axisOnly bool) []reporting.SchemaCard {
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
			identifierHaystack(card.Name+" "+strings.Join(columnNames(card), " ")), vocab, strict, axisOnly); ok {
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
		if have[coverageStem(w)] {
			seen[coverageStem(w)] = true
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
		// THE FOUR-LETTER FLOOR HAS ONE EXCEPTION, AND IT IS STRUCTURAL. `sex`
		// is a column on animal_current_scope, growth_adg_pairs and
		// weighing_latest_individual_weight and a param on the counts tool --
		// a dimension the farm genuinely models -- and a blanket len<4 drop
		// made it invisible to coverage by construction. Any identifier word
		// the dimension vocabulary itself names is kept whatever its length;
		// everything else still needs four letters, so `no`, `id` and `at`
		// stay out.
		if len(w) < 3 && dimensionNouns[w] == "" && leaderNouns[w] == "" {
			continue
		}
		stems[coverageStem(w)] = true
	}
	return stems
}

// coverageStem folds a question word and an identifier word onto ONE key, so
// the two vocabularies can meet. It is wordStem plus the canonicalisation
// dimensionNouns already maintains for the rest of the package: a leader says
// "workers" and the schema says `operator_label`; a leader says "disease" and
// mortality_base says `cause_established`. That map is not a coverage-only
// synonym list invented here -- answerfit binds dimensions through it, so the
// two agree by construction, and a word added there participates in coverage
// without a second edit.
//
// BOTH SIDES of every comparison run through this function. That is what keeps
// the fold honest: it can never make a question word match something the
// identifier vocabulary does not also fold onto.
func coverageStem(w string) string {
	stem := wordStem(w)
	if canonical, ok := dimensionNouns[stem]; ok {
		return canonical
	}
	if canonical, ok := dimensionNouns[w]; ok {
		return canonical
	}
	if canonical, ok := leaderNouns[stem]; ok {
		return wordStem(canonical)
	}
	if canonical, ok := leaderNouns[w]; ok {
		return wordStem(canonical)
	}
	return stem
}

// leaderNouns is THE FARM'S WORDS, given to the catalogue's vocabulary.
//
// The gap this closes was measured, not guessed: a 50-question sweep written
// the way a leader types found that "how much money did we make" and "who owes
// us money" nominated NOTHING, each killed by one word the schema spells
// differently. `revenue_rupees`, `outstanding_rupees` and
// `payment_received_rupees` are all right there on sales_buyer_summary. The
// question was answerable; only the spelling was not.
//
// IT IS A FOLD, NOT A LOOSENING, and the distinction is the whole safety
// argument. Both sides of every comparison run through coverageStem, exactly
// as dimensionNouns already does, so an entry here can only make a leader's
// word MEET a word the catalogue really names. A leader word folded onto
// something the catalogue does not name stays unmodelled and still makes the
// question strict — which is why "what is our cash runway" and "how much rent
// do we owe on the land" are still refused with `cash` and `owe` folded: their
// SUBJECTS (runway, rent) are named by nothing.
//
// The sanctioned way to close a vocabulary gap is to give the cards the farm's
// word. This map is that, kept in one place instead of scattered through the
// card definitions, so the schema stays the schema's and the leader's English
// stays visibly separate from it.
//
// Every entry names a word on the RIGHT that the live catalogue really carries
// (identity entries exist only to lift the four-letter floor in
// identifierHaystack for a short identifier the catalogue does name). The
// right-hand side is written as the ENGLISH word and stemmed on lookup, the
// same way the catalogue's own identifier words are, so `outstanding` here and
// `outstanding_rupees` on the card meet at the one stem wordStem produces from
// both. TestTheLeaderFoldOnlyEverMeetsAWordTheCatalogueNames checks every
// entry against the live catalogue, so a fold onto nothing cannot ship.
var leaderNouns = map[string]string{
	// Money. revenue_rupees / outstanding_rupees / payment_received_rupees on
	// sales_buyer_summary; amount_rupees on sales_deal_lines_closed.
	"money": "revenue", "monies": "revenue", "cash": "revenue",
	"owe": "outstanding", "owes": "outstanding", "owed": "outstanding", "owing": "outstanding",
	"paid": "payment", "pay": "payment", "pays": "payment", "unpaid": "payment",
	"sell": "sale", "sells": "sale", "sold": "sale", "selling": "sale",
	// Herd. sex on animal_current_scope / growth_adg_pairs; kid_deaths on
	// mortality_base and "kids" on the counts tool; births on
	// counts_movement_daily.
	"male": "sex", "males": "sex", "female": "sex", "females": "sex",
	"baby": "kid", "babies": "kid", "young": "kid",
	"born": "birth", "birth": "birth",
	"herd": "animal", "flock": "animal",
	// Operations. shed_capacity_current; overdue on vaccination_operator_status
	// and workforce_coverage_status; sop_execution_status; load_label on
	// procurement_loads_base.
	"full": "capacity", "capacity": "capacity",
	"behind": "overdue", "late": "overdue",
	"sop": "sop", "sops": "sop",
	"truck": "load", "trucks": "load", "lorry": "load", "lorries": "load",
	"feeding": "feed",
	// Units. kg is the catalogue's own spelling (directed_kg, fed_kg,
	// weight_kg, gain_kg) and is two letters, so it needs the floor lifted as
	// well as the fold.
	"kg": "kg", "kilo": "kg", "kilos": "kg", "kilogram": "kg", "kilograms": "kg",

	// THE ACTION VERBS, AND THE THIRD ROOT CAUSE THIS CLOSES. `spend`, `spent`,
	// `made`, `sold`, `died` were already in coverageVerbWords, which EXEMPTS a
	// word from making the question strict and gives it nothing to score on. A
	// question whose only subject is one of them therefore matched no source at
	// all: measured on the live catalogue, "how much did we make", "what did we
	// spend", "how many died last month", "who has not been vaccinated" and
	// "are we growing" each nominated NOTHING. Exempting a verb is not the same
	// as understanding it; a verb that names what the farm records has to score
	// on the thing it names, and the fold is where that is said.
	"make": "revenue", "makes": "revenue", "made": "revenue", "making": "revenue",
	"profit": "revenue", "earnings": "revenue",
	"spend": "purchase", "spent": "purchase", "spends": "purchase", "spending": "purchase",
	"buy": "purchase", "buys": "purchase", "buying": "purchase", "bought": "purchase",
	"bill": "payment", "bills": "payment", "wage": "payment", "wages": "payment",
	"die": "death", "dies": "death", "died": "death", "dying": "death",
	"vaccinate": "vaccine", "vaccinated": "vaccine", "vaccinating": "vaccine",
	"shot": "vaccine", "shots": "vaccine", "jab": "vaccine", "jabs": "vaccine",
	"grow": "growth", "grows": "growth", "growing": "growth", "grew": "growth", "gain": "growth",
	"fed": "feed", "feeds": "feed",
	"headcount": "animal", "livestock": "animal",
	"working": "work", "worked": "work", "works": "work",
	// Attendance. workforce_coverage_status.coverage_status is the farm's
	// answer to "who is here today"; `off` is two letters short of the floor,
	// so it needs the fold as well as the floor lift.
	"absent": "coverage", "absence": "coverage", "off": "coverage", "leave": "coverage",
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
		// The crude singular questionWords adds beside every plural must be
		// dropped with its plural, or half a noise word survives as a subject:
		// "versus" is filtered here while "versu" was not, and that fragment is
		// named by nothing in the catalogue, so it read as an unmodelled
		// subject and made every comparison question strict.
		if nonMeasureWords[w] || coverageNoiseWords[w] ||
			nonMeasureWords[w+"s"] || coverageNoiseWords[w+"s"] {
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

// catalogueVocabulary is every word the WHOLE catalogue names, folded the same
// way a question's words are folded. It is what lets one source's score be
// judged against the catalogue rather than against itself.
// It counts HOW MANY sources name each word, not merely that one does, because
// the count is what ranks them: `animal` is named by a dozen views and says
// almost nothing about which one answers a question, while `gain` is named by
// growth_adg_pairs alone and says everything. Sorting on a flat hit count put
// animals_base above growth_adg_pairs for "what daily gain are the animals
// putting on" and above mortality_base for a cause-of-death question — the
// misdirection round 3 filed, arriving through the ranking rather than the
// rule.
func catalogueVocabulary(cards []reporting.SchemaCard, catalog []ports.ToolSpec) map[string]int {
	vocab := map[string]int{}
	for _, card := range cards {
		for stem := range identifierHaystack(card.Name + " " + strings.Join(columnNames(card), " ")) {
			vocab[stem]++
		}
	}
	for _, spec := range catalog {
		for stem := range identifierHaystack(spec.Name + " " + spec.Description + " " + strings.Join(spec.Params, " ")) {
			vocab[stem]++
		}
	}
	return vocab
}

// stemWeight is how much one matched word is worth: the fewer sources name it,
// the more it distinguishes the one that does. A word nothing names is worth
// nothing, which can only happen for a stem that did not match at all.
func stemWeight(stem string, vocab map[string]int) int {
	named := vocab[stem]
	if named <= 0 {
		return 0
	}
	const scale = 64
	return scale / named
}

// hasUnmodelledSubject is THE STRUCTURAL DISCRIMINATOR this file turns on, and
// it is a property of the QUESTION against the WHOLE catalogue, not of any one
// source.
//
// Three rounds oscillated between "one shared word nominates" and "two shared
// words are required" because both are per-source thresholds, and no per-source
// threshold can separate these two questions:
//
//	"what was our revenue last month"        -> revenue_rupees      (must nominate)
//	"what is the interest rate on our loan"  -> reject_rate         (must not)
//
// Each matches exactly one common word on exactly one column. What differs is
// what the REST of the question does: "revenue" is the whole subject, while
// "interest" and "loan" are named by nothing in the catalogue at all. A farm
// that models neither interest nor loans is a farm that does not record the
// answer, and a `reject_rate` column on a video-review view does not change
// that -- so the planner's refusal is the honest answer and must ship.
//
// So: when a question carries a subject word the catalogue NOWHERE names, only
// a source whose own NAME is about the question still counts. A column that
// merely shares a word stops disarming the refusal. When every subject word the
// question used is modelled somewhere, the question is about this farm and a
// column match is trusted as before.
//
// Words that carry no subject are exempt from the test: dimension nouns (they
// are scored, but "buyer" being modelled is not what makes a question about
// buyers answerable), and verbs and ranking words, which say HOW the subject is
// asked about rather than WHAT it is.
// npOpeners are the CLOSED-CLASS words that open an English noun phrase:
// determiners, possessives, quantifiers and prepositions. English has a fixed,
// small set of them, which is exactly why this test does not require anybody
// to enumerate English content words.
var npOpeners = map[string]bool{
	"a": true, "an": true, "the": true, "this": true, "that": true, "these": true, "those": true,
	"my": true, "our": true, "your": true, "their": true, "its": true, "his": true, "her": true,
	"no": true, "any": true, "some": true, "every": true, "each": true, "all": true, "both": true,
	"many": true, "much": true, "few": true, "several": true,
	"of": true, "in": true, "on": true, "at": true, "by": true, "for": true, "from": true,
	"with": true, "about": true, "into": true, "per": true, "over": true, "under": true,
	"across": true, "during": true, "without": true, "within": true, "between": true,
}

// npBreakers end a noun phrase: auxiliaries, copulas, modals, wh-words,
// pronouns and conjunctions. Also a closed class.
var npBreakers = map[string]bool{
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true, "being": true,
	"am": true, "do": true, "does": true, "did": true, "have": true, "has": true, "had": true,
	"will": true, "would": true, "can": true, "could": true, "shall": true, "should": true,
	"may": true, "might": true, "must": true, "and": true, "or": true, "but": true, "not": true,
	"what": true, "which": true, "who": true, "whom": true, "whose": true, "when": true,
	"where": true, "why": true, "how": true, "there": true, "than": true, "if": true, "to": true,
	"we": true, "they": true, "it": true, "you": true, "he": true, "she": true, "us": true,
	"them": true, "me": true, "i": true, "so": true,
}

// nounPositionWords are the question's words that sit where a NOUN sits.
//
// THIS IS THE FIX FOR THE SECOND ROOT CAUSE, AND IT IS THE POLARITY THAT WAS
// WRONG. `hasUnmodelledSubject` used to read ANY English word the catalogue
// does not name -- and that no hand-written list happened to exempt -- as
// POSITIVE PROOF that the question is about another company. So one ordinary
// word poisoned a question every other word of which was covered: measured on
// the live catalogue, "how many people work here" nominated NOTHING even
// though people->operator and work->work both resolve, killed by `here`; and
// so did "how many animals are sick" (`sick`), "how heavy are our goats"
// (`heavy`) and "are any sheds overcrowded".
//
// Closing that by adding `here`, `sick` and `heavy` to a stop list is a seventh
// word-list patch and would be wrong for the eighth word. The CAUSE is that
// absence from a list was treated as evidence. Foreign scope has to be
// evidenced POSITIVELY, and the positive signal is SYNTACTIC: a question about
// another company NAMES that company's things, and a name sits in a noun
// position -- `the interest rate on our loan`, `our chickens`, `our payroll
// cost`, `the staff attrition rate`, `the stock market`. A predicate does not:
// `are sick`, `are overcrowded`, `is stuck`, `work here`, `how heavy`.
//
// So a word counts as evidence only when a closed-class noun-phrase opener
// introduces it. Determiners, possessives, quantifiers and prepositions are a
// genuinely closed class of English -- unlike nouns and adjectives -- so this
// mechanism needs no vocabulary to grow and an unknown word outside a noun
// phrase is simply NEUTRAL rather than proof of anything.
//
// The chain runs at most two content words past the opener, which is what
// carries an ordinary compound (`our cash runway`, `the staff attrition rate`)
// without walking the whole clause and swallowing a predicate three words
// later.
func nounPositionWords(text string) map[string]bool {
	out := map[string]bool{}
	inNP, pos := false, 0
	for _, w := range questionTokens(text) {
		switch {
		case npOpeners[w]:
			inNP, pos = true, 0
		case npBreakers[w]:
			inNP, pos = false, 0
		case inNP:
			pos++
			out[w] = true
			if pos >= 2 {
				inNP, pos = false, 0
			}
		}
	}
	return out
}

// npDeterminers are the subset of npOpeners that are DETERMINERS or
// POSSESSIVES. A quantifier or a preposition can head a verb phrase as easily
// as a noun phrase ("many loads ARRIVED", "in WEIGHING"), but the word straight
// after `the`/`our`/`their` is a noun -- including a gerund, which is the case
// this distinction exists for.
var npDeterminers = map[string]bool{
	"a": true, "an": true, "the": true, "this": true, "that": true, "these": true, "those": true,
	"my": true, "our": true, "your": true, "their": true, "its": true, "his": true, "her": true,
	// Prepositions belong here too: what follows one is its OBJECT, and an
	// object is a noun. "how much did we spend on FENCING" is a question about
	// fencing, and reading `fencing` as a verb because it ends in -ing hands a
	// procurement view to a question about something the farm never bought
	// through it.
	"of": true, "in": true, "on": true, "at": true, "by": true, "for": true, "from": true,
	"with": true, "about": true, "into": true, "per": true, "during": true, "without": true,
}

// determinerHeadedNouns are the words sitting IMMEDIATELY after a determiner or
// possessive. Such a word is a noun whatever its shape, which is what keeps
// `their milking session` an unmodelled SUBJECT rather than a verb the
// -ed/-ing exemption waves through. Without it "which sheds missed their
// milking session yesterday" is judged to be about this farm and reaches three
// shed views on their filing system alone.
func determinerHeadedNouns(text string) map[string]bool {
	out := map[string]bool{}
	afterDeterminer := false
	for _, w := range questionTokens(text) {
		if afterDeterminer && !npOpeners[w] && !npBreakers[w] {
			out[w] = true
		}
		afterDeterminer = npDeterminers[w]
	}
	return out
}

// questionIsOnlyAboutItsBreakdown reports a question whose ONLY subject is a
// breakdown dimension -- "sheds", "parks", "show me the sheds".
//
// axisDimensions and isColumnNoiseDimension exist to stop a question that HAS a
// real subject from being answered by a source that shares only its filing
// system ("which sheds missed their milking session" reaching a feed view on
// shed_label + session_no). When the breakdown IS the subject there is no other
// subject to prefer, and suppressing it just means the question is answered by
// nothing at all: measured, "sheds" and "parks" nominated NOTHING while
// `shed_capacity_current` and `vaccination_shed_status` sit in the catalogue.
func questionIsOnlyAboutItsBreakdown(questionText string) bool {
	sawDimension := false
	for w := range coverageWords(questionText) {
		// BOTH FORMS, because questionWords adds a crude singular beside every
		// plural and the fragment is not an independent subject: "categories"
		// arrives with "categorie" beside it, which folds onto nothing and
		// would otherwise read as a real subject sitting next to the breakdown.
		dimension := false
		for _, form := range wordForms(w) {
			formStem := coverageStem(form)
			if isAxisWord(formStem) || isColumnNoiseDimension(formStem) {
				dimension = true
			}
		}
		if dimension {
			sawDimension = true
			continue
		}
		if anyFormIsVerb(w) {
			continue
		}
		// A real subject word: the ordinary suppression applies.
		return false
	}
	return sawDimension
}

func hasUnmodelledSubject(questionText string, vocab map[string]int) bool {
	exempt := map[string]bool{}
	candidates := map[string]bool{}
	nounPos := nounPositionWords(questionText)
	headNoun := determinerHeadedNouns(questionText)
	// A SHORT WORD IS STILL A SUBJECT. coverageWords keeps a four-letter floor
	// so that `no`, `id` and `at` cannot nominate anything, but a word too
	// short to SCORE can still be the thing the question is about -- "how much
	// tax did we pay" is a question about tax, and reading only `pay` from it
	// answers a tax question from the sales ledger. Evidence of foreign scope
	// is therefore taken from the noun positions directly, floor and all.
	judged := coverageWords(questionText)
	for w := range nounPos {
		if len(w) < 3 || judged[w] {
			continue
		}
		if questionStopWords[w] || nonMeasureWords[w] || coverageNoiseWords[w] {
			continue
		}
		judged[w] = true
	}
	for w := range judged {
		stem := coverageStem(w)
		// THE CRUDE SINGULAR IS NOT AN INDEPENDENT SUBJECT. questionWords adds
		// a bare s-stripped form beside every plural, and that form is a
		// fragment, not a word a leader said: "status" arrives with "statu"
		// beside it, "statu" is named by nothing in the catalogue, and EVERY
		// question containing the word "status" was therefore strict — four of
		// the six schema-vocabulary questions this test suite says must
		// nominate were strict for that reason alone. coverageWords already
		// drops the fragment of a KNOWN noise word ("versu"); this judges the
		// two forms of an unknown one together, which is the general case.
		if wordFormsAreModelled(w, vocab) {
			exempt[stem] = true
			continue
		}
		if _, isDimension := dimensionNouns[stem]; isDimension {
			exempt[stem] = true
			continue
		}
		if anyFormIsVerb(w) && !headNoun[w] {
			exempt[stem] = true
			continue
		}
		// NOT IN A NOUN POSITION -> NEUTRAL, never evidence. See
		// nounPositionWords: absence from the catalogue is not by itself a
		// statement about WHOSE farm the question is about.
		if !nounPos[w] {
			continue
		}
		if vocab[stem] == 0 {
			candidates[stem] = true
		}
	}
	for stem := range candidates {
		if !exempt[stem] {
			return true
		}
	}
	return false
}

// wordForms is a question word beside the OTHER form questionWords may have
// produced it from, or produced beside it. The two are one word to a reader
// and must be judged as one.
func wordForms(w string) []string {
	if strings.HasSuffix(w, "s") {
		return []string{w, strings.TrimSuffix(w, "s")}
	}
	return []string{w, w + "s"}
}

// wordFormsAreModelled reports that the catalogue names either form of the
// word.
func wordFormsAreModelled(w string, vocab map[string]int) bool {
	for _, form := range wordForms(w) {
		if vocab[coverageStem(form)] > 0 {
			return true
		}
	}
	return false
}

// anyFormIsVerb reports that either form of the word is a verb. "owes" and
// "owe" are the same verb; only one of them is spelled the way the exemption
// list happens to hold it.
func anyFormIsVerb(w string) bool {
	for _, form := range wordForms(w) {
		if isVerbForm(form) {
			return true
		}
	}
	return false
}

// isVerbForm reports a word that describes what is being DONE rather than what
// the question is about. A verb naming nothing in the catalogue proves nothing:
// no view is called `arrived` or `collected`, and "how many loads arrived
// yesterday" is a question about loads.
func isVerbForm(w string) bool {
	if len(w) >= 5 && (strings.HasSuffix(w, "ed") || strings.HasSuffix(w, "ing")) {
		return true
	}
	return coverageVerbWords[w]
}

// coverageVerbWords are the irregular and bare-stem verbs the suffix test above
// cannot see. It is ordinary English, deliberately not farm vocabulary -- a
// domain noun must never be hidden here.
var coverageVerbWords = map[string]bool{
	"make": true, "made": true, "need": true, "needs": true, "know": true,
	"take": true, "took": true, "give": true, "gave": true, "come": true,
	"came": true, "went": true, "keep": true, "kept": true, "look": true,
	"want": true, "file": true, "owes": true, "hold": true, "held": true,
	"send": true, "sent": true, "sell": true, "sold": true, "paid": true,
	"get": true, "got": true, "gotten": true,
	"gets": true, "goes": true, "runs": true, "ship": true, "find": true,
	"seen": true, "using": true, "used": true, "book": true, "earn": true,
	"died": true, "dies": true, "dead": true, "grew": true, "ran": true,
	"earns": true, "spend": true, "spent": true, "puts": true, "sees": true,
	// "we DIRECT feed to the sheds" — a verb, and the only reason "how much
	// feed did we direct versus feed" was strict: the column is `directed_kg`,
	// whose identifier word stems to `directed`, and wordStem's gerund fold
	// does not reach a past participle.
	"direct": true, "directs": true,
}

// nominates decides whether one source covers the question, and returns the
// score used to rank it. nameHay is the source's own name; fullHay is the name
// plus its columns (a view) or its description and params (a tool). strict is
// hasUnmodelledSubject for the question -- the joint, catalogue-level half of
// the judgement.
//
// The two arms are scored TOGETHER rather than one at a time:
//
//   - the source's NAME carries a question word that is not a pure breakdown
//     axis. The source is named after the thing being asked about
//     (sales_buyer_summary for a buyer question, animal_current_scope for an
//     animal one), and that holds whatever the rest of the question says.
//   - ANY non-axis word of the question appears on the source at all --
//     `revenue_rupees` for "what was our revenue" -- but ONLY when the question
//     has no unmodelled subject. A single shared word on a column is weak
//     evidence, and weak evidence must not be what overrides a leader-visible
//     honesty refusal.
//
// What is deliberately NOT a way in, in either mode: a match that is only
// breakdown axes. Every view carries `park_label` and `shed_label`, so "which
// sheds missed their milking session yesterday" matched feed views on nothing
// but its breakdown.
func nominates(words map[string]bool, nameHay, fullHay map[string]bool, vocab map[string]int, strict, axisOnly bool) (int, bool) {
	all := stemHits(words, fullHay)
	name := stemHits(words, nameHay)
	score := 0
	for _, stem := range all {
		score += stemWeight(stem, vocab)
	}
	for _, stem := range name {
		score += stemWeight(stem, vocab)
	}
	// STRICT CLOSES BOTH ARMS, and the round-5 review is why. It used to close
	// only the column arm because the name arm returned first, so
	// hasUnmodelledSubject was structurally incapable of gating the place
	// three of five measured false nominations actually got in: "the mortality
	// rate of our chickens" reached mortality_base on the NAME `mortality`,
	// "the next audit by the bank" reached audit_activity_summary on `audit`,
	// "the staff attrition rate" reached vaccination_operator_status on
	// staff->operator. Each had already been judged to carry a subject the farm
	// models nowhere, and each overrode that judgement one line later.
	//
	// A discriminator that gates half of what it describes is not a
	// discriminator. Closing the other half is only safe because the same
	// round made the judgement itself accurate (leaderNouns, and the crude
	// singular no longer reading as an unmodelled subject): before that, six
	// of the schema's OWN sweep questions were strict and lived entirely on
	// this arm.
	if strict {
		return 0, false
	}
	for _, stem := range name {
		if axisOnly || !isAxisWord(stem) {
			return score, true
		}
	}
	for _, stem := range all {
		if axisOnly || !isColumnNoiseDimension(stem) {
			return score, true
		}
	}
	return 0, false
}

// isColumnNoiseDimension reports a dimension that says nothing about a source
// when it is matched on a COLUMN. Every view carries `park_label`,
// `shed_label`, a `status` and half carry `session_no`, so a question that
// shares only those with a source shares only its filing system -- which is how
// "which sheds missed their milking session yesterday" reached feed views on
// `shed_label` + `session_no` with neither "milking" nor "missed" scoring.
//
// It is deliberately WIDER than axisDimensions, which governs the NAME arm: a
// view CALLED sales_buyer_summary or procurement_loads_base really is about the
// thing it is named after, so the name arm keeps every non-axis dimension.
func isColumnNoiseDimension(stem string) bool {
	if isAxisWord(stem) {
		return true
	}
	switch stem {
	case "session", "status", "category", "stage":
		return true
	}
	return false
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
	// Ranking and degree words. They say how the answer is ORDERED, never what
	// it is about, and an unmodelled one would otherwise read as a subject the
	// farm does not record ("which disease is hitting us hardest").
	"still": true, "hardest": true, "highest": true, "lowest": true,
	"most": true, "least": true, "best": true, "worst": true,
	"top": true, "bottom": true, "too": true, "very": true, "quite": true, "rather": true,
	// Shape words: they say how the answer is CUT, not what it is about.
	"versus": true, "distribution": true, "breakdown": true, "split": true,
	"summary": true, "overview": true, "trend": true, "trends": true,
	"more": true, "less": true, "fewer": true, "biggest": true, "largest": true,
	"smallest": true, "better": true, "worse": true,
	// Quantity and shape words a leader reaches for instead of naming the
	// measure: "what is the outstanding AMOUNT per buyer" is a question about
	// `outstanding_rupees`, and "the age PROFILE of the herd" is a question
	// about age. Left in, each read as a subject the farm does not record and
	// made its whole question strict.
	"amount": true, "amounts": true, "profile": true, "profiles": true,
	"ratio": true, "ratios": true, "enough": true,
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
	// The MODALS, as a closed class. English has nine of them and no tenth, so
	// listing them is grammar rather than the word-by-word enumeration this
	// file exists to stop doing. `will` was already here; the rest arrived
	// through the contraction expansion — `couldn't` expands to `could`, which
	// is four letters, is in no other list, and was therefore read as a thing
	// the farm might record.
	"can": true, "could": true, "shall": true, "should": true,
	"would": true, "must": true, "might": true, "cannot": true,
}

// englishClitics are the CONTRACTION suffixes of English, and the list is
// closed: `n't`, `'s`, `'re`, `'ve`, `'ll`, `'d`, `'m` are every clitic the
// language has. Stripping them is grammar, not vocabulary -- nothing here
// names a farm word, and a contraction a leader invents tomorrow is already
// covered because it can only be built from these.
//
// WHY THIS IS A BLOCKER AND NOT A NICETY: the old tokenizer trimmed only EDGE
// punctuation, so `hasn't` survived whole, matched no schema name, and was
// read as a subject the farm models nowhere -- which closes BOTH arms of
// `nominates`. Measured on the live catalogue: "who hasn't paid us" nominated
// NOTHING while "who has not paid us" nominated two sources. The 127-question
// golden set contains zero `n't` forms, which is exactly why six review rounds
// could not see it.
var englishClitics = []string{"n't", "'re", "'ve", "'ll", "'s", "'d", "'m"}

// contractedIrregulars are the contractions whose stem is not the word itself.
// Everything else in English contracts by simple suffixing.
var contractedIrregulars = map[string]string{"won't": "will", "can't": "can", "shan't": "shall"}

// expandContraction returns the word a contraction was built from. It is
// applied to EVERY token before any matching, so `what's` is the word `what`
// and `aren't` is the word `are` -- both ordinary stop words -- rather than
// tokens the catalogue can never name.
func expandContraction(w string) string {
	w = strings.ReplaceAll(w, "’", "'")
	if stem, ok := contractedIrregulars[w]; ok {
		return stem
	}
	for _, clitic := range englishClitics {
		if strings.HasSuffix(w, clitic) && len(w) > len(clitic) {
			return strings.TrimSuffix(w, clitic)
		}
	}
	return w
}

// questionTokens is the question as ORDERED, normalised words -- contractions
// expanded, edge punctuation trimmed, nothing dropped. Order matters to
// nounPositionWords, which is why this is separate from questionWords.
func questionTokens(text string) []string {
	raw := strings.Fields(strings.ToLower(text))
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		w := strings.Trim(r, ".,;:?!()'\"’")
		if w == "" {
			continue
		}
		out = append(out, expandContraction(w))
	}
	return out
}

// questionWords is the set of subject-bearing words a question used.
func questionWords(text string) map[string]bool {
	words := map[string]bool{}
	for _, w := range questionTokens(text) {
		// THE FOUR-LETTER FLOOR HAS THE SAME EXCEPTION THE IDENTIFIER SIDE
		// HAS, and for the same reason: `buy` and `off` are words a leader
		// really types and the fold really carries, so dropping them by length
		// made "what did we buy this week" produce an EMPTY word set and bail
		// before it was scored at all.
		if len(w) < 4 && leaderNouns[w] == "" && dimensionNouns[w] == "" {
			continue
		}
		if questionStopWords[w] {
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
