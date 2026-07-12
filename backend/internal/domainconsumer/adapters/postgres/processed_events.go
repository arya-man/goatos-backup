package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	consumerapp "github.com/vgoats/goatos/backend/internal/domainconsumer/app"
	"github.com/vgoats/goatos/backend/internal/platform/pgconv"
)

const (
	defaultQueryTimeout         = 3 * time.Second
	defaultProcessingStaleAfter = 15 * time.Minute
)

type ProcessedEventStore struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewProcessedEventStore(pool *pgxpool.Pool, queryTimeout time.Duration) *ProcessedEventStore {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &ProcessedEventStore{pool: pool, timeout: queryTimeout}
}

func (s *ProcessedEventStore) BeginProcessing(ctx context.Context, event consumerapp.ProcessedEvent) (consumerapp.ProcessDecision, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	event = normalizeProcessedEvent(event)
	var status string
	// A row already in 'effects_committed' means a prior attempt's bus.Publish already succeeded
	// (C35-024): reclaim it for finalize-only retry without ever resetting status away from
	// 'effects_committed' (the CASE keeps it pinned), so the caller never replays handler side
	// effects. 'failed' and stale 'processing' rows have NOT had a confirmed successful publish, so
	// they reclaim into 'processing' for a full retry, same as before.
	err := s.pool.QueryRow(ctx, `
INSERT INTO domain_event_processed_events (
  tenant_id, subscription_id, event_id, event_type, message_id, delivery_attempt,
  status, attempt_count, started_at, updated_at
) VALUES (
  $1::uuid, $2, $3, $4, $5, $6, 'processing', 1, $7::timestamptz, $7::timestamptz
)
ON CONFLICT (tenant_id, subscription_id, event_id)
DO UPDATE SET
  event_type = EXCLUDED.event_type,
  message_id = EXCLUDED.message_id,
  delivery_attempt = EXCLUDED.delivery_attempt,
  status = CASE
    WHEN domain_event_processed_events.status = 'effects_committed' THEN 'effects_committed'
    ELSE 'processing'
  END,
  attempt_count = domain_event_processed_events.attempt_count + 1,
  started_at = EXCLUDED.started_at,
  updated_at = EXCLUDED.updated_at,
  last_error = NULL
WHERE domain_event_processed_events.status = 'failed'
   OR domain_event_processed_events.status = 'effects_committed'
   OR (
     domain_event_processed_events.status = 'processing'
     AND domain_event_processed_events.updated_at <= $8::timestamptz
   )
RETURNING status`,
		event.TenantID,
		event.SubscriptionID,
		event.EventID,
		event.EventType,
		event.MessageID,
		event.DeliveryAttempt,
		event.Now,
		event.Now.Add(-defaultProcessingStaleAfter),
	).Scan(&status)
	if err == nil {
		if status == "effects_committed" {
			return consumerapp.ProcessDecisionEffectsCommitted, nil
		}
		return consumerapp.ProcessDecisionClaimed, nil
	}
	if err == pgx.ErrNoRows {
		existing, err := s.existingStatus(ctx, event)
		if err != nil {
			return "", err
		}
		switch existing {
		case "processed":
			return consumerapp.ProcessDecisionAlreadyProcessed, nil
		case "effects_committed":
			return consumerapp.ProcessDecisionEffectsCommitted, nil
		case "processing", "failed":
			return consumerapp.ProcessDecisionInProgress, nil
		default:
			return consumerapp.ProcessDecisionInProgress, nil
		}
	}
	return "", err
}

// MarkEffectsCommitted durably records that bus.Publish already succeeded for this event, before
// the terminal MarkProcessed call is attempted. It is idempotent: it accepts rows already in
// 'processing' (the normal case, right after a successful publish) or already 'effects_committed'
// (a retry of this same call, or a best-effort re-assertion from the failure-path defer).
func (s *ProcessedEventStore) MarkEffectsCommitted(ctx context.Context, event consumerapp.ProcessedEvent) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	event = normalizeProcessedEvent(event)
	tag, err := s.pool.Exec(ctx, `
UPDATE domain_event_processed_events
SET status = 'effects_committed',
    updated_at = $4::timestamptz,
    last_error = NULL
WHERE tenant_id = $1::uuid
  AND subscription_id = $2
  AND event_id = $3
  AND status IN ('processing', 'effects_committed')`, event.TenantID, event.SubscriptionID, event.EventID, event.Now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: event_id=%s subscription_id=%s", consumerapp.ErrProcessedEventFinalizationLost, event.EventID, event.SubscriptionID)
	}
	return nil
}

func (s *ProcessedEventStore) existingStatus(ctx context.Context, event consumerapp.ProcessedEvent) (string, error) {
	var status string
	err := s.pool.QueryRow(ctx, `
SELECT status
FROM domain_event_processed_events
WHERE tenant_id = $1::uuid
  AND subscription_id = $2
  AND event_id = $3`, event.TenantID, event.SubscriptionID, event.EventID).Scan(&status)
	return status, err
}

