package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/notification/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	testTenantID = "00000000-0000-4000-8000-000000000001"
	testEventID  = "calendar:86000000-0000-4000-8000-000000010001"
)

func TestNotificationRepositoryClaimMarkAndReclaim(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	now := time.Date(2026, 6, 27, 9, 30, 0, 0, time.UTC)
	seedCalendarEvent(t, ctx, pool, testEventID)
	requestID := seedNotification(t, ctx, pool, testEventID, "notification-repo-claim", "queued", 0, nil)

	claimed, err := repo.ClaimDue(ctx, ports.ClaimParams{
		TenantID:    testTenantID,
		Limit:       10,
		MaxAttempts: 5,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 1 || claimed[0].NotificationRequestID != requestID || claimed[0].LeaseToken == "" || claimed[0].DeliveryAttempts != 1 {
		t.Fatalf("claimed=%#v want one leased request", claimed)
	}
	if err := repo.MarkSent(ctx, testTenantID, requestID, claimed[0].LeaseToken, "test-dispatcher", now.Add(time.Minute)); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	assertNotificationState(t, ctx, pool, requestID, "sent", 1, false)

	failedID := seedNotification(t, ctx, pool, testEventID, "notification-repo-reclaim", "sending", 1, nil)
	if _, err := pool.Exec(ctx, `
UPDATE notification_requests
SET lease_token = gen_random_uuid(),
    leased_at = $3::timestamptz
WHERE tenant_id = $1::uuid AND notification_request_id = $2::uuid`,
		testTenantID, failedID, now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("make stale sending: %v", err)
	}
	reclaimed, err := repo.ReclaimStaleSending(ctx, testTenantID, now, 2*time.Minute)
	if err != nil {
		t.Fatalf("ReclaimStaleSending: %v", err)
	}
	if reclaimed != 1 {
		t.Fatalf("reclaimed=%d want 1", reclaimed)
	}
	assertNotificationState(t, ctx, pool, failedID, "queued", 1, true)
}

func seedCalendarEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO calendar_event_projections (
  tenant_id, event_id, slice_key, event_type, owner_key, title, subtitle, status, severity,
  due_at, window_start, window_end, timezone, timezone_source, target_type, target_count,
  source_backed, source_label, source_target_type, source_target_id, assignee_label,
  executor_role, reminder_state, primary_notification_channel, escalation_state,
  system, cross_cutting, links, detail
) VALUES (
  $1::uuid, $2, 'vaccination', 'vaccination_dose_due', 'phc',
  'Notification repo event', 'Notification integration test', 'due', 'warning',
  now(), now(), now() + interval '1 day', 'Asia/Kolkata', 'fallback',
  'cohort', 1, true, 'notification repo source', 'calendar_event', NULL,
  'PHC test owner', 'phc_vaccinator', 'not_scheduled', 'local-stub', 'none',
  false, false, '{}'::jsonb,
  '{"summary":{"owner":"PHC"},"source_and_rule":{"source_backed":true},"execution":{},"stock":{},"proof":{},"verification":{},"notification_channels":["local-stub"],"notification_policy":{},"links":{}}'::jsonb
)
ON CONFLICT (tenant_id, event_id) DO NOTHING`, testTenantID, eventID); err != nil {
		t.Fatalf("seed calendar event: %v", err)
	}
}

func seedNotification(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID, key, status string, attempts int, nextAttemptAt *time.Time) string {
	t.Helper()
	var requestID string
	if err := pool.QueryRow(ctx, `
INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, target_id, notification_type, channel,
  title, body, status, idempotency_key, request_fingerprint, context,
  delivery_attempts, next_attempt_at
) VALUES (
  $1::uuid, $2, 'cohort', NULL, 'reminder', 'local-stub',
  'Notification repo test', 'Notification repo body', $3, $4, $5, '{}'::jsonb,
  $6, $7::timestamptz
)
RETURNING notification_request_id::text`,
		testTenantID, eventID, status, key, key+":fingerprint", attempts, nextAttemptAt).Scan(&requestID); err != nil {
		t.Fatalf("seed notification: %v", err)
	}
	return requestID
}

func assertNotificationState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, requestID, wantStatus string, wantAttempts int, wantFailure bool) {
	t.Helper()
	var status string
	var attempts int
	var leaseCleared bool
	var hasFailure bool
	if err := pool.QueryRow(ctx, `
SELECT status, delivery_attempts, lease_token IS NULL, COALESCE(failure_reason, '') <> ''
FROM notification_requests
WHERE tenant_id = $1::uuid AND notification_request_id = $2::uuid`,
		testTenantID, requestID).Scan(&status, &attempts, &leaseCleared, &hasFailure); err != nil {
		t.Fatalf("query notification state: %v", err)
	}
	if status != wantStatus || attempts != wantAttempts || !leaseCleared || hasFailure != wantFailure {
		t.Fatalf("notification state status=%s attempts=%d leaseCleared=%t hasFailure=%t, want %s/%d/true/%t",
			status, attempts, leaseCleared, hasFailure, wantStatus, wantAttempts, wantFailure)
	}
}
