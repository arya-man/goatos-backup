// Package sqlguard is the server-side safety validator and executor for the
// CEO/leadership assistant's read-only SQL fallback tier (routing tier 4 in
// docs/ceo-ai/ceo-chatbot-purpose-and-build-plan.md and mcp-toolbox-plan.md).
//
// Gemini may DRAFT a read-only query over the governed ceo_ai.* reporting
// schema, but the model never executes SQL and never decides permissions. Every
// draft flows through Validate before it can reach ExecuteReadOnly, which runs
// it on a dedicated non-privileged pool (mesha_ceo_readonly) inside a READ ONLY
// transaction with a statement timeout and a hard row cap.
//
// The validator is intentionally pure-Go and dependency-free: it tokenizes the
// statement (stripping string literals first so keywords/identifiers inside
// literals cannot smuggle instructions), then enforces a deny-by-default policy.
// It rejects anything that is not a single, bounded, tenant-scoped SELECT over
// ceo_ai.* tables. It is a guard, not a parser: when in doubt it rejects.
package sqlguard

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// MaxRowLimit is the hard upper bound the fallback SQL may request and the cap
// the executor wraps every query with. Leadership answers are aggregate-first;
// raw dumps are never a valid fallback.
const MaxRowLimit = 100

// MaxSQLBytes bounds the accepted statement size to keep tokenization cheap and
// to reject pathological model output.
const MaxSQLBytes = 8000

// AllowedSchema is the ONLY schema the fallback may read from. The dedicated
// role has no grants on public/pg_catalog/information_schema, but the validator
// enforces it independently (defense in depth) rather than trusting role grants.
const AllowedSchema = "ceo_ai"

// ValidationError is returned by Validate for any policy violation. The Reason
// is safe to log at the boundary; it never echoes bound literal values.
type ValidationError struct {
	Reason string
}

func (e *ValidationError) Error() string { return "sqlguard: " + e.Reason }

func rejit(format string, a ...any) error {
	return &ValidationError{Reason: fmt.Sprintf(format, a...)}
}

// ErrEmpty is a sentinel for an empty statement.
var ErrEmpty = &ValidationError{Reason: "empty statement"}

// bannedKeywords are tokens that must never appear in a fallback query. This
// list is the union of DML, DDL, DCL, transaction/session control, procedural,
// and data-movement verbs from the two design docs. Matching is case-insensitive
// and whole-token (so a column literally named e.g. "created_at" is unaffected).
var bannedKeywords = map[string]struct{}{
	// DML writes
	"INSERT": {}, "UPDATE": {}, "DELETE": {}, "UPSERT": {}, "MERGE": {},
	"REPLACE": {}, "RETURNING": {},
	// data movement
	"COPY": {}, "IMPORT": {}, "LOAD": {},
	// DDL
	"ALTER": {}, "CREATE": {}, "DROP": {}, "TRUNCATE": {}, "RENAME": {},
	"COMMENT": {}, "REINDEX": {}, "CLUSTER": {}, "REFRESH": {},
	// DCL
	"GRANT": {}, "REVOKE": {},
	// maintenance / session / txn control
	"ANALYZE": {}, "VACUUM": {}, "SET": {}, "RESET": {}, "LOCK": {},
	"BEGIN": {}, "COMMIT": {}, "ROLLBACK": {}, "SAVEPOINT": {}, "START": {},
	"DISCARD": {}, "LISTEN": {}, "NOTIFY": {}, "UNLISTEN": {}, "PREPARE": {},
	"DEALLOCATE": {}, "DECLARE": {}, "FETCH": {}, "MOVE": {}, "CLOSE": {},
	// procedural / dynamic execution
	"CALL": {}, "DO": {}, "EXECUTE": {}, "PERFORM": {}, "LANGUAGE": {},
	"FUNCTION": {}, "PROCEDURE": {}, "TRIGGER": {},
	// SELECT INTO (materialization) and locking clauses
	"INTO": {},
	// OFFSET pagination is a banned scale anti-pattern (keyset-not-OFFSET) and a
	// large-OFFSET scan-and-discard DoS vector; the fallback is aggregate-first
	// and never paginates by offset.
	"OFFSET": {},
	// CTEs are rejected wholesale (WITH-writes + recursion surface); the query
	// must be a single flat SELECT.
	"WITH": {},
	// Set operators append a SECOND query arm that the tenant predicate of the
	// first arm never scopes (`... WHERE tenant_id = '<session>' UNION ALL
	// TABLE ceo_ai.v`). The fallback is ONE flat SELECT; every set operator is
	// banned as a whole token, so no arm can exist to be unscoped.
	"UNION": {}, "INTERSECT": {}, "EXCEPT": {},
	// `TABLE <relation>` and `VALUES (...)` are statement forms that read (or
	// fabricate) a relation without any SELECT/WHERE tokens, so the
	// single-SELECT and tenant-predicate rules would never see them. Banned.
	"TABLE": {}, "VALUES": {},
}

