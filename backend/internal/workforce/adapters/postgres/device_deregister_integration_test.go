package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// Fixed test identities for the deregister-suppress suite. Reuses the same seed tenant as the
// roster/notification integration suites (00000000-0000-4000-8000-000000000001).
const (
	deregisterTenant = "00000000-0000-4000-8000-000000000001"
	deregisterMember = "97000000-0000-4000-8000-000000000001"
	deregisterActor  = "91000000-0000-4000-8000-000000000001"
	deregisterOther  = "91000000-0000-4000-8000-000000000002"
)

// seedDeregisterCalendarEvent is a deliberate no-op now: calendar_event_projections and the
// calendar_event_identities identity table its trigger fed (and the notification_requests FK that
// validated against calendar_event_identities) are all retired by the 5k-50k envelope cutover
// (migration 000189, docs/decisions/operational-kernel-5k-50k-scale-envelope.md).
// notification_requests.calendar_event_id is a plain, unconstrained text column now -- there is
// nothing left to seed a referential fixture row for. Kept as a function (rather than deleting every
// call site) so this test's intent at each call site stays self-documenting.
func seedDeregisterCalendarEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID string) {
	t.Helper()
}

// seedDeregisterNotification inserts one notification_requests row keyed by recipient_ref (the raw
// FCM token, matching how calendar's QueueRoleNotifications populates it -- see
// internal/calendar/ports/repository.go NotificationRecipient / QueueRoleNotifications).
func seedDeregisterNotification(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID, key, recipientRef, status string) string {
	t.Helper()
	var requestID string
	if err := pool.QueryRow(ctx, `
INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, target_id, notification_type, channel,
  recipient_ref, title, body, status, idempotency_key, request_fingerprint, context, trace_id
) VALUES (
  $1::uuid, $2, 'cohort', NULL, 'rework', 'push_fcm',
  $3, 'Deregister suppress test', 'body', $4, $5, $5 || ':fingerprint', '{}'::jsonb, 'trace-' || $5
)
RETURNING notification_request_id::text`,
		deregisterTenant, eventID, recipientRef, status, key).Scan(&requestID); err != nil {
		t.Fatalf("seed notification: %v", err)
	}
	return requestID
}

func notificationStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, requestID string) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `
SELECT status FROM notification_requests
WHERE tenant_id = $1::uuid AND notification_request_id = $2::uuid`,
		deregisterTenant, requestID).Scan(&status); err != nil {
		t.Fatalf("query notification status: %v", err)
	}
	return status
}

// TestDeregisterDeviceSuppressesQueuedNotificationsForClearedTokenWithDockerPostgres proves the
// offline-logout / shared-phone delivery window closed in DeregisterDevice: a notification_requests
// row already queued (or failed) against the device's raw fcm_token must flip to 'suppressed' in the
// SAME transaction as the device revoke, while a 'sent'/'read' row for the exact same token -- audit
// history -- is left untouched.
func TestDeregisterDeviceSuppressesQueuedNotificationsForClearedTokenWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	rawToken := "fcm-token-deregister-suppress-001"

	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint)
VALUES ($1, $2, $3, 'DEREG-01', 'Deregister Suppress Test Member', 'active', 'other')`,
		deregisterMember, deregisterTenant, deregisterActor); err != nil {
		t.Fatalf("seed workforce_members: %v", err)
	}

	var deviceID string
	if err := pool.QueryRow(ctx, `
