package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	consumerpg "github.com/vgoats/goatos/backend/internal/domainconsumer/adapters/postgres"
	consumerapp "github.com/vgoats/goatos/backend/internal/domainconsumer/app"
	consumerrepair "github.com/vgoats/goatos/backend/internal/domainconsumer/repair"
	consumerwiring "github.com/vgoats/goatos/backend/internal/domainconsumer/wiring"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	platformaudit "github.com/vgoats/goatos/backend/internal/platform/audit"
)

type failProcessedFinalizationOnce struct {
	inner consumerapp.ProcessedEventStore
	fail  bool
}

type repairStoryDelivery struct {
	message consumerrepair.Message
	acked   bool
	nacked  bool
}

func (d *repairStoryDelivery) Message() consumerrepair.Message { return d.message }
func (d *repairStoryDelivery) Ack(context.Context) error       { d.acked = true; return nil }
func (d *repairStoryDelivery) Nack(context.Context) error      { d.nacked = true; return nil }

type repairStoryPublisher struct{ consumer *consumerapp.Service }

func (p repairStoryPublisher) Publish(ctx context.Context, message consumerrepair.Message) error {
	return p.consumer.HandleMessage(ctx, consumerapp.Message{
		ID: message.BrokerID, Data: message.Data, Attributes: message.Attributes, DeliveryAttempt: 1,
	})
}

func (s *failProcessedFinalizationOnce) BeginProcessing(ctx context.Context, event consumerapp.ProcessedEvent) (consumerapp.ProcessDecision, error) {
	return s.inner.BeginProcessing(ctx, event)
}

func (s *failProcessedFinalizationOnce) MarkProcessed(ctx context.Context, event consumerapp.ProcessedEvent) error {
	if s.fail {
		s.fail = false
		return consumerapp.ErrProcessedEventFinalizationLost
	}
	return s.inner.MarkProcessed(ctx, event)
}

func (s *failProcessedFinalizationOnce) MarkFailed(ctx context.Context, event consumerapp.ProcessedEvent, reason string) error {
	return s.inner.MarkFailed(ctx, event, reason)
}

