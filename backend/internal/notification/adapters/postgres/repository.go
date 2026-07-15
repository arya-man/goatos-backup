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

	notificationSentEventType       = "notification.sent"
	notificationSentSchemaVersion   = "1.0.0"
	notificationSentSchemaRef       = "contracts/jsonschema/domain-event-envelope.schema.json#notification.sent"
	notificationSentTopic           = "calendar.notifications"
	notificationSentProducerService = "goatos-notification-dispatcher"

	deliveryAttemptResultSent   = "sent"
	deliveryAttemptResultFailed = "failed"
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

// OldestDuePendingAge returns how long the oldest currently-due, still-undelivered
// notification request (status queued/failed with next_attempt_at null or past)
// has been waiting since it was requested. The aggregate always returns one row;
// an empty backlog yields a NULL, surfaced as (0, false, nil).
func (r *Repository) OldestDuePendingAge(ctx context.Context, tenantID string, now time.Time) (time.Duration, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var ageSeconds pgtype.Float8
	err := r.pool.QueryRow(ctx, `
SELECT EXTRACT(EPOCH FROM ($2::timestamptz - MIN(requested_at)))
FROM notification_requests
WHERE tenant_id = $1::uuid
  AND status IN ('queued', 'failed')
  AND (next_attempt_at IS NULL OR next_attempt_at <= $2::timestamptz)`, tenantID, now).Scan(&ageSeconds)
	if err != nil {
		return 0, false, fmt.Errorf("notification: oldest due pending age: %w", err)
	}
	if !ageSeconds.Valid {
		return 0, false, nil
	}
	secs := ageSeconds.Float64
	if secs < 0 {
		// Clock skew / a request timestamped slightly in the future: clamp to 0
		// rather than report a negative backlog age.
		secs = 0
	}
	return time.Duration(secs * float64(time.Second)), true, nil
}

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

// MarkSent transitions a claimed request to 'sent'. In the same transaction it
// (1) appends an immutable notification_delivery_attempts row for this attempt
// (attempt_no = the request's delivery_attempts at claim time) and (2) emits a
// durable notification.sent outbox event, so the status flip, the per-attempt
// ledger, and the durable success event are all atomic: never one without the
// others. MarkSent is lease-guarded (WHERE lease_token = $3 AND status =
// 'sending'), so a replay of an already-sent request affects zero rows, returns
// an error, and never re-inserts an attempt row or a second event.
func (r *Repository) MarkSent(ctx context.Context, tenantID, notificationRequestID, leaseToken, deliveredBy string, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var row sentNotificationRow
	err = tx.QueryRow(ctx, `
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
  AND status = 'sending'
RETURNING
  channel,
  delivery_attempts,
  COALESCE(trace_id, '')`, tenantID, notificationRequestID, leaseToken, now, deliveredBy).Scan(
		&row.Channel,
		&row.DeliveryAttempts,
		&row.TraceID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("notification: mark sent claim missing")
		}
		return fmt.Errorf("notification: mark sent: %w", err)
	}

	// provider_message_id is left NULL: ports.Gateway.Send returns only an error today, so no
	// channel adapter (webhook/slack/email/incident/push_fcm) surfaces a provider-assigned
	// message id back to the dispatcher. Threading it through would require widening the
	// Gateway interface (Send returning (providerMessageID string, err error)) and updating
	// every adapter implementation and both fakes in service_test.go / gateway_test.go -- out of
	// scope for the audit-ledger change. Documented here per the ledger design; the column is
	// nullable specifically to allow this.
	if err := insertDeliveryAttempt(ctx, tx, tenantID, notificationRequestID, row.DeliveryAttempts, row.Channel, deliveryAttemptResultSent, now, nil, nil); err != nil {
		return err
	}
	if err := insertNotificationSentEvidence(ctx, tx, tenantID, notificationRequestID, now, row); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