INSERT INTO workforce_member_devices (
  tenant_id, workforce_member_id, app_install_id, push_token_hash, fcm_token,
  app_version, status, registered_by
) VALUES (
  $1::uuid, $2::uuid, 'install-deregister-suppress-001', 'hash-001', $3,
  '1.0.0', 'active', $4::uuid
)
RETURNING device_id::text`,
		deregisterTenant, deregisterMember, rawToken, deregisterActor).Scan(&deviceID); err != nil {
		t.Fatalf("seed workforce_member_devices: %v", err)
	}

	eventID := "calendar:deregister-suppress-" + deviceID
	seedDeregisterCalendarEvent(t, ctx, pool, eventID)

	queuedID := seedDeregisterNotification(t, ctx, pool, eventID, "deregister-suppress-queued", rawToken, "queued")
	failedID := seedDeregisterNotification(t, ctx, pool, eventID, "deregister-suppress-failed", rawToken, "failed")
	sentID := seedDeregisterNotification(t, ctx, pool, eventID, "deregister-suppress-sent", rawToken, "sent")
	readID := seedDeregisterNotification(t, ctx, pool, eventID, "deregister-suppress-read", rawToken, "read")
	// A different device/token's queued row must never be touched by this device's deregister.
	otherEventID := "calendar:deregister-suppress-other-" + deviceID
	seedDeregisterCalendarEvent(t, ctx, pool, otherEventID)
	otherTokenQueuedID := seedDeregisterNotification(t, ctx, pool, otherEventID, "deregister-suppress-other-token-queued", "fcm-token-unrelated-device", "queued")

	repo := NewRepository(pool, 5*time.Second)

	summary, err := repo.DeregisterDevice(ctx, ports.DeregisterDeviceCommand{
		TenantID: deregisterTenant,
		ActorID:  deregisterActor,
		DeviceID: deviceID,
	})
	if err != nil {
		t.Fatalf("DeregisterDevice: %v", err)
	}
	if summary.Status != "revoked" {
		t.Fatalf("device status = %q, want revoked", summary.Status)
	}
	if summary.FCMToken != nil {
		t.Fatalf("device fcm_token = %v, want nil after revoke", summary.FCMToken)
	}

	if got := notificationStatus(t, ctx, pool, queuedID); got != "suppressed" {
		t.Fatalf("queued notification status = %q, want suppressed", got)
	}
	if got := notificationStatus(t, ctx, pool, failedID); got != "suppressed" {
		t.Fatalf("failed notification status = %q, want suppressed", got)
	}
	if got := notificationStatus(t, ctx, pool, sentID); got != "sent" {
		t.Fatalf("sent notification status = %q, want untouched (sent) -- audit rows must never be rewritten", got)
	}
	if got := notificationStatus(t, ctx, pool, readID); got != "read" {
		t.Fatalf("read notification status = %q, want untouched (read) -- audit rows must never be rewritten", got)
	}
	if got := notificationStatus(t, ctx, pool, otherTokenQueuedID); got != "queued" {
		t.Fatalf("other device's queued notification status = %q, want untouched (queued)", got)
	}

	// Idempotent replay: deregistering the already-revoked device again must be a no-op (0 rows
	// affected by the revoke UPDATE) and must not re-run the suppress step or error.
	replay, err := repo.DeregisterDevice(ctx, ports.DeregisterDeviceCommand{
		TenantID: deregisterTenant,
		ActorID:  deregisterActor,
		DeviceID: deviceID,
	})
	if err != nil {
		t.Fatalf("DeregisterDevice replay: %v", err)
	}
	if replay.Status != "revoked" {
		t.Fatalf("replay device status = %q, want revoked", replay.Status)
	}
}

// TestDeregisterDeviceWithNoFCMTokenSkipsSuppressWithDockerPostgres proves a device that never had a
// fcm_token (nothing was ever queued keyed to it) deregisters cleanly without attempting the suppress
// step.
func TestDeregisterDeviceWithNoFCMTokenSkipsSuppressWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint)
VALUES ($1, $2, $3, 'DEREG-02', 'Deregister No-Token Test Member', 'active', 'other')`,
		"97000000-0000-4000-8000-000000000002", deregisterTenant, deregisterOther); err != nil {
		t.Fatalf("seed workforce_members: %v", err)
	}

	var deviceID string
	if err := pool.QueryRow(ctx, `
INSERT INTO workforce_member_devices (
  tenant_id, workforce_member_id, app_install_id, app_version, status, registered_by
) VALUES (
  $1::uuid, $2::uuid, 'install-deregister-no-token-001', '1.0.0', 'active', $3::uuid
)
RETURNING device_id::text`,
		deregisterTenant, "97000000-0000-4000-8000-000000000002", deregisterOther).Scan(&deviceID); err != nil {
		t.Fatalf("seed workforce_member_devices: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)
	summary, err := repo.DeregisterDevice(ctx, ports.DeregisterDeviceCommand{
		TenantID: deregisterTenant,
		ActorID:  deregisterOther,
		DeviceID: deviceID,
	})
	if err != nil {
		t.Fatalf("DeregisterDevice: %v", err)
	}
	if summary.Status != "revoked" {
		t.Fatalf("device status = %q, want revoked", summary.Status)
	}
}
