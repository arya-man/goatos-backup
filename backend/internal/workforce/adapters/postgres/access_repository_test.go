package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	accessTenant = "00000000-0000-4000-8000-000000000001"
	accessMember = "93000000-0000-4000-8000-000000000001"
	accessUser   = "93000000-0000-4000-8000-000000000002"
)

func TestResolvePermissionsDistinguishesConfiguredEmptyAccessFromMissingAccess(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewAccessRepository(pool)

	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'ACC-EMPTY-01', 'Empty Access', 'active', 'operator')`,
		accessMember, accessTenant, accessUser); err != nil {
		t.Fatalf("seed workforce member: %v", err)
	}

	perms, provisioned, err := repo.ResolvePermissions(ctx, accessTenant, accessUser)
	if err != nil {
		t.Fatalf("ResolvePermissions before person_access: %v", err)
	}
	if provisioned || len(perms) != 0 {
		t.Fatalf("missing person_access = (%#v, %v), want no perms and provisioned=false for migration fallback", perms, provisioned)
	}

	if _, err := pool.Exec(ctx, `
INSERT INTO person_access (tenant_id, workforce_member_id, scope_mode, designation_code)
VALUES ($1::uuid, $2::uuid, 'parks', 'operator')`,
		accessTenant, accessMember); err != nil {
		t.Fatalf("seed empty person_access: %v", err)
	}

	perms, provisioned, err = repo.ResolvePermissions(ctx, accessTenant, accessUser)
	if err != nil {
		t.Fatalf("ResolvePermissions after empty person_access: %v", err)
	}
	if !provisioned || len(perms) != 0 {
		t.Fatalf("empty configured person_access = (%#v, %v), want no perms and provisioned=true so auth denies instead of falling back", perms, provisioned)
	}
}
