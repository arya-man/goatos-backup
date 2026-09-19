package sqlguard

import (
	"errors"
	"strings"
	"testing"
)

const (
	sessionTenant = "11111111-1111-1111-1111-111111111111"
	victimTenant  = "22222222-2222-2222-2222-222222222222"
	entityUUID    = "33333333-3333-3333-3333-333333333333" // a park/load/goat id, NOT a tenant
)

// nilExec is an executor with no pool: any test that reaches execValidated
// panics on the nil pool, so a returned ErrTenantBinding proves the reject
// happened BEFORE any database access.
func nilExec() *Executor { return &Executor{} }

func TestExtractAllTenantPredicates_Multiple(t *testing.T) {
	sql := `SELECT count(*) FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `' AND park = 'p' AND tenant_id = '` + sessionTenant + `' LIMIT 10`
	got, err := ExtractAllTenantPredicates(sql)
	if err != nil {
		t.Fatalf("unexpected reject: %v", err)
	}
	if len(got) != 2 || got[0] != sessionTenant || got[1] != sessionTenant {
		t.Fatalf("want both literals extracted, got %v", got)
	}
	// Executor: every literal equals the session -> would proceed to the pool.
	// (We only assert the extractor here; the executor path is covered below.)
}

func TestExtractAllTenantPredicates_Divergent(t *testing.T) {
	// The FIRST predicate is the session tenant; the SECOND names the victim.
	// The old first-hit extractor would have returned only the session literal
	// and let this run; D0 compares EVERY literal.
	sql := `SELECT count(*) FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `' AND tenant_id = '` + victimTenant + `' LIMIT 10`
	got, err := ExtractAllTenantPredicates(sql)
	if err != nil {
		t.Fatalf("extractor must return both literals, got err %v", err)
	}
	if len(got) != 2 || got[1] != victimTenant {
		t.Fatalf("want [session victim], got %v", got)
	}
	// The executor rejects it before the pool: structurally (Validate: tenant_id
	// appears twice) ahead of the literal comparison.
	if _, err := nilExec().ExecuteReadOnlyForTenant(nil, sessionTenant, sql); err == nil || strings.Contains(err.Error(), "no pool") {
		t.Fatalf("executor must reject a divergent second tenant literal, got %v", err)
	}
}

func TestExtractAllTenantPredicates_NotEquals(t *testing.T) {
	for _, sql := range []string{
		`SELECT id FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `' AND tenant_id <> '` + victimTenant + `' LIMIT 10`,
		`SELECT id FROM ceo_ai.x WHERE tenant_id != '` + victimTenant + `' AND tenant_id = '` + sessionTenant + `' LIMIT 10`,
		`SELECT id FROM ceo_ai.x WHERE tenant_id > '` + sessionTenant + `' LIMIT 10`,
	} {
		if _, err := ExtractAllTenantPredicates(sql); err == nil {
			t.Fatalf("expected reject for non-equality tenant predicate: %s", sql)
		}
		if _, err := nilExec().ExecuteReadOnlyForTenant(nil, sessionTenant, sql); err == nil {
			t.Fatalf("executor must reject: %s", sql)
		}
	}
}

func TestExtractAllTenantPredicates_In(t *testing.T) {
	for _, sql := range []string{
		`SELECT id FROM ceo_ai.x WHERE tenant_id IN ('` + sessionTenant + `') LIMIT 10`,
		`SELECT id FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `' AND tenant_id IN ('` + victimTenant + `') LIMIT 10`,
		`SELECT id FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `' AND tenant_id = ANY ('{` + victimTenant + `}') LIMIT 10`,
	} {
		if _, err := ExtractAllTenantPredicates(sql); err == nil {
			t.Fatalf("expected reject for IN/ANY tenant predicate: %s", sql)
		}
	}
}

func TestExtractAllTenantPredicates_ColumnRef(t *testing.T) {
	for _, sql := range []string{
		`SELECT id FROM ceo_ai.x WHERE tenant_id = tenant_id LIMIT 10`,
		`SELECT id FROM ceo_ai.x a WHERE a.tenant_id = '` + sessionTenant + `' AND a.tenant_id = a.park_id LIMIT 10`,
		`SELECT id FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `' AND tenant_id IS NOT NULL LIMIT 10`,
		`SELECT id FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `' OR tenant_id IS NULL LIMIT 10`,
		`SELECT tenant_id, id FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `' LIMIT 10`,
		`SELECT count(*) FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `' GROUP BY tenant_id LIMIT 10`,
	} {
		if _, err := ExtractAllTenantPredicates(sql); err == nil {
			t.Fatalf("expected reject for column-ref/bare tenant_id: %s", sql)
		}
	}
}

