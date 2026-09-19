package sqlguard

import (
	"strings"
)

// DefaultTenantScopedColumns is the set of column names whose predicates bind
// tenant scope. `tenant_id` is the only one today; the schema-card work (P1a
// D1.1) may mark further columns `tenant_scoped: true` and pass them through
// ExtractAllTenantPredicatesWith. Matching is case-insensitive on the bare
// column token (an alias prefix such as `a.tenant_id` still matches on
// `tenant_id`).
var DefaultTenantScopedColumns = map[string]bool{"tenant_id": true}

// ErrNoTenantPredicate is returned when the statement carries no tenant-scoped
// equality at all. It is a *ValidationError so callers can log the reason.
var ErrNoTenantPredicate = &ValidationError{Reason: "no tenant_id = '<literal>' predicate found"}

// ExtractAllTenantPredicates scans the WHOLE raw statement (not just the first
// hit) and returns the string literal bound by EVERY tenant-scoped column
// occurrence, in source order. It is the D0 interim, tokenizer-grade extractor
// that replaced ExtractTenantEquals: where the old helper returned the first
// `tenant_id = '...'` and ignored everything after it, this one requires every
// `tenant_id` token to be followed by `=` and a single-quoted literal, and
// rejects any other shape outright:
//
//	tenant_id <> 'x'          tenant_id != 'x'        tenant_id IN ('a','b')
//	tenant_id IS NULL         tenant_id = other_col   tenant_id = $1
//	tenant_id = tenant_id     tenant_id OR ...        SELECT tenant_id, ...
//
// A statement with zero tenant predicates returns ErrNoTenantPredicate. The
// executor compares EVERY returned literal to the session tenant, so a second
// predicate `AND tenant_id = '<victim>'` can never widen scope. UUID literals
// bound to OTHER columns (park_id, load_id, goat_id, buyer_id, ...) are not
// inspected here: single-relation + the session tenant predicate already confine
// them to the caller's tenant.
//
// String literals are skipped as opaque data so a `tenant_id =` inside a quoted
// value is never mistaken for a predicate. Comments, `$` and non-ASCII outside
// literals are already rejected by Validate, which the executor always runs
// first; this function is deliberately conservative on anything it does not
// recognise (when in doubt it rejects).
func ExtractAllTenantPredicates(sql string) ([]string, error) {
	return ExtractAllTenantPredicatesWith(sql, DefaultTenantScopedColumns)
}

// ExtractAllTenantPredicatesWith is ExtractAllTenantPredicates with an explicit
// tenant-scoped column set (the schema-card hook). A nil/empty set falls back
// to DefaultTenantScopedColumns so a caller can never accidentally disable the
// check.
func ExtractAllTenantPredicatesWith(sql string, scoped map[string]bool) ([]string, error) {
	if len(scoped) == 0 {
		scoped = DefaultTenantScopedColumns
	}
	toks := lexRaw(sql)
	var out []string
	for i, t := range toks {
		if t.kind != rawIdent || !isScopedColumn(t.text, scoped) {
			continue
		}
		// The token must be a column reference: not itself the right-hand side of
		// a dotted reference where the left side is a schema (ceo_ai.tenant_id is
		// not a thing) — an alias prefix (`a.tenant_id`) is fine and is the
		// common shape, so no restriction on the preceding token is needed.
		if i+2 >= len(toks) {
			return nil, rejit("tenant predicate %q must be followed by = and a string literal", t.text)
		}
		op := toks[i+1]
		if op.kind != rawSym || op.text != "=" {
			return nil, rejit("tenant predicate %q must use = (got %q)", t.text, op.text)
		}
		rhs := toks[i+2]
		if rhs.kind != rawString {
			return nil, rejit("tenant predicate %q must bind a string literal (got %q)", t.text, rhs.text)
		}
		// `tenant_id = 'x' = ...` or `tenant_id = 'x' || ...` would extend the
		// expression past the literal; reject an operator immediately after.
		if i+3 < len(toks) {
			nxt := toks[i+3]
			if nxt.kind == rawSym {
				switch nxt.text {
				// A `::uuid` cast after the literal is harmless and allowed.
				case "=", "<", ">", "!", "|", "&", "+", "-", "*", "/", "%", "^", "~", "#", "(":
					return nil, rejit("tenant predicate %q literal must not be part of a larger expression", t.text)
				}
			}
		}
		out = append(out, rhs.text)
	}
	if len(out) == 0 {
		return nil, ErrNoTenantPredicate
	}
	return out, nil
}

