package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	mdvTenant   = "00000000-0000-4000-8000-000000000001"
	mdvPark     = "94000000-0000-4000-8000-000000000001"
	mdvVerifier = "97000000-0000-4000-8000-000000000001"
	mdvUser     = "97000000-0000-4000-8000-000000000002"
)

// TestResolveModuleDutyRecipientsFallsBackToVerifierGrant proves the C-defect-A verifier-gap fix:
// a tenant that has NOT (yet, or ever) materialized a park's 'verify' duty into
// position_module_duties/workforce_positions -- the exact gap PendingNotificationDutyModules /
// seed-position-duties exists to close, and confirmed live in the E2E stack on 2026-08-04 (Jyothi
// held an active tenant-scope 'verifier' user_scope_grants row and still received zero
// verification_pending pushes) -- must still resolve a reachable device via the SAME
// user_scope_grants fallback ResolvePositionRecipients already uses for leadership seats.
func TestResolveModuleDutyRecipientsFallsBackToVerifierGrant(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'MDV-VER-01', 'Verifier', 'active', 'verifier')`,
		mdvVerifier, mdvTenant, mdvUser); err != nil {
		t.Fatalf("seed workforce_members: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_member_devices (device_id, tenant_id, workforce_member_id, platform, app_install_id, fcm_token, app_version, os_version, status, last_seen_at, registered_by)
VALUES (gen_random_uuid(), $1::uuid, $2::uuid, 'android', 'mdv-install', 'mdv-token', '1.0.0', '14', 'active', now(), $2::uuid)`,
		mdvTenant, mdvVerifier); err != nil {
		t.Fatalf("seed device: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'verifier', 'tenant', $1::uuid, 'active', now() - interval '1 hour')`,
		mdvTenant, mdvUser); err != nil {
		t.Fatalf("seed user_scope_grants: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)

	// No workforce_positions and no position_module_duties row exists for this park/module at
	// all -- the position-based join alone must return nothing, so a non-empty result here can
	// only have come from the grant fallback.
	got, err := repo.ResolveModuleDutyRecipients(ctx, mdvTenant, "center", mdvPark, "pc.vaccination", "verify", time.Now())
	if err != nil {
		t.Fatalf("ResolveModuleDutyRecipients: %v", err)
	}
	if len(got) != 1 || got[0].FCMToken != "mdv-token" {
		t.Fatalf("ResolveModuleDutyRecipients = %+v, want exactly one recipient with fcm_token=mdv-token (via user_scope_grants role=verifier fallback)", got)
	}

	// A grant for a DIFFERENT duty type (e.g. 'execute') must NOT be treated as a verify-duty
	// holder -- the fallback is scoped to dutyType='verify' only.
	gotExecute, err := repo.ResolveModuleDutyRecipients(ctx, mdvTenant, "center", mdvPark, "pc.vaccination", "execute", time.Now())
	if err != nil {
		t.Fatalf("ResolveModuleDutyRecipients(execute): %v", err)
	}
	if len(gotExecute) != 0 {
		t.Fatalf("ResolveModuleDutyRecipients(execute) = %+v, want none: the verifier grant fallback must not leak into other duty types", gotExecute)
	}
}
