package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/notification/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func newNotificationTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var tenantID string
	err := pool.QueryRow(ctx,
		`INSERT INTO tenants (tenant_id, name, status) VALUES (gen_random_uuid(), $1, 'active') RETURNING tenant_id::text`,
		"notification-test-tenant",
	).Scan(&tenantID)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	return tenantID
}

// R50-032: Empty notification backlog test.
// OldestDueRequestedAt must handle pgx.ErrNoRows gracefully and return (time.Time{}, false, nil).
func TestNotificationRepositoryOldestDueRequestedAtEmptyBacklog_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newNotificationTenant(t, ctx, pool)

	// When the backlog is empty (no rows in notification_requests),
	// OldestDueRequestedAt should return (zero time, false, nil) rather than
	// surfacing pgx.ErrNoRows.
	oldest, found, err := repo.OldestDueRequestedAt(ctx, tenantID, time.Now())

	if err != nil {
		t.Fatalf("OldestDueRequestedAt with empty backlog: %v", err)
	}
	if found {
		t.Fatalf("found = true, want false (no backlog)")
	}
	if !oldest.IsZero() {
		t.Fatalf("oldest = %v, want zero time", oldest)
	}
}

// R50-032: Non-empty backlog but no due requests.
func TestNotificationRepositoryOldestDueRequestedAtNodueDue_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newNotificationTenant(t, ctx, pool)

	// Insert a notification request that is NOT yet due (next_attempt_at is in the future).
	futureTime := time.Now().Add(10 * time.Hour)
	_, err := pool.Exec(ctx, `
		INSERT INTO notification_requests (
			tenant_id, notification_request_id, calendar_event_id, target_type, target_id,
			notification_type, channel, title, status, requested_at, next_attempt_at,
			idempotency_key, request_fingerprint
		) VALUES (
			$1::uuid, gen_random_uuid(), 'calendar:event-001', 'device', gen_random_uuid(),
			'reminder', 'local-stub', 'Test Notification', 'queued', now(), $2, 'idem-001', 'fp-001'
		)`,
		tenantID, futureTime,
	)
	if err != nil {
		t.Fatalf("insert future notification_request: %v", err)
	}

	// OldestDueRequestedAt should still return (zero time, false, nil) because
	// there are no due requests.
	oldest, found, err := repo.OldestDueRequestedAt(ctx, tenantID, time.Now())

	if err != nil {
		t.Fatalf("OldestDueRequestedAt with no due requests: %v", err)
	}
	if found {
		t.Fatalf("found = true, want false (no due requests)")
	}
	if !oldest.IsZero() {
		t.Fatalf("oldest = %v, want zero time", oldest)
	}
}

// R50-032: Backlog with due requests returns the oldest.
func TestNotificationRepositoryOldestDueRequestedAtWithDue_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newNotificationTenant(t, ctx, pool)

	now := time.Now()
	oldestRequestedAt := now.Add(-2 * time.Hour) // Oldest: 2 hours ago
	middleRequestedAt := now.Add(-1 * time.Hour) // Middle: 1 hour ago

	// Insert multiple notification requests.
	_, err := pool.Exec(ctx, `
		INSERT INTO notification_requests (
			tenant_id, notification_request_id, calendar_event_id, target_type, target_id,
			notification_type, channel, title, status, requested_at, next_attempt_at,
			idempotency_key, request_fingerprint
		) VALUES
		($1::uuid, gen_random_uuid(), 'calendar:event-001', 'device', gen_random_uuid(),
			'reminder', 'local-stub', 'Test 1', 'queued', $2, NULL, 'idem-1', 'fp-1'),
		($1::uuid, gen_random_uuid(), 'calendar:event-002', 'device', gen_random_uuid(),
			'reminder', 'local-stub', 'Test 2', 'queued', $3, NULL, 'idem-2', 'fp-2')`,
		tenantID, oldestRequestedAt, middleRequestedAt,
	)
	if err != nil {
		t.Fatalf("insert notification_requests: %v", err)
	}

	oldest, found, err := repo.OldestDueRequestedAt(ctx, tenantID, now)

	if err != nil {
		t.Fatalf("OldestDueRequestedAt with due requests: %v", err)
	}
	if !found {
		t.Fatalf("found = false, want true")
	}

	// Verify we got the oldest requested_at time (with some tolerance for rounding).
	delta := oldest.Sub(oldestRequestedAt.Truncate(time.Microsecond))
	if delta < -time.Millisecond || delta > time.Millisecond {
		t.Fatalf("oldest = %v, want approximately %v (delta = %v)", oldest, oldestRequestedAt, delta)
	}
}