func (s *ProcessedEventStore) MarkProcessed(ctx context.Context, event consumerapp.ProcessedEvent) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	event = normalizeProcessedEvent(event)
	tag, err := s.pool.Exec(ctx, `
UPDATE domain_event_processed_events
SET status = 'processed',
    processed_at = $4::timestamptz,
    updated_at = $4::timestamptz,
    last_error = NULL
WHERE tenant_id = $1::uuid
  AND subscription_id = $2
  AND event_id = $3
  AND status IN ('processing', 'effects_committed')`, event.TenantID, event.SubscriptionID, event.EventID, event.Now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: event_id=%s subscription_id=%s", consumerapp.ErrProcessedEventFinalizationLost, event.EventID, event.SubscriptionID)
	}
	return nil
}

func (s *ProcessedEventStore) MarkFailed(ctx context.Context, event consumerapp.ProcessedEvent, reason string) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	event = normalizeProcessedEvent(event)
	reason = strings.TrimSpace(reason)
	if len(reason) > 240 {
		reason = reason[:240]
	}
	tag, err := s.pool.Exec(ctx, `
UPDATE domain_event_processed_events
SET status = 'failed',
    last_error = $4,
    updated_at = $5::timestamptz
WHERE tenant_id = $1::uuid
  AND subscription_id = $2
  AND event_id = $3
  AND status = 'processing'`, event.TenantID, event.SubscriptionID, event.EventID, reason, event.Now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: event_id=%s subscription_id=%s", consumerapp.ErrProcessedEventFinalizationLost, event.EventID, event.SubscriptionID)
	}
	return nil
}

// SweepProcessedBefore removes old processed dedupe rows in bounded batches. It never deletes
// processing or failed rows, because those rows still carry retry/repair state.
func (s *ProcessedEventStore) SweepProcessedBefore(ctx context.Context, tenantID string, before time.Time, limit int, dryRun bool) (int, error) {
	if limit < 1 {
		return 0, errors.New("limit must be positive")
	}
	if before.IsZero() {
		before = time.Now().UTC()
	} else {
		before = before.UTC()
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	tenant, err := optionalTenantUUID(tenantID)
	if err != nil {
		return 0, err
	}
	if dryRun {
		return s.countProcessedBefore(ctx, tenant, before, limit)
	}
	tag, err := s.pool.Exec(ctx, `
WITH expired AS (
  SELECT tenant_id, subscription_id, event_id
  FROM domain_event_processed_events
  WHERE status = 'processed'
    AND processed_at IS NOT NULL
    AND processed_at <= $1::timestamptz
    AND ($2::uuid IS NULL OR tenant_id = $2::uuid)
  ORDER BY processed_at ASC, tenant_id ASC, subscription_id ASC, event_id ASC
  LIMIT $3
  FOR UPDATE SKIP LOCKED
)
DELETE FROM domain_event_processed_events e
USING expired
WHERE e.tenant_id = expired.tenant_id
  AND e.subscription_id = expired.subscription_id
  AND e.event_id = expired.event_id`, before, tenant, limit)
	if err != nil {
		return 0, fmt.Errorf("domainconsumer: sweep processed events: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func (s *ProcessedEventStore) countProcessedBefore(ctx context.Context, tenant any, before time.Time, limit int) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, `
SELECT count(*)::int
FROM (
  SELECT 1
  FROM domain_event_processed_events
  WHERE status = 'processed'
    AND processed_at IS NOT NULL
    AND processed_at <= $1::timestamptz
    AND ($2::uuid IS NULL OR tenant_id = $2::uuid)
  ORDER BY processed_at ASC, tenant_id ASC, subscription_id ASC, event_id ASC
  LIMIT $3
) expired`, before, tenant, limit).Scan(&count); err != nil {
		return 0, fmt.Errorf("domainconsumer: count processed events: %w", err)
	}
	return count, nil
}

func optionalTenantUUID(tenantID string) (any, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, nil
	}
	tenant, err := pgconv.UUID(tenantID)
	if err != nil {
		return nil, fmt.Errorf("tenant-id must be a uuid: %w", err)
	}
	return tenant, nil
}

func normalizeProcessedEvent(event consumerapp.ProcessedEvent) consumerapp.ProcessedEvent {
	event.TenantID = strings.TrimSpace(event.TenantID)
	event.EventID = strings.TrimSpace(event.EventID)
	event.EventType = strings.TrimSpace(event.EventType)
	event.SubscriptionID = strings.TrimSpace(event.SubscriptionID)
	event.MessageID = strings.TrimSpace(event.MessageID)
	if event.SubscriptionID == "" {
		event.SubscriptionID = "direct"
	}
	if event.EventID == "" {
		event.EventID = event.MessageID
	}
	if event.Now.IsZero() {
		event.Now = time.Now().UTC()
	}
	event.Now = event.Now.UTC()
	return event
}