func TestExtractAllTenantPredicates_Param(t *testing.T) {
	for _, sql := range []string{
		`SELECT id FROM ceo_ai.x WHERE tenant_id = $1 LIMIT 10`,
		`SELECT id FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `' AND tenant_id = $2 LIMIT 10`,
		`SELECT id FROM ceo_ai.x WHERE tenant_id = :tenant LIMIT 10`,
	} {
		if _, err := ExtractAllTenantPredicates(sql); err == nil {
			t.Fatalf("expected reject for parameterised tenant predicate: %s", sql)
		}
		if _, err := nilExec().ExecuteReadOnlyForTenant(nil, sessionTenant, sql); err == nil {
			t.Fatalf("executor must reject: %s", sql)
		}
	}
}

func TestExtractAllTenantPredicates_ForeignTenantLiteral(t *testing.T) {
	sql := `SELECT id FROM ceo_ai.x WHERE tenant_id = '` + victimTenant + `' LIMIT 10`
	got, err := ExtractAllTenantPredicates(sql)
	if err != nil || len(got) != 1 || got[0] != victimTenant {
		t.Fatalf("extractor returns the literal as-is: got %v err %v", got, err)
	}
	if _, err := nilExec().ExecuteReadOnlyForTenant(nil, sessionTenant, sql); !errors.Is(err, ErrTenantBinding) {
		t.Fatalf("executor must reject a foreign tenant literal, got %v", err)
	}
	// Casing/whitespace/alias variants of the same bypass.
	for _, v := range []string{
		`SELECT id FROM ceo_ai.x WHERE TENANT_ID='` + victimTenant + `' LIMIT 10`,
		"SELECT id FROM ceo_ai.x WHERE\ttenant_id\n=\r\n'" + victimTenant + "' LIMIT 10",
		`SELECT id FROM ceo_ai.x a WHERE a.Tenant_Id = '` + victimTenant + `' LIMIT 10`,
	} {
		if _, err := nilExec().ExecuteReadOnlyForTenant(nil, sessionTenant, v); !errors.Is(err, ErrTenantBinding) {
			t.Fatalf("variant must be rejected: %s (err=%v)", v, err)
		}
	}
}

func TestExtractAllTenantPredicates_EntityUUIDAllowed(t *testing.T) {
	// A UUID literal on a NON-tenant column (park_id, load_id, goat_id, ...) is a
	// legitimate same-tenant filter: single relation + the session tenant
	// predicate already confine it. It must NOT be treated as a foreign tenant.
	sql := `SELECT count(*) FROM ceo_ai.animal_current_scope WHERE tenant_id = '` + sessionTenant + `' AND park_id = '` + entityUUID + `' AND shed_id = '` + victimTenant + `' LIMIT 10`
	got, err := ExtractAllTenantPredicates(sql)
	if err != nil {
		t.Fatalf("entity UUID literal must not be rejected: %v", err)
	}
	if len(got) != 1 || got[0] != sessionTenant {
		t.Fatalf("only the tenant predicate literal is extracted, got %v", got)
	}
	if err := Validate(sql); err != nil {
		t.Fatalf("validator must accept it too: %v", err)
	}
	// Schema-card hook: once park_id is marked tenant_scoped, the same statement
	// is rejected by the executor comparison because park_id's literal is not the
	// session tenant. (The hook exists; nothing registers it in P1a.)
	got, err = ExtractAllTenantPredicatesWith(sql, map[string]bool{"tenant_id": true, "park_id": true})
	if err != nil || len(got) != 2 || got[1] != entityUUID {
		t.Fatalf("tenant-scoped hook must extract park_id literal, got %v err %v", got, err)
	}
}

