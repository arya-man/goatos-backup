package eventbus

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type domainEventEnvelope struct {
	EventID         string          `json:"event_id"`
	EventType       string          `json:"event_type"`
	AggregateID     string          `json:"aggregate_id"`
	OccurredAt      string          `json:"occurred_at"`
	RecordedAt      string          `json:"recorded_at"`
	Payload         json.RawMessage `json:"payload"`
	VisibilityScope map[string]any  `json:"visibility_scope"`
}

// EventFromEnvelope decodes the shared domain-event envelope into an eventbus event.
// Fallback values let transport attributes supply tenant/type when older payloads omit them.
func EventFromEnvelope(payload []byte, fallback Event) (Event, error) {
	var env domainEventEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return Event{}, fmt.Errorf("decode domain event envelope: %w", err)
	}
	eventType := env.EventType
	if eventType == "" {
		eventType = fallback.Type
	}
	eventID := strings.TrimSpace(env.EventID)
	if eventID == "" {
		eventID = strings.TrimSpace(fallback.ID)
	}
	tenantID, _ := env.VisibilityScope["tenant_id"].(string)
	if tenantID == "" {
		tenantID = fallback.TenantID
	}
	key := env.AggregateID
	if key == "" {
		key = fallback.Key
	}
	occurredAt := fallback.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if env.OccurredAt != "" {
		if parsed, err := time.Parse("2006-01-02T15:04:05.000000Z", env.OccurredAt); err == nil {
			occurredAt = parsed
		} else if parsed, err := time.Parse(time.RFC3339, env.OccurredAt); err == nil {
			occurredAt = parsed
		}
	}
	var recordedAt time.Time
	if env.RecordedAt != "" {
		if parsed, err := time.Parse("2006-01-02T15:04:05.000000Z", env.RecordedAt); err == nil {
			recordedAt = parsed
		} else if parsed, err := time.Parse(time.RFC3339, env.RecordedAt); err == nil {
			recordedAt = parsed
		}
	}
	eventPayload := env.Payload
	if len(eventPayload) == 0 {
		eventPayload = payload
	}
	return Event{
		ID:         eventID,
		Type:       eventType,
		TenantID:   tenantID,
		Key:        key,
		Payload:    eventPayload,
		OccurredAt: occurredAt,
		RecordedAt: recordedAt,
	}, nil
}
