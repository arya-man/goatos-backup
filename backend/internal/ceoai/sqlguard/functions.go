package sqlguard

// functions.go is the FUNCTION ALLOW-LIST of the fallback SQL guard (PR #318
// round-2 blocker R2-1).
//
// Why an allow-list and not a deny-list: Postgres ships many read-shaped
// functions that take a table name or a QUERY STRING and execute it with the
// caller's privileges — table_to_xml, schema_to_xml, query_to_xml_and_xmlschema,
// ts_stat, xmltable, xpath over table_to_xml, tablefunc's crosstab/connectby,
// dblink, and more arrive with every extension. The query text lives inside a
// string literal, which Validate strips as opaque data, so every structural
// rule (one FROM, one tenant conjunct, no set-ops) is satisfied by the OUTER
// statement while the INNER read is unscoped. The dedicated role holds SELECT
// on every ceo_ai.* view, so the inner read succeeds. A deny-list can never be
// complete against that surface; the only shape that holds is "a function may
// be called only when it is on this list".
//
// Detection is token-based on the literal-stripped stream Validate tokenizes:
// ANY identifier immediately followed by `(` is a function call. A
// schema-qualified call (`schema.fn(`) is rejected outright — every allowed
// function is a bare builtin, and a qualified name is how an extension or a
// user-defined function in ceo_ai/public would be reached. Double-quoted
// identifiers (`"fn"(`) are rejected earlier by stripStringLiterals, so a
// quoted function name can never reach this check.
//
// A handful of SQL keywords legitimately precede `(` without being a call
// (`WHERE (a = 1)`, `IN (...)`, `= ANY (...)`, `count(*) FILTER (WHERE ...)`,
// `OVER (...)`); those are listed in parenKeywords and skipped. `CASE` is a
// keyword, never a call. `CAST(x AS type)` and `x::type` are the two casting
// forms; the type name in `::type` is never followed by `(`, so it is not a
// call. A type with modifiers (`numeric(10,2)`) tokenizes as a call to
// `numeric` and is rejected — the fallback casts to bare `text`/`numeric`.
//
// Token shapes worth knowing: `extract(field FROM x)` carries a second FROM
// token, which the single-relation rule (exactly one FROM) rejects before the
// function check runs — drafts use `date_part('field', x)` instead, and
// `extract` is listed so the reject reason names the FROM rule rather than the
// function. `interval '1 month'` is a keyword followed by a literal (stripped
// to a space), not a call.

import (
	"sort"
	"strings"
)

// allowedFunctions is the closed set of builtins a model-drafted read may
// call. Everything else — every *_to_xml*, ts_stat, xmltable, xpath, crosstab,
// connectby, pg_* (pg_sleep, pg_read_file, pg_get_viewdef, ...), query_to_*,
// dblink*, lo_*, set_config, current_setting, format, regexp_* — is rejected
// simply because it is not here. Keep every entry lower-case.
var allowedFunctions = map[string]struct{}{
	// aggregates and arithmetic
	"count": {}, "sum": {}, "avg": {}, "min": {}, "max": {},
	"coalesce": {}, "nullif": {}, "round": {}, "abs": {}, "floor": {}, "ceil": {},
	"greatest": {}, "least": {}, "percentile_cont": {}, "percentile_disc": {},
	"bool_and": {}, "bool_or": {},
	// date / time
	"date_trunc": {}, "date_part": {}, "extract": {}, "to_char": {}, "to_date": {},
	"now": {}, "current_date": {}, "make_date": {}, "age": {}, "interval": {},
	// strings
	"lower": {}, "upper": {}, "trim": {}, "length": {}, "substring": {},
	"left": {}, "right": {}, "concat": {}, "concat_ws": {}, "replace": {}, "initcap": {},
	// casting
	"cast": {},
}

// parenKeywords are SQL keywords that may legitimately be followed by `(`
// without being a function call. Anything not listed here and not in
// allowedFunctions that precedes `(` is rejected as a disallowed function.
var parenKeywords = map[string]struct{}{
	"AND": {}, "OR": {}, "NOT": {}, "IN": {}, "ANY": {}, "SOME": {}, "ALL": {},
	"WHERE": {}, "ON": {}, "HAVING": {}, "WHEN": {}, "THEN": {}, "ELSE": {},
	"SELECT": {}, "DISTINCT": {}, "BY": {}, "AS": {}, "FILTER": {}, "OVER": {},
	"BETWEEN": {}, "LIKE": {}, "ILIKE": {},
	// `percentile_cont(f) WITHIN GROUP (ORDER BY x)` and `GROUP BY (x)`
	"GROUP": {},
}

// bannedFunctions is the belt-and-braces explicit ban kept from the original
// deny-list: these identifiers are rejected even when NOT followed by `(`
// (a bare reference to them is never a legitimate column of a ceo_ai.* view).
// The allow-list above is the real control; this set exists so a regression
// that loosens the allow-list is still caught by the explicit query_to_xml
// test and the original deny cases.
var bannedFunctions = map[string]struct{}{
	"query_to_xml": {},
	"pg_sleep":     {}, "pg_sleep_for": {}, "pg_sleep_until": {},
	"pg_read_file": {}, "pg_read_binary_file": {}, "pg_ls_dir": {},
	"pg_stat_file": {}, "lo_import": {}, "lo_export": {}, "lo_get": {},
	"pg_reload_conf": {}, "pg_terminate_backend": {}, "pg_cancel_backend": {},
	"dblink": {}, "dblink_exec": {}, "pg_read_server_files": {},
	"set_config": {}, "current_setting": {},
	"format": {}, "chr": {}, "convert_from": {}, "decode": {}, "encode": {},
	"quote_literal": {}, "quote_ident": {}, "dollar_quote": {},
}

// AllowedFunctions returns the sorted, lower-case list of function names a
// model-drafted read may call. The slice is a fresh copy on every call; it is
// exported so the planner prompt / schema-card tests can stay consistent with
// the guard without re-deriving the set.
func AllowedFunctions() []string {
	out := make([]string, 0, len(allowedFunctions))
	for k := range allowedFunctions {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// checkFunctionCalls enforces the allow-list on every `ident (` in the token
// stream. It is called from scanTokens for each identifier token.
func checkFunctionCalls(tokens []token, i int) error {
	t := tokens[i]
	lower := strings.ToLower(t.text)
	if _, banned := bannedFunctions[lower]; banned {
		if nextSym(tokens, i) == "(" {
			return rejit("banned function %q()", lower)
		}
		return rejit("banned identifier %q", lower)
	}
	if nextSym(tokens, i) != "(" {
		return nil
	}
	// schema.fn( — a qualified call is never an allowed builtin.
	if i >= 2 && tokens[i-1].isSym && tokens[i-1].text == "." && !tokens[i-2].isSym && !tokens[i-2].isNum {
		return rejit("schema-qualified function call %q.%s() is not allowed", strings.ToLower(tokens[i-2].text), lower)
	}
	if _, kw := parenKeywords[strings.ToUpper(t.text)]; kw {
		return nil
	}
	if _, ok := allowedFunctions[lower]; ok {
		return nil
	}
	return rejit("function %q() is not on the allow-list", lower)
}
