package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// Fixed test identities for the register-device-multi-device suite.
const (
	supersedeTenant = "00000000-0000-4000-8000-000000000001"
	supersedeMember = "97000000-0000-4000-8000-000000000101"
	supersedeActor  = "91000000-0000-4000-8000-000000000101"
)

func strPtr(s string) *string { return &s }

// TestRegisterDeviceNeverSupersedesSiblingTokensWithDockerPostgres proves the fix for the
// multi-device regression: registering a device for a member must NEVER clear another active
// device's fcm_token, whether that other row is a genuinely different physical device (the
// maintainer's explicit multi-device requirement -- device_id is a primary analytics key for
// exactly this reason) or a stale row left behind by a `pm clear` reinstall (device_public_key_hash
// is NULL for every device in this fleet today, so registration has no reliable way to tell the two
// cases apart). Both must keep their own token; cleanup of a genuinely dead install is left to the
// delivery-time self-heal (SuppressInvalidRecipient), not to registration.
func TestRegisterDeviceNeverSupersedesSiblingTokensWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint)
VALUES ($1, $2, $3, 'SUPERSEDE-01', 'Supersede Test Member', 'active', 'operator')`,
		supersedeMember, supersedeTenant, supersedeActor); err != nil {
		t.Fatalf("seed workforce_members: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)

	// First device: could be a reinstall-orphaned row OR a genuinely separate physical device --
	// registration cannot and must not tell the difference.
	first, err := repo.RegisterDevice(ctx, ports.RegisterDeviceCommand{
		TenantID: supersedeTenant,
		ActorID:  supersedeActor,
		Body: domain.RegisterDeviceRequest{
			AppInstallID: "install-supersede-first",
			FcmToken:     strPtr("fcm-token-first-install"),
			AppVersion:   "1.0.0",
			OSVersion:    "13",
		},
	})
	if err != nil {
		t.Fatalf("RegisterDevice(first): %v", err)
	}
	if first.FCMToken == nil || *first.FCMToken != "fcm-token-first-install" {
		t.Fatalf("first device fcm_token = %v, want fcm-token-first-install", first.FCMToken)
	}

	// Second device: same member, a different app_install_id. This is the genuine multi-device
	// case the maintainer requires (a spare handset, a shared phone) -- it must NOT silence the
	// first device.
	second, err := repo.RegisterDevice(ctx, ports.RegisterDeviceCommand{
		TenantID: supersedeTenant,
		ActorID:  supersedeActor,
		Body: domain.RegisterDeviceRequest{
			AppInstallID: "install-supersede-second",
			FcmToken:     strPtr("fcm-token-second-install"),
			AppVersion:   "1.1.0",
			OSVersion:    "13",
		},
	})
	if err != nil {
		t.Fatalf("RegisterDevice(second): %v", err)
	}
	if second.DeviceID == first.DeviceID {
		t.Fatalf("expected a distinct device row for the new app_install_id, got the same device_id")
	}
	if second.FCMToken == nil || *second.FCMToken != "fcm-token-second-install" {
		t.Fatalf("second device fcm_token = %v, want fcm-token-second-install", second.FCMToken)
	}

	// The FIRST device's token must survive the SECOND device's registration -- this is exactly
	// the regression: registering device B must never silence device A.
	var firstToken *string
	var firstStatus string
	var firstRevokedAt *time.Time
	if err := pool.QueryRow(ctx, `
SELECT fcm_token, status, revoked_at FROM workforce_member_devices WHERE device_id = $1::uuid`,
		first.DeviceID).Scan(&firstToken, &firstStatus, &firstRevokedAt); err != nil {
		t.Fatalf("query first device: %v", err)
	}
	if firstToken == nil || *firstToken != "fcm-token-first-install" {
		t.Fatalf("first device fcm_token = %v, want fcm-token-first-install (registering a second device must never clear a sibling's token)", firstToken)
	}
	if firstStatus != "active" {
		t.Fatalf("first device status = %q, want active", firstStatus)
	}
	if firstRevokedAt != nil {
		t.Fatalf("first device revoked_at = %v, want nil", *firstRevokedAt)
	}

	// The second (current) install keeps its own token too.
	var secondToken *string
	if err := pool.QueryRow(ctx, `
SELECT fcm_token FROM workforce_member_devices WHERE device_id = $1::uuid`,
		second.DeviceID).Scan(&secondToken); err != nil {
		t.Fatalf("query second device: %v", err)
	}
	if secondToken == nil || *secondToken != "fcm-token-second-install" {
		t.Fatalf("second device fcm_token = %v, want fcm-token-second-install untouched", secondToken)
	}

	// Recipient resolution must now return BOTH reachable devices -- proving a member with two
	// live installs gets pushes fanned out to both, not just the most-recently-registered one.
	recipients, err := repo.ResolveMemberRecipients(ctx, supersedeTenant, supersedeMember)
	if err != nil {
		t.Fatalf("ResolveMemberRecipients: %v", err)
	}
	if len(recipients) != 2 {
		t.Fatalf("ResolveMemberRecipients returned %d recipients, want exactly 2 (both live installs)", len(recipients))
	}
	gotDeviceIDs := map[string]string{}
	for _, rec := range recipients {
		gotDeviceIDs[rec.DeviceID] = rec.FCMToken
	}
	if gotDeviceIDs[first.DeviceID] != "fcm-token-first-install" {
		t.Fatalf("resolved recipients missing/wrong token for first device: %v", gotDeviceIDs)
	}
	if gotDeviceIDs[second.DeviceID] != "fcm-token-second-install" {
		t.Fatalf("resolved recipients missing/wrong token for second device: %v", gotDeviceIDs)
	}
}
