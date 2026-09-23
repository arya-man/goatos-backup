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
// It is a NARROW gate on purpose. It fires only on a column a card explicitly
// declares, only on a test for emptiness, and never on a comparison to a
// value: `backup_label = 'Backup 6'` is a perfectly good question and passes.
// Nothing here loosens sqlguard, touches the tenant predicate, or changes
// which rows a tenant can see — it only ever REFUSES a statement the guard
// would otherwise have run.

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
	toks := lexSQLTokens(sql)
	for i, tok := range toks {
		if tok.kind != tokIdent || !guarded[strings.ToLower(tok.text)] {
			continue
		}
		if !testsForEmptiness(toks, i) {
			continue
		}
		return fmt.Errorf(
			"%s is filled only on the rows it applies to, so a blank one is not a missing thing: ask %s instead of testing %s for emptiness",
			tok.text, card.AbsenceAuthority, tok.text)
	}
	return nil
}

// testsForEmptiness reports that the identifier at toks[i] is the subject of a
// NULL test or a comparison against the empty string, in either order. It
// looks at the significant tokens around the column and nothing else — a
// `coalesce(backup_label, '')` wrapper is caught by the `= ''` on its right,
// and a bare mention in a SELECT list or a GROUP BY is not caught at all,
// which is correct: rendering the column is not the same as judging by it.
func testsForEmptiness(toks []sqlToken, i int) bool {
	right := significantAfter(toks, i)
	if len(right) >= 2 && eqFold(right[0], "is") {
		// IS NULL, and IS NOT NULL — the inverse of the same false question.
		if eqFold(right[1], "null") {
			return true
		}
		if len(right) >= 3 && eqFold(right[1], "not") && eqFold(right[2], "null") {
			return true
		}
	}
	if comparesToEmptyString(right) {
		return true
	}
	// `'' = backup_label`, the same predicate written the other way round.
	return comparesToEmptyString(significantBefore(toks, i))
}

// comparesToEmptyString reports a `= ''` / `<> ''` / `!= ''` applied to the
// column beside it.
//
// The operator is not always adjacent — `coalesce(backup_label, '')` puts a
// comma, a literal and a closing paren in between — so the scan steps over
// exactly those three shapes and STOPS at anything else. That narrowness is
// the point: an unbounded look-ahead would read the `= ''` of an unrelated
// predicate further along the WHERE clause as this column's, and refuse a
// statement that never asked the false question at all.
func comparesToEmptyString(side []string) bool {
	for _, tok := range side {
		switch {
		case isEqualityOperator(tok):
			// The operand is whatever follows; found below by returning here.
			return operandAfter(side, tok)
		case tok == "," || tok == ")" || tok == "(" || strings.HasPrefix(tok, "'"):
			continue
		default:
			return false
		}
	}
	return false
}

// operandAfter reports that the token following the first occurrence of op in
// side is the empty string literal.
func operandAfter(side []string, op string) bool {
	for i, tok := range side {
		if tok == op {
			return i+1 < len(side) && isEmptyStringLiteral(side[i+1])
		}
	}
	return false
}

// isEqualityOperator covers both senses: `<> ''` asks the same false question
// as `= ''`, inverted, and is just as wrong an answer to "who has no backup".
// The lexer emits `<>` and `!=` whole.
func isEqualityOperator(tok string) bool { return tok == "=" || tok == "<>" || tok == "!=" }

// isEmptyStringLiteral reports the lexer's rendering of `''`, which carries
// its own quotes.
func isEmptyStringLiteral(tok string) bool { return tok == "''" }

// significantAfter returns up to six non-space token texts after index i.
func significantAfter(toks []sqlToken, i int) []string {
	var out []string
	for j := i + 1; j < len(toks) && len(out) < 6; j++ {
		if toks[j].kind == tokSpace {
			continue
		}
		out = append(out, toks[j].text)
	}
	return out
}

// significantBefore returns up to six non-space token texts before index i,
// nearest first, so the caller reads outward from the column exactly as
// significantAfter does.
func significantBefore(toks []sqlToken, i int) []string {
	var out []string
	for j := i - 1; j >= 0 && len(out) < 6; j-- {
		if toks[j].kind == tokSpace {
			continue
		}
		out = append(out, toks[j].text)
	}
	return out
}

func eqFold(a, b string) bool { return strings.EqualFold(a, b) }
