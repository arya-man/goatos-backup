package app

// identityfilter.go: make a question's spelling of an animal tag or a buyer
// name match the way the ceo_ai views STORE it.
//
// `ceo_ai.weighing_latest_individual_weight.animal_key` and
// `ceo_ai.growth_adg_pairs.animal_key` are `lower(btrim(...))` of the ear tag,
// and `sales_buyer_summary.buyer_key` is `lower(...)` of the buyer name: the
// folding is what collapses two spellings of one animal, or one buyer, into a
// single row. Nothing about a QUESTION knows that. A reader asks about
// `MG-100001` — which is exactly how `goat_identifiers.normalized_value`
// stores the tag (`upper(lower(btrim(...)))`) and exactly how it is printed on
// the ear tag — a model-drafted read filters `animal_key = 'MG-100001'`, and
// the read comes back empty. The reader is then told "No records found", which
// is not true: the animal is there, under `mg-100001`.
//
// That is a fail-closed-and-quiet bug, so it is fixed deterministically and
// before execution rather than by the model-repair loop: the comparison itself
// is rewritten to the identity module's own normal form, `upper(trim(x))` on
// BOTH sides. Only identity-key columns named by the view's schema card are
// touched (reporting.SchemaCard.IdentityKeyColumns), never `tenant_id`, never
// a measure, never anything inside a string literal — so the tenant conjunct
// the guard binds on is left byte-identical.

import (
	"strings"

	"github.com/vgoats/goatos/backend/internal/ceoai/reporting"
)

// normalizeIdentityFilters rewrites `<identity_key> = 'Tag'`, `LIKE 'Tag%'`
// and `IN ('A','B')` comparisons into `upper(trim(<identity_key>))` against
// `upper(trim('Tag'))`, so the comparison is case- and padding-insensitive.
// A statement whose view has no identity-key column, or that has no such
// comparison, is returned unchanged.
func normalizeIdentityFilters(sql string) string {
	card, ok := reporting.CardForSQL(sql)
	if !ok {
		return sql
	}
	keys := card.IdentityKeyColumns()
	if len(keys) == 0 {
		return sql
	}
	isKey := make(map[string]bool, len(keys))
	for _, k := range keys {
		isKey[strings.ToLower(k)] = true
	}
	toks := lexSQLTokens(sql)
	if len(toks) == 0 {
		return sql
	}

	// out is built from the ORIGINAL bytes, so every byte not part of a
	// rewritten comparison survives verbatim (whitespace, casing, comments the
	// guard already rejected, everything).
	var out strings.Builder
	out.Grow(len(sql) + 64)
	cursor := 0
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.kind != tokIdent || !isKey[strings.ToLower(t.text)] {
			continue
		}
		op := nextSignificant(toks, i)
		if op < 0 {
			continue
		}
		rewritten, end, ok := foldComparison(sql, toks, i, op)
		if !ok {
			continue
		}
		out.WriteString(sql[cursor:t.start])
		out.WriteString(rewritten)
		cursor = end
		// Continue AFTER the comparison we just consumed.
		for i+1 < len(toks) && toks[i+1].start < cursor {
			i++
		}
	}
	if cursor == 0 {
		return sql
	}
	out.WriteString(sql[cursor:])
	return out.String()
}

// foldComparison returns the rewritten text for the comparison that starts at
// the identity-key token toks[col] with operator token toks[op], plus the
// offset just past it. ok is false when the shape is not one this rewrites
// (notably ILIKE, which is already case-insensitive, and any right-hand side
// that is not a string literal — a column-to-column join must not be folded).
func foldComparison(sql string, toks []sqlToken, col, op int) (string, int, bool) {
	name := sql[toks[col].start:toks[col].end]
	folded := "upper(trim(" + name + "))"
	switch strings.ToLower(toks[op].text) {
	case "=", "<>", "!=", "like":
		rhs := nextSignificant(toks, op)
		if rhs < 0 || toks[rhs].kind != tokString {
			return "", 0, false
		}
		return folded + " " + sql[toks[op].start:toks[op].end] + " upper(trim(" + sql[toks[rhs].start:toks[rhs].end] + "))", toks[rhs].end, true
	case "in":
		open := nextSignificant(toks, op)
		if open < 0 || toks[open].text != "(" {
			return "", 0, false
		}
		var lits []string
		i := open
		for {
			next := nextSignificant(toks, i)
			if next < 0 {
				return "", 0, false
			}
			if toks[next].kind != tokString {
				return "", 0, false
			}
			lits = append(lits, "upper(trim("+sql[toks[next].start:toks[next].end]+"))")
			sep := nextSignificant(toks, next)
			if sep < 0 {
				return "", 0, false
			}
			if toks[sep].text == ")" {
				return folded + " IN (" + strings.Join(lits, ", ") + ")", toks[sep].end, true
			}
			if toks[sep].text != "," {
				return "", 0, false
			}
			i = sep
		}
	}
	return "", 0, false
}

func nextSignificant(toks []sqlToken, i int) int {
	for j := i + 1; j < len(toks); j++ {
		if toks[j].kind != tokSpace {
			return j
		}
	}
	return -1
}

type sqlTokenKind int

const (
	tokSpace sqlTokenKind = iota
	tokIdent
	tokString
	tokOther
)

type sqlToken struct {
	kind       sqlTokenKind
	text       string
	start, end int
}

// lexSQLTokens splits a statement into whitespace, identifiers/keywords,
// single-quoted string literals (doubled-quote escapes included) and single
// other bytes. It is
// deliberately minimal: it exists so the rewrite above can tell a column name
// from the same word INSIDE a literal, not to parse SQL. The guard
// (sqlguard.Validate) has already rejected comments, stacking and quoted
// identifiers by the time any of this runs.
func lexSQLTokens(sql string) []sqlToken {
	var toks []sqlToken
	i := 0
	for i < len(sql) {
		c := sql[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			j := i
			for j < len(sql) && (sql[j] == ' ' || sql[j] == '\t' || sql[j] == '\n' || sql[j] == '\r') {
				j++
			}
			toks = append(toks, sqlToken{kind: tokSpace, text: sql[i:j], start: i, end: j})
			i = j
		case c == '\'':
			j := i + 1
			for j < len(sql) {
				if sql[j] == '\'' {
					if j+1 < len(sql) && sql[j+1] == '\'' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			toks = append(toks, sqlToken{kind: tokString, text: sql[i:j], start: i, end: j})
			i = j
		case isIdentByte(c) && !(c >= '0' && c <= '9'):
			j := i
			for j < len(sql) && isIdentByte(sql[j]) {
				j++
			}
			toks = append(toks, sqlToken{kind: tokIdent, text: sql[i:j], start: i, end: j})
			i = j
		case c == '<' && i+1 < len(sql) && sql[i+1] == '>', c == '!' && i+1 < len(sql) && sql[i+1] == '=':
			toks = append(toks, sqlToken{kind: tokOther, text: sql[i : i+2], start: i, end: i + 2})
			i += 2
		default:
			toks = append(toks, sqlToken{kind: tokOther, text: sql[i : i+1], start: i, end: i + 1})
			i++
		}
	}
	return toks
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