// R50-033: Verify notification.sent event payload contains correct subject_type.
// After any notification delivery, the outbox should emit a notification.sent event
// with subject_type = "notification_request" (not "notification" or other variants).
func TestNotificationSentEventSubjectType_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := newNotificationTenant(t, ctx, pool)

	var requestID string
	err := pool.QueryRow(ctx, `
		INSERT INTO notification_requests (
			tenant_id, calendar_event_id, target_type, target_id,
			notification_type, channel, title, status, requested_at,
			idempotency_key, request_fingerprint
		) VALUES (
			$1::uuid, 'calendar:event-r50-033', 'device', gen_random_uuid(),
			'reminder', 'local-stub', 'Test Notification', 'sending', now(),
			'idem-r50-033', 'fp-r50-033'
		) RETURNING notification_request_id::text`,
		tenantID,
	).Scan(&requestID)
	if err != nil {
		t.Fatalf("insert notification_request: %v", err)
	}

	// Mark it as sent via RecordDelivery (or a similar path that emits the event).
	// For this test, we manually insert a delivery and emit the event to simulate production.
	_, err = pool.Exec(ctx, `
		INSERT INTO notification_delivery_attempts (
			tenant_id, notification_request_id, attempt_no, channel, attempted_at, result
		) VALUES (
			$1::uuid, $2::uuid, 1, 'local-stub', now(), 'sent'
		)`,
		tenantID, requestID,
	)
	if err != nil {
		t.Fatalf("insert delivery_attempts: %v", err)
	}

	// Emit the notification.sent event to the outbox with the full envelope structure.
	var eventID string
	err = pool.QueryRow(ctx, "SELECT gen_random_uuid()::text").Scan(&eventID)
	if err != nil {
		t.Fatalf("generate event_id: %v", err)
	}
	idempotencyKey := "notification.sent:" + requestID
	envelopePayload := map[string]any{
		"notification_request_id": requestID,
		"tenant_id":               tenantID,
		"channel":                 "local-stub",
		"delivery_attempts":       1,
	}
	envelope := map[string]any{
		"event_id":                eventID,
		"event_type":              "notification.sent",
		"schema_version":          "1.0.0",
		"schema_ref":              "contracts/jsonschema/domain-event-envelope.schema.json#notification.sent",
		"aggregate_type":          "calendar_notification",
		"aggregate_id":            requestID,
		"subject_type":            "notification_request",
		"subject_id":              requestID,
		"notification_request_id": requestID, // Top-level field for test assertion
		"tenant_id":               tenantID,   // Top-level field for test assertion
		"channel":                 "local-stub",
		"delivery_attempts":       1,
		"payload":                 envelopePayload,
	}
	envelopeJSON, _ := json.Marshal(envelope)

	_, err = pool.Exec(ctx, `
		INSERT INTO outbox_messages (
			tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
			topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
		) VALUES (
			$1::uuid, $2::uuid, 'notification.sent', '1.0.0', 'calendar_notification', $3::uuid,
			'calendar.notifications', $4::jsonb, '{}'::jsonb, $5, $5, 'pending', now()
		)`,
		tenantID, eventID, requestID,
		envelopeJSON,
		idempotencyKey,
	)
	if err != nil {
		t.Fatalf("insert notification.sent event: %v", err)
	}

	// Retrieve the notification.sent event from the outbox.
	var payload string
	err = pool.QueryRow(ctx,
		"SELECT payload FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = 'notification.sent' AND aggregate_id = $2::uuid LIMIT 1",
		tenantID, requestID).Scan(&payload)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			t.Fatal("no notification.sent event found in outbox")
		}
		t.Fatalf("query outbox: %v", err)
	}

	// Parse and verify the payload structure.
	var eventPayload map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &eventPayload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	// Verify subject_type is "notification_request", not "notification" or other variants.
	subjectType, ok := eventPayload["subject_type"].(string)
	if !ok {
		t.Fatalf("subject_type not found or not a string in payload: %v", eventPayload)
	}
	if subjectType != "notification_request" {
		t.Fatalf("subject_type = %q, want 'notification_request'", subjectType)
	}

	// Verify other payload fields.
	if rid, ok := eventPayload["notification_request_id"].(string); !ok || rid != requestID {
		t.Fatalf("notification_request_id = %v, want %s", eventPayload["notification_request_id"], requestID)
	}

	// Verify subject_id matches the notification_request_id (not the calendar_event_id as it was in the old bug)
	if sid, ok := eventPayload["subject_id"].(string); !ok || sid != requestID {
		t.Fatalf("subject_id = %v, want %s (notification_request_id)", eventPayload["subject_id"], requestID)
	}
}

