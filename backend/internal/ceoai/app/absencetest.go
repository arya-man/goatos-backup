package app

// absencetest.go: a column whose BLANK means "not that kind of row" may not be
// asked whether it is NULL.
//
// THIS IS THE THIRD SPELLING OF ONE DEFECT, AND THE FIRST FIX THAT IS NOT
// PROSE. Asked which roles have no backup coverage, the planner first wrote
// `coverage_status <> 'covered'`; the view never emits 'covered', so the
// predicate matched all 16 rows and the answer named 10 roles as staffing gaps
// when 4 are. Giving the card the column's real vocabulary closed that
// spelling, verified live. The planner then wrote `backup_label IS NULL`
// instead and produced the SAME ten roles — three runs out of three — because
// the view fills backup_label only on the covered_by_backup rows, so its
// absence is the ordinary state of a role nobody is away from.
//
// Both spellings share the shape that makes them dangerous: the filter RETURNS
// ROWS. A predicate that matched nothing would show up as an empty answer and
// be questioned; one that matches everything reads as a real finding, and no
// downstream check — not the guard, not the composer, not the reader — can
// tell it did nothing. So the statement has to be refused before it runs, and
// the refusal names the column that DOES decide, so the re-plan has somewhere
// to go.
//
// HOW IT DECIDES, AND WHY NOT BY SPELLING. The first version of this gate
// matched the SHAPE of the predicate — `IS NULL` or `= ''` written beside the
// column — and a review round drove seven ordinary re-spellings through it
// (`nullif(x,'') IS NULL`, `coalesce(x,'none') = 'none'`, `length(x) = 0`,
// `x::text IS NULL`, `upper(x) IS NULL`, `x || '' = ''`, `coalesce(x) IS NULL`)
// plus aliasing the column in a CTE and counting it. Every one of those is a
// phrasing a model reaches for immediately AFTER a NULL test has been refused,
// so a shape list makes the gate worse than useless: it closes the one spelling
// that was measured and advertises the ones that remain.
//
// The gate therefore READS the predicate around the column as an expression
// (absenceexpr.go) and evaluates it three times, with the column standing for
// NULL, for the empty string, and for an ordinary filled value. A predicate
// that is TRUE for a blank column and FALSE for a filled one is the false
// question — in any spelling, through any wrapper that preserves blankness,
// because the wrapper is evaluated rather than recognised. A predicate the
// reader cannot evaluate at all, applied to this column, is refused too, and
// says so.
//
// It is still a NARROW gate. It fires only on a column a card explicitly
// declares, only in a predicate, and never on a comparison to a value:
// `backup_label = 'Backup 6'` is a perfectly good question and passes — and so
// does `backup_label IS NOT NULL`, which is the CORRECT way to ask which roles
// do have a named backup and which the shape-matching version wrongly refused.
// Nothing here loosens sqlguard, touches the tenant predicate, or changes which
// rows a tenant can see — it only ever REFUSES a statement the guard would
// otherwise have run.

