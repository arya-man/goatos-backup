package sqlguard

import "testing"

// TestValidateRejectsTopLevelOr proves the boolean-disjunction cross-tenant
// bypass is blocked: `tenant_id = '<session>' OR park_label = 'x'` textually
// satisfies the tenant predicate yet would widen the scan to every tenant.
func TestValidateRejectsTopLevelOr(t *testing.T) {
	bad := `SELECT vaccine, park_label FROM ceo_ai.vaccination_shed_status WHERE tenant_id = '11111111-1111-1111-1111-111111111111' OR park_label = 'Coimbatore' LIMIT 100`
	if err := Validate(bad); err == nil {
		t.Fatal("expected top-level OR in WHERE to be rejected")
	}

	// A parenthesized disjunction AND-ed under the tenant scope stays safe.
	ok := `SELECT shed_label FROM ceo_ai.vaccination_shed_status WHERE tenant_id = '1' AND (status = 'due' OR status = 'overdue') LIMIT 20`
	if err := Validate(ok); err != nil {
		t.Fatalf("parenthesized OR under tenant scope must pass, got %v", err)
	}
}

func TestExtractTenantEquals(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want string
		ok   bool
	}{
		{"bare", `SELECT count(*) FROM ceo_ai.x WHERE tenant_id = 'abc' LIMIT 10`, "abc", true},
		{"alias-qualified", `SELECT a.x FROM ceo_ai.x a WHERE a.tenant_id = 'sess-1' AND a.park = 'p' LIMIT 5`, "sess-1", true},
		{"literal-embedded decoy", `SELECT x FROM ceo_ai.x WHERE park = 'tenant_id = fake' AND tenant_id = 'real' LIMIT 5`, "real", true},
		{"none", `SELECT x FROM ceo_ai.x WHERE park = 'p' LIMIT 5`, "", false},
	}
	for _, tc := range cases {
		got, ok := ExtractTenantEquals(tc.sql)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: got (%q,%v) want (%q,%v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

// TestExecuteReadOnlyForTenantBindsSession proves the executor rejects a draft
// whose tenant literal is not the session tenant BEFORE touching the pool (nil
// pool never reached), closing the value-not-compared-to-session vector.
func TestExecuteReadOnlyForTenantBindsSession(t *testing.T) {
	e := &Executor{} // nil pool: a passing tenant-bind would reach it and panic
	other := `SELECT id FROM ceo_ai.animal_current_scope WHERE tenant_id = '00000000-0000-0000-0000-000000000002' LIMIT 10`
	if _, err := e.ExecuteReadOnlyForTenant(nil, "00000000-0000-0000-0000-000000000001", other); err != ErrTenantBinding {
		t.Fatalf("expected ErrTenantBinding for mismatched tenant literal, got %v", err)
	}

	empty := `SELECT id FROM ceo_ai.animal_current_scope WHERE tenant_id = '1' LIMIT 10`
	if _, err := e.ExecuteReadOnlyForTenant(nil, "", empty); err != ErrTenantBinding {
		t.Fatalf("expected ErrTenantBinding for empty session tenant, got %v", err)
	}
}
