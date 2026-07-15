package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/outbox/domain"
	"github.com/vgoats/goatos/backend/internal/outbox/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
)

const defaultQueryTimeout = 3 * time.Second
const maxDeadLetterReplays = 3

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

func (r *Repository) Ping(ctx context.Context) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return r.pool.Ping(ctx)
}

func (r *Repository) ReclaimStalePublishing(ctx context.Context, now time.Time, leaseTimeout time.Duration) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	cutoff := now.Add(-leaseTimeout)
	tag, err := r.pool.Exec(ctx, `
UPDATE outbox_messages
SET status = 'pending',
    next_attempt_at = NULL,
    updated_at = $1
WHERE status = 'publishing'
  AND updated_at <= $2`, now, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *Repository) ClaimPending(ctx context.Context, params ports.ClaimParams) (*ports.ClaimResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	rows, err := tx.Query(ctx, `
SELECT
  outbox_id::text,
  tenant_id::text,
  event_id::text,
  event_type,
  schema_version,
  aggregate_type,
  aggregate_id::text,
  topic,
  headers,
  payload,
  idempotency_key,
  trace_id,
  attempt_count,
  created_at,
  updated_at
FROM outbox_messages
WHERE status = 'pending'
  AND (next_attempt_at IS NULL OR next_attempt_at <= $1)
ORDER BY created_at, outbox_id
LIMIT $2
FOR UPDATE SKIP LOCKED`, params.Now, params.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var selected []domain.Message
	for rows.Next() {
		var message domain.Message
		if err := rows.Scan(
			&message.OutboxID,
			&message.TenantID,
			&message.EventID,
			&message.EventType,
			&message.SchemaVersion,
			&message.AggregateType,
			&message.AggregateID,
			&message.Topic,
			&message.Headers,
			&message.Payload,
			&message.IdempotencyKey,
			&message.TraceID,
			&message.AttemptCount,
			&message.CreatedAt,
			&message.UpdatedAt,
		); err != nil {
			return nil, err
		}
		selected = append(selected, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := &ports.ClaimResult{Messages: make([]domain.Message, 0, len(selected))}
	for _, message := range selected {
		if message.AttemptCount >= params.MaxAttempts {
			tag, err := tx.Exec(ctx, `
UPDATE outbox_messages
SET status = 'dead_letter',
    next_attempt_at = NULL,
    last_error = $2,
    updated_at = $3
WHERE outbox_id = $1
  AND status = 'pending'`, message.OutboxID, "max_attempts_exhausted_before_publish", params.Now)
			if err != nil {
				return nil, err
			}
			result.DeadLetterCount += int(tag.RowsAffected())
			continue
		}
		tag, err := tx.Exec(ctx, `
UPDATE outbox_messages
SET status = 'publishing',
    attempt_count = attempt_count + 1,
    next_attempt_at = NULL,
    updated_at = $2
WHERE outbox_id = $1
  AND status = 'pending'`, message.OutboxID, params.Now)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		message.AttemptCount++
		result.Messages = append(result.Messages, message)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return result, nil
}

// ClaimMessage claims one specific pending outbox row for publishing without touching other ready
// rows. It is used by local harnesses that need to drive one durable envelope through the
// production consumer path without draining unrelated pending setup events.
func (r *Repository) ClaimMessage(ctx context.Context, outboxID string, now time.Time) (*domain.Message, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var message domain.Message
	err = tx.QueryRow(ctx, `
SELECT
  outbox_id::text,
  tenant_id::text,
  event_id::text,
  event_type,
  schema_version,
  aggregate_type,
  aggregate_id::text,
  topic,
  headers,
  payload,
  idempotency_key,
  trace_id,
  attempt_count,
  created_at,
  updated_at
FROM outbox_messages
WHERE outbox_id = $1::uuid
  AND status = 'pending'
  AND (next_attempt_at IS NULL OR next_attempt_at <= $2)
FOR UPDATE`, outboxID, now).Scan(
		&message.OutboxID,
		&message.TenantID,
		&message.EventID,
		&message.EventType,
		&message.SchemaVersion,
		&message.AggregateType,
		&message.AggregateID,
		&message.Topic,
		&message.Headers,
		&message.Payload,
		&message.IdempotencyKey,
		&message.TraceID,
		&message.AttemptCount,
		&message.CreatedAt,
		&message.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	tag, err := tx.Exec(ctx, `
UPDATE outbox_messages
SET status = 'publishing',
    attempt_count = attempt_count + 1,
    next_attempt_at = NULL,
    updated_at = $2
WHERE outbox_id = $1::uuid
  AND status = 'pending'`, outboxID, now)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, pgx.ErrNoRows
	}
	message.AttemptCount++

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &message, nil
}

func (r *Repository) MarkPublished(ctx context.Context, outboxID string, now time.Time) error {
	return r.execStatusUpdate(ctx, `
UPDATE outbox_messages
SET status = 'published',
    published_at = $2,
    next_attempt_at = NULL,
    last_error = NULL,
    updated_at = $2
WHERE outbox_id = $1
  AND status = 'publishing'`, outboxID, now)
}

func (r *Repository) MarkRetry(ctx context.Context, outboxID string, nextAttemptAt time.Time, lastError string, now time.Time) error {
	return r.execStatusUpdate(ctx, `
UPDATE outbox_messages
SET status = 'pending',
    next_attempt_at = $2,
    last_error = $3,
    updated_at = $4
WHERE outbox_id = $1
  AND status = 'publishing'`, outboxID, nextAttemptAt, lastError, now)
}

func (r *Repository) MarkFailed(ctx context.Context, outboxID string, lastError string, now time.Time) error {
	return r.execStatusUpdate(ctx, `
UPDATE outbox_messages
SET status = 'failed',
    last_error = $2,
    updated_at = $3
WHERE outbox_id = $1
  AND status = 'publishing'`, outboxID, lastError, now)
}

func (r *Repository) MarkDeadLetter(ctx context.Context, outboxID string, lastError string, now time.Time) error {
	return r.execStatusUpdate(ctx, `
UPDATE outbox_messages
SET status = 'dead_letter',
    last_error = $2,
    updated_at = $3
WHERE outbox_id = $1
	AND status = 'publishing'`, outboxID, lastError, now)
}

// ReleasePublishing releases a message from 'publishing' back to 'pending'
// with no next_attempt_at (making it immediately available for re-claim).
// Used when ctx cancellation interrupts a drain mid-batch: unprocessed claimed
// messages are released so the next tick re-claims them immediately instead of
// waiting ~5 minutes for the lease to expire (KERN-02 mitigation).
func (r *Repository) ReleasePublishing(ctx context.Context, outboxID string, now time.Time) error {
	return r.execStatusUpdate(ctx, `
UPDATE outbox_messages
SET status = 'pending',
    next_attempt_at = NULL,
    updated_at = $2
WHERE outbox_id = $1
  AND status = 'publishing'`, outboxID, now)
}

func (r *Repository) ListDeadLetters(ctx context.Context, q ports.DeadLetterQuery) ([]domain.DeadLetterMessage, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if q.Limit <= 0 {
		q.Limit = 50
	}
	if q.Limit > 500 {
		q.Limit = 500
	}
	status := q.Status
	if status == "" {
		status = domain.StatusDeadLetter
	}
	if status != domain.StatusDeadLetter && status != domain.StatusFailed && status != domain.StatusDiscarded {
		return nil, fmt.Errorf("outbox: dead letter status must be %q, %q, or %q", domain.StatusDeadLetter, domain.StatusFailed, domain.StatusDiscarded)
	}
	rows, err := r.pool.Query(ctx, `
SELECT
  outbox_id::text,
  tenant_id::text,
  event_id::text,
  event_type,
  schema_version,
  aggregate_type,
  aggregate_id::text,
  topic,
  status,
  attempt_count,
  replay_count,
  COALESCE(last_error, ''),
  idempotency_key,
  trace_id,
  headers,
  payload,
  created_at,
  updated_at
FROM outbox_messages
WHERE tenant_id = $1::uuid
  AND status = $2
  AND ($3::text = '' OR event_type = $3)
  AND ($4::text = '' OR topic = $4)
ORDER BY updated_at DESC, outbox_id DESC
LIMIT $5`, q.TenantID, status, q.EventType, q.Topic, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("outbox: list dead letters: %w", err)
	}
	defer rows.Close()
	var messages []domain.DeadLetterMessage
	for rows.Next() {
		var message domain.DeadLetterMessage
		if err := rows.Scan(
			&message.OutboxID,
			&message.TenantID,
			&message.EventID,
			&message.EventType,
			&message.SchemaVersion,
			&message.AggregateType,
			&message.AggregateID,
			&message.Topic,
			&message.Status,
			&message.AttemptCount,
			&message.ReplayCount,
			&message.LastError,
			&message.IdempotencyKey,
			&message.TraceID,
			&message.Headers,
			&message.Payload,
			&message.CreatedAt,
			&message.UpdatedAt,
		); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func (r *Repository) Health(ctx context.Context, tenantID string, now time.Time) (domain.Health, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var health domain.Health
	err := r.pool.QueryRow(ctx, `
SELECT
  COUNT(*) FILTER (WHERE status = 'pending')::bigint AS pending_count,
  COUNT(*) FILTER (WHERE status = 'publishing')::bigint AS publishing_count,
  COUNT(*) FILTER (WHERE status = 'failed')::bigint AS failed_count,
  COUNT(*) FILTER (WHERE status = 'dead_letter')::bigint AS dead_letter_count,
  MIN(created_at) FILTER (WHERE status = 'pending') AS oldest_pending_at,
  MIN(updated_at) FILTER (WHERE status IN ('failed', 'dead_letter')) AS oldest_failure_at,
  MAX(published_at) FILTER (WHERE status = 'published') AS last_published_at
FROM outbox_messages
WHERE tenant_id = $1::uuid`, tenantID).Scan(
		&health.PendingCount,
		&health.PublishingCount,
		&health.FailedCount,
		&health.DeadLetterCount,
		&health.OldestPendingAt,
		&health.OldestFailureAt,
		&health.LastPublishedAt,
	)
	if err != nil {
		return domain.Health{}, fmt.Errorf("outbox: health: %w", err)
	}
	health.Status = "healthy"
	if health.FailedCount > 0 || health.DeadLetterCount > 0 {
		health.Status = "degraded"
	}
	if health.OldestPendingAt != nil && now.Sub(*health.OldestPendingAt) > 15*time.Minute {
		health.Status = "degraded"
	}
	return health, nil
}

func (r *Repository) ReplayDeadLetters(ctx context.Context, params ports.ReplayDeadLettersParams) (ports.DLQActionResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if params.Now.IsZero() {
		params.Now = time.Now().UTC()
	}
	return r.runDLQAction(ctx, dlqActionParams{
		TenantID:       params.TenantID,
		OutboxIDs:      params.OutboxIDs,
		Reason:         params.Reason,
		Now:            params.Now,
		IdempotencyKey: params.IdempotencyKey,
		RequestHash:    params.RequestHash,
		Action:         "replay",
		UpdateSQL: `
UPDATE outbox_messages
SET status = 'pending',
    attempt_count = 0,
    replay_count = replay_count + 1,
    next_attempt_at = $4::timestamptz,
    published_at = NULL,
    last_error = $3,
    updated_at = $4::timestamptz
WHERE tenant_id = $1::uuid
  AND outbox_id::text = ANY($2::text[])
  AND status IN ('dead_letter', 'failed')
  AND replay_count < $5`,
		MaxReplays: maxDeadLetterReplays,
	})
}

func (r *Repository) DiscardDeadLetters(ctx context.Context, params ports.DiscardDeadLettersParams) (ports.DLQActionResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if params.Now.IsZero() {
		params.Now = time.Now().UTC()
	}
	return r.runDLQAction(ctx, dlqActionParams{
		TenantID:       params.TenantID,
		OutboxIDs:      params.OutboxIDs,
		Reason:         params.Reason,
		Now:            params.Now,
		IdempotencyKey: params.IdempotencyKey,
		RequestHash:    params.RequestHash,
		Action:         "discard",
		UpdateSQL: `
UPDATE outbox_messages
SET status = 'discarded',
    next_attempt_at = NULL,
    published_at = NULL,
    last_error = $3,
    updated_at = $4::timestamptz
WHERE tenant_id = $1::uuid
  AND outbox_id::text = ANY($2::text[])
  AND status IN ('dead_letter', 'failed')`,
	})
}

type dlqActionParams struct {
	TenantID       string
	OutboxIDs      []string
	Reason         string
	Now            time.Time
	IdempotencyKey string
	RequestHash    string
	Action         string
	UpdateSQL      string
	MaxReplays     int
}

func (r *Repository) runDLQAction(ctx context.Context, params dlqActionParams) (ports.DLQActionResult, error) {
	if params.RequestHash == "" {
		params.RequestHash = dlqRepoActionHash(params.Action, params.OutboxIDs, params.Reason)
	}
	if params.IdempotencyKey == "" {
		params.IdempotencyKey = params.Action + ":" + params.RequestHash
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.DLQActionResult{}, fmt.Errorf("outbox: begin dlq action: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var actionID, status, requestHash string
	var updatedCount int64
	err = tx.QueryRow(ctx, `
INSERT INTO outbox_dlq_actions (
  tenant_id, idempotency_key, action, request_hash, reason, outbox_ids, status, created_at, updated_at
) VALUES (
  $1::uuid, $2, $3, $4, $5, $6::text[], 'running', $7::timestamptz, $7::timestamptz
)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING action_id::text, status, request_hash, updated_count`,
		params.TenantID, params.IdempotencyKey, params.Action, params.RequestHash, params.Reason, params.OutboxIDs, params.Now).Scan(
		&actionID, &status, &requestHash, &updatedCount,
	)
	fresh := true
	if errors.Is(err, pgx.ErrNoRows) {
		fresh = false
		err = tx.QueryRow(ctx, `
SELECT action_id::text, status, request_hash, updated_count
FROM outbox_dlq_actions
WHERE tenant_id = $1::uuid
  AND idempotency_key = $2
FOR UPDATE`, params.TenantID, params.IdempotencyKey).Scan(&actionID, &status, &requestHash, &updatedCount)
	}
	if err != nil {
		return ports.DLQActionResult{}, fmt.Errorf("outbox: record dlq action: %w", err)
	}
	if requestHash != params.RequestHash {
		return ports.DLQActionResult{}, ports.ErrDLQActionConflict
	}
	if !fresh {
		if status == "completed" {
			if err := tx.Commit(ctx); err != nil {
				return ports.DLQActionResult{}, err
			}
			committed = true
			return ports.DLQActionResult{Updated: updatedCount, Replayed: true}, nil
		}
		return ports.DLQActionResult{}, ports.ErrDLQActionPending
	}

	reason := replayReason(params.Reason)
	if params.Action == "discard" {
		reason = discardReason(params.Reason)
	}
	if params.MaxReplays > 0 {
		tag, execErr := tx.Exec(ctx, params.UpdateSQL, params.TenantID, params.OutboxIDs, reason, params.Now, params.MaxReplays)
		err = execErr
		if err == nil {
			updatedCount = tag.RowsAffected()
		}
	} else {
		tag, execErr := tx.Exec(ctx, params.UpdateSQL, params.TenantID, params.OutboxIDs, reason, params.Now)
		err = execErr
		if err == nil {
			updatedCount = tag.RowsAffected()
		}
	}
	if err != nil {
		return ports.DLQActionResult{}, fmt.Errorf("outbox: %s dead letters: %w", params.Action, err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE outbox_dlq_actions
SET status = 'completed',
    updated_count = $3,
    completed_at = $4::timestamptz,
    updated_at = $4::timestamptz
WHERE tenant_id = $1::uuid
  AND action_id = $2::uuid`,
		params.TenantID, actionID, updatedCount, params.Now); err != nil {
		return ports.DLQActionResult{}, fmt.Errorf("outbox: finish dlq action: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ports.DLQActionResult{}, err
	}
	committed = true
	return ports.DLQActionResult{Updated: updatedCount}, nil
}

func (r *Repository) RecordDLQActionAudit(ctx context.Context, params ports.RecordDLQActionAuditParams) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if len(params.OutboxIDs) == 0 {
		return nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("outbox: begin dlq audit: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	var actionID, action, status, requestHash, reason string
	var updatedCount int64
	if err := tx.QueryRow(ctx, `
SELECT action_id::text, action, status, request_hash, reason, updated_count
FROM outbox_dlq_actions
WHERE tenant_id = $1::uuid
  AND idempotency_key = $2
FOR UPDATE`, params.TenantID, params.IdempotencyKey).Scan(&actionID, &action, &status, &requestHash, &reason, &updatedCount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ports.ErrDLQActionPending
		}
		return fmt.Errorf("outbox: load dlq action for audit: %w", err)
	}
	if requestHash != params.RequestHash || action != params.Action {
		return ports.ErrDLQActionConflict
	}
	if status != "completed" {
		return ports.ErrDLQActionPending
	}

	recorder := audit.NewTxRecorder(tx)
	result := "no_rows"
	if updatedCount > 0 {
		result = "updated"
	}
	for _, outboxID := range params.OutboxIDs {
		var exists bool
		if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM audit_log
  WHERE tenant_id = $1::uuid
    AND action = $2
    AND resource_type = 'outbox_message'
    AND resource_id = $3::uuid
    AND metadata->>'idempotency_key' = $4
    AND metadata->>'request_hash' = $5
)`, params.TenantID, "operations.dlq."+action, outboxID, params.IdempotencyKey, requestHash).Scan(&exists); err != nil {
			return fmt.Errorf("outbox: check dlq audit: %w", err)
		}
		if exists {
			continue
		}
		if err := recorder.Record(ctx, audit.Event{
			TenantID:     params.TenantID,
			ActorID:      params.ActorID,
			ActorType:    "human",
			Action:       "operations.dlq." + action,
			ResourceType: "outbox_message",
			ResourceID:   outboxID,
			Metadata: map[string]any{
				"domain":          "operations",
				"module":          "dlq",
				"category":        "outbox",
				"status":          action,
				"result":          result,
				"reason":          reason,
				"updated_count":   updatedCount,
				"requested_count": len(params.OutboxIDs),
				"idempotency_key": params.IdempotencyKey,
				"request_hash":    requestHash,
				"dlq_action_id":   actionID,
			},
			TraceID: params.TraceID,
		}); err != nil {
			return fmt.Errorf("outbox: record dlq audit: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

func dlqRepoActionHash(action string, outboxIDs []string, reason string) string {
	ids := append([]string(nil), outboxIDs...)
	sort.Strings(ids)
	raw, _ := json.Marshal(struct {
		Action    string   `json:"action"`
		OutboxIDs []string `json:"outbox_ids"`
		Reason    string   `json:"reason"`
	}{
		Action:    action,
		OutboxIDs: ids,
		Reason:    reason,
	})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (r *Repository) execStatusUpdate(ctx context.Context, sql string, args ...any) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("outbox: status update matched no publishing row")
	}
	return nil
}

func replayReason(reason string) string {
	if reason == "" {
		return "replayed_from_dlq"
	}
	if len(reason) > 180 {
		reason = reason[:180]
	}
	return "replayed_from_dlq: " + reason
}

func discardReason(reason string) string {
	if reason == "" {
		return "discarded_from_dlq"
	}
	if len(reason) > 180 {
		reason = reason[:180]
	}
	return "discarded_from_dlq: " + reason
}

func (r *Repository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.timeout)
}
