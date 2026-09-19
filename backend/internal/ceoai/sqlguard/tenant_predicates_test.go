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
	if _, err := nilExec().ExecuteReadOnlyForTenant(nil, sessionTenant, sql); !errors.Is(err, ErrTenantBinding) {
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

// bypassCorpus is the seed corpus of tenant-bypass SHAPES. Every entry, with
// <S> = the session tenant and <V> = a victim tenant, must be rejected by the
// model-SQL executor path (Validate + ExtractAllTenantPredicates + literal
// comparison). Comments are already banned by Validate; the corpus still
// includes them so a regression there is caught here too.
var bypassCorpus = []string{
	// second predicate naming the victim
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND tenant_id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>' AND tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND (tenant_id = '<V>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND park = 'p' AND tenant_id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x a WHERE a.tenant_id = '<S>' AND a.tenant_id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND tenant_id='<V>' LIMIT 10`,
	// the plain foreign literal
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id='<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE TENANT_ID = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE Tenant_Id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x a WHERE a.tenant_id = '<V>' LIMIT 10`,
	"SELECT id FROM ceo_ai.x WHERE tenant_id\t=\t'<V>' LIMIT 10",
	"SELECT id FROM ceo_ai.x WHERE tenant_id\n=\n'<V>' LIMIT 10",
	"SELECT id FROM ceo_ai.x WHERE tenant_id\r\n= '<V>' LIMIT 10",
	"SELECT id FROM ceo_ai.x WHERE tenant_id \v= '<V>' LIMIT 10",
	"SELECT id FROM ceo_ai.x WHERE tenant_id \f= '<V>' LIMIT 10",
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>'::uuid LIMIT 10`,
	// non-equality / widening operators
	`SELECT id FROM ceo_ai.x WHERE tenant_id <> '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id != '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND tenant_id <> '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id > '' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id >= '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id LIKE '%' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id ILIKE '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id ~ '.*' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id BETWEEN '<S>' AND '<V>' LIMIT 10`,
	// IN / ANY / lists
	`SELECT id FROM ceo_ai.x WHERE tenant_id IN ('<S>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id IN ('<S>', '<V>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND tenant_id IN ('<V>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = ANY ('{<S>,<V>}') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = ANY (ARRAY['<S>','<V>']) LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = SOME ('{<V>}') LIMIT 10`,
	// OR / IS / NOT
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' OR tenant_id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' OR park = 'p' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE (tenant_id = '<S>' OR tenant_id = '<V>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id IS NOT NULL LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id IS NULL OR tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE NOT tenant_id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND NOT tenant_id <> '<V>' LIMIT 10`,
	// column refs / self-equality / expressions
	`SELECT id FROM ceo_ai.x WHERE tenant_id = tenant_id LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = park_id LIMIT 10`,
	`SELECT id FROM ceo_ai.x a WHERE a.tenant_id = a.tenant_id LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND tenant_id = tenant_id LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' || '' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' = true LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = lower('<S>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = ('<S>') LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE (tenant_id) = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id::text = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE '<S>' = tenant_id LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE '<V>' = tenant_id AND tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE coalesce(tenant_id, '<S>') = '<S>' LIMIT 10`,
	// params / placeholders
	`SELECT id FROM ceo_ai.x WHERE tenant_id = $1 LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = :tid LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = ? LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND tenant_id = $2 LIMIT 10`,
	// tenant_id outside a predicate
	`SELECT tenant_id FROM ceo_ai.x WHERE tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' GROUP BY tenant_id LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' ORDER BY tenant_id LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE park = 'p' LIMIT 10`,
	`SELECT tenant_id, id FROM ceo_ai.x WHERE park = 'p' LIMIT 10`,
	// unicode lookalikes / non-ASCII outside literals
	"SELECT id FROM ceo_ai.x WHERE tenant_іd = '<S>' LIMIT 10", // Cyrillic і
	"SELECT id FROM ceo_ai.x WHERE тenant_id = '<S>' LIMIT 10", // Cyrillic т
	"SELECT id FROM ceo_ai.x WHERE tenant_id ＝ '<S>' LIMIT 10", // fullwidth =
	"SELECT id FROM ceo_ai.x WHERE tenant_id = ‘<S>’ LIMIT 10", // curly quotes
	"SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' LIMIT 10", // NBSP
	"SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>'​ AND tenant_id = '<V>' LIMIT 10",
	// comments / stacking / quoting tricks (banned upstream by Validate)
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' -- AND tenant_id = '<V>'
LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' /* */ AND tenant_id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>'; SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE "tenant_id" = '<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = E'<V>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = $$<V>$$ LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>' AND tenant_id = '<S>' LIMIT 10 OFFSET 0`,
	// multi-relation vectors (JOIN / comma / subquery)
	`SELECT a.id FROM ceo_ai.x a JOIN ceo_ai.y b ON b.tenant_id = '<V>' WHERE a.tenant_id = '<S>' LIMIT 10`,
	`SELECT a.id FROM ceo_ai.x a, ceo_ai.y b WHERE a.tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' AND id IN (SELECT id FROM ceo_ai.y WHERE tenant_id = '<V>') LIMIT 10`,
	`SELECT (SELECT count(*) FROM ceo_ai.y WHERE tenant_id = '<V>') FROM ceo_ai.x WHERE tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>' UNION SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>' LIMIT 10`,
	`WITH v AS (SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>') SELECT id FROM v WHERE tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM public.goats WHERE tenant_id = '<S>' LIMIT 10`,
	`SELECT id FROM goats WHERE tenant_id = '<S>' LIMIT 10`,
	// no LIMIT / oversize LIMIT with a victim literal
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>'`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<V>' LIMIT 1000`,
	// empty / whitespace tenant literal
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = ' <S>' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S> ' LIMIT 10`,
	`SELECT id FROM ceo_ai.x WHERE tenant_id = '<S>''' LIMIT 10`,
}

func expandBypass(shape string) string {
	return strings.NewReplacer("<S>", sessionTenant, "<V>", victimTenant).Replace(shape)
}

// TestTenantBypassCorpusAllRejected is the deterministic pass over the seed
// corpus: every shape must be rejected before any pool access.
func TestTenantBypassCorpusAllRejected(t *testing.T) {
	if len(bypassCorpus) < 50 {
		t.Fatalf("bypass corpus must hold >= 50 shapes, has %d", len(bypassCorpus))
	}
	for _, shape := range bypassCorpus {
		sql := expandBypass(shape)
		if _, err := nilExec().ExecuteReadOnlyForTenant(nil, sessionTenant, sql); err == nil {
			t.Errorf("bypass shape NOT rejected: %q", sql)
		}
	}
	// And the one honest shape still runs (reaches the nil pool -> pool error, not a reject).
	ok := `SELECT count(*) FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `' LIMIT 10`
	if _, err := nilExec().ExecuteReadOnlyForTenant(nil, sessionTenant, ok); err == nil || !strings.Contains(err.Error(), "no pool") {
		t.Fatalf("honest statement must pass the guard and reach the pool, got %v", err)
	}
}

// FuzzTenantPredicateBypass mutates the corpus shapes and asserts the invariant
// that matters: whenever the executor would proceed (no reject), EVERY extracted
// tenant literal equals the session tenant and the victim tenant appears in NO
// tenant predicate. The fuzzer can never produce a run where the victim literal
// is bound to a tenant column and the statement is still accepted.
func FuzzTenantPredicateBypass(f *testing.F) {
	for _, shape := range bypassCorpus {
		f.Add(expandBypass(shape))
	}
	f.Add(`SELECT count(*) FROM ceo_ai.x WHERE tenant_id = '` + sessionTenant + `' LIMIT 10`)
	f.Add(`SELECT count(*) FROM ceo_ai.x a WHERE a.tenant_id = '` + sessionTenant + `' AND a.park_id = '` + entityUUID + `' LIMIT 10`)
	f.Fuzz(func(t *testing.T, sql string) {
		_, err := nilExec().ExecuteReadOnlyForTenant(nil, sessionTenant, sql)
		if err == nil || !strings.Contains(err.Error(), "no pool") {
			return // rejected before the pool: the safe outcome, whatever the input
		}
		// Accepted by the guard: the invariants below must hold.
		literals, xerr := ExtractAllTenantPredicates(sql)
		if xerr != nil || len(literals) == 0 {
			t.Fatalf("accepted statement has no extractable tenant predicate: %q", sql)
		}
		for _, lit := range literals {
			if lit != sessionTenant {
				t.Fatalf("accepted statement binds a non-session tenant literal %q: %q", lit, sql)
			}
		}
		if verr := Validate(sql); verr != nil {
			t.Fatalf("accepted statement fails Validate: %v: %q", verr, sql)
		}
	})
}
