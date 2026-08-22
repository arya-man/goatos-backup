package authallow

import (
	"context"
	"testing"
)

type staticDynamic map[string]bool

func (s staticDynamic) EmailAllowed(_ context.Context, tenantID, email string) bool {
	return s[tenantID+"|"+email]
}

func boolPtr(v bool) *bool { return &v }

func TestAllowsWithDynamicUnionsEnvAndDB(t *testing.T) {
	envSet, err := NewEmailSet([]string{"env@mesha.sg"})
	if err != nil {
		t.Fatalf("NewEmailSet: %v", err)
	}
	dynamic := staticDynamic{"tenant-a|db@mesha.sg": true}
	ctx := context.Background()

	if !AllowsWithDynamic(ctx, envSet, dynamic, "tenant-a", "env@mesha.sg", boolPtr(true)) {
		t.Fatalf("env email must pass")
	}
	if !AllowsWithDynamic(ctx, envSet, dynamic, "tenant-a", "DB@mesha.sg", boolPtr(true)) {
		t.Fatalf("DB-allowlisted email must pass (normalized)")
	}
	if AllowsWithDynamic(ctx, envSet, dynamic, "tenant-a", "other@mesha.sg", boolPtr(true)) {
		t.Fatalf("email in neither set must be refused")
	}
}

func TestAllowsWithDynamicScopesDBAllowlistByTenant(t *testing.T) {
	envSet, err := NewEmailSet([]string{"env@mesha.sg"})
	if err != nil {
		t.Fatalf("NewEmailSet: %v", err)
	}
	dynamic := staticDynamic{"tenant-a|db@mesha.sg": true}
	ctx := context.Background()

	if !AllowsWithDynamic(ctx, envSet, dynamic, "tenant-a", "db@mesha.sg", boolPtr(true)) {
		t.Fatalf("tenant A dynamic email must pass")
	}
	if AllowsWithDynamic(ctx, envSet, dynamic, "tenant-b", "db@mesha.sg", boolPtr(true)) {
		t.Fatalf("tenant B must not inherit tenant A's dynamic allowlist row")
	}
}

func TestAllowsWithDynamicKeepsVerificationRequirement(t *testing.T) {
	envSet, _ := NewEmailSet([]string{"env@mesha.sg"})
	dynamic := staticDynamic{"tenant-a|db@mesha.sg": true}
	ctx := context.Background()

	if AllowsWithDynamic(ctx, envSet, dynamic, "tenant-a", "db@mesha.sg", boolPtr(false)) {
		t.Fatalf("unverified email must be refused even when DB-allowlisted")
	}
	if AllowsWithDynamic(ctx, envSet, dynamic, "tenant-a", "db@mesha.sg", nil) {
		t.Fatalf("unknown verification state must be refused")
	}
}

// Enforcement stays keyed on the ENV set: an empty env allowlist means
// "allowlist disabled" (local dev) — the dynamic source must not silently turn
// enforcement on.
func TestAllowsWithDynamicDisabledWhenEnvSetEmpty(t *testing.T) {
	dynamic := staticDynamic{"tenant-a|db@mesha.sg": true}
	if !AllowsWithDynamic(context.Background(), nil, dynamic, "tenant-a", "anyone@example.com", nil) {
		t.Fatalf("empty env set must keep allow-all semantics")
	}
}

func TestAllowsWithDynamicNilSourceMatchesEnvOnly(t *testing.T) {
	envSet, _ := NewEmailSet([]string{"env@mesha.sg"})
	ctx := context.Background()
	if !AllowsWithDynamic(ctx, envSet, nil, "tenant-a", "env@mesha.sg", boolPtr(true)) {
		t.Fatalf("env email must pass without a dynamic source")
	}
	if AllowsWithDynamic(ctx, envSet, nil, "tenant-a", "db@mesha.sg", boolPtr(true)) {
		t.Fatalf("non-env email must be refused without a dynamic source")
	}
}
