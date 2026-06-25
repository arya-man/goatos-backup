package eventbuspublisher

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/outbox/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

type Publisher struct {
	bus eventbus.Bus
}

func New(bus eventbus.Bus) *Publisher {
	return &Publisher{bus: bus}
}

var _ ports.Publisher = (*Publisher)(nil)

type envelope struct {
	EventType       string          `json:"event_type"`
	AggregateID     string          `json:"aggregate_id"`
	OccurredAt      string          `json:"occurred_at"`
	Payload         json.RawMessage `json:"payload"`
	VisibilityScope map[string]any  `json:"visibility_scope"`
}

func (p *Publisher) Publish(ctx context.Context, message ports.PublishMessage) error {
	if p == nil || p.bus == nil {
		return ports.PermanentPublishError(fmt.Errorf("eventbus publisher is not configured"))
	}
	var env envelope
	if err := json.Unmarshal(message.Payload, &env); err != nil {
		return ports.PermanentPublishError(fmt.Errorf("decode domain event envelope: %w", err))
	}
	eventType := env.EventType
	if eventType == "" {
		eventType = message.EventType
	}
	tenantID, _ := env.VisibilityScope["tenant_id"].(string)
	if tenantID == "" {
		tenantID = message.TenantID
	}
	occurredAt := time.Now().UTC()
	if env.OccurredAt != "" {
		if parsed, err := time.Parse("2006-01-02T15:04:05.000000Z", env.OccurredAt); err == nil {
			occurredAt = parsed
		} else if parsed, err := time.Parse(time.RFC3339, env.OccurredAt); err == nil {
			occurredAt = parsed
		}
	}
	payload := env.Payload
	if len(payload) == 0 {
		payload = message.Payload
	}
	if err := p.bus.Publish(ctx, eventbus.Event{
		Type:       eventType,
		TenantID:   tenantID,
		Key:        env.AggregateID,
		Payload:    payload,
		OccurredAt: occurredAt,
	}); err != nil {
		return ports.RetryablePublishError(fmt.Errorf("eventbus dispatch %s/%s: %w", eventType, env.AggregateID, err))
	}
	return nil
}
