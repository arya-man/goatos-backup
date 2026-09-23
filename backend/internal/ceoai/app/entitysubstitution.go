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

// documentPrefixes are the prefixes of identifiers that are NOT animals.
//
// The digit floor above was described as keeping non-animals out; it keeps
// operational LOCATIONS out and nothing else, so `INV-1001`, `PO-20241`,
// `SOP-1234` and `ISO-9001` all matched — and a leader who asked about an
// invoice was then told the read "reports a figure for the whole scope it
// covers, not for one animal", a sentence about the wrong kind of thing
// entirely.
//
// It names DOCUMENTS, deliberately, not animals: a new ear-tag prefix
// participates the day it is issued without an edit here, and only a
// recognised paperwork prefix is excluded. Matching is case-insensitive.
var documentPrefixes = map[string]bool{
	"inv": true, "po": true, "pr": true, "so": true, "sop": true, "iso": true,
	"gst": true, "hsn": true, "ref": true, "txn": true, "rcpt": true,
	"chq": true, "utr": true, "doc": true, "grn": true, "dc": true,
}

// isDocumentIdentifier reports that a matched token is paperwork rather than
// an animal.
func isDocumentIdentifier(token string) bool {
	hyphen := strings.IndexByte(token, '-')
	if hyphen < 0 {
		return false
	}
	return documentPrefixes[strings.ToLower(token[:hyphen])]
}

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
		if isDocumentIdentifier(m) {
			continue
		}
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
	// WHEN THERE IS A STATEMENT, THE STATEMENT IS THE ANSWER, and the fact
	// labels do not get a second vote. A model may write
	//
	//	SELECT 'MG-100001' AS label, avg(latest_weight_kg) AS value FROM …
	//
	// which satisfied BOTH discriminators — the tag is in Params["sql"] and
	// the fact it produces echoes it — and shipped the herd average wearing
	// one animal's name, which is the exact defect this file exists to stop.
	// A literal in the SELECT list is a caption the model wrote; only a
	// literal the database filtered on is evidence that the read is about the
	// animal. So a read that came with SQL is judged on whether that SQL
	// narrows to the tag, full stop.
	if sql, ok := statementParam(sub.Params); ok {
		return sqlNarrowsToEntity(sql, tag)
	}
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

// statementParam returns the SQL statement this sub-question ran, if it ran
// one. A read API takes its animal as an ordinary parameter and has no
// statement; only a SQL read does.
func statementParam(params map[string]any) (string, bool) {
	for _, key := range []string{"sql", "statement", "query"} {
		if s, ok := params[key].(string); ok && looksLikeSelect(s) {
			return s, true
		}
	}
	return "", false
}

func looksLikeSelect(s string) bool {
	trimmed := strings.TrimLeft(strings.TrimSpace(s), "(")
	return strings.HasPrefix(strings.ToUpper(trimmed), "SELECT")
}

// sqlNarrowsToEntity reports that the statement ASKED THE DATABASE FOR THIS
// ANIMAL: the tag appears as a string LITERAL that is an operand of a
// comparison whose other side reads a COLUMN.
//
// IT USED TO BE A SUBSTRING SEARCH, AND A SUBSTRING SEARCH OVER THE MODEL'S
// OWN TEXT IS NOT EVIDENCE. The previous version asked only whether the tag
// appeared anywhere from the outermost FROM onwards. Measured, each of these
// cleared the guard on a statement that selected nothing of the kind:
//
//	SELECT avg(weight_kg) FROM ceo_ai.animal_current_scope -- GT-1234
//	… WHERE management_stage='K2' AND 'GT-1234' <> ''
//	… FROM ceo_ai.animal_current_scope gt_1234 WHERE …
//	… GROUP BY 1 ORDER BY 'GT-1234'
//
// The first answers "what does GT-1234 weigh" with a herd average, which is
// the exact defect this file exists to stop, reopened by a comment. So the
// question is asked structurally instead: a comment is not a predicate, an
// ALIAS is an identifier and never a literal, `ORDER BY 'x'` compares nothing,
// and `'x' <> ''` compares a literal to a literal — none of them narrows.
//
// `WHERE animal_key = 'GT-1234'`, `animal_key IN ('GT-1234','GT-9')`,
// `upper(animal_key) = 'GT-1234'` and `animal_key ILIKE '%GT-1234%'` all do,
// and all still clear — which is the case this arm exists for: a read filtered
// to one animal that returns a bare figure ("Latest weight: 22.9") is a
// correct answer that names the animal nowhere.
//
// It stays crude in the SAFE direction. A tag that appears only in the
// outermost SELECT list is a caption and is not looked at; a statement this
// reader cannot follow simply fails to clear, and the refusal stands.
func sqlNarrowsToEntity(sql, tag string) bool {
	toks := filterRegionTokens(sql)
	for i, t := range toks {
		if t.kind != tokString || !containsFold(t.text, tag) {
			continue
		}
		if comparedAgainstAColumn(toks, i) {
			return true
		}
	}
	return false
}