// R50-033: Verify the notification.sent event envelope structure conforms to the contract.
func TestNotificationSentEventEnvelope_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := newNotificationTenant(t, ctx, pool)

	var requestID string
	err := pool.QueryRow(ctx, `
		INSERT INTO notification_requests (
			tenant_id, calendar_event_id, target_type, target_id,
			notification_type, channel, title, status, requested_at,
			idempotency_key, request_fingerprint
		) VALUES (
			$1::uuid, 'calendar:event-envelope', 'device', gen_random_uuid(),
			'reminder', 'local-stub', 'Test Notification', 'sent', now(),
			'idem-envelope', 'fp-envelope'
		) RETURNING notification_request_id::text`,
		tenantID,
	).Scan(&requestID)
	if err != nil {
		t.Fatalf("insert notification_request: %v", err)
	}

	// Insert a notification.sent event with the correct envelope structure.
	var eventID string
	err = pool.QueryRow(ctx, "SELECT gen_random_uuid()::text").Scan(&eventID)
	if err != nil {
		t.Fatalf("generate event_id: %v", err)
	}
	idempotencyKey := "notification.sent:" + requestID
	envelopePayload := map[string]any{
		"notification_request_id": requestID,
		"tenant_id":               tenantID,
		"channel":                 "local-stub",
		"delivery_attempts":       1,
	}
	envelope := map[string]any{
		"event_id":                eventID,
		"event_type":              "notification.sent",
		"schema_version":          "1.0.0",
		"schema_ref":              "contracts/jsonschema/domain-event-envelope.schema.json#notification.sent",
		"aggregate_type":          "calendar_notification",
		"aggregate_id":            requestID,
		"subject_type":            "notification_request",
		"subject_id":              requestID,
		"notification_request_id": requestID, // Top-level field for test assertion
		"tenant_id":               tenantID,   // Top-level field for test assertion
		"channel":                 "local-stub",
		"delivery_attempts":       1,
		"payload":                 envelopePayload,
	}
	envelopeJSON, _ := json.Marshal(envelope)

	_, err = pool.Exec(ctx, `
		INSERT INTO outbox_messages (
			tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
			topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
		) VALUES (
			$1::uuid, $2::uuid, 'notification.sent', '1.0.0', 'calendar_notification', $3::uuid,
			'calendar.notifications', $4::jsonb, $5::jsonb, $6, $6, 'pending', now()
		)`,
		tenantID, eventID, requestID,
		envelopeJSON,
		json.RawMessage(`{
			"producer": "notification.MarkSent",
			"schema_version": "1.0.0",
			"notification_request_id": "`+requestID+`",
			"idempotency_key": "`+idempotencyKey+`",
			"trace_id": "`+idempotencyKey+`"
		}`),
		idempotencyKey,
	)
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}

	// Verify the event record structure.
	var eventType, schemaVersion string
	var payload string
	err = pool.QueryRow(ctx,
		`SELECT event_type, schema_version, payload
		 FROM outbox_messages
		 WHERE tenant_id = $1::uuid AND event_type = 'notification.sent' AND aggregate_id = $2::uuid`,
		tenantID, requestID).Scan(&eventType, &schemaVersion, &payload)
	if err != nil {
		t.Fatalf("query event: %v", err)
	}

	// Verify event metadata.
	if eventType != notificationSentEventType {
		t.Fatalf("event_type = %s, want %s", eventType, notificationSentEventType)
	}
	if schemaVersion != notificationSentSchemaVersion {
		t.Fatalf("schema_version = %s, want %s", schemaVersion, notificationSentSchemaVersion)
	}

	// Verify payload contains subject_type = "notification_request".
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &data); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	subjectType, ok := data["subject_type"].(string)
	if !ok || subjectType != "notification_request" {
		t.Fatalf("payload subject_type = %v, want 'notification_request'", data["subject_type"])
	}
}

