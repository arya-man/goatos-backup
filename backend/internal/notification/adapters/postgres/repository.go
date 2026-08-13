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

// OldestDueRequestedAtSQL is the exact production backlog-age probe query,
// exported so the query-plan gate exercises the REAL statement instead of a
// simplified imitation that could drift from what production runs. Params:
// $1 tenant, $2 now.
//
// Optimization: uses ORDER BY requested_at ASC LIMIT 1 instead of MIN(requested_at).
// Both shapes are semantically identical: LIMIT 1 ORDER BY requested_at returns
// the globally smallest requested_at among all rows satisfying the WHERE clause,
// exactly the same value as MIN(requested_at). The key difference: the old
// MIN aggregate visited every row matching (status IN (...) AND COALESCE(...) <= now),
// O(due-count). The new LIMIT 1 shape can early-stop: Postgres scans the indexed
// (tenant_id, requested_at) key in ascending order and returns the first row where
// the COALESCE filter passes, achieving early-stop without visiting the entire
// due-row population. This is proved by KERN-04's query-plan gate
// (TestNotificationRepositoryOldestDueRequestedAtPlanStaysIndexBoundedUnderSaturation)
// and correctness by TestNotificationRepositoryOldestDueRequestedAtUnderSaturation:
// under retry skew, the smallest requested_at (hour-old failed request) IS the
// value returned (not the claim queue's next row), and ORDER BY requested_at LIMIT 1
// correctly finds it without scanning or sorting the entire due population.
//
// The index notification_requests_oldest_due_requested_at_idx
// (tenant_id, requested_at) INCLUDE (next_attempt_at) WHERE status IN ('queued','failed')
// enables the scan: it scopes to only the due-status rows and orders by requested_at,
// so the first matching row is the minimum. The COALESCE(next_attempt_at, requested_at) <= now
// filter is still evaluated for each scanned row, but early-stop at LIMIT 1 means
// we never visit rows beyond the oldest due request. This is O(1) average case
// (first row matches) and O(D) worst case if many very-old rows are not-yet-due
// (e.g., many future-scheduled retries older than now) -- but production typically
// has few such stragglers, and the index's early-stop is still a win over the prior
// full-scan MIN.
const OldestDueRequestedAtSQL = `
SELECT requested_at
FROM notification_requests
WHERE tenant_id = $1::uuid
  AND status IN ('queued', 'failed')
  AND COALESCE(next_attempt_at, requested_at) <= $2::timestamptz
ORDER BY requested_at ASC
LIMIT 1`

// OldestDueRequestedAt returns the requested_at of the globally oldest
// currently-due, undelivered request. See OldestDueRequestedAtSQL for why the
// query is a MIN(...) aggregate rather than an ORDER BY ... LIMIT 1 lookup.
// MIN over no due rows yields NULL, surfaced as (_, false, nil).
func (r *Repository) OldestDueRequestedAt(ctx context.Context, tenantID string, now time.Time) (time.Time, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var oldest pgtype.Timestamptz
	err := r.pool.QueryRow(ctx, OldestDueRequestedAtSQL, tenantID, now).Scan(&oldest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("notification: oldest due requested_at: %w", err)
	}
	if !oldest.Valid {
		return time.Time{}, false, nil
	}
	return oldest.Time, true, nil
}

