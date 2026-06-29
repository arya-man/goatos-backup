// Package postgres implements durable notification request persistence.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/notification/domain"
	"github.com/vgoats/goatos/backend/internal/notification/ports"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
)

const (
	defaultQueryTimeout                  = 3 * time.Second
	notificationExhaustedEventType       = "notification.exhausted"
	notificationExhaustedSchemaVersion   = "1.0.0"
	notificationExhaustedSchemaRef       = "contracts/jsonschema/domain-event-envelope.schema.json#notification.exhausted"
	notificationExhaustedTopic           = "calendar.notifications"
	notificationExhaustedProducerService = "goatos-notification-dispatcher"
)

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

var _ ports.Repository = (*Repository)(nil)

func (r *Repository) ReclaimStaleSending(ctx context.Context, tenantID string, now time.Time, leaseTimeout time.Duration) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if leaseTimeout <= 0 {
		leaseTimeout = 2 * time.Minute
	}
	cutoff := now.Add(-leaseTimeout)
	tag, err := r.pool.Exec(ctx, `
UPDATE notification_requests
SET status = 'queued',
    lease_token = NULL,
    leased_at = NULL,
    next_attempt_at = $2::timestamptz,
    failure_reason = COALESCE(NULLIF(failure_reason, ''), 'notification_delivery_lease_expired'),
    updated_at = $2::timestamptz
WHERE tenant_id = $1::uuid
  AND status = 'sending'
  AND (leased_at IS NULL OR leased_at <= $3::timestamptz)`, tenantID, now, cutoff)
	if err != nil {
		return 0, fmt.Errorf("notification: reclaim stale sending: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (r *Repository) ClaimDue(ctx context.Context, params ports.ClaimParams) ([]domain.Request, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.MaxAttempts <= 0 {
		params.MaxAttempts = 5
	}
	rows, err := r.pool.Query(ctx, `
WITH candidates AS (
  SELECT notification_request_id
  FROM notification_requests
  WHERE tenant_id = $1::uuid
    AND status IN ('queued', 'failed')
    AND (next_attempt_at IS NULL OR next_attempt_at <= $2::timestamptz)
    AND delivery_attempts < $4
  ORDER BY COALESCE(next_attempt_at, requested_at), notification_request_id
  LIMIT $3
  FOR UPDATE SKIP LOCKED
),
claimed AS (
  UPDATE notification_requests nr
  SET status = 'sending',
      lease_token = gen_random_uuid(),
      leased_at = $2::timestamptz,
      delivery_attempts = delivery_attempts + 1,
      updated_at = $2::timestamptz
  FROM candidates c
  WHERE nr.notification_request_id = c.notification_request_id
  RETURNING
    nr.notification_request_id::text,
    nr.tenant_id::text,
    nr.calendar_event_id,
    nr.target_type,
    COALESCE(nr.target_id::text, ''),
    nr.notification_type,
    nr.channel,
    COALESCE(nr.recipient_ref, ''),
    nr.title,
    nr.body,
    nr.status,
    COALESCE(nr.trace_id, ''),
    nr.context,
    nr.delivery_attempts,
    nr.lease_token::text,
    nr.requested_at
)
SELECT *
FROM claimed
ORDER BY requested_at, notification_request_id`, params.TenantID, params.Now, params.Limit, params.MaxAttempts)
	if err != nil {
		return nil, fmt.Errorf("notification: claim due: %w", err)
	}
	defer rows.Close()
	var requests []domain.Request
	for rows.Next() {
		var request domain.Request
		if err := rows.Scan(
			&request.NotificationRequestID,
			&request.TenantID,
			&request.CalendarEventID,
			&request.TargetType,
			&request.TargetID,
			&request.NotificationType,
			&request.Channel,
			&request.RecipientRef,
			&request.Title,
			&request.Body,
			&request.Status,
			&request.TraceID,
			&request.Context,
			&request.DeliveryAttempts,
			&request.LeaseToken,
			&request.RequestedAt,
		); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return requests, nil
}

func (r *Repository) MarkSent(ctx context.Context, tenantID, notificationRequestID, leaseToken, deliveredBy string, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tag, err := r.pool.Exec(ctx, `
UPDATE notification_requests
SET status = 'sent',
    sent_at = $4::timestamptz,
    failure_reason = NULL,
    next_attempt_at = NULL,
    lease_token = NULL,
    leased_at = NULL,
    delivered_by = NULLIF($5, ''),
    updated_at = $4::timestamptz
WHERE tenant_id = $1::uuid
  AND notification_request_id = $2::uuid
  AND lease_token = $3::uuid
  AND status = 'sending'`, tenantID, notificationRequestID, leaseToken, now, deliveredBy)
	if err != nil {
		return fmt.Errorf("notification: mark sent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("notification: mark sent claim missing")
	}
	return nil
}

func (r *Repository) MarkFailed(ctx context.Context, tenantID, notificationRequestID, leaseToken, deliveredBy, failureReason string, nextAttemptAt *time.Time, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var next pgtype.Timestamptz
	status := "exhausted"
	if nextAttemptAt != nil {
		next = pgtype.Timestamptz{Time: nextAttemptAt.UTC(), Valid: true}
		status = "failed"
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var row failedNotificationRow
	err = tx.QueryRow(ctx, `
UPDATE notification_requests
SET status = $8,
    failure_reason = $4,
    next_attempt_at = $5::timestamptz,
    lease_token = NULL,
    leased_at = NULL,
    delivered_by = NULLIF($6, ''),
    updated_at = $7::timestamptz
WHERE tenant_id = $1::uuid
  AND notification_request_id = $2::uuid
  AND lease_token = $3::uuid
  AND status = 'sending'
RETURNING
  calendar_event_id,
  target_type,
  COALESCE(target_id::text, ''),
  notification_type,
  channel,
  title,
  body,
  delivery_attempts,
  COALESCE(trace_id, ''),
  context`, tenantID, notificationRequestID, leaseToken, failureReason, next, deliveredBy, now, status).Scan(
		&row.CalendarEventID,
		&row.TargetType,
		&row.TargetID,
		&row.NotificationType,
		&row.Channel,
		&row.Title,
		&row.Body,
		&row.DeliveryAttempts,
		&row.TraceID,
		&row.Context,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("notification: mark failed claim missing")
		}
		return fmt.Errorf("notification: mark failed: %w", err)
	}
	if status == "exhausted" {
		if err := insertNotificationExhaustedEvidence(ctx, tx, tenantID, notificationRequestID, deliveredBy, failureReason, now, row); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

type failedNotificationRow struct {
	CalendarEventID  string
	TargetType       string
	TargetID         string
	NotificationType string
	Channel          string
	Title            string
	Body             string
	DeliveryAttempts int
	TraceID          string
	Context          []byte
}

func insertNotificationExhaustedEvidence(ctx context.Context, tx pgx.Tx, tenantID, notificationRequestID, deliveredBy, failureReason string, now time.Time, row failedNotificationRow) error {
	idempotencyKey := notificationExhaustedEventType + ":" + notificationRequestID
	eventID := platformoutbox.DeterministicUUID(notificationExhaustedEventType + ":" + tenantID + ":" + notificationRequestID)
	traceID := strings.TrimSpace(row.TraceID)
	if traceID == "" {
		traceID = idempotencyKey
	}
	recorded := now.UTC().Format(time.RFC3339Nano)
	payload := map[string]any{
		"tenant_id":               tenantID,
		"notification_request_id": notificationRequestID,
		"calendar_event_id":       row.CalendarEventID,
		"notification_type":       row.NotificationType,
		"channel":                 row.Channel,
		"status":                  domain.StatusExhausted,
		"failure_reason":          failureReason,
		"delivery_attempts":       row.DeliveryAttempts,
	}
	if row.TargetType != "" {
		payload["target_type"] = row.TargetType
	}
	if row.TargetID != "" {
		payload["target_id"] = row.TargetID
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     notificationExhaustedEventType,
		"schema_version": notificationExhaustedSchemaVersion,
		"schema_ref":     notificationExhaustedSchemaRef,
		"aggregate_type": "calendar_notification",
		"aggregate_id":   notificationRequestID,
		"occurred_at":    recorded,
		"recorded_at":    recorded,
		"producer": map[string]any{
			"service": notificationExhaustedProducerService,
			"module":  "notification",
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": "system_rule",
			"actor_id":   nil,
			"actor_ref":  nil,
		},
		"subject_type": "calendar_event",
		"subject_id":   row.CalendarEventID,
		"visibility_scope": map[string]any{
			"tenant_id": tenantID,
		},
		"evidence_refs": []map[string]string{{
			"evidence_type": "notification_request",
			"evidence_id":   notificationRequestID + ":exhausted",
		}},
		"payload":  payload,
		"trace_id": traceID,
	})
	if err != nil {
		return fmt.Errorf("notification: exhausted envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":                "notification.MarkFailed",
		"schema_version":          notificationExhaustedSchemaVersion,
		"notification_request_id": notificationRequestID,
		"idempotency_key":         idempotencyKey,
		"trace_id":                traceID,
	})
	if err != nil {
		return fmt.Errorf("notification: exhausted headers: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, 'calendar_notification', $5::uuid,
  $6, $7::jsonb, $8::jsonb, $9, $10, 'pending', now()
)
ON CONFLICT (event_id) DO NOTHING`,
		tenantID, eventID, notificationExhaustedEventType, notificationExhaustedSchemaVersion,
		notificationRequestID, notificationExhaustedTopic, envelope, headers, idempotencyKey, traceID)
	if err != nil {
		return fmt.Errorf("notification: exhausted outbox: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"domain":                  "calendar",
		"module":                  "notification",
		"category":                "notification_delivery",
		"calendar_event_id":       row.CalendarEventID,
		"notification_request_id": notificationRequestID,
		"notification_type":       row.NotificationType,
		"channel":                 row.Channel,
		"status":                  domain.StatusExhausted,
		"result":                  domain.StatusExhausted,
		"delivery_attempts":       row.DeliveryAttempts,
		"delivered_by":            deliveredBy,
		"trace_id":                traceID,
	})
	if err != nil {
		return fmt.Errorf("notification: exhausted audit metadata: %w", err)
	}
	afterState, err := json.Marshal(map[string]any{
		"status":            domain.StatusExhausted,
		"failure_reason":    failureReason,
		"delivery_attempts": row.DeliveryAttempts,
	})
	if err != nil {
		return fmt.Errorf("notification: exhausted audit after_state: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO audit_log (
  tenant_id, actor_type, action, resource_type, resource_id, scope_type,
  scope_id, after_state, metadata, trace_id, recorded_at
) VALUES (
  $1::uuid, 'system', $2, 'calendar_notification', $3::uuid, 'notification_request',
  $3::uuid, $4::jsonb, $5::jsonb, $6, $7::timestamptz
)`,
		tenantID,
		notificationExhaustedEventType,
		notificationRequestID,
		afterState,
		metadata,
		traceID,
		now,
	)
	if err != nil {
		return fmt.Errorf("notification: exhausted audit: %w", err)
	}
	return nil
}