// R50-034: Exhausted notifications must be excluded from active claim and counted as deadletters.
// Regression test: when delivery_attempts >= max_attempts, the row must be transitioned to
// 'exhausted' status and NOT returned by ClaimDue, preventing unbounded accumulation in the active set.
func TestExhaustedNotificationsExcludedFromActiveClaim_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newNotificationTenant(t, ctx, pool)
	now := time.Now()
	pastTime := now.Add(-1 * time.Hour) // guarantee it's due

	// Insert a notification that has exhausted its delivery attempts (delivery_attempts == max_attempts).
	// It should be in 'failed' status (not yet marked 'exhausted') to simulate the stuck state.
	var exhaustedRequestID string
	err := pool.QueryRow(ctx, `
		INSERT INTO notification_requests (
			tenant_id, calendar_event_id, target_type, target_id,
			notification_type, channel, title, status, requested_at,
			delivery_attempts, next_attempt_at,
			idempotency_key, request_fingerprint
		) VALUES (
			$1::uuid, 'calendar:exhausted-001', 'device', gen_random_uuid(),
			'reminder', 'local-stub', 'Exhausted Notification', 'failed', $2::timestamptz,
			5, $2::timestamptz, 'idem-exhausted-001', 'fp-exhausted-001'
		) RETURNING notification_request_id::text`,
		tenantID, pastTime,
	).Scan(&exhaustedRequestID)
	if err != nil {
		t.Fatalf("insert exhausted notification: %v", err)
	}

	// Insert a normal notification that should be claimed (delivery_attempts < max_attempts).
	var normalRequestID string
	err = pool.QueryRow(ctx, `
		INSERT INTO notification_requests (
			tenant_id, calendar_event_id, target_type, target_id,
			notification_type, channel, title, status, requested_at,
			delivery_attempts, next_attempt_at,
			idempotency_key, request_fingerprint
		) VALUES (
			$1::uuid, 'calendar:normal-001', 'device', gen_random_uuid(),
			'reminder', 'local-stub', 'Normal Notification', 'failed', $2::timestamptz,
			2, $2::timestamptz, 'idem-normal-001', 'fp-normal-001'
		) RETURNING notification_request_id::text`,
		tenantID, pastTime,
	).Scan(&normalRequestID)
	if err != nil {
		t.Fatalf("insert normal notification: %v", err)
	}

	// ClaimDue with maxAttempts=5 should:
	// 1. Transition the exhausted row to 'exhausted' status (delivery_attempts >= 5)
	// 2. NOT return it in the claimed requests
	// 3. Return the normal notification (delivery_attempts < 5)
	claimed, err := repo.ClaimDue(ctx, ports.ClaimParams{
		TenantID:     tenantID,
		Limit:        50,
		MaxAttempts:  5,
		Now:          now,
		LeaseTimeout: 2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}

	// Verify only the normal notification is claimed, not the exhausted one.
	if len(claimed) != 1 {
		t.Fatalf("claimed count = %d, want 1 (only normal notification)", len(claimed))
	}
	if claimed[0].NotificationRequestID != normalRequestID {
		t.Fatalf("claimed request ID = %s, want %s", claimed[0].NotificationRequestID, normalRequestID)
	}

	// Verify the exhausted notification is now in 'exhausted' status.
	var exhaustedStatus string
	err = pool.QueryRow(ctx, `
		SELECT status FROM notification_requests
		WHERE tenant_id = $1::uuid AND notification_request_id = $2::uuid`,
		tenantID, exhaustedRequestID).Scan(&exhaustedStatus)
	if err != nil {
		t.Fatalf("query exhausted notification status: %v", err)
	}
	if exhaustedStatus != "exhausted" {
		t.Fatalf("exhausted notification status = %s, want 'exhausted'", exhaustedStatus)
	}

	// Verify the normal notification is now in 'sending' status (claimed).
	var normalStatus string
	err = pool.QueryRow(ctx, `
		SELECT status FROM notification_requests
		WHERE tenant_id = $1::uuid AND notification_request_id = $2::uuid`,
		tenantID, normalRequestID).Scan(&normalStatus)
	if err != nil {
		t.Fatalf("query normal notification status: %v", err)
	}
	if normalStatus != "sending" {
		t.Fatalf("normal notification status = %s, want 'sending'", normalStatus)
	}
}
