package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/outbox/domain"
	"github.com/vgoats/goatos/backend/internal/outbox/ports"
)

var serviceTestNow = time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)

func TestRunOnceReturnsRepositoryMarkError(t *testing.T) {
	markErr := errors.New("synthetic mark published failure")
	repo := &serviceFakeRepo{
		messages:         []domain.Message{serviceTestMessage(t, 1, validServiceEnvelope(t, 1))},
		markPublishedErr: markErr,
	}
	publisher := &serviceFakePublisher{}
	service := NewService(repo, publisher, serviceTestValidator(t), Config{
		Limit:       10,
		MaxAttempts: 5,
		Now:         func() time.Time { return serviceTestNow },
	})

	result, err := service.RunOnce(context.Background())
	if !errors.Is(err, markErr) {
		t.Fatalf("RunOnce error=%v want mark error", err)
	}
	if result == nil || result.ClaimedCount != 1 || result.PublishedCount != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if publisher.callCount != 1 {
		t.Fatalf("publisher call count=%d want 1", publisher.callCount)
	}
}

func TestRunUntilDrainedProcessesMultipleBatches(t *testing.T) {
	repo := &serviceFakeRepo{
		batches: [][]domain.Message{
			{
				serviceTestMessage(t, 1, validServiceEnvelope(t, 1)),
				serviceTestMessage(t, 2, validServiceEnvelope(t, 2)),
			},
			{
				serviceTestMessage(t, 3, validServiceEnvelope(t, 3)),
			},
			nil,
		},
	}
	publisher := &serviceFakePublisher{}
	service := NewService(repo, publisher, serviceTestValidator(t), Config{
		Limit:       2,
		MaxAttempts: 5,
		Now:         func() time.Time { return serviceTestNow },
	})

	result, err := service.RunUntilDrained(context.Background())
	if err != nil {
		t.Fatalf("RunUntilDrained: %v", err)
	}
	if result.BatchesProcessed != 3 || result.ClaimedCount != 3 || result.PublishedCount != 3 {
		t.Fatalf("unexpected drain result: %#v", result)
	}
	if publisher.callCount != 3 {
		t.Fatalf("publisher call count=%d want 3", publisher.callCount)
	}
	if repo.claimCalls != 3 {
		t.Fatalf("claim calls=%d want 3", repo.claimCalls)
	}
}

