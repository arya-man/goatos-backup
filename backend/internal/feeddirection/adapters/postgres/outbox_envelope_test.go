package postgres

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
)

// THE GUARD WHOSE ABSENCE LET THIS SHIP WRONG TWICE.
//
// Nothing validated a produced feed envelope against contracts/jsonschema/domain-event-envelope
// .schema.json. As a result all three feed producers -- feed.packing.completed,
// feed.distribution.completed and the inert feed.direction.completed -- emitted envelopes the relay
// could not accept, and the first repair fixed the MISSING FIELDS while leaving the VALUES
// (aggregate_type, subject_type, actor_type) outside their enums, so they were still rejected.
//
// Why it survived so long: the failure is invisible at the write. The outbox INSERT succeeds, the
// transaction commits, the operator's screen flips to completed, and the envelope is only rejected
// LATER by the relay as `invalid_event_envelope`. Nothing on any screen says the event never
// arrived. `make domain-event-architecture-guard` passes throughout, because it matches literal
// event-type strings and the INSERT -- not whether the payload is valid.
//
// These tests compile the REAL schema with the REAL production validator
// (outboxapp.NewEnvelopeValidator, the exact type the relay is constructed with), so the check
// cannot drift from what production enforces.
func feedEnvelopeValidator(t *testing.T) *outboxapp.EnvelopeValidator {
	t.Helper()
	schema := filepath.Join("..", "..", "..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json")
	v, err := outboxapp.NewEnvelopeValidator(schema)
	if err != nil {
		t.Fatalf("compile the production domain-event envelope schema: %v", err)
	}
	return v
}

// TestFeedOutboxEnvelopesValidateAgainstTheProductionSchema is the regression: every feed envelope
// the module can emit must satisfy the contract the relay enforces.
func TestFeedOutboxEnvelopesValidateAgainstTheProductionSchema(t *testing.T) {
	t.Parallel()
	validator := feedEnvelopeValidator(t)

	const (
		tenant     = "00000000-0000-4000-8000-000000000001"
		park       = "00000000-0000-4000-8000-000000003001"
		shed       = "00000000-0000-4000-8000-000000004001"
		completion = "00000000-0000-4000-8000-000000009001"
		verifier   = "00000000-0000-4000-8000-000000005002"
		eventID    = "00000000-0000-4000-8000-00000000e001"
	)

	cases := []struct {
		name          string
		eventType     string
		aggregateType string
		actorID       string
	}{
		{
			name:          "feed.packing.completed after a verifier approval",
			eventType:     feedPackingCompletedEventType,
			aggregateType: feedPackingCompletedAggregateType,
			actorID:       verifier,
		},
		{
			name:          "feed.distribution.completed after a verifier approval",
			eventType:     feedDistributionCompletedEventType,
			aggregateType: feedDistributionCompletedAggregateType,
			actorID:       verifier,
		},
		{
			// The inert pre-gate path. It is validated too: leaving one broken copy behind is how the
			// next author learns the wrong shape from the codebase.
			name:          "feed.direction.completed (inert path)",
			eventType:     feedDirectionCompletedEventType,
			aggregateType: feedDirectionCompletedAggregateType,
			actorID:       "",
		},
		{
			// A SYSTEM-attributed event: no verifier id, so the builder must emit actor_type
			// "system_rule". "system" reads perfectly plausibly and is NOT in the enum -- a wrong value
			// fails validation exactly as a missing field does, which is what the first repair missed.
			name:          "an event with no human actor",
			eventType:     feedPackingCompletedEventType,
			aggregateType: feedPackingCompletedAggregateType,
			actorID:       "",
		},
		{
			// The afternoon correction's reopen (2026-08-29). Always system-attributed -- no human
			// presses anything -- and its event_type must be in the schema enum or the packer's push
			// silently never fires (the exact invisible-at-the-write failure this file exists for).
			name:          "feed.packing.reopened by the afternoon correction",
			eventType:     feedPackingReopenedEventType,
			aggregateType: feedPackingCompletedAggregateType,
			actorID:       "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			envelope := feedEventEnvelope{
				EventID: eventID, EventType: tc.eventType,
				SchemaVersion: "1.0.0", SchemaRef: "domain-event-envelope.v1",
				AggregateType: tc.aggregateType, AggregateID: completion,
				IdempotencyKey: tc.eventType + ":" + completion,
				TenantID:       tenant, ParkID: park, ShedID: shed,
				ActorID:    tc.actorID,
				OccurredAt: businessInstant("2026-07-30"),
				Payload:    map[string]any{"completion_id": completion, "workflow": "normal"},
				TraceID:    "trace-1",
			}.build()

			raw, err := json.Marshal(envelope)
			if err != nil {
				t.Fatalf("marshal envelope: %v", err)
			}
			if err := validator.Validate(raw); err != nil {
				t.Fatalf("the relay would REJECT this envelope as invalid_event_envelope, so the event "+
					"would never reach a consumer:\n%v\n\npayload: %s", err, raw)
			}

			// The specific values that were wrong, asserted by NAME as well as by validity, so a future
			// schema that happens to widen its enums cannot quietly re-admit the old vocabulary.
			if got := envelope["subject_type"]; got != "location" {
				t.Errorf("subject_type = %v, want location (the enum has no \"shed\"; a shed IS a location row)", got)
			}
			actor, _ := envelope["actor"].(map[string]any)
			wantActor := "system_rule"
			if tc.actorID != "" {
				wantActor = "human"
			}
			if got := actor["actor_type"]; got != wantActor {
				t.Errorf("actor_type = %v, want %s", got, wantActor)
			}
		})
	}
}

