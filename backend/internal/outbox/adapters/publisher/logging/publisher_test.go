package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/outbox/ports"
)

func TestPublisherLogsOnlySafeMetadata(t *testing.T) {
	var buf bytes.Buffer
	publisher := NewPublisher(slog.New(slog.NewTextHandler(&buf, nil)))
	traceID := "trace-outbox-test"
	payload := json.RawMessage(`{"animal_identifier_1":"AID-SYNTHETIC-001","animal_identifier_2":"AID-SYNTHETIC-002"}`)

	if err := publisher.Publish(context.Background(), ports.PublishMessage{
		OutboxID:  "10000000-0000-4000-8000-000000000001",
		TenantID:  "00000000-0000-4000-8000-000000000001",
		EventID:   "20000000-0000-4000-8000-000000000001",
		EventType: "goat.created",
		Topic:     "identity.events",
		Payload:   payload,
		TraceID:   &traceID,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	logs := buf.String()
	for _, forbidden := range []string{"9900000000000000000000000000001", "SYNTHETIC_PRIVATE_TAG", string(payload)} {
		if strings.Contains(logs, forbidden) {
			t.Fatalf("log leaked payload content %q in %s", forbidden, logs)
		}
	}
	for _, want := range []string{"goat.created", "identity.events", "trace-outbox-test"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("log missing safe metadata %q in %s", want, logs)
		}
	}
}
