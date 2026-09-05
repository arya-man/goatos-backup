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
	if !strings.Contains(block, "cachedTenantGrants") || !strings.Contains(block, "storeTenantGrants") {
		t.Fatal("ActiveTenantGrants must use the bounded burst cache so route switches do not repeat the same grant lookup per API")
	}
	if !strings.Contains(src, "cacheTTL: 30 * time.Second") {
		t.Fatal("authorization cache must stay bounded to the feed/read cache window; do not extend it without explicit revocation invalidation")
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
