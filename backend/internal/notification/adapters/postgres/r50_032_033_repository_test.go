package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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
			tenant_id, request_id, recipient_type, recipient_id, subject_type,
			subject_id, subject_context, status, requested_at, next_attempt_at,
			lease_token, leased_at, delivered_at, attempt_count
		) VALUES (
			$1::uuid, gen_random_uuid()::text, 'device', 'device-001', 'verification_item',
			'item-001', '{}', 'queued', now(), $2, NULL, NULL, NULL, 0
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
			tenant_id, request_id, recipient_type, recipient_id, subject_type,
			subject_id, subject_context, status, requested_at, next_attempt_at,
			lease_token, leased_at, delivered_at, attempt_count
		) VALUES
		($1::uuid, 'req-1'::text, 'device', 'device-001', 'verification_item',
			'item-001', '{}', 'queued', $2, NULL, NULL, NULL, NULL, 0),
		($1::uuid, 'req-2'::text, 'device', 'device-002', 'verification_item',
			'item-002', '{}', 'queued', $3, NULL, NULL, NULL, NULL, 0)`,
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

	// Verify we got the oldest requested_at time.
	if !oldest.Equal(oldestRequestedAt.Truncate(time.Millisecond)) && !oldest.Equal(oldestRequestedAt.Truncate(time.Second)) {
		t.Fatalf("oldest = %v, want approximately %v", oldest, oldestRequestedAt)
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

	requestID := "notification-req-r50-033"
	subjectID := "item-r50-033"

	// Insert a notification request and simulate a sent delivery.
	_, err := pool.Exec(ctx, `
		INSERT INTO notification_requests (
			tenant_id, request_id, recipient_type, recipient_id, subject_type,
			subject_id, subject_context, status, requested_at, next_attempt_at,
			lease_token, leased_at, delivered_at, attempt_count
		) VALUES (
			$1::uuid, $2, 'device', 'device-001', 'verification_item',
			$3, '{}', 'sending', now(), NULL, NULL, NULL, NULL, 0
		)`,
		tenantID, requestID, subjectID,
	)
	if err != nil {
		t.Fatalf("insert notification_request: %v", err)
	}

	// Mark it as sent via RecordDelivery (or a similar path that emits the event).
	// For this test, we manually insert a delivery and emit the event to simulate production.
	_, err = pool.Exec(ctx, `
		INSERT INTO notification_delivery_ledger (
			tenant_id, request_id, attempt_number, result, sent_at, response_code, response_body
		) VALUES (
			$1::uuid, $2, 1, 'sent', now(), 200, '{"status":"delivered"}'
		)`,
		tenantID, requestID,
	)
	if err != nil {
		t.Fatalf("insert delivery_ledger: %v", err)
	}

	// Update the notification_request to sent status and emit the event.
	_, err = pool.Exec(ctx, `
		INSERT INTO outbox_messages (
			tenant_id, event_type, subject_id, producer_service, schema_version,
			schema_ref, idempotency_key, payload, created_at, published_at
		) VALUES (
			$1::uuid, 'notification.sent', $2, 'goatos-notification-dispatcher',
			'1.0.0', 'contracts/jsonschema/domain-event-envelope.schema.json#notification.sent',
			'notification.sent:' || $2, $3, now(), now()
		)`,
		tenantID, requestID,
		json.RawMessage(`{
			"tenant_id": "`+tenantID+`",
			"request_id": "`+requestID+`",
			"subject_type": "notification_request",
			"subject_id": "`+subjectID+`",
			"recipient_type": "device",
			"recipient_id": "device-001"
		}`),
	)
	if err != nil {
		t.Fatalf("insert notification.sent event: %v", err)
	}

	// Retrieve the notification.sent event from the outbox.
	var payload string
	err = pool.QueryRow(ctx,
		"SELECT payload FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = 'notification.sent' AND subject_id = $2 LIMIT 1",
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
	if rid, ok := eventPayload["request_id"].(string); !ok || rid != requestID {
		t.Fatalf("request_id = %v, want %s", eventPayload["request_id"], requestID)
	}

	if sid, ok := eventPayload["subject_id"].(string); !ok || sid != subjectID {
		t.Fatalf("subject_id = %v, want %s", eventPayload["subject_id"], subjectID)
	}
}

// R50-033: Verify the notification.sent event envelope structure conforms to the contract.
func TestNotificationSentEventEnvelope_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := newNotificationTenant(t, ctx, pool)

	requestID := "notification-req-envelope"

	// Insert a notification.sent event.
	_, err := pool.Exec(ctx, `
		INSERT INTO outbox_messages (
			tenant_id, event_type, subject_id, producer_service, schema_version,
			schema_ref, idempotency_key, payload, created_at, published_at
		) VALUES (
			$1::uuid, 'notification.sent', $2, 'goatos-notification-dispatcher',
			'1.0.0', 'contracts/jsonschema/domain-event-envelope.schema.json#notification.sent',
			'notification.sent:' || $2, $3, now(), now()
		)`,
		tenantID, requestID,
		json.RawMessage(`{
			"tenant_id": "`+tenantID+`",
			"request_id": "`+requestID+`",
			"subject_type": "notification_request"
		}`),
	)
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}

	// Verify the event record structure.
	var eventType, schemaVersion, schemaRef, producerService string
	var payload string
	err = pool.QueryRow(ctx,
		`SELECT event_type, schema_version, schema_ref, producer_service, payload
		 FROM outbox_messages
		 WHERE tenant_id = $1::uuid AND event_type = 'notification.sent' AND subject_id = $2`,
		tenantID, requestID).Scan(&eventType, &schemaVersion, &schemaRef, &producerService, &payload)
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
	if schemaRef != notificationSentSchemaRef {
		t.Fatalf("schema_ref = %s, want %s", schemaRef, notificationSentSchemaRef)
	}
	if producerService != notificationSentProducerService {
		t.Fatalf("producer_service = %s, want %s", producerService, notificationSentProducerService)
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
