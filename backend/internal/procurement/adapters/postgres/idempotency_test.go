package postgres

import "testing"

// JSON fields (Context/Metadata/ProofRefs/flags/vaccination history) enter the request fingerprint, so they
// must be canonicalized — otherwise the same payload with reordered object keys would falsely conflict.
func TestCanonicalJSONIsKeyOrderInsensitive(t *testing.T) {
	if a, b := canonicalJSON([]byte(`{"b":1,"a":2}`)), canonicalJSON([]byte(`{"a":2,"b":1}`)); a != b {
		t.Fatalf("reordered object keys must canonicalize equal: %q vs %q", a, b)
	}
	if a, b := canonicalJSON([]byte(`{"x":{"q":1,"p":2}}`)), canonicalJSON([]byte(`{"x":{"p":2,"q":1}}`)); a != b {
		t.Fatalf("nested reordered keys must canonicalize equal: %q vs %q", a, b)
	}
	// Array order is semantic and must be preserved (distinct fingerprints).
	if canonicalJSON([]byte(`[1,2]`)) == canonicalJSON([]byte(`[2,1]`)) {
		t.Fatal("array order must be preserved")
	}
	// Large integers must not be rounded by float conversion.
	big := `{"n":9007199254740993}`
	if got := canonicalJSON([]byte(big)); got != big {
		t.Fatalf("large integer must survive canonicalization: %q", got)
	}
	if canonicalJSON(nil) != "" || canonicalJSON([]byte("")) != "" {
		t.Fatal("empty JSON must canonicalize to empty")
	}
}

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