type sentNotificationRow struct {
	Channel          string
	DeliveryAttempts int
	TraceID          string
}

// MarkFailed transitions a claimed request to 'failed' (retryable, nextAttemptAt != nil) or
// 'exhausted' (permanent, nextAttemptAt == nil). Both branches append the attempt's
// notification_delivery_attempts row (result='failed', error=failureReason) inside the SAME
// transaction as the status update, so the ledger and the status flip are atomic. The exhausted
// branch additionally emits the existing durable notification.exhausted outbox event.
func (r *Repository) MarkFailed(ctx context.Context, tenantID, notificationRequestID, leaseToken, deliveredBy, failureReason string, nextAttemptAt *time.Time, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if nextAttemptAt != nil {
		next := pgtype.Timestamptz{Time: nextAttemptAt.UTC(), Valid: true}

		tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()

		var channel string
		var deliveryAttempts int
		err = tx.QueryRow(ctx, `
UPDATE notification_requests
SET status = 'failed',
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
RETURNING channel, delivery_attempts`, tenantID, notificationRequestID, leaseToken, failureReason, next, deliveredBy, now).Scan(&channel, &deliveryAttempts)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("notification: mark failed claim missing")
			}
			return fmt.Errorf("notification: mark failed: %w", err)
		}
		if err := insertDeliveryAttempt(ctx, tx, tenantID, notificationRequestID, deliveryAttempts, channel, deliveryAttemptResultFailed, now, &failureReason, nil); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return nil
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var row failedNotificationRow
	err = tx.QueryRow(ctx, `
UPDATE notification_requests
SET status = 'exhausted',
    failure_reason = $4,
    next_attempt_at = NULL,
    lease_token = NULL,
    leased_at = NULL,
    delivered_by = NULLIF($5, ''),
    updated_at = $6::timestamptz
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
  delivery_attempts,
  COALESCE(trace_id, '')`, tenantID, notificationRequestID, leaseToken, failureReason, deliveredBy, now).Scan(
		&row.CalendarEventID,
		&row.TargetType,
		&row.TargetID,
		&row.NotificationType,
		&row.Channel,
		&row.DeliveryAttempts,
		&row.TraceID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("notification: mark failed claim missing")
		}
		return fmt.Errorf("notification: mark failed: %w", err)
	}
	if err := insertDeliveryAttempt(ctx, tx, tenantID, notificationRequestID, row.DeliveryAttempts, row.Channel, deliveryAttemptResultFailed, now, &failureReason, nil); err != nil {
		return err
	}
	if err := insertNotificationExhaustedEvidence(ctx, tx, tenantID, notificationRequestID, deliveredBy, failureReason, now, row); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}