func TestExtractAllTenantPredicates_NoneAndDecoys(t *testing.T) {
	if _, err := ExtractAllTenantPredicates(`SELECT id FROM ceo_ai.x WHERE park = 'p' LIMIT 5`); !errors.Is(err, ErrNoTenantPredicate) {
		t.Fatalf("want ErrNoTenantPredicate, got %v", err)
	}
	// A `tenant_id =` inside a literal is data, not a predicate.
	got, err := ExtractAllTenantPredicates(`SELECT x FROM ceo_ai.x WHERE park = 'tenant_id = ''` + victimTenant + `''' AND tenant_id = '` + sessionTenant + `' LIMIT 5`)
	if err != nil || len(got) != 1 || got[0] != sessionTenant {
		t.Fatalf("decoy inside literal must be ignored: got %v err %v", got, err)
	}
	// Doubled quotes inside the tenant literal are unescaped (never equal to a UUID).
	got, err = ExtractAllTenantPredicates(`SELECT x FROM ceo_ai.x WHERE tenant_id = 'a''b' LIMIT 5`)
	if err != nil || got[0] != "a'b" {
		t.Fatalf("unescape: got %v err %v", got, err)
	}
	// A cast after the literal is allowed; an operator continuing the expression is not.
	if _, err := ExtractAllTenantPredicates(`SELECT x FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `'::uuid LIMIT 5`); err != nil {
		t.Fatalf("::uuid cast must pass: %v", err)
	}
	if _, err := ExtractAllTenantPredicates(`SELECT x FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `' || '' LIMIT 5`); err == nil {
		t.Fatal("concatenation after the tenant literal must be rejected")
	}
}

// --- trusted SQL contract ---

func TestTrustedSQLRejectsTenantLiteral(t *testing.T) {
	for _, sql := range []string{
		`SELECT label FROM ceo_ai.source_entry_health_status WHERE tenant_id = '` + sessionTenant + `' LIMIT 50`,
		`SELECT label FROM ceo_ai.source_entry_health_status WHERE tenant_id = $1 AND tenant_id = '` + victimTenant + `' LIMIT 50`,
		`SELECT l.load_id FROM procurement_loads l JOIN procurement_source_health_checks h ON h.tenant_id = l.tenant_id WHERE l.tenant_id = $1 AND h.tenant_id = '` + victimTenant + `'`,
	} {
		if err := ValidateTrusted(sql); err == nil {
			t.Fatalf("trusted SQL with a tenant literal must be rejected: %s", sql)
		}
		if _, err := nilExec().ExecuteTrustedReadOnlyForTenant(nil, sessionTenant, sql); err == nil {
			t.Fatalf("executor must reject before the pool: %s", sql)
		}
	}
}

func TestTrustedSQLRequiresTenantParam(t *testing.T) {
	for _, sql := range []string{
		`SELECT label FROM ceo_ai.source_entry_health_status WHERE park_location_id = $1 LIMIT 50`,
		`SELECT label FROM ceo_ai.source_entry_health_status WHERE tenant_id = $2 AND park_location_id = $1 LIMIT 50`,
		`SELECT label FROM ceo_ai.source_entry_health_status WHERE tenant_id IN ($1) LIMIT 50`,
		`SELECT label FROM ceo_ai.source_entry_health_status WHERE tenant_id IS NOT NULL LIMIT 50`,
		`SELECT l.load_id FROM procurement_loads l JOIN x h ON h.tenant_id = l.load_id WHERE l.tenant_id = $1`,
		`SELECT label FROM ceo_ai.source_entry_health_status LIMIT 50`,
	} {
		if err := ValidateTrusted(sql); err == nil {
			t.Fatalf("trusted SQL without tenant_id = $1 must be rejected: %s", sql)
		}
	}
	// The accepted shapes: $1 alone, $1 plus an equi-join between tenant columns.
	for _, sql := range []string{
		`SELECT label FROM ceo_ai.source_entry_health_status WHERE tenant_id = $1 AND park_location_id = $2 LIMIT 50`,
		`SELECT l.load_id FROM procurement_loads l JOIN procurement_source_health_checks h ON h.tenant_id = l.tenant_id AND h.load_id = l.load_id WHERE l.tenant_id = $1`,
	} {
		if err := ValidateTrusted(sql); err != nil {
			t.Fatalf("trusted SQL with $1 tenant binding must pass: %s (%v)", sql, err)
		}
	}
}

// --- fuzz: tenant-bypass corpus ---

func expandBypass(shape string) string {
	return ExpandBypassShape(shape, sessionTenant, victimTenant)
}