// bannedFunctions are function identifiers that are read-shaped in name but are
// side-effecting, filesystem/network-touching, volatile, or DoS vectors.
var bannedFunctions = map[string]struct{}{
	"pg_sleep": {}, "pg_sleep_for": {}, "pg_sleep_until": {},
	"pg_read_file": {}, "pg_read_binary_file": {}, "pg_ls_dir": {},
	"pg_stat_file": {}, "lo_import": {}, "lo_export": {}, "lo_get": {},
	"pg_reload_conf": {}, "pg_terminate_backend": {}, "pg_cancel_backend": {},
	"dblink": {}, "dblink_exec": {}, "pg_read_server_files": {},
	"query_to_xml": {}, "set_config": {}, "current_setting": {},
	// string builders commonly abused to construct dynamic SQL / smuggle
	"format": {}, "chr": {}, "convert_from": {}, "decode": {}, "encode": {},
	"quote_literal": {}, "quote_ident": {}, "dollar_quote": {},
}

// Validate performs the full deny-by-default policy check. A nil return means
// the statement is safe to hand to ExecuteReadOnly. Callers MUST treat any
// non-nil error as a hard reject and never fall through to execution.
func Validate(sql string) error {
	raw := strings.TrimSpace(sql)
	if raw == "" {
		return ErrEmpty
	}
	if len(raw) > MaxSQLBytes {
		return rejit("statement exceeds %d bytes", MaxSQLBytes)
	}

	// 1. Strip string literals up-front. Everything a client-influenced value
	//    could contain (park names, injected instructions, semicolons, banned
	//    keywords) lives inside single-quoted literals and must be treated as
	//    opaque data, never as SQL. Dollar-quoting and E'' escapes are rejected
	//    outright below because they change literal semantics.
	stripped, err := stripStringLiterals(raw)
	if err != nil {
		return err
	}

	// 2. Outside of literals, only ASCII is allowed. Non-ASCII outside a literal
	//    is either an obfuscation attempt or a homoglyph keyword bypass.
	if i, r, ok := firstNonASCII(stripped); ok {
		return rejit("non-ASCII rune %q at position %d outside string literal", r, i)
	}

	// 3. No comments and no statement terminators outside literals. A ';' would
	//    permit statement stacking; comments can hide payloads from a scanner.
	if idx := indexAny(stripped, ";"); idx >= 0 {
		return rejit("statement separator ';' is not allowed")
	}
	if strings.Contains(stripped, "--") {
		return rejit("SQL line comment '--' is not allowed")
	}
	if strings.Contains(stripped, "/*") || strings.Contains(stripped, "*/") {
		return rejit("SQL block comment is not allowed")
	}
	// Dollar-quoted string tags ($$ / $tag$) enable procedural bodies and
	// literal smuggling; a lone '$' also appears in $1 params which the fallback
	// does not use (we build a parameterless read). Reject any '$'.
	if strings.Contains(stripped, "$") {
		return rejit("'$' (dollar-quoting or bind placeholder) is not allowed")
	}
	// Backslash outside a literal has no legitimate use here and signals E''
	// escape trickery attempts.
	if strings.Contains(stripped, "\\") {
		return rejit("backslash is not allowed outside string literals")
	}

	// 4. Tokenize the literal-free statement.
	tokens := tokenize(stripped)
	if len(tokens) == 0 {
		return ErrEmpty
	}

	// 4a. Parentheses must balance and never close below depth 0: every
	//     depth-based rule below (single top-level FROM/WHERE, AND-conjunct
	//     split, OR detection) assumes a well-nested statement.
	if err := checkParensBalanced(tokens); err != nil {
		return err
	}

	// 5. Must be a single SELECT statement. First meaningful token is SELECT;
	//    WITH/other leading verbs are rejected (WITH is in bannedKeywords too).
	if !strings.EqualFold(tokens[0].text, "SELECT") {
		return rejit("statement must begin with SELECT, got %q", tokens[0].text)
	}

	// 5a. Exactly ONE SELECT in the whole statement. A second SELECT can only be a
	//     nested subquery — a scalar subquery in the projection
	//     (`SELECT (SELECT max(x) FROM ceo_ai.v2) ...`), an `IN (SELECT ...)`
	//     membership list, or a derived table in FROM. None of those inner reads
	//     carries a tenant predicate (the executor binds only the OUTER
	//     `tenant_id = '...'` literal and the ceo_ai.* views are NOT themselves
	//     tenant-filtered), so any nested SELECT is a cross-tenant read vector.
	//     The fallback is a single flat aggregate read; reject every subquery.
	if n := countKeyword(tokens, "SELECT"); n != 1 {
		return rejit("statement must be a single flat SELECT; nested subqueries are not allowed")
	}

	// 5b. Exactly ONE relation. A multi-relation read needs a tenant predicate
	//     on EVERY relation, but the guard binds exactly one, so a second
	//     relation — explicit JOIN, comma cross-join (`FROM a, b`), a second
	//     FROM anywhere (a set-operator arm, an EXTRACT(x FROM y) that a token
	//     guard cannot tell from a relation) — is rejected outright. The ceo_ai.*
	//     views are denormalized leadership rollups built so leadership
	//     questions never need a cross-view JOIN; a single tenant-scoped
	//     relation is the only safe shape for a token guard (not a full parser)
	//     to enforce.
	if n := countKeyword(tokens, "FROM"); n != 1 {
		return rejit("statement must read exactly one %s.* relation (found %d FROM)", AllowedSchema, n)
	}
	if !atDepthZero(tokens, "FROM") {
		return rejit("FROM must be at the top level of the statement, not inside parentheses")
	}
	if hasKeyword(tokens, "JOIN") {
		return rejit("JOIN is not allowed; the fallback reads a single tenant-scoped %s.* relation", AllowedSchema)
	}
	if hasTopLevelCommaInFromClause(tokens) {
		return rejit("comma cross-join is not allowed; the fallback reads a single tenant-scoped %s.* relation", AllowedSchema)
	}

	// 6. Scan for banned keywords and banned function calls, and enforce that
	//    every schema-qualified reference and every FROM/JOIN source resolves to
	//    ceo_ai.*.
	if err := scanTokens(tokens); err != nil {
		return err
	}

	// 7. Mandatory bounded LIMIT <= MaxRowLimit.
	if err := enforceLimit(tokens); err != nil {
		return err
	}

	// 8. Mandatory STRUCTURAL tenant scope (plan v3 D0). It is not enough for a
	//    `tenant_id = '<literal>'` predicate to appear somewhere in the WHERE
	//    clause with the right literal: `NOT tenant_id = 'A'`, `tenant_id = 'A'
	//    IS FALSE`, `(tenant_id = 'A') = false`, `CASE WHEN tenant_id = 'A'
	//    THEN false ELSE true END` and `(tenant_id = 'A' OR x)` all carry the
	//    honest literal and all return OTHER tenants' rows. The only shape that
	//    actually confines the scan is a positive, paren-depth-0 AND conjunct
	//    of the (single) WHERE clause, so that is the only shape accepted; see
	//    checkTenantConjunct for the exact grammar.
	if err := checkTenantConjunct(tokens); err != nil {
		return err
	}

	return nil
}

