package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	consumerapp "github.com/vgoats/goatos/backend/internal/domainconsumer/app"
)

const defaultQueryTimeout = 3 * time.Second
const processingStaleAfter = 15 * time.Minute

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
	staleBefore := event.Now.Add(-processingStaleAfter)
	var status string
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
  status = 'processing',
  attempt_count = domain_event_processed_events.attempt_count + 1,
  started_at = EXCLUDED.started_at,
  updated_at = EXCLUDED.updated_at,
  last_error = NULL
WHERE domain_event_processed_events.status = 'failed'
   OR (
     domain_event_processed_events.status = 'processing'
     AND domain_event_processed_events.started_at <= $8::timestamptz
   )
RETURNING status`,
		event.TenantID,
		event.SubscriptionID,
		event.EventID,
		event.EventType,
		event.MessageID,
		event.DeliveryAttempt,
		event.Now,
		staleBefore,
	).Scan(&status)
	if err == nil {
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
		case "processing", "failed":
			return consumerapp.ProcessDecisionInProgress, nil
		default:
			return consumerapp.ProcessDecisionInProgress, nil
		}
	}
	return "", err
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
	_, err := s.pool.Exec(ctx, `
UPDATE domain_event_processed_events
SET status = 'processed',
    processed_at = $4::timestamptz,
    updated_at = $4::timestamptz,
    last_error = NULL
WHERE tenant_id = $1::uuid
  AND subscription_id = $2
  AND event_id = $3
  AND status = 'processing'`, event.TenantID, event.SubscriptionID, event.EventID, event.Now)
	return err
}

func (s *ProcessedEventStore) MarkFailed(ctx context.Context, event consumerapp.ProcessedEvent, reason string) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	event = normalizeProcessedEvent(event)
	reason = strings.TrimSpace(reason)
	if len(reason) > 240 {
		reason = reason[:240]
	}
	_, err := s.pool.Exec(ctx, `
UPDATE domain_event_processed_events
SET status = 'failed',
    last_error = $4,
    updated_at = $5::timestamptz
WHERE tenant_id = $1::uuid
  AND subscription_id = $2
  AND event_id = $3
  AND status = 'processing'`, event.TenantID, event.SubscriptionID, event.EventID, reason, event.Now)
	return err
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