// reachedPool reports whether the nil-pool executor ACCEPTED the statement
// (every guard passed and execution was attempted).
func reachedPool(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no pool")
}

// TestTenantBypassCorpusAllRejected is the deterministic pass over the seed
// corpus: every shape must be rejected BEFORE any pool access (a nil error or
// the nil-pool error would both mean the guard let it through).
func TestTenantBypassCorpusAllRejected(t *testing.T) {
	if len(TenantBypassCorpus) < 120 {
		t.Fatalf("bypass corpus must hold >= 120 shapes, has %d", len(TenantBypassCorpus))
	}
	seen := map[string]bool{}
	for _, shape := range TenantBypassCorpus {
		if seen[shape] {
			t.Errorf("duplicate corpus shape: %q", shape)
		}
		seen[shape] = true
		sql := expandBypass(shape)
		if _, err := nilExec().ExecuteReadOnlyForTenant(nil, sessionTenant, sql); err == nil || reachedPool(err) {
			t.Errorf("bypass shape NOT rejected: %q (err=%v)", sql, err)
		}
	}
	// And the honest shapes still run (reach the nil pool -> pool error, not a reject).
	for _, ok := range []string{
		`SELECT count(*) FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `' LIMIT 10`,
		`SELECT park, count(*) FROM ceo_ai.x WHERE park = 'p' AND tenant_id = '` + sessionTenant + `' AND (stage = 'a' OR stage = 'b') GROUP BY park ORDER BY 2 DESC LIMIT 10`,
		`SELECT count(*) FROM ceo_ai.x a WHERE (a.tenant_id = '` + sessionTenant + `') AND a.event_date >= '2026-08-01' AND a.event_date < '2026-09-01' LIMIT 10`,
		`SELECT count(*) FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `'::uuid LIMIT 10`,
		`SELECT count(*) FILTER (WHERE stage = 'x') FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `' LIMIT 10`,
	} {
		if _, err := nilExec().ExecuteReadOnlyForTenant(nil, sessionTenant, ok); !reachedPool(err) {
			t.Fatalf("honest statement must pass the guard and reach the pool, got %v: %s", err, ok)
		}
	}
}