// token is a single lexical unit outside string literals.
type token struct {
	text  string
	isNum bool
	isSym bool // single punctuation char
}

// tokenize splits literal-free SQL into identifier/number/symbol tokens.
// Identifiers: [A-Za-z_][A-Za-z0-9_]* . Numbers: digits with optional single dot.
// Everything else becomes single-char symbol tokens (including '.', '(', ')').
func tokenize(s string) []token {
	var out []token
	runes := []rune(s)
	n := len(runes)
	i := 0
	for i < n {
		c := runes[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\v' || c == '\f':
			i++
		case isIdentStart(c):
			j := i + 1
			for j < n && isIdentPart(runes[j]) {
				j++
			}
			out = append(out, token{text: string(runes[i:j])})
			i = j
		case c >= '0' && c <= '9':
			j := i + 1
			dot := false
			for j < n && (runes[j] >= '0' && runes[j] <= '9' || (runes[j] == '.' && !dot)) {
				if runes[j] == '.' {
					dot = true
				}
				j++
			}
			out = append(out, token{text: string(runes[i:j]), isNum: true})
			i = j
		default:
			out = append(out, token{text: string(c), isSym: true})
			i++
		}
	}
	return out
}

func isIdentStart(c rune) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c rune) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// scanTokens rejects banned keywords/functions and enforces ceo_ai.* references.
func scanTokens(tokens []token) error {
	for i, t := range tokens {
		if t.isNum || t.isSym {
			continue
		}
		up := strings.ToUpper(t.text)
		if _, banned := bannedKeywords[up]; banned {
			return rejit("banned keyword %q", up)
		}
		lower := strings.ToLower(t.text)
		if _, banned := bannedFunctions[lower]; banned {
			// Only treat as a function call when immediately followed by '('.
			if next := nextSym(tokens, i); next == "(" {
				return rejit("banned function %q()", lower)
			}
			// Even non-call use (e.g. as a bare identifier) of these is suspect.
			return rejit("banned identifier %q", lower)
		}

		// Schema-qualified reference: ident '.' ident. Reject disallowed schemas.
		if i+2 < len(tokens) && tokens[i+1].isSym && tokens[i+1].text == "." && !tokens[i+2].isSym {
			schema := lower
			switch schema {
			case AllowedSchema:
				// ok
			case "public", "pg_catalog", "information_schema", "pg_temp", "pg_toast":
				return rejit("reference to schema %q is not allowed; only %s.* is permitted", schema, AllowedSchema)
			default:
				// A dotted reference whose left side is not a known schema may be
				// a table alias (alias.column). That is permitted; only FROM/JOIN
				// *sources* must be ceo_ai-qualified, which is checked below.
			}
		}

		// FROM / JOIN source must be ceo_ai.<name>. This is the core allowlist:
		// no bare tables, no other schemas, no subquery/VALUES/function sources.
		if up == "FROM" || up == "JOIN" {
			if err := checkTableSource(tokens, i); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkTableSource validates the token(s) following a FROM or JOIN keyword.
func checkTableSource(tokens []token, kwIdx int) error {
	j := kwIdx + 1
	if j >= len(tokens) {
		return rejit("dangling %q with no table source", tokens[kwIdx].text)
	}
	src := tokens[j]
	if src.isSym {
		// '(' => subquery/derived table/VALUES; anything else is malformed.
		return rejit("%q source must be a %s.* table, not a subquery/expression", tokens[kwIdx].text, AllowedSchema)
	}
	if src.isNum {
		return rejit("%q source must be a %s.* table", tokens[kwIdx].text, AllowedSchema)
	}
	// LATERAL / ONLY prefixes are not permitted (LATERAL enables subqueries).
	if strings.EqualFold(src.text, "LATERAL") || strings.EqualFold(src.text, "ONLY") {
		return rejit("%q %s modifier is not allowed", tokens[kwIdx].text, strings.ToUpper(src.text))
	}
	// Require ceo_ai '.' name.
	if !strings.EqualFold(src.text, AllowedSchema) {
		return rejit("%q source %q must be schema-qualified as %s.<view>", tokens[kwIdx].text, src.text, AllowedSchema)
	}
	if j+2 >= len(tokens) || !tokens[j+1].isSym || tokens[j+1].text != "." || tokens[j+2].isSym || tokens[j+2].isNum {
		return rejit("%q source must be %s.<view>", tokens[kwIdx].text, AllowedSchema)
	}
	// The view name must be a plain identifier, never a clause keyword: a
	// `FROM ceo_ai.WHERE tenant_id = ...` draft would otherwise consume the
	// WHERE keyword as the relation name and leave the predicate clause-less.
	if _, kw := reservedClauseWords[strings.ToUpper(tokens[j+2].text)]; kw {
		return rejit("%q source %s.%s is not a view name", tokens[kwIdx].text, AllowedSchema, tokens[j+2].text)
	}
	// What follows the relation must be a clause boundary or a plain alias
	// (`AS a` / `a`) followed by a clause boundary, so the WHERE clause the
	// tenant check inspects is really the statement's WHERE clause.
	k := j + 3
	if k < len(tokens) && !tokens[k].isSym && !tokens[k].isNum && strings.EqualFold(tokens[k].text, "AS") {
		k++
		if k >= len(tokens) || tokens[k].isSym || tokens[k].isNum {
			return rejit("%q source alias is malformed", tokens[kwIdx].text)
		}
		if _, kw := reservedClauseWords[strings.ToUpper(tokens[k].text)]; kw {
			return rejit("%q source alias %q is a keyword", tokens[kwIdx].text, tokens[k].text)
		}
		k++
	} else if k < len(tokens) && !tokens[k].isSym && !tokens[k].isNum && !isClauseBoundary(tokens[k].text) {
		if _, kw := reservedClauseWords[strings.ToUpper(tokens[k].text)]; kw {
			return rejit("%q source is followed by %q, not a clause", tokens[kwIdx].text, tokens[k].text)
		}
		k++ // bare alias
	}
	if k < len(tokens) && (tokens[k].isSym || tokens[k].isNum || !isClauseBoundary(tokens[k].text)) {
		return rejit("%q source must be followed by WHERE/GROUP/ORDER/HAVING/LIMIT, got %q", tokens[kwIdx].text, tokens[k].text)
	}
	return nil
}

// isClauseBoundary reports whether an identifier token opens a clause that
// may follow the FROM relation (or its alias).
func isClauseBoundary(text string) bool {
	switch strings.ToUpper(text) {
	case "WHERE", "GROUP", "ORDER", "HAVING", "LIMIT", "WINDOW", "FETCH", "OFFSET", "FOR":
		return true
	}
	return false
}

// reservedClauseWords are SQL keywords that can never be a ceo_ai view name
// (a fuzz-found `FROM ceo_ai.WHERE ...` swallowed the WHERE clause).
var reservedClauseWords = map[string]struct{}{
	"SELECT": {}, "FROM": {}, "WHERE": {}, "GROUP": {}, "ORDER": {}, "HAVING": {},
	"LIMIT": {}, "AND": {}, "OR": {}, "NOT": {}, "AS": {}, "ON": {}, "IN": {},
	"IS": {}, "NULL": {}, "TRUE": {}, "FALSE": {}, "CASE": {}, "WHEN": {},
	"THEN": {}, "ELSE": {}, "END": {}, "DISTINCT": {}, "ALL": {}, "ANY": {},
	"SOME": {}, "BETWEEN": {}, "LIKE": {}, "ILIKE": {}, "JOIN": {}, "LEFT": {},
	"RIGHT": {}, "INNER": {}, "OUTER": {}, "CROSS": {}, "FULL": {}, "NATURAL": {},
	"USING": {}, "WINDOW": {}, "FETCH": {}, "OFFSET": {}, "FOR": {}, "ASC": {},
	"DESC": {}, "NULLS": {}, "EXISTS": {}, "ARRAY": {}, "CAST": {}, "FILTER": {},
	"OVER": {}, "PARTITION": {}, "BY": {}, "ONLY": {}, "LATERAL": {},
}

// enforceLimit requires exactly a bounded LIMIT <constant> with constant <= max.
func enforceLimit(tokens []token) error {
	found := false
	for i, t := range tokens {
		if t.isSym || t.isNum {
			continue
		}
		if !strings.EqualFold(t.text, "LIMIT") {
			continue
		}
		found = true
		if i+1 >= len(tokens) {
			return rejit("LIMIT requires a numeric bound")
		}
		arg := tokens[i+1]
		if !arg.isNum {
			return rejit("LIMIT must be a numeric constant, got %q", arg.text)
		}
		if strings.Contains(arg.text, ".") {
			return rejit("LIMIT must be an integer, got %q", arg.text)
		}
		v, err := strconv.Atoi(arg.text)
		if err != nil {
			return rejit("LIMIT is not a valid integer: %q", arg.text)
		}
		if v <= 0 {
			return rejit("LIMIT must be positive, got %d", v)
		}
		if v > MaxRowLimit {
			return rejit("LIMIT %d exceeds maximum of %d", v, MaxRowLimit)
		}
		// The bound must be a single integer constant, not the leading operand of
		// an arithmetic expression. `LIMIT 50+51` tokenizes to 50 (which passes the
		// <=100 check) but Postgres evaluates 50+51=101, silently exceeding the cap.
		// Reject any arithmetic/grouping operator immediately following the bound.
		if i+2 < len(tokens) {
			nxt := tokens[i+2]
			if nxt.isSym {
				switch nxt.text {
				case "+", "-", "*", "/", "%", "^", "|", "&", "#", "~", "(":
					return rejit("LIMIT must be a single integer constant, not an expression")
				}
			}
		}
	}
	if !found {
		return rejit("query must include a LIMIT <= %d", MaxRowLimit)
	}
	return nil
}

// --- token helpers ---

func hasToken(tokens []token, ident string) bool {
	for _, t := range tokens {
		if !t.isSym && !t.isNum && strings.EqualFold(t.text, ident) {
			return true
		}
	}
	return false
}

func hasKeyword(tokens []token, kw string) bool { return hasToken(tokens, kw) }

// checkParensBalanced rejects a statement whose parentheses do not nest.
func checkParensBalanced(tokens []token) error {
	depth := 0
	for _, t := range tokens {
		if !t.isSym {
			continue
		}
		switch t.text {
		case "(":
			depth++
		case ")":
			depth--
			if depth < 0 {
				return rejit("unbalanced parentheses")
			}
		}
	}
	if depth != 0 {
		return rejit("unbalanced parentheses")
	}
	return nil
}

// atDepthZero reports whether the (first) identifier token matching kw sits at
// parenthesis depth 0.
func atDepthZero(tokens []token, kw string) bool {
	depth := 0
	for _, t := range tokens {
		if t.isSym {
			switch t.text {
			case "(":
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
			}
			continue
		}
		if !t.isNum && strings.EqualFold(t.text, kw) {
			return depth == 0
		}
	}
	return false
}

// countKeyword counts identifier tokens matching kw (case-insensitive).
func countKeyword(tokens []token, kw string) int {
	n := 0
	for _, t := range tokens {
		if !t.isSym && !t.isNum && strings.EqualFold(t.text, kw) {
			n++
		}
	}
	return n
}

// hasTopLevelCommaInFromClause reports whether a paren-depth-0 comma appears in
// the FROM clause — i.e. between the FROM keyword and the first clause terminator
// (WHERE/GROUP/ORDER/HAVING/LIMIT/WINDOW/OFFSET/FETCH/UNION/INTERSECT/EXCEPT).
// Such a comma is an implicit cross-join separator (`FROM a, b`), the comma-form
// twin of an explicit JOIN, and is rejected for the same cross-tenant reason.
// Projection-list commas are BEFORE FROM and never reach this scan; commas inside
// parentheses (function args, IN lists) are at depth > 0 and are ignored.
func hasTopLevelCommaInFromClause(tokens []token) bool {
	fromIdx := -1
	for i, t := range tokens {
		if !t.isSym && !t.isNum && strings.EqualFold(t.text, "FROM") {
			fromIdx = i
			break
		}
	}
	if fromIdx < 0 {
		return false
	}
	depth := 0
	for i := fromIdx + 1; i < len(tokens); i++ {
		t := tokens[i]
		if t.isSym {
			switch t.text {
			case "(":
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
			case ",":
				if depth == 0 {
					return true // cross-join separator
				}
			}
			continue
		}
		if t.isNum {
			continue
		}
		if depth == 0 {
			switch strings.ToUpper(t.text) {
			case "WHERE", "GROUP", "ORDER", "HAVING", "LIMIT", "WINDOW",
				"OFFSET", "FETCH", "UNION", "INTERSECT", "EXCEPT":
				return false // FROM clause ended without a top-level comma
			}
		}
	}
	return false
}

// whereClauseSpan locates the single paren-depth-0 WHERE keyword and returns
// the half-open token range of its predicate: everything after WHERE up to the
// first depth-0 clause terminator (GROUP/ORDER/HAVING/LIMIT/WINDOW/OFFSET/
// FETCH/FOR) or the end of the statement. A `FILTER (WHERE ...)` aggregate
// clause sits at depth > 0 and is neither the WHERE nor part of its span.
// ok=false when the statement has no depth-0 WHERE; a second depth-0 WHERE
// cannot occur in a single flat SELECT and is reported as an error.
func whereClauseSpan(tokens []token) (start, end int, ok bool, err error) {
	depth := 0
	whereIdx := -1
	for i, t := range tokens {
		if t.isSym {
			switch t.text {
			case "(":
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
			}
			continue
		}
		if t.isNum || depth != 0 || !strings.EqualFold(t.text, "WHERE") {
			continue
		}
		if whereIdx >= 0 {
			return 0, 0, false, rejit("statement must have exactly one top-level WHERE clause")
		}
		whereIdx = i
	}
	if whereIdx < 0 {
		return 0, 0, false, nil
	}
	depth = 0
	end = len(tokens)
	for i := whereIdx + 1; i < len(tokens); i++ {
		t := tokens[i]
		if t.isSym {
			switch t.text {
			case "(":
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
			}
			continue
		}
		if t.isNum || depth != 0 {
			continue
		}
		switch strings.ToUpper(t.text) {
		case "GROUP", "ORDER", "HAVING", "LIMIT", "WINDOW", "OFFSET", "FETCH", "FOR":
			return whereIdx + 1, i, true, nil
		}
	}
	return whereIdx + 1, end, true, nil
}

// splitTopLevelConjuncts splits a predicate token span into its paren-depth-0
// AND conjuncts. It returns an error for a depth-0 OR anywhere in the span (a
// disjunction can widen the scan past any tenant conjunct, whether or not the
// tenant predicate is inside the parenthesised group) and for an empty
// conjunct (a dangling AND).
func splitTopLevelConjuncts(span []token) ([][]token, error) {
	var out [][]token
	depth := 0
	cur := 0
	for i, t := range span {
		if t.isSym {
			switch t.text {
			case "(":
				depth++
			case ")":
				if depth > 0 {
					depth--
				}
			}
			continue
		}
		if t.isNum || depth != 0 {
			continue
		}
		switch strings.ToUpper(t.text) {
		case "OR":
			return nil, rejit("top-level OR in the WHERE clause is not allowed; the tenant scope must stay a conjunctive AND filter")
		case "AND":
			if i == cur {
				return nil, rejit("malformed WHERE clause (empty conjunct)")
			}
			out = append(out, span[cur:i])
			cur = i + 1
		}
	}
	if cur >= len(span) {
		return nil, rejit("malformed WHERE clause (empty conjunct)")
	}
	out = append(out, span[cur:])
	return out, nil
}

// unwrapOnce strips ONE pair of enclosing parentheses when the conjunct is
// wholly wrapped, i.e. its first token is '(' and the matching ')' is its
// last token. `( a ) = false` is NOT wholly wrapped and is returned as-is.
func unwrapOnce(c []token) []token {
	if len(c) < 2 || !c[0].isSym || c[0].text != "(" {
		return c
	}
	depth := 0
	for i, t := range c {
		if !t.isSym {
			continue
		}
		switch t.text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				if i == len(c)-1 {
					return c[1 : len(c)-1]
				}
				return c
			}
		}
	}
	return c
}

// isTenantConjunct reports whether a (literal-stripped) conjunct is EXACTLY the
// positive tenant equality the executor binds:
//
//	[alias .] tenant_id = <literal>            (the literal was stripped)
//	[alias .] tenant_id = <literal> :: ident   (a harmless RHS cast)
//
// Anything longer (a trailing operator, IS, CASE, NOT, a column RHS) or
// shorter is not the tenant conjunct. The literal itself is checked against the
// session tenant by ExtractAllTenantPredicates on the raw statement.
func isTenantConjunct(c []token) bool {
	i := 0
	if len(c) >= 3 && !c[0].isSym && !c[0].isNum && c[1].isSym && c[1].text == "." {
		i = 2
	}
	rest := c[i:]
	if len(rest) < 2 || rest[0].isSym || rest[0].isNum || !strings.EqualFold(rest[0].text, "tenant_id") {
		return false
	}
	if !rest[1].isSym || rest[1].text != "=" {
		return false
	}
	switch len(rest) {
	case 2:
		return true
	case 5:
		return rest[2].isSym && rest[2].text == ":" && rest[3].isSym && rest[3].text == ":" && !rest[4].isSym && !rest[4].isNum
	default:
		return false
	}
}

// checkTenantConjunct enforces the structural tenant-scope grammar: the
// statement has exactly one depth-0 WHERE; its predicate has no depth-0 OR;
// exactly ONE of its depth-0 AND conjuncts (optionally parenthesised once) is
// the positive `tenant_id = '<literal>'` equality; and the tenant_id token
// appears NOWHERE else in the statement (not in another conjunct, not under
// NOT/CASE/IS, not in the projection, GROUP BY, HAVING or ORDER BY), so no
// second predicate, inversion or disjunction can widen the scan.
func checkTenantConjunct(tokens []token) error {
	if n := countKeyword(tokens, "tenant_id"); n != 1 {
		if n == 0 {
			if _, _, ok, err := whereClauseSpan(tokens); err != nil {
				return err
			} else if !ok {
				return rejit("query must include a WHERE clause with tenant scope")
			}
			return rejit("query must include a tenant_id filter predicate in its WHERE clause")
		}
		return rejit("tenant_id may appear exactly once, as the WHERE clause's tenant predicate (found %d)", n)
	}
	start, end, ok, err := whereClauseSpan(tokens)
	if err != nil {
		return err
	}
	if !ok {
		return rejit("query must include a WHERE clause with tenant scope")
	}
	conjuncts, err := splitTopLevelConjuncts(tokens[start:end])
	if err != nil {
		return err
	}
	found := 0
	for _, c := range conjuncts {
		if isTenantConjunct(unwrapOnce(c)) {
			found++
			continue
		}
		if countKeyword(c, "tenant_id") > 0 {
			return rejit("tenant_id predicate must be a positive top-level AND conjunct of the form tenant_id = '<literal>' (not under NOT, IS, CASE, OR or a larger expression)")
		}
	}
	if found != 1 {
		return rejit("query must include a tenant_id filter predicate in its WHERE clause")
	}
	return nil
}

func skipSpace(runes []rune, i int) int {
	n := len(runes)
	for i < n {
		switch runes[i] {
		case ' ', '\t', '\n', '\r', '\v', '\f':
			i++
		default:
			return i
		}
	}
	return i
}

// readLiteral reads a single-quoted literal starting at runes[i]=='\” and
// returns the unescaped value (doubled ” -> '), the index after the closing
// quote, and ok.
func readLiteral(runes []rune, i int) (string, int, bool) {
	n := len(runes)
	if i >= n || runes[i] != '\'' {
		return "", i, false
	}
	i++
	var b strings.Builder
	for i < n {
		if runes[i] == '\'' {
			if i+1 < n && runes[i+1] == '\'' {
				b.WriteRune('\'')
				i += 2
				continue
			}
			return b.String(), i + 1, true
		}
		b.WriteRune(runes[i])
		i++
	}
	return "", i, false // unterminated
}

// nextSym returns the text of the next token after i. Empty string if none.
func nextSym(tokens []token, i int) string {
	if i+1 < len(tokens) {
		return tokens[i+1].text
	}
	return ""
}

// --- literal stripping and byte scanning ---

// stripStringLiterals removes single-quoted string literals (with ” escaping),
// replacing each with a single space so surrounding tokens stay separated. It
// rejects double-quoted identifiers and E”/U&” escape literals, which change
// literal parsing semantics and could hide payloads. Unterminated literals are
// caught by unbalancedSingleQuotes.
func stripStringLiterals(s string) (string, error) {
	var b strings.Builder
	runes := []rune(s)
	n := len(runes)
	i := 0
	for i < n {
		c := runes[i]
		if c == '\'' {
			if prevMarkerIsEscape(runes, i) {
				return "", rejit("escape/unicode string literal (E'' or U&'') is not allowed")
			}
			i++ // consume opening quote
			for i < n {
				if runes[i] == '\'' {
					if i+1 < n && runes[i+1] == '\'' {
						i += 2 // '' escaped quote stays inside literal
						continue
					}
					i++ // closing quote
					break
				}
				i++
			}
			// Two literals separated only by whitespace: Postgres concatenates
			// them across a newline ('abc'\n'def' == 'abcdef') and errors
			// otherwise, so the literal the guard read is not the literal that
			// would run. Reject.
			if k := skipSpace(runes, i); k < n && runes[k] == '\'' {
				return "", rejit("adjacent string literals are not allowed")
			}
			b.WriteByte(' ')
			continue
		}
		if c == '"' {
			// Double-quoted identifiers are not needed for ceo_ai.* references and
			// can quote otherwise-banned identifiers; reject them.
			return "", rejit("double-quoted identifiers are not allowed")
		}
		b.WriteRune(c)
		i++
	}
	if unbalancedSingleQuotes(runes) {
		return "", rejit("unterminated or unbalanced string literal")
	}
	return b.String(), nil
}

// prevMarkerIsEscape reports whether the single quote at index i is an escape or
// unicode-escape string literal opener (E'...', e'...', or U&'...').
func prevMarkerIsEscape(runes []rune, i int) bool {
	if i == 0 {
		return false
	}
	p := runes[i-1]
	if p == 'E' || p == 'e' {
		if i-2 < 0 || !isIdentPart(runes[i-2]) {
			return true
		}
	}
	if p == '&' && i-2 >= 0 && (runes[i-2] == 'U' || runes[i-2] == 'u') {
		if i-3 < 0 || !isIdentPart(runes[i-3]) {
			return true
		}
	}
	return false
}

// unbalancedSingleQuotes reports whether single quotes (accounting for ”
// escapes) do not close, i.e. an unterminated literal.
func unbalancedSingleQuotes(runes []rune) bool {
	n := len(runes)
	i := 0
	inLit := false
	for i < n {
		if runes[i] == '\'' {
			if inLit {
				if i+1 < n && runes[i+1] == '\'' {
					i += 2
					continue
				}
				inLit = false
				i++
				continue
			}
			inLit = true
			i++
			continue
		}
		i++
	}
	return inLit
}

func firstNonASCII(s string) (int, rune, bool) {
	for i, r := range s {
		if r > 127 {
			return i, r, true
		}
	}
	return 0, 0, false
}

func indexAny(s, chars string) int { return strings.IndexAny(s, chars) }

// AsValidationError extracts a *ValidationError if err is one.
func AsValidationError(err error) (*ValidationError, bool) {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return ve, true
	}
	return nil, false
}
