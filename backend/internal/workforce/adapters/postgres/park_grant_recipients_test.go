package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	pgrTenant = "00000000-0000-4000-8000-000000000001"
	pgrPark   = "94000000-0000-4000-8000-000000000002"
	pgrOther  = "94000000-0000-4000-8000-000000000003"
	pgrMember = "97000000-0000-4000-8000-000000000011"
	pgrUser   = "97000000-0000-4000-8000-000000000012"
)

// TestResolvePositionRecipientsReachesParkScopedGrants pins the 2026-09-08 finding from the
// notification-audience E2E: a park head is a PARK-SCOPED ROLE GRANT under the per-person access
// model (four park_head grants on the STG mirror, zero park_head seats in workforce_positions), so
// resolving a 'center' scope from seats alone reached nobody and every "park head" push silently
// went to no one. The grant must resolve at ITS park and not at another park.
func TestResolvePositionRecipientsReachesParkScopedGrants(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	for _, park := range []struct{ id, code string }{{pgrPark, "PGR-P1"}, {pgrOther, "PGR-P2"}} {
		if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', $3, $3, 'active')`, park.id, pgrTenant, park.code); err != nil {
			t.Fatalf("seed park: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'PGR-PH-01', 'Park Head', 'active', 'park_head')`,
		pgrMember, pgrTenant, pgrUser); err != nil {
		t.Fatalf("seed workforce_members: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_member_devices (device_id, tenant_id, workforce_member_id, platform, app_install_id, fcm_token, app_version, os_version, status, last_seen_at, registered_by)
VALUES (gen_random_uuid(), $1::uuid, $2::uuid, 'android', 'pgr-install', 'pgr-token', '1.0.0', '14', 'active', now(), $2::uuid)`,
		pgrTenant, pgrMember); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'park_head', 'park', $3::uuid, 'active', now() - interval '1 hour')`,
		pgrTenant, pgrUser, pgrPark); err != nil {
		t.Fatalf("seed user_scope_grants: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)
	got, err := repo.ResolvePositionRecipients(ctx, pgrTenant, "center", pgrPark, "park_head", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FCMToken != "pgr-token" {
		t.Fatalf("park_head at its park = %+v, want the grant holder's device", got)
	}
	other, err := repo.ResolvePositionRecipients(ctx, pgrTenant, "center", pgrOther, "park_head", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("park_head at ANOTHER park = %+v, want none", other)
	}
	batch, err := repo.ResolvePositionRecipientsBatch(ctx, pgrTenant, "center", []string{pgrPark, pgrOther}, []string{"park_head"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(batch[pgrPark+"|park_head"]) != 1 || len(batch[pgrOther+"|park_head"]) != 0 {
		t.Fatalf("batch = %+v", batch)
	}
}