// TestKernelStoryAI_DomainConsumerNacksAndSafelyReplaysFinalizationFailure proves the exact gap that
// used to ACK success: the business handler succeeds, processed-event finalization fails, the
// service returns an error for broker NACK, durable failure evidence is written, and redelivery
// replays idempotently before the event reaches terminal processed state.
func TestKernelStoryAI_DomainConsumerNacksAndSafelyReplaysFinalizationFailure(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-ai", "Consumer finalization failure NACKs and retries without duplicate work",
		"A real identity health event reaches the production vaccination recheck handler. The first processed-event finalization is fault-injected to fail after dispatch; GoatOS records the failure and returns an error for Pub/Sub NACK. Redelivery completes without duplicating the obligation.")
	story.Certify("backend kernel + real Postgres processed-event store/audit + domain-consumer DLQ repair service")
	defer story.Finish()

	const (
		shedID  = "83000000-0000-4000-8000-000000000001"
		stageID = "83000000-0000-4000-8000-000000000002"
		goatID  = "83000000-0000-4000-8000-000000000003"
	)
	fx.SeedShed(shedID, "E2E-AI", stageID)
	dob := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob})
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_ai", 21, 7, nil)
	occurredAt := time.Date(2026, 7, 12, 5, 0, 0, 0, time.UTC)
	body, _ := json.Marshal(map[string]any{
		"health_status": "under_treatment",
		"reason":        "production consumer retry E2E",
		"occurred_at":   occurredAt,
		"evidence_refs": []map[string]string{{"evidence_type": "source_record", "evidence_id": "story-ai-health"}},
		"row_version":   fx.goatRowVersion(goatID),
	})
	if _, err := fx.Identity.HealthGoat(fx.Ctx, identityapp.HealthGoatInput{
		TenantID: fxTenant, ActorID: fxParty, IdempotencyKey: "story-ai-health-change",
		TraceID: "trace-story-ai", GoatID: goatID, RawBody: body,
	}); err != nil {
		t.Fatalf("health command: %v", err)
	}
	payload := fx.scanText(`
SELECT payload::text FROM outbox_messages
WHERE tenant_id=$1::uuid AND aggregate_id=$2 AND event_type='goat.health.changed'
ORDER BY created_at DESC LIMIT 1`, fxTenant, goatID)
	var envelope struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		t.Fatalf("decode outbox envelope: %v", err)
	}

	bus := consumerwiring.BuildDomainBus(fx.Pool, 5*time.Second, nil)
	validator, err := outboxapp.NewEnvelopeValidator(filepath.Join("..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json"))
	if err != nil {
		t.Fatalf("validator: %v", err)
	}
	realStore := consumerpg.NewProcessedEventStore(fx.Pool, 5*time.Second)
	store := &failProcessedFinalizationOnce{inner: realStore, fail: true}
	consumer := consumerapp.NewService(bus, validator).WithProcessedEventStore(store)
	message := consumerapp.Message{
		ID: "story-ai-message", Data: []byte(payload), DeliveryAttempt: 1,
		Attributes: map[string]string{"event_type": "goat.health.changed", "tenant_id": fxTenant},
	}

	story.Step("Dispatch succeeds but processed-event finalization fails",
		"The consumer must return an error, which the tested production subscriber seam converts to NACK. It must never report success in this state.")
	err = consumer.HandleMessage(fx.Ctx, message)
	story.Assert("first delivery returns finalization error for NACK", errors.Is(err, consumerapp.ErrProcessedEventFinalizationLost), "err=%v", err)
	status := fx.scanText(`SELECT status FROM domain_event_processed_events WHERE tenant_id=$1::uuid AND subscription_id='direct' AND event_id=$2`, fxTenant, envelope.EventID)
	story.Assert("failed delivery has durable retry evidence", status == "failed", "status=%s", status)
	firstCount := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1::uuid AND protocol_version_id=$2::uuid AND target_id=$3::uuid`, fxTenant, versionID, goatID)
	story.Assert("business handler created one obligation before finalization failure", firstCount == 1, "obligations=%d", firstCount)

	story.Step("Pub/Sub redelivery reclaims failed work and finishes terminal processing",
		"The handler replays through its idempotency keys. The processed-event row becomes processed and no second obligation appears.")
	message.DeliveryAttempt = 2
	err = consumer.HandleMessage(fx.Ctx, message)
	story.Assert("redelivery succeeds only after durable terminal mark", err == nil, "err=%v", err)
	status = fx.scanText(`SELECT status FROM domain_event_processed_events WHERE tenant_id=$1::uuid AND subscription_id='direct' AND event_id=$2`, fxTenant, envelope.EventID)
	story.Assert("processed-event row is terminal processed", status == "processed", "status=%s", status)
	finalCount := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1::uuid AND protocol_version_id=$2::uuid AND target_id=$3::uuid`, fxTenant, versionID, goatID)
	story.Assert("redelivery did not duplicate business work", finalCount == 1, "obligations=%d", finalCount)

	story.Step("An operator replays the exact domain-consumer dead letter",
		"The repair service preserves the original event id, strips only Pub/Sub forwarding metadata, republishes through the real consumer, records the operator audit, and ACKs only after both succeed.")
	delivery := &repairStoryDelivery{message: consumerrepair.Message{
		BrokerID: "story-ai-dlq-message", Data: []byte(payload),
		Attributes: map[string]string{
			"event_id": envelope.EventID, "event_type": "goat.health.changed", "tenant_id": fxTenant,
			"CloudPubSubDeadLetterSourceSubscription":  "projects/goatos-dev/subscriptions/goatos-dev-domain-events",
			"CloudPubSubDeadLetterSourceDeliveryCount": "5",
		},
	}}
	repairService := consumerrepair.Service{
		SourceSubscription: "goatos-dev-domain-events",
		Publisher:          repairStoryPublisher{consumer: consumer},
		Auditor:            platformaudit.NewPostgresRecorder(fx.Pool, 5*time.Second),
	}
	repairResult, repairErr := repairService.Handle(fx.Ctx, consumerrepair.Request{
		Action: consumerrepair.ActionReplay, TargetEventID: envelope.EventID,
		ActorID: fxParty, Reason: "story AI root cause fixed and replay approved",
	}, delivery)
	story.Assert("replay publishes, audits, then ACKs", repairErr == nil && repairResult.Settled && delivery.acked && !delivery.nacked,
		"result=%+v err=%v acked=%t nacked=%t", repairResult, repairErr, delivery.acked, delivery.nacked)
	auditCount := fx.countRows(`SELECT count(*) FROM audit_log WHERE tenant_id=$1::uuid AND actor_id=$2::uuid AND action='domain_consumer.dlq_replay' AND resource_id=$3::uuid`, fxTenant, fxParty, envelope.EventID)
	story.Assert("operator replay has durable audit evidence", auditCount == 1, "audit_rows=%d", auditCount)
	afterRepairCount := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1::uuid AND protocol_version_id=$2::uuid AND target_id=$3::uuid`, fxTenant, versionID, goatID)
	story.Assert("repaired redelivery remains idempotent", afterRepairCount == 1, "obligations=%d", afterRepairCount)
}
