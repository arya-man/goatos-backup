package eventbuspublisher

import (
	"context"
	"fmt"

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

func (p *Publisher) Publish(ctx context.Context, message ports.PublishMessage) error {
	if p == nil || p.bus == nil {
		return ports.PermanentPublishError(fmt.Errorf("eventbus publisher is not configured"))
	}
	event, err := eventbus.EventFromEnvelope(message.Payload, eventbus.Event{
		Type:     message.EventType,
		TenantID: message.TenantID,
	})
	if err != nil {
		return ports.PermanentPublishError(err)
	}
	if err := p.bus.Publish(ctx, event); err != nil {
		return ports.RetryablePublishError(fmt.Errorf("eventbus dispatch %s/%s: %w", event.Type, event.Key, err))
	}
	return nil
}
