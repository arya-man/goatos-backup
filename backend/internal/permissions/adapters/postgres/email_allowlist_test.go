package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestAllowedEmailSourceIsTenantScoped(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)

	const (
		tenantA = "00000000-0000-4000-8000-000000000001"
		tenantB = "00000000-0000-4000-8000-000000000002"
		email   = "same.user@mesha.sg"
	)
	if _, err := pool.Exec(ctx, `
INSERT INTO tenants (tenant_id, name, status)
VALUES ($1::uuid, 'Tenant B', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, tenantB); err != nil {
		t.Fatalf("seed tenant B: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO auth_allowed_emails (tenant_id, email, normalized_email, status, source)
VALUES ($1::uuid, $2, $2, 'active', 'test')`, tenantA, email); err != nil {
		t.Fatalf("seed allowlist row: %v", err)
	}

	source := NewAllowedEmailSource(pool, 5*time.Second, nil)
	if !source.EmailAllowed(ctx, tenantA, email) {
		t.Fatalf("tenant A email should be allowed")
	}
	if source.EmailAllowed(ctx, tenantB, email) {
		t.Fatalf("tenant B must not inherit tenant A's dynamic allowlist row")
	}
}
