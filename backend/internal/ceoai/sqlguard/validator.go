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

	// 5b. No JOINs. A multi-relation JOIN needs a tenant predicate on EVERY
	//     relation, but the executor binds only the FIRST `tenant_id = '...'`
	//     literal, so a second relation that is unscoped (`JOIN v2 ON true`) or
	//     that carries a DIFFERENT tenant literal (`b.tenant_id = '<victim>'`)
	//     joins in another tenant's rows while the outer literal still equals the
	//     session tenant. The ceo_ai.* views are denormalized leadership rollups
	//     built so leadership questions never need a cross-view JOIN; a single
	//     tenant-scoped relation is the only safe shape for a token guard (not a
	//     full parser) to enforce. Reject JOIN outright.
	if hasKeyword(tokens, "JOIN") {
		return rejit("JOIN is not allowed; the fallback reads a single tenant-scoped %s.* relation", AllowedSchema)
	}

	// 5c. No implicit comma cross-join. A multi-relation FROM list
	//     (`FROM ceo_ai.a, ceo_ai.b`) has NO JOIN token, so rule 5b never fires,
	//     yet it cartesian-joins a SECOND relation exactly like an explicit JOIN:
	//     the executor binds only the FIRST `tenant_id = '...'` literal, so the
	//     second relation is unscoped (or carries a divergent victim tenant
	//     literal) and leaks cross-tenant rows. A single-table FROM clause never
	//     contains a top-level comma — projection-list commas sit BEFORE FROM and
	//     FROM sources are bare `ceo_ai.<view>` relations (no function/VALUES args,
	//     enforced by checkTableSource) — so any paren-depth-0 comma between FROM
	//     and the first clause terminator is a cross-join separator. Reject it.
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

	// 8. Mandatory tenant scoping predicate. Scope comes from the server session;
	//    the planner must have bound tenant_id into the WHERE clause. A fallback
	//    without an explicit tenant_id filter is rejected so a bug in the planner
	//    can never produce a cross-tenant read. It is NOT enough for the token
	//    tenant_id to appear anywhere (e.g. in the SELECT projection list): it
	//    must appear inside the WHERE clause as an actual `tenant_id = '<literal>'`
	//    equality predicate, otherwise the read is unscoped over the
	//    mesha_ceo_readonly role, which does not itself enforce tenant isolation.
	//    Only the `=` form is accepted so the validator's grammar matches the
	//    executor's server-side binding (ExtractTenantEquals).
	if !hasKeyword(tokens, "WHERE") {
		return rejit("query must include a WHERE clause with tenant scope")
	}
	if !hasTenantScopeInWhere(tokens) {
		return rejit("query must include a tenant_id filter predicate in its WHERE clause")
	}

	// 9. Reject any top-level (paren-depth 0) OR in the WHERE clause. A boolean
	//    disjunction around the tenant predicate — e.g.
	//    `WHERE tenant_id = '<session>' OR park_label = 'x'` — passes the tenant
	//    predicate check yet returns rows for EVERY tenant, because the OR widens
	//    the scan past the tenant filter. Leadership fallback filters are
	//    conjunctive and tenant-scoped; a disjunction that must span both branches
	//    has to be parenthesized (depth > 0), which cannot dissolve the top-level
	//    AND tenant scope. This is defense in depth alongside the executor's
	//    server-bound tenant equality (ExecuteReadOnlyForTenant).
	if hasTopLevelOrInWhere(tokens) {
		return rejit("top-level OR in the WHERE clause is not allowed; parenthesize disjunctions so the tenant scope stays conjunctive")
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
	if j+2 >= len(tokens) || !tokens[j+1].isSym || tokens[j+1].text != "." || tokens[j+2].isSym {
		return rejit("%q source must be %s.<view>", tokens[kwIdx].text, AllowedSchema)
	}
	return nil
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

// hasTenantScopeInWhere reports whether a real tenant_id filter predicate exists
// inside the WHERE clause. It requires a `tenant_id` identifier that appears
// AFTER the WHERE keyword and is immediately followed by an equality (`=`)
// operator. A bare `tenant_id` in the SELECT projection list (or
// anywhere before WHERE) does NOT satisfy tenant scoping and must be rejected, or
// an unscoped read like `SELECT tenant_id, id FROM ceo_ai.v WHERE park = 'p1'`
// would leak every tenant's rows.
func hasTenantScopeInWhere(tokens []token) bool {
	whereIdx := -1
	for i, t := range tokens {
		if !t.isSym && !t.isNum && strings.EqualFold(t.text, "WHERE") {
			whereIdx = i
			break
		}
	}
	if whereIdx < 0 {
		return false
	}
	for i := whereIdx + 1; i < len(tokens); i++ {
		t := tokens[i]
		if t.isSym || t.isNum || !strings.EqualFold(t.text, "tenant_id") {
			continue
		}
		j := i + 1
		if j >= len(tokens) {
			continue
		}
		nxt := tokens[j]
		if nxt.isSym && nxt.text == "=" {
			return true
		}
		// NOTE: only `tenant_id = '<literal>'` counts as tenant scope. An
		// `tenant_id IN (...)` form is deliberately NOT accepted here: the sole
		// production entry point (ExecuteReadOnlyForTenant) binds tenant via
		// ExtractTenantEquals, which recognizes ONLY the `=` form and hard-rejects
		// everything else with ErrTenantBinding. Accepting IN at validation while
		// the executor rejects it lets the validator declare a draft well-formed
		// that then always fails to run — the two layers must agree on the
		// accepted tenant-predicate grammar. (Nested `tenant_id IN (SELECT ...)`
		// is already rejected by the single-SELECT rule.)
	}
	return false
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

// hasTopLevelOrInWhere reports whether an `OR` keyword appears at parenthesis
// depth 0 after the WHERE keyword (and before GROUP/ORDER/LIMIT/HAVING, which
// end the predicate). String literals are already stripped, so an `OR` inside a
// quoted value (e.g. `management_stage = ' OR 1=1'`) never reaches here.
func hasTopLevelOrInWhere(tokens []token) bool {
	whereIdx := -1
	for i, t := range tokens {
		if !t.isSym && !t.isNum && strings.EqualFold(t.text, "WHERE") {
			whereIdx = i
			break
		}
	}
	if whereIdx < 0 {
		return false
	}
	depth := 0
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
		if t.isNum {
			continue
		}
		if depth == 0 {
			switch strings.ToUpper(t.text) {
			case "GROUP", "ORDER", "LIMIT", "HAVING", "WINDOW", "OFFSET", "FETCH":
				return false // predicate ended without a top-level OR
			case "OR":
				return true
			}
		}
	}
	return false
}

// ExtractTenantEquals returns the single-quoted literal value bound by the FIRST
// `tenant_id = '...'` equality predicate in the raw statement (the column may be
// alias-qualified, e.g. `a.tenant_id`). ok is false when no such equality exists.
// The executor uses this to bind tenant server-side: it compares the returned
// value byte-for-byte to the session actor's tenant and rejects any mismatch, so
// a planner/injection that emits a different tenant UUID can never read another
// tenant's rows even though the value textually satisfies the WHERE clause. This
// scans the RAW sql (not the literal-stripped form) because it needs the literal.
func ExtractTenantEquals(sql string) (string, bool) {
	runes := []rune(sql)
	n := len(runes)
	i := 0
	for i < n {
		c := runes[i]
		// Skip over string literals so a `tenant_id =` sitting inside a quoted
		// value is never mistaken for the predicate.
		if c == '\'' {
			i++
			for i < n {
				if runes[i] == '\'' {
					if i+1 < n && runes[i+1] == '\'' {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			continue
		}
		if isIdentStart(c) {
			j := i + 1
			for j < n && isIdentPart(runes[j]) {
				j++
			}
			word := string(runes[i:j])
			if strings.EqualFold(word, "tenant_id") {
				// Require a bare or alias-qualified `tenant_id` (the char before
				// must not be an identifier part or '.', so `x.tenant_id` still
				// matches on the `tenant_id` token itself).
				k := skipSpace(runes, j)
				if k < n && runes[k] == '=' {
					k = skipSpace(runes, k+1)
					if k < n && runes[k] == '\'' {
						if val, end, ok := readLiteral(runes, k); ok {
							_ = end
							return val, true
						}
					}
				}
			}
			i = j
			continue
		}
		i++
	}
	return "", false
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