// structurallyScoped is an INDEPENDENT oracle for the safety property (it
// shares no code with checkTenantConjunct): on the raw statement, using the
// literal-aware lexer, it requires
//
//   - exactly one SELECT, exactly one FROM, and no JOIN / UNION / INTERSECT /
//     EXCEPT / TABLE / VALUES / WITH token anywhere;
//   - exactly one tenant_id token in the whole statement;
//   - that token sits after a depth-0 WHERE, at paren depth 0 or inside ONE
//     wholly-enclosing pair, is immediately followed by `=` and a string
//     literal equal to session, and the literal is followed by AND, a clause
//     keyword, or end of statement (optionally after `::ident`);
//   - the token is preceded by WHERE / AND (or by `(` that is itself preceded
//     by WHERE / AND), so the conjunct is not the operand of anything;
//   - no OR token at depth 0 of the WHERE predicate (NOT / IS / CASE around
//     the tenant conjunct are excluded by the preceded-by / followed-by
//     rules; elsewhere they cannot widen an AND-ed tenant conjunct), and the
//     FROM source is `ceo_ai.<ident>` with no depth-0 comma after it.
func structurallyScoped(sql, session string) bool {
	toks := lexRaw(sql)
	counts := map[string]int{}
	for _, t := range toks {
		if t.kind == rawIdent {
			counts[strings.ToUpper(t.text)]++
		}
	}
	if counts["SELECT"] != 1 || counts["FROM"] != 1 || counts["TENANT_ID"] != 1 {
		return false
	}
	for _, kw := range []string{"JOIN", "UNION", "INTERSECT", "EXCEPT", "TABLE", "VALUES", "WITH"} {
		if counts[kw] != 0 {
			return false
		}
	}
	// No read outside the scoped relation (PR #318 R2-1): a function call is
	// a possible second read (table_to_xml, query_to_xml*, ts_stat, dblink,
	// crosstab, ... take a table name or query text as an argument), so every
	// `ident (` must be a bare builtin from the oracle's OWN list, never
	// schema-qualified, and a double-quoted identifier is never accepted.
	for i, t := range toks {
		if t.kind == rawSym && t.text == `"` {
			return false
		}
		if t.kind != rawSym || t.text != "(" || i == 0 || toks[i-1].kind != rawIdent {
			continue
		}
		if i >= 2 && toks[i-2].kind == rawSym && toks[i-2].text == "." {
			return false
		}
		if !oracleBuiltins[strings.ToLower(toks[i-1].text)] {
			return false
		}
	}
	depth := 0
	whereIdx := -1
	fromIdx := -1
	for i, t := range toks {
		if t.kind == rawSym && t.text == "(" {
			depth++
		} else if t.kind == rawSym && t.text == ")" {
			depth--
		} else if t.kind == rawSym && t.text == "," && depth == 0 && fromIdx >= 0 && whereIdx < 0 {
			return false // comma cross-join
		} else if t.kind == rawIdent && depth == 0 && strings.EqualFold(t.text, "FROM") {
			fromIdx = i
			if i+3 > len(toks) || toks[i+1].kind != rawIdent || !strings.EqualFold(toks[i+1].text, "ceo_ai") || toks[i+2].kind != rawSym || toks[i+2].text != "." || toks[i+3].kind != rawIdent {
				return false
			}
		} else if t.kind == rawIdent && depth == 0 && strings.EqualFold(t.text, "WHERE") {
			whereIdx = i
			break
		}
	}
	if whereIdx < 0 || fromIdx < 0 {
		return false
	}
	depth = 0
	tenantIdx := -1
	for i := whereIdx + 1; i < len(toks); i++ {
		t := toks[i]
		if t.kind == rawSym && t.text == "(" {
			depth++
			continue
		}
		if t.kind == rawSym && t.text == ")" {
			depth--
			continue
		}
		if t.kind != rawIdent {
			continue
		}
		up := strings.ToUpper(t.text)
		if depth == 0 && (up == "GROUP" || up == "ORDER" || up == "HAVING" || up == "LIMIT" || up == "WINDOW" || up == "FETCH" || up == "OFFSET" || up == "FOR") {
			break
		}
		switch up {
		case "OR":
			if depth == 0 {
				return false
			}
		case "TENANT_ID":
			if depth > 1 {
				return false
			}
			tenantIdx = i
		}
	}
	if tenantIdx < 0 || tenantIdx+2 >= len(toks) {
		return false
	}
	// What precedes the column: optional `alias .`, optional one `(`, then
	// WHERE or AND — the conjunct is an operand of nothing else.
	p := tenantIdx - 1
	if p >= 1 && toks[p].kind == rawSym && toks[p].text == "." && toks[p-1].kind == rawIdent {
		p -= 2
	}
	if p >= 0 && toks[p].kind == rawSym && toks[p].text == "(" {
		p--
	}
	if p < 0 || toks[p].kind != rawIdent || (!strings.EqualFold(toks[p].text, "WHERE") && !strings.EqualFold(toks[p].text, "AND")) {
		return false
	}
	if toks[tenantIdx+1].kind != rawSym || toks[tenantIdx+1].text != "=" {
		return false
	}
	if toks[tenantIdx+2].kind != rawString || toks[tenantIdx+2].text != session {
		return false
	}
	// What follows the literal: optional `::ident`, optional one `)`, then AND /
	// clause keyword / end.
	j := tenantIdx + 3
	if j+2 < len(toks) && toks[j].kind == rawSym && toks[j].text == ":" && toks[j+1].kind == rawSym && toks[j+1].text == ":" && toks[j+2].kind == rawIdent {
		j += 3
	}
	if j < len(toks) && toks[j].kind == rawSym && toks[j].text == ")" {
		j++
	}
	if j >= len(toks) {
		return true
	}
	if toks[j].kind != rawIdent {
		return false
	}
	switch strings.ToUpper(toks[j].text) {
	case "AND", "GROUP", "ORDER", "HAVING", "LIMIT", "WINDOW", "FETCH", "OFFSET", "FOR":
		return true
	}
	return false
}