// TestFeedOutboxEnvelopeCarriesEverySchemaRequiredField states the seventeen required fields
// independently of the schema file, so deleting one from the builder fails HERE with a name rather
// than only inside a validator error message.
func TestFeedOutboxEnvelopeCarriesEverySchemaRequiredField(t *testing.T) {
	t.Parallel()

	envelope := feedEventEnvelope{
		EventID: "00000000-0000-4000-8000-00000000e001", EventType: feedPackingCompletedEventType,
		SchemaVersion: "1.0.0", SchemaRef: "domain-event-envelope.v1",
		AggregateType:  feedPackingCompletedAggregateType,
		AggregateID:    "00000000-0000-4000-8000-000000009001",
		IdempotencyKey: "k",
		TenantID:       "00000000-0000-4000-8000-000000000001",
		Payload:        map[string]any{},
	}.build()

	// The five that were MISSING from all three hand-built copies are listed first, because those are
	// the ones a future hand-rolled envelope will forget again.
	for _, field := range []string{
		"occurred_at", "recorded_at", "actor", "visibility_scope", "evidence_refs",
		"event_id", "event_type", "schema_version", "schema_ref", "aggregate_type", "aggregate_id",
		"producer", "idempotency_key", "subject_type", "subject_id", "payload", "trace_id",
	} {
		if _, ok := envelope[field]; !ok {
			t.Errorf("envelope is missing the schema-required field %q", field)
		}
	}

	// producer is an OBJECT, not a string. All three copies spelled it "feeddirection".
	if _, ok := envelope["producer"].(map[string]any); !ok {
		t.Errorf("producer = %#v, want an object {service, module, version}", envelope["producer"])
	}
}

// TestFeedOutboxEnvelopeOmitsAnAbsentScope: an event with no park or shed must OMIT those keys
// rather than send empty strings, or a consumer filtering on scope matches "" against a real id.
func TestFeedOutboxEnvelopeOmitsAnAbsentScope(t *testing.T) {
	t.Parallel()

	envelope := feedEventEnvelope{
		EventID: "00000000-0000-4000-8000-00000000e001", EventType: feedPackingCompletedEventType,
		SchemaVersion: "1.0.0", SchemaRef: "domain-event-envelope.v1",
		AggregateType:  feedPackingCompletedAggregateType,
		AggregateID:    "00000000-0000-4000-8000-000000009001",
		IdempotencyKey: "k",
		TenantID:       "00000000-0000-4000-8000-000000000001",
		Payload:        map[string]any{},
	}.build()

	scope, _ := envelope["visibility_scope"].(map[string]any)
	if _, present := scope["park_id"]; present {
		t.Errorf("visibility_scope carries an empty park_id: %#v", scope)
	}
	if _, present := scope["shed_id"]; present {
		t.Errorf("visibility_scope carries an empty shed_id: %#v", scope)
	}
	if scope["tenant_id"] == "" || scope["tenant_id"] == nil {
		t.Errorf("visibility_scope must always carry the tenant: %#v", scope)
	}
}

// TestFeedOutboxEnvelopeStampsTheBusinessDay: occurred_at is the FEED DAY, not the moment the
// verdict landed. Every downstream time bucket reads that field, and a feed event bucketed by the
// instant a verifier happened to press approve reports the wrong day's work.
func TestFeedOutboxEnvelopeStampsTheBusinessDay(t *testing.T) {
	t.Parallel()

	envelope := feedEventEnvelope{
		EventID: "00000000-0000-4000-8000-00000000e001", EventType: feedPackingCompletedEventType,
		SchemaVersion: "1.0.0", SchemaRef: "domain-event-envelope.v1",
		AggregateType:  feedPackingCompletedAggregateType,
		AggregateID:    "00000000-0000-4000-8000-000000009001",
		IdempotencyKey: "k",
		TenantID:       "00000000-0000-4000-8000-000000000001",
		Payload:        map[string]any{},
		// 2026-07-30 00:00 IST is 2026-07-29T18:30:00Z. Asserting the UTC form proves the India
		// business day was resolved rather than a UTC midnight being assumed.
		OccurredAt: businessInstant("2026-07-30"),
	}.build()

	occurred, _ := envelope["occurred_at"].(string)
	if !strings.HasPrefix(occurred, "2026-07-29T18:30:00") {
		t.Fatalf("occurred_at = %q, want the 2026-07-30 India business day (2026-07-29T18:30:00Z)", occurred)
	}

	// A zero OccurredAt must fall back to a real instant, never to the Go zero time, which would
	// bucket every such event into year 1.
	fallback := feedEventEnvelope{
		EventID: "00000000-0000-4000-8000-00000000e001", EventType: feedPackingCompletedEventType,
		SchemaVersion: "1.0.0", SchemaRef: "domain-event-envelope.v1",
		AggregateType:  feedPackingCompletedAggregateType,
		AggregateID:    "00000000-0000-4000-8000-000000009001",
		IdempotencyKey: "k",
		TenantID:       "00000000-0000-4000-8000-000000000001",
		Payload:        map[string]any{},
	}.build()
	if stamp, _ := fallback["occurred_at"].(string); strings.HasPrefix(stamp, "0001-") {
		t.Fatalf("occurred_at fell back to the Go zero time: %q", stamp)
	}
}
