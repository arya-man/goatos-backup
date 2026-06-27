package eventbus

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventFromEnvelope(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"event_type":   "goat.location.changed",
		"aggregate_id": "10000000-0000-4000-8000-000000000001",
		"occurred_at":  "2026-06-27T01:02:03.000000Z",
		"visibility_scope": map[string]any{
			"tenant_id": "00000000-0000-4000-8000-000000000001",
		},
		"payload": map[string]any{
			"scope_type": "shed",
			"scope_id":   "00000000-0000-4000-8000-000000004001",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	event, err := EventFromEnvelope(raw, Event{})
	if err != nil {
		t.Fatalf("EventFromEnvelope: %v", err)
	}
	if event.Type != "goat.location.changed" || event.Key != "10000000-0000-4000-8000-000000000001" || event.TenantID == "" {
		t.Fatalf("unexpected event: %#v", event)
	}
	if event.OccurredAt.Format(time.RFC3339) != "2026-06-27T01:02:03Z" {
		t.Fatalf("occurred_at = %s", event.OccurredAt)
	}
	if string(event.Payload) != `{"scope_id":"00000000-0000-4000-8000-000000004001","scope_type":"shed"}` {
		t.Fatalf("payload = %s", event.Payload)
	}
}

func TestEventFromEnvelopeFallsBackToTransportAttributes(t *testing.T) {
	event, err := EventFromEnvelope([]byte(`{"payload":{"ok":true}}`), Event{
		Type:     "goat.created",
		TenantID: "tenant-1",
		Key:      "goat-1",
	})
	if err != nil {
		t.Fatalf("EventFromEnvelope: %v", err)
	}
	if event.Type != "goat.created" || event.TenantID != "tenant-1" || event.Key != "goat-1" {
		t.Fatalf("fallback failed: %#v", event)
	}
}