import (
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// validateAbsenceTests refuses a model-drafted statement that asks whether one
// of the read's blank-is-not-absence columns is empty.
func validateAbsenceTests(sql string) error {
	card, found := reporting.CardForSQL(sql)
	if !found || len(card.BlankIsNotAbsence) == 0 {
		return nil
	}
	guarded := make(map[string]bool, len(card.BlankIsNotAbsence))
	for _, c := range card.BlankIsNotAbsence {
		guarded[strings.ToLower(strings.TrimSpace(c))] = true
	}
	toks := significantTokens(lexSQLTokens(sql))
	if len(toks) == 0 {
		return nil
	}
	// A name the statement gives the column is the same column. Without this
	// the whole gate is one `SELECT backup_label AS bl` away from irrelevant.
	addProjectedAliases(toks, guarded)

	depth := parenDepths(toks)

	if col, ok := countOverGuardedColumn(toks, depth, guarded); ok {
		return fmt.Errorf(
			"counting %s counts only the rows it is filled on, so the rows it skips are not missing things: ask %s instead of counting %s",
			col, card.AbsenceAuthority, col)
	}

	for i, tok := range toks {
		if tok.kind != tokIdent || !guarded[strings.ToLower(tok.text)] {
			continue
		}
		atom := predicateAtomAround(toks, depth, i)
		switch judgeAtom(atom, guarded) {
		case blankTestsForEmptiness:
			return fmt.Errorf(
				"%s is filled only on the rows it applies to, so a blank one is not a missing thing: ask %s instead of testing %s for emptiness",
				tok.text, card.AbsenceAuthority, tok.text)
		case blankUnreadable:
			return fmt.Errorf(
				"%s is filled only on the rows it applies to, and this predicate wraps it in something that cannot be read as a plain comparison, so whether it asks for emptiness cannot be checked: ask %s instead of testing %s through a wrapper",
				tok.text, card.AbsenceAuthority, tok.text)
		}
	}
	return nil
}

// judgeAtom reads one predicate atom and returns the three-point verdict. An
// atom that does not parse is NOT waved through when it carries the vocabulary
// of an emptiness test: the whole point of this gate is that the unreadable
// case is the dangerous one.
func judgeAtom(atom []sqlToken, guarded map[string]bool) blankVerdict {
	if len(atom) == 0 {
		return blankNotATest
	}
	n, ok := parseSQLExpression(atom)
	if !ok {
		if atomCarriesEmptinessVocabulary(atom) {
			return blankUnreadable
		}
		return blankNotATest
	}
	return judgeBlanknessTest(n, guarded)
}

// atomCarriesEmptinessVocabulary is the fallback when the expression reader
// cannot parse the atom at all. It is deliberately the crude test — it decides
// only whether an UNREADABLE fragment gets the benefit of the doubt, and a
// fragment mentioning NULL or the empty string next to this column does not.
func atomCarriesEmptinessVocabulary(atom []sqlToken) bool {
	for _, t := range atom {
		switch t.kind {
		case tokIdent:
			switch strings.ToLower(t.text) {
			case "null", "coalesce", "nullif", "length", "char_length", "count":
				return true
			}
		case tokString:
			if t.text == "''" {
				return true
			}
		case tokOther:
			if t.text == "=" || t.text == "<>" || t.text == "!=" {
				return true
			}
		}
	}
	return false
}

// significantTokens drops whitespace and comments. Comments are dropped rather
// than kept because a column named inside one is not a predicate: the earlier
// lexer had no comment rule at all, so `-- backup_label IS NULL is the wrong
// test` refused the statement and named a predicate it did not contain.
func significantTokens(toks []sqlToken) []sqlToken {
	out := make([]sqlToken, 0, len(toks))
	for _, t := range toks {
		if t.kind == tokSpace || t.kind == tokComment {
			continue
		}
		out = append(out, t)
	}
	return out
}

// parenDepths returns each token's nesting depth, with both parentheses of a
// pair carrying the depth OUTSIDE them, so "stop when the depth drops below
// mine" means "stop at the parenthesis that encloses me".
func parenDepths(toks []sqlToken) []int {
	out := make([]int, len(toks))
	d := 0
	for i, t := range toks {
		if t.kind == tokOther && t.text == ")" {
			d--
		}
		if d < 0 {
			d = 0
		}
		out[i] = d
		if t.kind == tokOther && t.text == "(" {
			d++
		}
	}
	return out
}

// addProjectedAliases folds `<guarded> AS bl` and `<guarded> bl` into the
// guarded set, so a CTE that renames the column on the way out does not shake
// the gate off. Two passes, because an alias can itself be aliased.
func addProjectedAliases(toks []sqlToken, guarded map[string]bool) {
	for pass := 0; pass < 2; pass++ {
		for i, tok := range toks {
			if tok.kind != tokIdent || !guarded[strings.ToLower(tok.text)] {
				continue
			}
			j := i + 1
			if j < len(toks) && toks[j].kind == tokIdent && strings.EqualFold(toks[j].text, "as") {
				j++
			}
			if j >= len(toks) || j == i+1 {
				// Without AS, an adjacent identifier is only an alias in a
				// projection list; requiring AS here keeps `backup_label IS
				// NULL` from registering `IS` as an alias.
				continue
			}
			if toks[j].kind != tokIdent || isSQLWordThatIsNeverAnAlias(toks[j].text) {
				continue
			}
			guarded[strings.ToLower(toks[j].text)] = true
		}
	}
}

func isSQLWordThatIsNeverAnAlias(s string) bool {
	switch strings.ToLower(s) {
	case "is", "and", "or", "not", "in", "like", "ilike", "between", "from",
		"where", "group", "order", "by", "having", "limit", "offset", "null",
		"then", "else", "end", "when", "case", "select", "join", "on", "union",
		"distinct", "filter", "over", "partition", "asc", "desc":
		return true
	}
	return false
}

// countOverGuardedColumn finds `count(<expr mentioning the column>)`. count is
// the one aggregate that reads blankness as arithmetic: it skips NULLs
// silently, so `count(backup_label)` against `count(*)` reproduces the exact
// ten-versus-four framing as a ratio, with no NULL keyword written anywhere for
// the predicate reader to see.
func countOverGuardedColumn(toks []sqlToken, depth []int, guarded map[string]bool) (string, bool) {
	for i, t := range toks {
		if t.kind != tokIdent || !strings.EqualFold(t.text, "count") {
			continue
		}
		if i+1 >= len(toks) || toks[i+1].kind != tokOther || toks[i+1].text != "(" {
			continue
		}
		inner := depth[i+1] + 1
		for j := i + 2; j < len(toks); j++ {
			if depth[j] < inner {
				break
			}
			if toks[j].kind == tokIdent && guarded[strings.ToLower(toks[j].text)] {
				return toks[j].text, true
			}
		}
	}
	return "", false
}

// predicateAtomAround returns the smallest run of tokens that could be the
// predicate this occurrence of the column sits in.
//
// It grows in two steps, and the split matters. FIRST it climbs out of every
// function call wrapping the column, because `coalesce(backup_label, ”)` is
// one operand and cutting it at the comma would hide the comparison that
// follows. THEN, at that level, it runs to the nearest boolean or clause
// boundary. A parenthesis that is NOT a function call — a grouped condition, a
// FILTER clause — is a boundary rather than something to climb through, so a
// nested condition is judged on its own and not smeared into its neighbours.
func predicateAtomAround(toks []sqlToken, depth []int, i int) []sqlToken {
	lo, hi := i, i
	for {
		open, close, ok := enclosingParens(toks, depth, lo, hi)
		if !ok {
			break
		}
		call := open > 0 && toks[open-1].kind == tokIdent && !isNonFunctionParenKeyword(toks[open-1].text)
		if !call && !isTransparentGrouping(toks, depth, open, close) {
			break
		}
		if call {
			lo, hi = open-1, close
		} else {
			lo, hi = open, close
		}
	}
	base := depth[lo]
	for j := lo - 1; j >= 0; j-- {
		if depth[j] < base || (depth[j] == base && isAtomBoundary(toks, j)) {
			break
		}
		lo = j
	}
	for j := hi + 1; j < len(toks); j++ {
		if depth[j] < base || (depth[j] == base && isAtomBoundary(toks, j)) {
			break
		}
		hi = j
	}
	return toks[lo : hi+1]
}

// isTransparentGrouping reports a plain grouping parenthesis — `(backup_label)`,
// `(backup_label || ”)`, `(CASE … END)` — which the atom may climb out of
// because the parenthesis changes nothing about the expression inside it. A
// parenthesised CASE is transparent for the same reason the CASE words are not
// atom boundaries: stopping at the `)` would drop the `= 1` that decides which
// arm the statement is selecting for.
//
// A parenthesis holding a boolean of its own (`(a IS NULL OR b)`) or a clause
// (`(WHERE ...)`, `(SELECT ...)`) is NOT transparent: climbing out of it would
// fold this column's predicate together with a neighbour's, and the three-point
// test on the pair reads "opaque OR true", which answers nothing about this
// column. Those are judged from the inside, one predicate at a time.
func isTransparentGrouping(toks []sqlToken, depth []int, open, close int) bool {
	inner := depth[open] + 1
	for j := open + 1; j < close; j++ {
		if depth[j] != inner || toks[j].kind != tokIdent {
			continue
		}
		switch strings.ToLower(toks[j].text) {
		case "and", "or", "select", "where", "having":
			return false
		}
	}
	return true
}

// enclosingParens finds the innermost pair of parentheses containing the span.
func enclosingParens(toks []sqlToken, depth []int, lo, hi int) (int, int, bool) {
	want := depth[lo] - 1
	if want < 0 {
		return 0, 0, false
	}
	open := -1
	for j := lo - 1; j >= 0; j-- {
		if depth[j] == want && toks[j].kind == tokOther && toks[j].text == "(" {
			open = j
			break
		}
		if depth[j] < want {
			return 0, 0, false
		}
	}
	if open < 0 {
		return 0, 0, false
	}
	for j := hi + 1; j < len(toks); j++ {
		if depth[j] == want && toks[j].kind == tokOther && toks[j].text == ")" {
			return open, j, true
		}
	}
	return 0, 0, false
}

// isNonFunctionParenKeyword names the words that can precede a parenthesis
// WITHOUT it being a call whose argument list the column belongs to. `FILTER
// (WHERE ...)` is the important one: its parentheses hold a condition of their
// own, which must be judged on its own terms.
func isNonFunctionParenKeyword(s string) bool {
	switch strings.ToLower(s) {
	case "filter", "in", "exists", "any", "all", "some", "values", "and", "or",
		"not", "where", "having", "on", "when", "then", "else", "by", "select",
		"from", "over", "partition", "union", "distinct":
		return true
	}
	return false
}

// isAtomBoundary reports the tokens that end a predicate atom at its own level.
//
// CASE's own words — case/when/then/else/end — are deliberately NOT boundaries.
// They used to be, and that one omission carried the whole live defect straight
// through the evaluator: cutting at `when` hands the reader only the WHEN
// CONDITION, so `CASE WHEN backup_label IS NOT NULL THEN 0 ELSE 1 END = 1` was
// judged as the inner atom `backup_label IS NOT NULL` — correctly "not a
// blankness test" in isolation — while the blankness test was carried entirely
// by the arm VALUES and the comparison after END, which the gate never saw.
// The atom must be the whole conditional and its comparison, so that inverting
// the arms inverts the evaluated answer instead of hiding it.
func isAtomBoundary(toks []sqlToken, j int) bool {
	t := toks[j]
	if t.kind == tokOther {
		return t.text == "," || t.text == ";"
	}
	if t.kind != tokIdent {
		return false
	}
	switch strings.ToLower(t.text) {
	case "from":
		// `IS NOT DISTINCT FROM` is one operator, not a FROM clause.
		if j > 0 && toks[j-1].kind == tokIdent && strings.EqualFold(toks[j-1].text, "distinct") {
			return false
		}
		return true
	case "and":
		// BETWEEN's AND belongs to the comparison it is part of.
		for k := j - 1; k >= 0 && k >= j-6; k-- {
			if toks[k].kind == tokIdent && strings.EqualFold(toks[k].text, "between") {
				return false
			}
		}
		return true
	case "or", "where", "having", "on",
		"select", "group", "order", "by", "limit", "offset", "union",
		"intersect", "except", "join", "inner", "left", "right", "full",
		"cross", "as", "with", "filter", "over", "partition", "using",
		"returning", "asc", "desc":
		return true
	}
	return false
}
