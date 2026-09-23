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
		// THE QUALIFIER TRAVELS WITH THE COLUMN. A model drafts
		// `FROM ceo_ai.weighing_latest_individual_weight w WHERE w.animal_key =
		// 'MG-100001'`, which is ordinary legal SQL the guard's own bypass
		// corpus expects. Splicing from the bare token while folding only the
		// bare token composed the two into `w.upper(trim(animal_key))`, which
		// the guard then rejects as a schema-qualified function call -- so a
		// read that worked before this file existed failed, silently, on the
		// exact question this file was written to fix. The whole qualified
		// reference is consumed and re-emitted inside the fold.
		start := qualifiedStart(sql, toks, i)
		op := nextSignificant(toks, i)
		if op < 0 {
			continue
		}
		rewritten, end, ok := foldComparison(sql, toks, start, i, op)
		if !ok {
			continue
		}
		if start < cursor {
			continue
		}
		out.WriteString(sql[cursor:start])
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

// qualifiedStart walks back over `<ident> .` pairs so `w.animal_key` and
// `ceo_ai.weighing_latest_individual_weight.animal_key` are treated as one
// reference. It returns the byte offset the whole reference starts at.
func qualifiedStart(sql string, toks []sqlToken, col int) int {
	start := toks[col].start
	i := col
	for i >= 2 {
		dot := previousSignificant(toks, i)
		if dot < 0 || toks[dot].text != "." {
			break
		}
		qual := previousSignificant(toks, dot)
		if qual < 0 || toks[qual].kind != tokIdent {
			break
		}
		start = toks[qual].start
		i = qual
	}
	return start
}

func previousSignificant(toks []sqlToken, i int) int {
	for j := i - 1; j >= 0; j-- {
		if toks[j].kind != tokSpace {
			return j
		}
	}
	return -1
}

// foldComparison returns the rewritten text for the comparison whose column
// reference starts at byte offset refStart and whose bare identifier token is
// toks[col], with operator token toks[op], plus the offset just past it. ok is
// false when the shape is not one this rewrites (notably ILIKE, which is
// already case-insensitive, and any right-hand side that is not a plain string
// literal -- a column-to-column join must not be folded).
func foldComparison(sql string, toks []sqlToken, refStart, col, op int) (string, int, bool) {
	name := sql[refStart:toks[col].end]
	folded := "upper(trim(" + name + "))"
	switch strings.ToLower(toks[op].text) {
	case "=", "<>", "!=", "like":
		rhs := nextSignificant(toks, op)
		if rhs < 0 || toks[rhs].kind != tokString {
			return "", 0, false
		}
		if !endsComparison(toks, rhs) {
			return "", 0, false
		}
		return folded + " " + sql[toks[op].start:toks[op].end] + " upper(trim(" + normalizedLiteral(sql[toks[rhs].start:toks[rhs].end]) + "))", toks[rhs].end, true
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
			lits = append(lits, "upper(trim("+normalizedLiteral(sql[toks[next].start:toks[next].end])+"))")
			sep := nextSignificant(toks, next)
			if sep < 0 {
				return "", 0, false
			}
			if toks[sep].text == ")" {
				if !endsComparison(toks, sep) {
					return "", 0, false
				}
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

// endsComparison reports that nothing binds to the right of the token the fold
// is about to close its parens after.
//
// The rewrite moves the literal INSIDE `upper(trim(...))`, so anything that
// used to bind to it is pushed outside and re-associates:
// `animal_key = 'MG-' || 'a0001'` became
// `upper(trim(animal_key)) = upper(trim('MG-')) || 'a0001'`, which passes the
// guard, EXECUTES, and compares an upper-cased column against a string whose
// tail is still lower case -- a row that matched before now does not. A `::`
// cast is the one thing that may legitimately follow, and it is applied to the
// literal either way, so it is allowed through.
func endsComparison(toks []sqlToken, last int) bool {
	next := nextSignificant(toks, last)
	if next < 0 {
		return true
	}
	if toks[next].text == ":" {
		return true
	}
	switch toks[next].kind {
	case tokOther:
		return toks[next].text == ")" || toks[next].text == ";" || toks[next].text == ","
	case tokIdent:
		switch strings.ToUpper(toks[next].text) {
		case "AND", "OR", "ORDER", "GROUP", "LIMIT", "OFFSET", "HAVING", "WINDOW", "FETCH":
			return true
		}
	}
	return false
}

// normalizedLiteral collapses runs of whitespace INSIDE a string literal, which
// is the half of the identity module's normal form `upper(trim(...))` cannot
// express in SQL the guard allows.
//
// `sales_buyer_summary.buyer_key` is
// `lower(regexp_replace(btrim(d.buyer_name), '\s+', ' ', 'g'))`
// (000393:256), so the STORED key already has its internal whitespace
// collapsed, while `upper(trim(col))` only strips the ends. A reader typing
// `'Ravi  Traders'` -- exactly the spelling variance that normalisation exists
// to absorb -- still got "no records found". `regexp_replace` is deliberately
// NOT in sqlguard's closed function set and this does not add it: the LITERAL
// is ours to rewrite before the statement is validated, and collapsing it there
// makes both sides agree without widening what a model may draft.
func normalizedLiteral(literal string) string {
	return strings.Join(strings.Fields(literal), " ")
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
	// tokComment is `-- to end of line` and `/* ... */`. sqlguard refuses both
	// outright, but this lexer runs BEFORE the guard, so it has to know what a
	// comment is or every word inside one is read as a column name.
	tokComment
)

type sqlToken struct {
	kind       sqlTokenKind
	text       string
	start, end int
}

// lexSQLTokens splits a statement into whitespace, comments,
// identifiers/keywords, single-quoted string literals (doubled-quote escapes
// included) and single other bytes. It is deliberately minimal: it exists so
// the rewrite above can tell a column name from the same word INSIDE a literal
// or a comment, not to parse SQL.
//
// ON ORDERING, which an earlier version of this comment had backwards: this
// lexer runs BEFORE sqlguard.Validate, not after. registry.go rewrites the
// statement at :138 and validates it at :143, so every byte of a
// comment-bearing statement passes through here first. The guard does reject
// `--` and `/* */` a moment later, which means a comment cannot reach
// execution — but "the next check happens to catch it" is not a reason for
// this one to mis-read it, and it was not one: with no comment rule at all, a
// column named in a comment was read as a column and refused a statement that
// did not contain the predicate the refusal described.
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
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			j := i + 2
			for j < len(sql) && sql[j] != '\n' {
				j++
			}
			toks = append(toks, sqlToken{kind: tokComment, text: sql[i:j], start: i, end: j})
			i = j
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			j := i + 2
			for j < len(sql) {
				if sql[j] == '*' && j+1 < len(sql) && sql[j+1] == '/' {
					j += 2
					break
				}
				j++
			}
			if j > len(sql) {
				j = len(sql)
			}
			toks = append(toks, sqlToken{kind: tokComment, text: sql[i:j], start: i, end: j})
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
