package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestActiveTenantGrantsDoesNotCacheAuthorizationState(t *testing.T) {
	b, err := os.ReadFile("grants.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	block := src[strings.Index(src, "func (g *GrantSource) ActiveTenantGrants("):]
	if strings.Contains(block, "cachedTenantGrants") || strings.Contains(block, "storeTenantGrants") {
		t.Fatal("ActiveTenantGrants must not cache authorization state; rely on the active-grant lookup index instead")
	}
	if strings.Contains(src, "cacheTTL") || strings.Contains(src, "cachedGrants") {
		t.Fatal("authorization grant source must not keep cross-request result cache without explicit revocation invalidation")
	}
}

func TestActiveTenantGrantLookupIndexMatchesAuthQueryShape(t *testing.T) {
	b, err := os.ReadFile("../../../../migrations/postgres/000255_user_scope_grants_active_lookup.sql")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, required := range []string{
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS user_scope_grants_active_lookup_idx",
		"ON public.user_scope_grants (user_id, tenant_id, role, scope_type, scope_id)",
		"INCLUDE (valid_from, valid_to)",
		"WHERE status = 'active'",
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("active grant lookup index must preserve auth hot-path shape; missing %q", required)
		}
	}
}
