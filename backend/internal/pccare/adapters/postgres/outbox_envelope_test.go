package postgres

import (
	"encoding/json"
	"path/filepath"
	"testing"

	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
)

func pcCareEnvelopeValidator(t *testing.T) *outboxapp.EnvelopeValidator {
	t.Helper()
	schema := filepath.Join("..", "..", "..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json")
	validator, err := outboxapp.NewEnvelopeValidator(schema)
	if err != nil {
		t.Fatalf("compile production domain-event envelope schema: %v", err)
	}
	return validator
}

func TestPCCareOutboxEnvelopesValidateAgainstProductionSchema(t *testing.T) {
	t.Parallel()
	validator := pcCareEnvelopeValidator(t)

	const (
		tenant   = "00000000-0000-4000-8000-000000000001"
		park     = "00000000-0000-4000-8000-000000003001"
		shed     = "00000000-0000-4000-8000-000000004001"
		task     = "00000000-0000-4000-8000-000000009001"
		operator = "00000000-0000-4000-8000-000000005001"
		eventID  = "00000000-0000-4000-8000-00000000e001"
	)

	cases := []struct {
		name          string
		eventType     string
		schemaVersion string
		schemaRef     string
		aggregateType string
		actorID       string
	}{
		{
			name:          "pending verification with proof media",
			eventType:     pcCarePendingVerificationEventType,
			schemaVersion: pcCarePendingVerificationSchemaVersion,
			schemaRef:     pcCarePendingVerificationSchemaRef,
			aggregateType: pcCarePendingVerificationAggregateType,
			actorID:       operator,
		},
		{
			name:          "completed after verifier approval",
			eventType:     pcCareTaskCompletedEventType,
			schemaVersion: pcCareTaskCompletedSchemaVersion,
			schemaRef:     pcCareTaskCompletedSchemaRef,
			aggregateType: pcCareTaskCompletedAggregateType,
			actorID:       operator,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelope := pcCareEventEnvelope{
				EventID:        eventID,
				EventType:      tc.eventType,
				SchemaVersion:  tc.schemaVersion,
				SchemaRef:      tc.schemaRef,
				AggregateType:  tc.aggregateType,
				AggregateID:    task,
				IdempotencyKey: tc.eventType + ":" + task,
				TenantID:       tenant,
				ParkID:         park,
				ShedID:         shed,
				ActorID:        tc.actorID,
				OccurredAt:     businessInstant("2026-08-26"),
				Payload: map[string]any{
					"task_id":               task,
					"category":              "inventory_vaccine",
					"park_id":               park,
					"shed_id":               shed,
					"planned_business_date": "2026-08-26",
					"media_refs": []map[string]string{
						{"label": "Fridge stock proof", "proof_ref": "proof-1"},
					},
				},
				TraceID: "trace-1",
			}.build()

			raw, err := json.Marshal(envelope)
			if err != nil {
				t.Fatalf("marshal envelope: %v", err)
			}
			if err := validator.Validate(raw); err != nil {
				t.Fatalf("relay would reject PC Care envelope as invalid_event_envelope:\n%v\n\npayload: %s", err, raw)
			}
			if got := envelope["aggregate_type"]; got != "verification_item" {
				t.Errorf("aggregate_type = %v, want verification_item (the envelope schema has no pc_care_task aggregate)", got)
			}
			if got := envelope["subject_type"]; got != "location" {
				t.Errorf("subject_type = %v, want location", got)
			}
		})
	}
}