func TestEnvelopeValidatorAcceptsProtocolPublished(t *testing.T) {
	eventID := serviceTestUUID("21000000", 1)
	versionID := "62000000-0000-4000-8000-000000000001"
	payload, err := json.Marshal(map[string]any{
		"event_id":        eventID,
		"event_type":      "protocol.version.published",
		"schema_version":  "1.0.0",
		"schema_ref":      "contracts/jsonschema/domain-event-envelope.schema.json#protocol.version.published",
		"aggregate_type":  "protocol_version",
		"aggregate_id":    versionID,
		"occurred_at":     serviceTestNow.Format(time.RFC3339),
		"recorded_at":     serviceTestNow.Format(time.RFC3339),
		"producer":        map[string]any{"service": "goatos-test", "module": "protocol", "version": nil},
		"idempotency_key": "protocol:version:published:" + versionID,
		"actor":           map[string]any{"actor_type": "system_rule", "actor_id": nil, "actor_ref": nil},
		"subject_type":    "protocol_version",
		"subject_id":      versionID,
		"visibility_scope": map[string]any{
			"tenant_id": "00000000-0000-4000-8000-000000000001",
		},
		"evidence_refs": []any{},
		"payload": map[string]any{
			"protocol_version_id": versionID,
			"category":            "vaccination",
		},
		"trace_id": "protocol:version:published:" + versionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := serviceTestValidator(t).Validate(payload); err != nil {
		t.Fatalf("protocol published envelope should validate: %v", err)
	}
}

func TestEnvelopeValidatorAcceptsProtocolRetired(t *testing.T) {
	eventID := serviceTestUUID("21000000", 2)
	versionID := "62000000-0000-4000-8000-000000000002"
	replacedByID := "62000000-0000-4000-8000-000000000003"
	payload, err := json.Marshal(map[string]any{
		"event_id":        eventID,
		"event_type":      "protocol.version.retired",
		"schema_version":  "1.0.0",
		"schema_ref":      "contracts/jsonschema/domain-event-envelope.schema.json#protocol.version.retired",
		"aggregate_type":  "protocol_version",
		"aggregate_id":    versionID,
		"occurred_at":     serviceTestNow.Format(time.RFC3339),
		"recorded_at":     serviceTestNow.Format(time.RFC3339),
		"producer":        map[string]any{"service": "goatos-test", "module": "protocol", "version": nil},
		"idempotency_key": "protocol:version:retired:" + versionID + ":by:" + replacedByID,
		"actor":           map[string]any{"actor_type": "system_rule", "actor_id": nil, "actor_ref": nil},
		"subject_type":    "protocol_version",
		"subject_id":      versionID,
		"visibility_scope": map[string]any{
			"tenant_id": "00000000-0000-4000-8000-000000000001",
		},
		"evidence_refs": []any{},
		"payload": map[string]any{
			"protocol_version_id":             versionID,
			"category":                        "vaccination",
			"replaced_by_protocol_version_id": replacedByID,
		},
		"trace_id": "protocol:version:retired:" + versionID + ":by:" + replacedByID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := serviceTestValidator(t).Validate(payload); err != nil {
		t.Fatalf("protocol retired envelope should validate: %v", err)
	}
}

func TestEnvelopeValidatorAcceptsOperationalKernelEvents(t *testing.T) {
	validator := serviceTestValidator(t)
	tests := []struct {
		name          string
		eventType     string
		aggregateType string
		subjectType   string
		aggregateID   string
		subjectID     string
		evidenceRefs  []any
	}{
		{
			name:          "calendar escalation queued",
			eventType:     "calendar.escalation.queued",
			aggregateType: "calendar_notification",
			subjectType:   "calendar_event",
			aggregateID:   "63000000-0000-4000-8000-000000000001",
			subjectID:     "obligation:64000000-0000-4000-8000-000000000001",
			evidenceRefs:  []any{map[string]any{"evidence_type": "event", "evidence_id": "obligation:64000000-0000-4000-8000-000000000001"}},
		},
		{
			name:          "obligation missed",
			eventType:     "obligation.missed",
			aggregateType: "obligation_instance",
			subjectType:   "obligation_instance",
			aggregateID:   "64000000-0000-4000-8000-000000000002",
			subjectID:     "64000000-0000-4000-8000-000000000002",
			evidenceRefs:  []any{map[string]any{"evidence_type": "obligation_status_event", "evidence_id": "64000000-0000-4000-8000-000000000002:missed"}},
		},
		{
			name:          "notification exhausted",
			eventType:     "notification.exhausted",
			aggregateType: "calendar_notification",
			subjectType:   "calendar_event",
			aggregateID:   "63000000-0000-4000-8000-000000000002",
			subjectID:     "obligation:64000000-0000-4000-8000-000000000002",
			evidenceRefs:  []any{map[string]any{"evidence_type": "notification_request", "evidence_id": "63000000-0000-4000-8000-000000000002:exhausted"}},
		},
		{
			name:          "vaccination completed",
			eventType:     "vaccination.completed",
			aggregateType: "obligation_instance",
			subjectType:   "obligation_instance",
			aggregateID:   "64000000-0000-4000-8000-000000000003",
			subjectID:     "64000000-0000-4000-8000-000000000003",
			evidenceRefs:  []any{map[string]any{"evidence_type": "obligation_status_event", "evidence_id": "64000000-0000-4000-8000-000000000003:completed"}},
		},
		{
			name:          "counts base count anchor recorded",
			eventType:     "counts.base_count_anchor.recorded",
			aggregateType: "count_base_anchor",
			subjectType:   "count_base_anchor",
			aggregateID:   "65000000-0000-4000-8000-000000000001",
			subjectID:     "65000000-0000-4000-8000-000000000001",
			evidenceRefs:  []any{map[string]any{"evidence_type": "count_base_anchor", "evidence_id": "65000000-0000-4000-8000-000000000001"}},
		},
		{
			name:          "counts shifting event recorded",
			eventType:     "counts.shifting_event.recorded",
			aggregateType: "shifting_event",
			subjectType:   "shifting_event",
			aggregateID:   "65000000-0000-4000-8000-000000000002",
			subjectID:     "65000000-0000-4000-8000-000000000002",
			evidenceRefs:  []any{map[string]any{"evidence_type": "shifting_event", "evidence_id": "65000000-0000-4000-8000-000000000002"}},
		},
	}
	for idx, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventID := serviceTestUUID("22000000", idx+1)
			payload, err := json.Marshal(map[string]any{
				"event_id":        eventID,
				"event_type":      tt.eventType,
				"schema_version":  "1.0.0",
				"schema_ref":      "contracts/jsonschema/domain-event-envelope.schema.json#" + tt.eventType,
				"aggregate_type":  tt.aggregateType,
				"aggregate_id":    tt.aggregateID,
				"occurred_at":     serviceTestNow.Format(time.RFC3339),
				"recorded_at":     serviceTestNow.Format(time.RFC3339),
				"producer":        map[string]any{"service": "goatos-test", "module": "operational-kernel", "version": nil},
				"idempotency_key": "operational-kernel-test:" + eventID,
				"actor":           map[string]any{"actor_type": "system_rule", "actor_id": nil, "actor_ref": nil},
				"subject_type":    tt.subjectType,
				"subject_id":      tt.subjectID,
				"visibility_scope": map[string]any{
					"tenant_id": "00000000-0000-4000-8000-000000000001",
				},
				"evidence_refs": tt.evidenceRefs,
				"payload":       map[string]any{"status": "queued"},
				"trace_id":      "operational-kernel-test:" + eventID,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := validator.Validate(payload); err != nil {
				t.Fatalf("%s envelope should validate: %v", tt.eventType, err)
			}
		})
	}
}

func serviceTestMessage(t *testing.T, suffix int, payload json.RawMessage) domain.Message {
	t.Helper()
	eventID := serviceTestUUID("20000000", suffix)
	return domain.Message{
		OutboxID:       serviceTestUUID("10000000", suffix),
		TenantID:       "00000000-0000-4000-8000-000000000001",
		EventID:        eventID,
		EventType:      "goat.created",
		SchemaVersion:  "1.0.0",
		AggregateType:  "goat",
		AggregateID:    "91000000-0000-4000-8000-000000000001",
		Topic:          "identity.events",
		Headers:        json.RawMessage(`{"source":"outbox-service-test"}`),
		Payload:        payload,
		IdempotencyKey: "outbox-service-test-" + eventID,
		AttemptCount:   1,
		CreatedAt:      serviceTestNow,
		UpdatedAt:      serviceTestNow,
	}
}

func validServiceEnvelope(t *testing.T, suffix int) json.RawMessage {
	t.Helper()
	eventID := serviceTestUUID("20000000", suffix)
	envelope := map[string]any{
		"event_id":        eventID,
		"event_type":      "goat.created",
		"schema_version":  "1.0.0",
		"schema_ref":      "contracts/jsonschema/domain-event-envelope.schema.json",
		"aggregate_type":  "goat",
		"aggregate_id":    "91000000-0000-4000-8000-000000000001",
		"occurred_at":     serviceTestNow.Format(time.RFC3339),
		"recorded_at":     serviceTestNow.Format(time.RFC3339),
		"producer":        map[string]any{"service": "goatos-test", "module": "outbox"},
		"idempotency_key": "outbox-service-test-" + eventID,
		"actor":           map[string]any{"actor_type": "system_rule", "actor_id": nil, "actor_ref": "outbox-service-test"},
		"subject_type":    "goat",
		"subject_id":      "91000000-0000-4000-8000-000000000001",
		"visibility_scope": map[string]any{
			"tenant_id": "00000000-0000-4000-8000-000000000001",
		},
		"evidence_refs": []any{},
		"payload":       map[string]any{"source": "synthetic_outbox_service_test"},
		"trace_id":      "trace-outbox-service-test",
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func serviceTestValidator(t *testing.T) *EnvelopeValidator {
	t.Helper()
	validator, err := NewEnvelopeValidator(filepath.Join(serviceRepoRoot(t), "contracts", "jsonschema", "domain-event-envelope.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	return validator
}

func serviceRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(dir)
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("repo root not found")
		}
		dir = next
	}
}

func serviceTestUUID(prefix string, suffix int) string {
	return fmt.Sprintf("%s-0000-4000-8000-%012d", prefix, suffix)
}

type serviceFakeRepo struct {
	messages         []domain.Message
	batches          [][]domain.Message
	claimCalls       int
	markPublishedErr error
}

func (r *serviceFakeRepo) ReclaimStalePublishing(context.Context, time.Time, time.Duration) (int64, error) {
	return 0, nil
}

func (r *serviceFakeRepo) ClaimPending(context.Context, ports.ClaimParams) (*ports.ClaimResult, error) {
	r.claimCalls++
	if len(r.batches) > 0 {
		messages := r.batches[0]
		r.batches = r.batches[1:]
		return &ports.ClaimResult{Messages: messages}, nil
	}
	return &ports.ClaimResult{Messages: r.messages}, nil
}

func (r *serviceFakeRepo) MarkPublished(context.Context, string, time.Time) error {
	return r.markPublishedErr
}

func (r *serviceFakeRepo) MarkRetry(context.Context, string, time.Time, string, time.Time) error {
	return nil
}

func (r *serviceFakeRepo) MarkFailed(context.Context, string, string, time.Time) error {
	return nil
}

func (r *serviceFakeRepo) MarkDeadLetter(context.Context, string, string, time.Time) error {
	return nil
}

func (r *serviceFakeRepo) ListDeadLetters(context.Context, ports.DeadLetterQuery) ([]domain.DeadLetterMessage, error) {
	return nil, nil
}

func (r *serviceFakeRepo) Health(context.Context, string, time.Time) (domain.Health, error) {
	return domain.Health{Status: "healthy"}, nil
}

func (r *serviceFakeRepo) ReplayDeadLetters(context.Context, ports.ReplayDeadLettersParams) (ports.DLQActionResult, error) {
	return ports.DLQActionResult{}, nil
}

func (r *serviceFakeRepo) DiscardDeadLetters(context.Context, ports.DiscardDeadLettersParams) (ports.DLQActionResult, error) {
	return ports.DLQActionResult{}, nil
}

func (r *serviceFakeRepo) Ping(context.Context) error {
	return nil
}

type serviceFakePublisher struct {
	callCount int
}

func (p *serviceFakePublisher) Publish(context.Context, ports.PublishMessage) error {
	p.callCount++
	return nil
}