// oracleBuiltins is the oracle's OWN copy of what may precede `(` — the
// syntactic keywords plus the builtins a leadership aggregate needs. It is
// deliberately not derived from AllowedFunctions() so the two lists can
// disagree and the test notice.
var oracleBuiltins = map[string]bool{
	"and": true, "or": true, "not": true, "in": true, "any": true, "some": true, "all": true,
	"where": true, "on": true, "having": true, "when": true, "then": true, "else": true,
	"select": true, "distinct": true, "by": true, "as": true, "filter": true, "over": true,
	"between": true, "like": true, "ilike": true, "group": true,
	"count": true, "sum": true, "avg": true, "min": true, "max": true, "coalesce": true,
	"nullif": true, "round": true, "abs": true, "floor": true, "ceil": true, "greatest": true,
	"least": true, "percentile_cont": true, "percentile_disc": true, "bool_and": true, "bool_or": true,
	"date_trunc": true, "date_part": true, "extract": true, "to_char": true, "to_date": true,
	"now": true, "current_date": true, "make_date": true, "age": true, "interval": true,
	"lower": true, "upper": true, "trim": true, "length": true, "substring": true, "left": true,
	"right": true, "concat": true, "concat_ws": true, "replace": true, "initcap": true, "cast": true,
}

// TestStructurallyScopedOracle pins the independent oracle itself: it must
// accept the honest shapes and reject every corpus shape, so the fuzz
// invariant below is not vacuous.
func TestStructurallyScopedOracle(t *testing.T) {
	for _, ok := range []string{
		`SELECT count(*) FROM ceo_ai.x WHERE tenant_id = '<S>' LIMIT 10`,
		`SELECT park, count(*) FROM ceo_ai.x WHERE park = 'p' AND (tenant_id = '<S>') AND (stage = 'a' OR stage = 'b') GROUP BY park LIMIT 10`,
		`SELECT count(*) FROM ceo_ai.x WHERE tenant_id = '<S>'::uuid AND x = 1 LIMIT 10`,
	} {
		if !structurallyScoped(expandBypass(ok), sessionTenant) {
			t.Errorf("oracle must accept honest shape: %s", ok)
		}
	}
	// Shapes Validate rejects for a POLICY reason (OFFSET, FOR UPDATE, a
	// dangling AND) rather than a scoping one: the oracle judges scope only, so
	// these are the only corpus shapes it may accept.
	policyOnly := map[string]bool{
		"SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND\u3000NOT true LIMIT 10": true, // non-ASCII
		`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' OFFSET 0 LIMIT 10`:          true,
		`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND LIMIT 10`:               true,
		`SELECT id FROM ceo_ai.x WHERE AND tenant_id = '<S>' LIMIT 10`:               true,
		`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' FOR UPDATE LIMIT 10`:        true,
		`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' LIMIT 1e9`:                  true,
		`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' LIMIT 0x10`:                 true,
	}
	for _, shape := range TenantBypassCorpus {
		if structurallyScoped(expandBypass(shape), sessionTenant) != policyOnly[shape] {
			t.Errorf("oracle verdict for corpus shape must be %v: %s", policyOnly[shape], shape)
		}
	}
}

// FuzzTenantPredicateBypass mutates the corpus shapes and asserts the
// invariant that matters: whenever the executor would proceed (every guard
// passed and the nil pool was reached), the statement is STRUCTURALLY scoped
// to the session tenant per the independent oracle above — a positive
// depth-0 AND tenant conjunct, one relation, no set-op arm, no inversion,
// no disjunction, literal == session. "The literals equal the session" alone
// is NOT the property (PR #318 judge): it is what every leaking shape
// satisfied.
func FuzzTenantPredicateBypass(f *testing.F) {
	for _, shape := range TenantBypassCorpus {
		f.Add(expandBypass(shape))
	}
	f.Add(`SELECT count(*) FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `' LIMIT 10`)
	f.Add(`SELECT count(*) FROM ceo_ai.x a WHERE a.tenant_id = '` + sessionTenant + `' AND a.park_id = '` + entityUUID + `' LIMIT 10`)
	f.Fuzz(func(t *testing.T, sql string) {
		_, err := nilExec().ExecuteReadOnlyForTenant(nil, sessionTenant, sql)
		if !reachedPool(err) {
			return // rejected before the pool: the safe outcome, whatever the input
		}
		if !structurallyScoped(sql, sessionTenant) {
			t.Fatalf("guard ACCEPTED a statement that is not structurally tenant-scoped: %q", sql)
		}
		if verr := Validate(sql); verr != nil {
			t.Fatalf("accepted statement fails Validate: %v: %q", verr, sql)
		}
	})
}