// filterRegionTokens is the statement from its outermost FROM onwards, as
// significant tokens. Whitespace and COMMENTS are gone before anything is
// judged — that is what makes a trailing `-- GT-1234` unable to speak — and the
// FROM is found among tokens rather than bytes, so a comment or a literal
// carrying the word "from" cannot move the boundary.
func filterRegionTokens(sql string) []sqlToken {
	toks := significantTokens(lexSQLTokens(sql))
	depth := parenDepths(toks)
	for i, t := range toks {
		if depth[i] == 0 && t.kind == tokIdent && strings.EqualFold(t.text, "from") {
			return toks[i:]
		}
	}
	return nil
}

// comparedAgainstAColumn reports that the literal at `at` sits on one side of a
// comparison whose OTHER side reads a column.
func comparedAgainstAColumn(toks []sqlToken, at int) bool {
	for _, dir := range []int{-1, 1} {
		op := nearestComparator(toks, at, dir)
		if op < 0 {
			continue
		}
		if operandReadsAColumn(toks, op, dir) {
			return true
		}
	}
	return false
}

// nearestComparator walks out from the literal across the rest of its own
// operand — parentheses, commas, other literals, and the function names and
// `ARRAY` spelling that wrap one — and returns the comparison it belongs to.
// It stops at a boolean connective or a clause keyword, because past one of
// those the literal is in a different predicate altogether.
func nearestComparator(toks []sqlToken, at, dir int) int {
	for i := at + dir; i >= 0 && i < len(toks); i += dir {
		t := toks[i]
		switch {
		case isComparisonToken(t):
			return i
		case t.kind == tokIdent && sqlPredicateBreakWord(t.text):
			return -1
		case t.kind == tokOther:
			switch t.text {
			case "(", ")", ",", "[", "]", ".":
				continue
			default:
				return -1
			}
		case t.kind == tokString || t.kind == tokIdent:
			continue
		default:
			return -1
		}
	}
	return -1
}

// operandReadsAColumn walks the operand on the far side of the comparison and
// reports whether any ordinary identifier appears in it. `''`, `'x'` and a bare
// number do not read a column; `animal_key` and `upper(animal_key)` do.
func operandReadsAColumn(toks []sqlToken, op, dir int) bool {
	// The other operand is on the opposite side of the operator from the
	// literal, so keep walking in the same direction we came from.
	for i := op + dir; i >= 0 && i < len(toks); i += dir {
		t := toks[i]
		switch {
		case isComparisonToken(t):
			return false
		case t.kind == tokIdent && sqlPredicateBreakWord(t.text):
			return false
		case t.kind == tokIdent:
			if !sqlNonColumnWord(t.text) {
				return true
			}
		case t.kind == tokOther:
			switch t.text {
			case "(", ")", ",", "[", "]", ".":
				continue
			default:
				return false
			}
		}
	}
	return false
}

func isComparisonToken(t sqlToken) bool {
	if t.kind == tokOther {
		switch t.text {
		case "=", "<>", "!=", "<", ">", "<=", ">=", "~", "!~", "~*", "!~*":
			return true
		}
		return false
	}
	if t.kind != tokIdent {
		return false
	}
	switch strings.ToLower(t.text) {
	case "like", "ilike", "in", "similar":
		return true
	}
	return false
}

// sqlPredicateBreakWord ends one predicate: past it the literal belongs to a
// different comparison or a different clause.
func sqlPredicateBreakWord(w string) bool {
	switch strings.ToLower(w) {
	case "and", "or", "not", "where", "group", "order", "having", "limit",
		"offset", "from", "join", "on", "using", "select", "case", "when",
		"then", "else", "end", "union", "except", "intersect":
		return true
	}
	return false
}

// sqlNonColumnWord names the identifiers that are SQL spelling rather than a
// column a row could carry.
func sqlNonColumnWord(w string) bool {
	switch strings.ToLower(w) {
	case "any", "all", "array", "null", "true", "false", "distinct", "as",
		"asc", "desc", "is", "between", "text", "varchar", "escape":
		return true
	}
	return false
}

// afterProjection returns the statement from its outermost FROM onwards, or
// "" when there is no FROM at all (a projection-only statement can filter on
// nothing).
func afterProjection(sql string) string {
	upper := strings.ToUpper(sql)
	depth := 0
	for i := 0; i < len(upper); i++ {
		switch upper[i] {
		case '(':
			depth++
		case ')':
			depth--
		case 'F':
			if depth != 0 || !strings.HasPrefix(upper[i:], "FROM") {
				continue
			}
			if i > 0 && !isSQLBreak(upper[i-1]) {
				continue
			}
			if i+4 < len(upper) && !isSQLBreak(upper[i+4]) {
				continue
			}
			return sql[i:]
		}
	}
	return ""
}

func isSQLBreak(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '(', ')', ',', ';':
		return true
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