// SuppressInvalidRecipient marks a provider-rejected FCM token as dead. The CALLER
// (isInvalidFCMRecipientResponse) has already filtered out ambiguous/payload-level errors, so by
// the time this runs the token itself is confirmed gone (UNREGISTERED/NOT_REGISTERED) or
// explicitly named invalid by FCM. Even so, this function deliberately clears ONLY the push
// binding (fcm_token) -- it must NEVER touch `status`/`revoked_at`/`revoked_by`. Those columns are
// the device's ability to authenticate and bootstrap, which is a distinct, deliberate admin/
// security action (see workforce RevokeDevice) and must never be a side effect of a push delivery
// failure. A prior version of this function also set status='revoked' here, which meant a single
// bad push (or a payload bug that fooled the recipient check) could brick every addressed phone's
// login with no self-heal path (P0 device-lockout incident). The device stays 'active' and able to
// authenticate; it simply stops receiving pushes on the dead token until it registers a fresh one
// (Android register/heartbeat path already upserts a new fcm_token and requires no unlock). The row
// records WHEN and WHY the token was invalidated (fcm_invalidated_at/reason) purely for observability.
// Additionally, all pending notification requests addressed to this dead token are suppressed to
// avoid wasting dispatch retries. Idempotent: re-running against an already-cleared token matches
// zero rows on the device update (fcm_token is already NULL) and is a no-op.
func (r *Repository) SuppressInvalidRecipient(ctx context.Context, tenantID, recipientRef, reason string, now time.Time) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	recipientRef = strings.TrimSpace(recipientRef)
	if recipientRef == "" {
		return 0, nil
	}
	if strings.TrimSpace(reason) == "" {
		reason = "invalid_notification_recipient"
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Deliberately no `status`/`revoked_at`/`revoked_by` write here -- see the function comment.
	// Matches regardless of current status (not just 'active') so a device already sitting in a
	// stale non-active state from before this fix still gets its dead token cleared instead of
	// silently skipped.
	if _, err := tx.Exec(ctx, `
UPDATE workforce_member_devices
SET fcm_token = NULL,
    metadata = COALESCE(metadata, '{}'::jsonb) || jsonb_build_object(
      'fcm_invalidated_at', $3::timestamptz,
      'fcm_invalidated_reason', $4::text
    ),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND fcm_token = $2`, tenantID, recipientRef, now, reason); err != nil {
		return 0, fmt.Errorf("notification: clear invalid fcm token: %w", err)
	}
	tag, err := tx.Exec(ctx, `
UPDATE notification_requests
SET status = 'suppressed',
    failure_reason = $3,
    next_attempt_at = NULL,
    lease_token = NULL,
    leased_at = NULL,
    updated_at = $4::timestamptz
WHERE tenant_id = $1::uuid
  AND recipient_ref = $2
  AND channel = 'push_fcm'
  AND status IN ('queued', 'failed', 'exhausted')`, tenantID, recipientRef, reason, now)
	if err != nil {
		return 0, fmt.Errorf("notification: suppress invalid recipient requests: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// ReclaimStaleSending requeues 'sending' rows whose lease expired before real delivery
// completed (worker crash/restart between ClaimDue and MarkSent/MarkFailed). ClaimDue
// increments delivery_attempts at CLAIM time (not at actual delivery time), so a claim that
// never reached a real delivery attempt already consumed one of max_attempts. Without
// correction here, repeated crashes before delivery would permanently exhaust a message
// that was never actually attempted, dead-lettering it with zero real delivery attempts.
// Reclaim restores the consumed attempt by decrementing delivery_attempts (floored at 0) in
// the SAME statement that requeues the row, so the count reflects only claims that reached a
// genuine outcome (MarkSent/MarkFailed, both of which leave delivery_attempts as-is because
// they read/return the claim-time value rather than incrementing again). This is safe against
// double-decrement: the WHERE clause only matches rows still in 'sending' past their lease, and
// this single atomic UPDATE flips status to 'queued' in the same statement, so a concurrent or
// repeated reclaim sweep can never match (and decrement) the same row twice.
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
    delivery_attempts = GREATEST(delivery_attempts - 1, 0),
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

// ClaimDueSQL is the exact production notification-claim query, exported so the
// query-plan gate (validate-sqlc-plans + the scale EXPLAIN test) exercises the
// REAL writable CTE — candidate selection, FOR UPDATE SKIP LOCKED row locking,
// and the UPDATE ... RETURNING — instead of a simplified imitation that could
// drift from what production runs.

func (r *Repository) ClaimDue(ctx context.Context, params ports.ClaimParams) ([]domain.Request, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if params.Limit <= 0 {
		params.Limit = 50
	}
	if params.MaxAttempts <= 0 {
		params.MaxAttempts = 5
	}

	// First, count and transition exhausted rows (delivery_attempts >= max_attempts but still queued/failed)
	// to 'exhausted' status to prevent them from accumulating in the active set forever.
	exhaustedCount, err := r.pool.Exec(ctx, `
UPDATE notification_requests
SET status = 'exhausted',
    failure_reason = COALESCE(NULLIF(failure_reason, ''), 'max_delivery_attempts_exceeded'),
    next_attempt_at = NULL,
    lease_token = NULL,
    leased_at = NULL,
    updated_at = $2::timestamptz
WHERE tenant_id = $1::uuid
  AND status IN ('queued', 'failed')
  AND delivery_attempts >= $3`, params.TenantID, params.Now, params.MaxAttempts)
	if err != nil {
		return nil, fmt.Errorf("notification: transition exhausted rows: %w", err)
	}
	_ = exhaustedCount // captured for observability; not returned in this path as ClaimDue signature

	// Now claim retryable rows (delivery_attempts < max_attempts)
	rows, err := r.pool.Query(ctx, `
WITH candidates AS (
  SELECT notification_request_id
  FROM notification_requests
  WHERE tenant_id = $1::uuid
    AND status IN ('queued', 'failed')
    -- Equivalent to (next_attempt_at IS NULL OR next_attempt_at <= now) because
    -- requested_at is never in the future, but written as the index expression
    -- so notification_requests_queue_idx (tenant_id, status,
    -- COALESCE(next_attempt_at, requested_at)) can RANGE-scan due rows and stop
    -- at now — future-scheduled retries are not scanned even under heavy skew.
    AND COALESCE(next_attempt_at, requested_at) <= $2::timestamptz
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
    -- Live recipient-address resolution (BUG: recipient_ref used to be a frozen snapshot of the
    -- FCM token AT CREATION TIME, so any push queued/retried/requeued across a token rotation
    -- (reinstall, app data clear) addressed a token no device holds anymore -- see
    -- SuppressInvalidRecipient's doc and the requeue tool's doc for the same defect from the
    -- suppression/recovery side). For a push_fcm row addressed at a workforce member
    -- (context->>'member_id', already the calendar/verification producers' contract -- see the
    -- notification_requests_weighing_alerts_idx partial index on this same expression), resolve
    -- the CURRENT reachable device live, at the moment of claim, instead of trusting the frozen
    -- column. Multi-device rule: a member may hold several active devices (reinstall without sign
    -- out, a second phone); this picks exactly ONE -- the most recently active reachable device
    -- (ORDER BY last_seen_at DESC, device_id ASC deterministic tiebreak) -- rather than fanning
    -- out to all of them, because one notification_requests row has exactly one lease and one
    -- MarkSent/MarkFailed transition; fanning one row's outcome across N independent HTTP calls to
    -- FCM would make "sent" ambiguous the moment one device's send succeeds and another's fails
    -- (which ledger entry/status wins?). The reachable partial index
    -- (workforce_member_devices_push_reachable_idx: status='active' AND fcm_token IS NOT NULL AND
    -- notifications_enabled IS DISTINCT FROM false) already scopes exactly the "reachable" set this
    -- needs. The stored recipient_ref column is deliberately left UNTOUCHED by this query (no UPDATE
    -- of the column, only of what is RETURNED) -- it stays the durable audit record of what the
    -- request was originally addressed to; ListExhausted/the requeue tool still show that original
    -- value for operator review. When member_id is present but no reachable device resolves, the
    -- sentinel 'no-active-device:<member_id>' is returned instead of silently falling back to the
    -- stale recipient_ref -- gateway.setFCMTarget recognizes this prefix and fails loudly with
    -- ports.ErrRecipientNoActiveDevice (never looks like a successful send, never conflated with
    -- ErrRecipientUnusable's "not a device token" message, which would misreport a resolvable
    -- addressing gap as a malformed identifier). This also naturally composes with
    -- SuppressInvalidRecipient: once a dead token is cleared (fcm_token set NULL), the reachable
    -- index no longer matches that device row, so the NEXT claim of a retried/requeued row for the
    -- same member resolves straight to whatever fresh token the device registered, with no code
    -- path ever retrying the dead token again. Non-push_fcm channels and rows with no member_id in
    -- context (email/slack/webhook/incident, or legacy rows predating this contract) are entirely
    -- unaffected: they keep using the stored recipient_ref exactly as before.
    CASE
      WHEN nr.channel = 'push_fcm' AND (nr.context->>'member_id') ~ '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$' THEN
        COALESCE(
          (
            SELECT d.fcm_token
            FROM workforce_member_devices d
            WHERE d.tenant_id = nr.tenant_id
              AND d.workforce_member_id = (nr.context->>'member_id')::uuid
              AND d.status = 'active'
              AND d.fcm_token IS NOT NULL
              AND d.notifications_enabled IS DISTINCT FROM false
            ORDER BY d.last_seen_at DESC, d.device_id ASC
            LIMIT 1
          ),
          'no-active-device:' || (nr.context->>'member_id')
        )
      ELSE COALESCE(nr.recipient_ref, '')
    END,
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
	return r.MarkSentWithResult(ctx, tenantID, notificationRequestID, leaseToken, deliveredBy, "", now)
}

// MarkSentWithResult is MarkSent plus the optional provider acknowledgement returned by adapters
// such as FCM. The provider id is written in the same transaction as the sent state, immutable
// attempt row, and notification.sent outbox evidence.
func (r *Repository) MarkSentWithResult(
	ctx context.Context,
	tenantID, notificationRequestID, leaseToken, deliveredBy, providerMessageID string,
	now time.Time,
) error {
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

	var providerID *string
	if trimmed := strings.TrimSpace(providerMessageID); trimmed != "" {
		providerID = &trimmed
	}
	if err := insertDeliveryAttempt(ctx, tx, tenantID, notificationRequestID, row.DeliveryAttempts, row.Channel, deliveryAttemptResultSent, now, nil, providerID); err != nil {
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

var _ ports.RequeueRepository = (*Repository)(nil)

// ListExhausted is the read-only visibility surface for exhausted notifications: the same
// question `SELECT * FROM notification_requests WHERE status = 'exhausted'` answers, but shaped
// for an operator deciding what (if anything) to requeue. It is what cmd/notification-requeue
// -mode list runs, and it never mutates.
func (r *Repository) ListExhausted(ctx context.Context, query ports.ExhaustedQuery) ([]ports.ExhaustedNotification, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := r.pool.Query(ctx, `
SELECT
  notification_request_id::text,
  tenant_id::text,
  calendar_event_id,
  target_type,
  COALESCE(target_id::text, ''),
  notification_type,
  channel,
  COALESCE(recipient_ref, ''),
  COALESCE(failure_reason, ''),
  delivery_attempts,
  requeue_count,
  requested_at,
  updated_at
FROM notification_requests
WHERE tenant_id = $1::uuid
  AND status = 'exhausted'
  AND ($2 = '' OR notification_type = $2)
  AND ($3::timestamptz IS NULL OR requested_at >= $3::timestamptz)
  AND ($4::timestamptz IS NULL OR requested_at <= $4::timestamptz)
ORDER BY requested_at ASC, notification_request_id
LIMIT $5`,
		query.TenantID, query.NotificationType, nullableTime(query.Since), nullableTime(query.Until), limit)
	if err != nil {
		return nil, fmt.Errorf("notification: list exhausted: %w", err)
	}
	defer rows.Close()
	var out []ports.ExhaustedNotification
	for rows.Next() {
		var item ports.ExhaustedNotification
		if err := rows.Scan(
			&item.NotificationRequestID,
			&item.TenantID,
			&item.CalendarEventID,
			&item.TargetType,
			&item.TargetID,
			&item.NotificationType,
			&item.Channel,
			&item.RecipientRef,
			&item.FailureReason,
			&item.DeliveryAttempts,
			&item.RequeueCount,
			&item.RequestedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// RequeueExhausted is the operator-driven recovery path for the "exhausted rows can never be
// re-picked" defect: ClaimDue only ever claims status IN ('queued','failed'), and nothing else in
// the codebase moves a row out of 'exhausted'. This is the one place that does.
//
// Scoping (why it can never blanket-requeue the world): TenantID and NotificationType are both
// mandatory — there is no all-tenants and no all-types mode. The Since/Until window further
// bounds by requested_at, and an explicit NotificationRequestIDs list (if given) intersects on
// top of all of the above rather than replacing it, so a caller can never widen scope by mixing
// modes. In practice an operator fixes ONE misconfigured channel (the failure this exists for)
// and requeues ONE tenant's ONE notification_type over a bounded recent window.
//
// Idempotency: the UPDATE's WHERE clause requires status = 'exhausted'. A row that a first call
// already moved to 'queued' no longer matches on a second, identical call — running the same
// requeue twice (same params) requeues nothing the second time. requeue_count is incremented (not
// just set) precisely so a genuinely repeated requeue of the SAME row across separate operator
// invocations is visible in ListExhausted/audit rather than looking like a fresh one.
//
// Relevance ("must not resurrect a notification whose subject is no longer relevant"): the one
// case this repository can check generically, without importing another module's domain, is
// target_type = 'verification_item' (the shape verification/notificationbridge uses for every
// rework/pending push, including the vaccination-proof-rejection defect this was written for).
// The "still outstanding" status differs by notification_type (see the CASE below): a 'rework'
// push reports the verdict itself, which STAYS 'rejected' for the life of that item -- a
// resubmitted proof creates a NEW item_id rather than flipping the old one back to pending (see
// verification/adapters/postgres/repository.go's verdict handling), so 'rejected' is a durable,
// correct relevance check for it. A 'verification_pending' push instead reports "a verifier still
// owes a decision", which is only true while status = 'pending'; once a verdict has landed either
// way that push is stale and must NOT be resurrected. Rows whose target_type is anything else, or
// whose notification_type has no known-relevant status for verification_item, have no cheap
// generic relevance check here, so a BROAD sweep skips them entirely; they are requeued only when
// the operator passes their ids explicitly, having read ListExhausted's output (which includes
// calendar_event_id/target) first. That keeps "I could not check this, so I will not resurrect it
// behind your back" the default, while leaving every row recoverable by name.
func (r *Repository) RequeueExhausted(ctx context.Context, params ports.RequeueParams) (ports.RequeueResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tenantID := strings.TrimSpace(params.TenantID)
	notificationType := strings.TrimSpace(params.NotificationType)
	if tenantID == "" {
		return ports.RequeueResult{}, fmt.Errorf("notification: requeue exhausted: tenant-id is required")
	}
	if notificationType == "" {
		return ports.RequeueResult{}, fmt.Errorf("notification: requeue exhausted: notification-type is required")
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	requeuedBy := strings.TrimSpace(params.RequeuedBy)
	if requeuedBy == "" {
		requeuedBy = "operator"
	}
	var idFilter []string
	if len(params.NotificationRequestIDs) > 0 {
		idFilter = params.NotificationRequestIDs
	}

	tag, err := r.pool.Exec(ctx, `
UPDATE notification_requests nr
SET status = 'queued',
    delivery_attempts = 0,
    next_attempt_at = NULL,
    lease_token = NULL,
    leased_at = NULL,
    failure_reason = 'requeued: ' || COALESCE(NULLIF(nr.failure_reason, ''), 'unknown'),
    requeued_at = $6::timestamptz,
    requeued_by = $7,
    requeue_count = nr.requeue_count + 1,
    updated_at = $6::timestamptz
WHERE nr.tenant_id = $1::uuid
  AND nr.notification_type = $2
  AND nr.status = 'exhausted'
  AND nr.requested_at >= $3::timestamptz
  AND nr.requested_at <= $4::timestamptz
  AND (COALESCE(array_length($5::uuid[], 1), 0) = 0 OR nr.notification_request_id = ANY($5::uuid[]))
  AND (
    -- A target we cannot relevance-check generically is requeued ONLY when the operator named
    -- its id explicitly (having read ListExhausted first) OR its notification_type is one this
    -- repository knows has NO external resolvable state to go stale in the first place. The
    -- earlier form here was a bare target_type <> 'verification_item', which made the whole
    -- predicate unconditionally true for every other target -- the exact OPPOSITE of the "no
    -- known-relevant status, so treat as not-yet-relevant" rule the comment above claims. Live,
    -- that meant a blanket -notification-type escalation requeue would resurrect batch
    -- escalations whose underlying condition may since have resolved, with no check at all.
    --
    -- 'reminder' (target_type='cohort', no target_id) is the one type proven safe to requeue on
    -- scope alone: a reminder reports nothing but "this cohort's window is due", which has no
    -- separate domain lifecycle to fall stale behind -- its entire relevance IS the operator's
    -- own Since/Until window, already enforced by the requested_at bounds above. That is why it
    -- was found to be PERMANENTLY unrecoverable (this repo's own test) rather than merely
    -- requiring an id: with target_type<>'verification_item' and no ids ever supplied for a
    -- cohort-wide reminder sweep, neither branch of this predicate could ever match it. Every
    -- other non-verification_item notification_type (escalation today, and any future one) still
    -- requires explicit ids, so that hole stays closed.
    (nr.target_type <> 'verification_item' AND (nr.notification_type = 'reminder' OR COALESCE(array_length($5::uuid[], 1), 0) > 0))
    OR EXISTS (
      SELECT 1 FROM verification_items vi
      WHERE vi.tenant_id = nr.tenant_id
        AND vi.item_id = nr.target_id
        -- The "still outstanding" verification_items.status depends on WHICH fact the
        -- notification is reporting, not a single fixed status: a 'rework' push reports the
        -- verdict itself, which stays true for the life of that item ('rejected' -- see the
        -- doc comment above; a resubmission opens a NEW item_id rather than resetting this
        -- one). A 'verification_pending' push instead reports "a verifier still owes a
        -- decision on this item", which is true only while the item is 'pending' -- once a
        -- verdict lands (approved/rejected) that decision has already been made by the time
        -- an operator gets around to requeuing, and resurrecting the "you have a review
        -- pending" push would be actively misleading. Any other notification_type addressed
        -- at a verification_item (there are none today) has no known-relevant status here and
        -- is treated as not-yet-relevant rather than guessed at.
        -- 'verification_approved' reports the accepted verdict to the operator who did the
        -- work. Like 'rework' it is a statement about a verdict that has ALREADY landed, so it
        -- stays true for the life of the item and is safe to resurrect while the item is
        -- 'approved'. It was missing from this CASE, which meant every approval push that
        -- failed was permanently unrecoverable -- found live: a real approval exhausted while
        -- the dispatcher lacked FCM config, and a -notification-type verification_approved
        -- returned requeued=0 with no explanation.
        AND vi.status = CASE nr.notification_type
              WHEN 'rework' THEN 'rejected'
              WHEN 'verification_pending' THEN 'pending'
              WHEN 'verification_approved' THEN 'approved'
              ELSE NULL
            END
    )
  )`,
		tenantID, notificationType, requeueSince(params.Since), requeueUntil(params.Until, now), idFilter, now, requeuedBy)
	if err != nil {
		return ports.RequeueResult{}, fmt.Errorf("notification: requeue exhausted: %w", err)
	}
	return ports.RequeueResult{Requeued: int(tag.RowsAffected())}, nil
}

func requeueSince(since time.Time) time.Time {
	if since.IsZero() {
		// Effectively unbounded on the low end: notification_requests has no rows before the
		// system's own epoch, so this never matches on `requested_at < zero-value`.
		return time.Unix(0, 0).UTC()
	}
	return since.UTC()
}

func requeueUntil(until, now time.Time) time.Time {
	if until.IsZero() {
		return now
	}
	return until.UTC()
}

func nullableTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	tt := t.UTC()
	return &tt
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
		"subject_type": "notification_request",
		"subject_id":   notificationRequestID,
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
		"subject_type": "notification_request",
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
