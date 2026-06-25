package postgres

import "testing"

// The shared idempotency_keys table has a global primary key on idempotency_key, so the scoped key must
// carry the tenant — otherwise two tenants reusing the same client key for the same operation would collide
// (one tenant's write would replay/return the other's result).
func TestIdemScopedKeyIsTenantAndOperationScoped(t *testing.T) {
	const (
		tenantA = "00000000-0000-4000-8000-0000000000a1"
		tenantB = "00000000-0000-4000-8000-0000000000b2"
		key     = "client-key-1"
	)

	if a, b := idemScopedKey(tenantA, "procurement.load_goat", key), idemScopedKey(tenantB, "procurement.load_goat", key); a == b {
		t.Fatalf("same client key + operation across tenants must scope differently: %q == %q", a, b)
	}
	if a, b := idemScopedKey(tenantA, "procurement.load_goat", key), idemScopedKey(tenantA, "procurement.dispatch", key); a == b {
		t.Fatalf("same client key across operations within a tenant must scope differently: %q == %q", a, b)
	}
	if a, b := idemScopedKey(tenantA, "procurement.load_goat", " "+key+" "), idemScopedKey(tenantA, "procurement.load_goat", key); a != b {
		t.Fatalf("client key whitespace must be trimmed: %q != %q", a, b)
	}
}