func isScopedColumn(ident string, scoped map[string]bool) bool {
	for col := range scoped {
		if strings.EqualFold(ident, col) {
			return true
		}
	}
	return false
}

// trustedTenantParamCheck enforces the trusted-SQL tenant contract: every
// tenant-scoped column token must be followed by `=` and then either the `$1`
// bind parameter or another column reference (an equi-join between two
// tenant_id columns, e.g. `h.tenant_id = l.tenant_id`). A string literal, a
// different parameter, IN/IS/<>, or anything else is rejected, and at least one
// `tenant_id = $1` occurrence is required so the session tenant is always bound.
func trustedTenantParamCheck(sql string, scoped map[string]bool) error {
	if len(scoped) == 0 {
		scoped = DefaultTenantScopedColumns
	}
	toks := lexRaw(sql)
	bound := 0
	consumed := map[int]bool{} // RHS tenant columns of an equi-join, already checked
	for i, t := range toks {
		if consumed[i] || t.kind != rawIdent || !isScopedColumn(t.text, scoped) {
			continue
		}
		if i+2 >= len(toks) {
			return rejit("trusted SQL: tenant predicate %q must be followed by = and $1", t.text)
		}
		op := toks[i+1]
		if op.kind != rawSym || op.text != "=" {
			return rejit("trusted SQL: tenant predicate %q must use = (got %q)", t.text, op.text)
		}
		rhs := toks[i+2]
		switch rhs.kind {
		case rawString:
			return rejit("trusted SQL: tenant predicate %q must bind $1, not a string literal", t.text)
		case rawParam:
			if rhs.text != "$1" {
				return rejit("trusted SQL: tenant predicate %q must bind $1 (got %s)", t.text, rhs.text)
			}
			bound++
		case rawIdent:
			// Column-to-column equi-join. The joined column must itself be a
			// tenant-scoped column, otherwise `l.tenant_id = h.load_id` would pass.
			// An alias prefix means the ident we see here is the alias; look past
			// a following '.' to the real column name.
			col := rhs.text
			colIdx := i + 2
			if i+4 < len(toks) && toks[i+3].kind == rawSym && toks[i+3].text == "." && toks[i+4].kind == rawIdent {
				col = toks[i+4].text
				colIdx = i + 4
			}
			if !isScopedColumn(col, scoped) {
				return rejit("trusted SQL: tenant predicate %q may only join another tenant column (got %q)", t.text, col)
			}
			consumed[colIdx] = true
		default:
			return rejit("trusted SQL: tenant predicate %q must bind $1 (got %q)", t.text, rhs.text)
		}
	}
	if bound == 0 {
		return rejit("trusted SQL: at least one tenant_id = $1 predicate is required")
	}
	return nil
}

// --- raw lexer (literal-aware; used on the RAW statement, not the stripped one) ---

type rawKind int

const (
	rawIdent rawKind = iota
	rawNumber
	rawString // single-quoted literal; text holds the unescaped value
	rawParam  // $N bind placeholder; text holds "$N"
	rawSym    // single punctuation char
)

type rawToken struct {
	kind rawKind
	text string
}

// lexRaw tokenizes raw SQL keeping string literals (as one token with their
// unescaped value) and $N parameters. Unterminated literals consume to the end
// of input and are emitted as a string token; Validate has already rejected
// unbalanced quotes for model SQL, and trusted SQL is server-authored.
func lexRaw(sql string) []rawToken {
	runes := []rune(sql)
	n := len(runes)
	var out []rawToken
	i := 0
	for i < n {
		c := runes[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f':
			i++
		case c == '\'':
			val, end, _ := readLiteral(runes, i)
			out = append(out, rawToken{kind: rawString, text: val})
			i = end
		case c == '$':
			j := i + 1
			for j < n && runes[j] >= '0' && runes[j] <= '9' {
				j++
			}
			if j > i+1 {
				out = append(out, rawToken{kind: rawParam, text: string(runes[i:j])})
			} else {
				out = append(out, rawToken{kind: rawSym, text: "$"})
			}
			i = j
		case isIdentStart(c):
			j := i + 1
			for j < n && isIdentPart(runes[j]) {
				j++
			}
			out = append(out, rawToken{kind: rawIdent, text: string(runes[i:j])})
			i = j
		case c >= '0' && c <= '9':
			j := i + 1
			for j < n && (runes[j] >= '0' && runes[j] <= '9' || runes[j] == '.') {
				j++
			}
			out = append(out, rawToken{kind: rawNumber, text: string(runes[i:j])})
			i = j
		default:
			out = append(out, rawToken{kind: rawSym, text: string(c)})
			i++
		}
	}
	return out
}
