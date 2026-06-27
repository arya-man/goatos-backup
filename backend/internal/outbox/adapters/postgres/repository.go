package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/outbox/domain"
	"github.com/vgoats/goatos/backend/internal/outbox/ports"
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
	if status != domain.StatusDeadLetter && status != domain.StatusFailed {
		return nil, fmt.Errorf("outbox: dead letter status must be %q or %q", domain.StatusDeadLetter, domain.StatusFailed)
	}
	rows, err := r.pool.Query(ctx, `
SELECT
  outbox_id::text,
  tenant_id::text,
  event_id::text,
  event_type,
  aggregate_type,
  aggregate_id::text,
  topic,
  status,
  attempt_count,
  COALESCE(last_error, ''),
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
			&message.AggregateType,
			&message.AggregateID,
			&message.Topic,
			&message.Status,
			&message.AttemptCount,
			&message.LastError,
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

func (r *Repository) ReplayDeadLetters(ctx context.Context, params ports.ReplayDeadLettersParams) (int64, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if params.Now.IsZero() {
		params.Now = time.Now().UTC()
	}
	tag, err := r.pool.Exec(ctx, `
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
		params.TenantID, params.OutboxIDs, replayReason(params.Reason), params.Now, maxDeadLetterReplays)
	if err != nil {
		return 0, fmt.Errorf("outbox: replay dead letters: %w", err)
	}
	return tag.RowsAffected(), nil
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

func (r *Repository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.timeout)
}
