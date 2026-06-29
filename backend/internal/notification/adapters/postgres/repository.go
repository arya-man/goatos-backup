// Package postgres implements durable notification request persistence.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/notification/domain"
	"github.com/vgoats/goatos/backend/internal/notification/ports"
)

const defaultQueryTimeout = 3 * time.Second

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
	tag, err := r.pool.Exec(ctx, `
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
  AND status = 'sending'`, tenantID, notificationRequestID, leaseToken, failureReason, next, deliveredBy, now, status)
	if err != nil {
		return fmt.Errorf("notification: mark failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("notification: mark failed claim missing")
	}
	return nil
}
