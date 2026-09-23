package app

// entitysubstitution.go: a question that NAMES ONE ANIMAL must not be answered
// with an average of the herd.
//
// The reviewer's measurement, on this branch and on the baseline alike:
//
//	Q: "What does MG-100001 weigh now and what did it weigh at the previous
//	    weighing?"
//	A: "Average weight kg: selected scope 29.6683333333333333."
//
// The reader named ONE animal and was handed the mean of every animal in
// scope, with nothing in the sentence saying so. MG-100001 genuinely has two
// weights on file. A leader reads 29.67 as that goat's weight; it is not.
//
// WHY THE TWO EXISTING GATES MISS IT, structurally rather than by accident.
// measureUnmodelled needs EVERY term of the question unmodelled, and "weigh"
// is modelled by the weighing views. subjectSubstitution looks for a COMPOUND
// SUBJECT -- two adjacent content words, one of which nothing in the catalogue
// models -- and an ear tag is one token that no catalogue word will ever
// match. Both gates judge the question's VOCABULARY; this one judges its
// SUBJECT's IDENTITY, which is a different thing and needs its own check.
//
// It is NOT a planned-vs-fallback gate, and the correction matters for anyone
// reading the reviewer's note: postReadHonesty already runs on both paths. The
// hole was coverage, not routing. This gate runs in the same place and so
// covers the deterministic path -- where the defect was actually measured,
// because `naturalOperationalSQLPlan` answers a weighing question with a
// scope-wide avg() and has nowhere to put a tag -- as well as the planned one.
//
// THE DISCRIMINATOR IS WHETHER THE READ SELECTED THE ANIMAL, never whether the
// answer happens to print its tag. A read that filters on the tag and returns
// a bare figure ("Latest weight: 22.9") is a correct answer that names the
// animal nowhere, and refusing it would be the opposite mistake.

import (
	"regexp"
	"strings"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// questionEarTag matches the ear tag a reader writes: a short letter prefix, a
// hyphen and at least four digits. The digit floor keeps operational
// locations out ("Castro 1", "Godel 2 - Part 1" carry no such token) and a
// bare date cannot match, having no letter prefix.
var questionEarTag = regexp.MustCompile(`\b[A-Za-z]{2,4}-[0-9]{4,}\b`)

// namedEntitySubstitution reports that the question named a specific animal by
// its ear tag, rows came back, and NOTHING that ran selected that animal -- so
// whatever figure is about to be composed belongs to something else.
//
// It returns the tag as the reader wrote it and the view that answered, for
// the refusal sentence.
func namedEntitySubstitution(questionText string, subs []domain.SubQuestion, results []domain.ToolResult) (bool, string, string) {
	tags := earTagsIn(questionText)
	if len(tags) == 0 {
		return false, "", ""
	}
	substituted, view := false, ""
	for i := range results {
		if results[i].Err != nil || len(results[i].Facts) == 0 {
			continue
		}
		var sub domain.SubQuestion
		if i < len(subs) {
			sub = subs[i]
		}
		for _, tag := range tags {
			if readSelectedEntity(sub, results[i], tag) {
				// SOME read answered about the animal. A second read that did
				// not is ordinary supporting context, not a substitution.
				return false, "", ""
			}
		}
		if !substituted {
			substituted, view = true, results[i].SourceView
		}
	}
	if !substituted {
		// No read returned rows at all. An empty answer is honest whatever the
		// question named -- there is no borrowed figure to refuse.
		return false, "", ""
	}
	return true, tags[0], view
}

// earTagsIn returns the ear tags a question names, in the order it says them
// and in the spelling the reader used.
func earTagsIn(questionText string) []string {
	matches := questionEarTag.FindAllString(questionText, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		key := strings.ToUpper(m)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	return out
}

// readSelectedEntity reports that this read is ABOUT the named animal: either
// the statement that ran narrowed to the tag, or a row it returned carries it.
// The SQL is checked first and is the real evidence -- a read filtered to one
// animal answers about that animal whether or not it echoes the tag back.
func readSelectedEntity(sub domain.SubQuestion, r domain.ToolResult, tag string) bool {
	if paramsNameEntity(sub.Params, tag) {
		return true
	}
	for _, f := range r.Facts {
		if containsFold(f.Label, tag) || containsFold(f.Scope, tag) || containsFold(f.Value, tag) {
			return true
		}
	}
	return false
}

// paramsNameEntity walks the sub-question's bound params for the tag. It reads
// the SQL and every other string argument, because a read API takes the animal
// as a parameter rather than in a statement.
func paramsNameEntity(params map[string]any, tag string) bool {
	for _, v := range params {
		switch t := v.(type) {
		case string:
			if containsFold(t, tag) {
				return true
			}
		case []string:
			for _, s := range t {
				if containsFold(s, tag) {
					return true
				}
			}
		case []any:
			for _, e := range t {
				if s, ok := e.(string); ok && containsFold(s, tag) {
					return true
				}
			}
		}
	}
	return false
}

func containsFold(haystack, needle string) bool {
	if haystack == "" || needle == "" {
		return false
	}
	return strings.Contains(strings.ToUpper(haystack), strings.ToUpper(needle))
}

// substitutedEntityRefusal is what a leader is told instead of a scope average
// wearing one animal's name. It keeps the shape the other two refusals use --
// what could not be reached, what WAS read, and the promise not to answer from
// a neighbour -- because a reader meets all three in the same product.
func substitutedEntityRefusal(tag string, sourceView string) string {
	from := ""
	if sourceView != "" {
		from = " I read " + sourceView + ", which reports a figure for the whole scope it covers, not for one animal."
	}
	return "I don't have a read that selects " + tag + ", so I can't answer that for it." + from + substitutionIsRefused
}