// insertDeliveryAttempt appends one immutable row to the per-attempt delivery ledger. attemptNo
// is the request's delivery_attempts value at the moment of this attempt (set by ClaimDue's
// `delivery_attempts = delivery_attempts + 1`), so it is stable and monotonic per request. The
// ON CONFLICT DO NOTHING on (tenant_id, notification_request_id, attempt_no) is defense in depth:
// MarkSent/MarkFailed are already lease + status guarded so a genuine duplicate call is rejected
// before reaching this insert, but the guard keeps the ledger append-only-safe even so.
func insertDeliveryAttempt(ctx context.Context, tx pgx.Tx, tenantID, notificationRequestID string, attemptNo int, channel, result string, attemptedAt time.Time, errText, providerMessageID *string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO notification_delivery_attempts (
  tenant_id, notification_request_id, attempt_no, channel, attempted_at, result, provider_message_id, error
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5::timestamptz, $6, $7, $8
)
ON CONFLICT (tenant_id, notification_request_id, attempt_no) DO NOTHING`,
		tenantID, notificationRequestID, attemptNo, channel, attemptedAt, result, providerMessageID, errText)
	if err != nil {
		return fmt.Errorf("notification: insert delivery attempt: %w", err)
	}
	return nil
}

type failedNotificationRow struct {
	CalendarEventID  string
	TargetType       string
	TargetID         string
	NotificationType string
	Channel          string
	DeliveryAttempts int
	TraceID          string
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

// insertNotificationSentEvidence mirrors insertNotificationExhaustedEvidence, giving the durable
// event stream success symmetry with notification.exhausted (docs/decisions/vaccination-
// notification-rules.md audit section, gap 2: previously only failures emitted a durable event).
// The idempotency key is scoped to the notification_request_id (not the attempt), matching
// MarkSent's lease + status guard: a request can only ever transition to 'sent' once, so
// ON CONFLICT (event_id) DO NOTHING on the deterministic event id is defense in depth, not the
// primary guard -- the primary guard is that a replayed MarkSent call affects zero rows and never
// reaches this function at all.
func insertNotificationSentEvidence(ctx context.Context, tx pgx.Tx, tenantID, notificationRequestID string, now time.Time, row sentNotificationRow) error {
	idempotencyKey := notificationSentEventType + ":" + notificationRequestID
	eventID := platformoutbox.DeterministicUUID(notificationSentEventType + ":" + tenantID + ":" + notificationRequestID)
	traceID := strings.TrimSpace(row.TraceID)
	if traceID == "" {
		traceID = idempotencyKey
	}
	recorded := now.UTC().Format(time.RFC3339Nano)
	payload := map[string]any{
		"notification_request_id": notificationRequestID,
		"tenant_id":               tenantID,
		"channel":                 row.Channel,
		"delivery_attempts":       row.DeliveryAttempts,
		"sent_at":                 recorded,
		"trace_id":                traceID,
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     notificationSentEventType,
		"schema_version": notificationSentSchemaVersion,
		"schema_ref":     notificationSentSchemaRef,
		"aggregate_type": "calendar_notification",
		"aggregate_id":   notificationRequestID,
		"occurred_at":    recorded,
		"recorded_at":    recorded,
		"producer": map[string]any{
			"service": notificationSentProducerService,
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
		"subject_id":   notificationRequestID,
		"visibility_scope": map[string]any{
			"tenant_id": tenantID,
		},
		"evidence_refs": []map[string]string{{
			"evidence_type": "notification_request",
			"evidence_id":   notificationRequestID + ":sent",
		}},
		"payload":  payload,
		"trace_id": traceID,
	})
	if err != nil {
		return fmt.Errorf("notification: sent envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":                "notification.MarkSent",
		"schema_version":          notificationSentSchemaVersion,
		"notification_request_id": notificationRequestID,
		"idempotency_key":         idempotencyKey,
		"trace_id":                traceID,
	})
	if err != nil {
		return fmt.Errorf("notification: sent headers: %w", err)
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
		tenantID, eventID, notificationSentEventType, notificationSentSchemaVersion,
		notificationRequestID, notificationSentTopic, envelope, headers, idempotencyKey, traceID)
	if err != nil {
		return fmt.Errorf("notification: sent outbox: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"domain":                  "calendar",
		"module":                  "notification",
		"category":                "notification_delivery",
		"notification_request_id": notificationRequestID,
		"channel":                 row.Channel,
		"status":                  domain.StatusSent,
		"result":                  domain.StatusSent,
		"delivery_attempts":       row.DeliveryAttempts,
		"trace_id":                traceID,
	})
	if err != nil {
		return fmt.Errorf("notification: sent audit metadata: %w", err)
	}
	afterState, err := json.Marshal(map[string]any{
		"status":            domain.StatusSent,
		"delivery_attempts": row.DeliveryAttempts,
	})
	if err != nil {
		return fmt.Errorf("notification: sent audit after_state: %w", err)
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
		notificationSentEventType,
		notificationRequestID,
		afterState,
		metadata,
		traceID,
		now,
	)
	if err != nil {
		return fmt.Errorf("notification: sent audit: %w", err)
	}
	return nil
}
