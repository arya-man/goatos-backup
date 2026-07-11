package repair

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	platformaudit "github.com/vgoats/goatos/backend/internal/platform/audit"
)

const (
	testEvent = "11111111-1111-4111-8111-111111111111"
	testActor = "22222222-2222-4222-8222-222222222222"
)

type fakeDelivery struct {
	message Message
	ackErr  error
	nackErr error
	acked   int
	nacked  int
}

func (d *fakeDelivery) Message() Message           { return d.message }
func (d *fakeDelivery) Ack(context.Context) error  { d.acked++; return d.ackErr }
func (d *fakeDelivery) Nack(context.Context) error { d.nacked++; return d.nackErr }

type fakePublisher struct {
	messages []Message
	err      error
}

func (p *fakePublisher) Publish(_ context.Context, message Message) error {
	p.messages = append(p.messages, message)
	return p.err
}

type fakeAuditor struct {
	events []platformaudit.Event
	err    error
}

func (a *fakeAuditor) Record(_ context.Context, event platformaudit.Event) error {
	a.events = append(a.events, event)
	return a.err
}

func forwardedDelivery() *fakeDelivery {
	return &fakeDelivery{message: Message{
		BrokerID:    "dlq-broker-id",
		Data:        []byte(`{"schema_version":"1","event_id":"` + testEvent + `"}`),
		OrderingKey: "aggregate-1",
		Attributes: map[string]string{
			"event_id": testEvent, "event_type": "goat.updated", "tenant_id": "33333333-3333-4333-8333-333333333333",
			"custom": "preserved", "CloudPubSubDeadLetterSourceSubscription": "projects/goatos-dev/subscriptions/goatos-dev-domain-events",
			"CloudPubSubDeadLetterSourceDeliveryCount": "5", "CloudPubSubDeadLetterSourceTopicPublishTime": "2026-07-12T00:00:00Z",
		},
	}}
}

func TestReplayPreservesOriginalMessageAuditsThenAcks(t *testing.T) {
	delivery := forwardedDelivery()
	publisher := &fakePublisher{}
	auditor := &fakeAuditor{}
	result, err := (Service{SourceSubscription: "goatos-dev-domain-events", Publisher: publisher, Auditor: auditor}).Handle(context.Background(), Request{
		Action: ActionReplay, TargetEventID: testEvent, ActorID: testActor, Reason: "fixed malformed upstream mapping",
	}, delivery)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Settled || delivery.acked != 1 || delivery.nacked != 0 {
		t.Fatalf("result=%+v delivery=%+v", result, delivery)
	}
	if len(publisher.messages) != 1 || string(publisher.messages[0].Data) != string(delivery.message.Data) {
		t.Fatalf("published=%+v", publisher.messages)
	}
	if publisher.messages[0].OrderingKey != "aggregate-1" {
		t.Fatalf("ordering key=%q", publisher.messages[0].OrderingKey)
	}
	if !reflect.DeepEqual(publisher.messages[0].Attributes, map[string]string{
		"event_id": testEvent, "event_type": "goat.updated", "tenant_id": "33333333-3333-4333-8333-333333333333", "custom": "preserved",
	}) {
		t.Fatalf("attributes=%#v", publisher.messages[0].Attributes)
	}
	if len(auditor.events) != 1 || auditor.events[0].Action != "domain_consumer.dlq_replay" {
		t.Fatalf("audit=%+v", auditor.events)
	}
}

func TestDiscardAuditsBeforeAck(t *testing.T) {
	delivery := forwardedDelivery()
	auditor := &fakeAuditor{}
	result, err := (Service{SourceSubscription: "goatos-dev-domain-events", Auditor: auditor}).Handle(context.Background(), Request{
		Action: ActionDiscard, TargetEventID: testEvent, ActorID: testActor, Reason: "confirmed obsolete event",
	}, delivery)
	if err != nil || !result.Settled || delivery.acked != 1 || len(auditor.events) != 1 {
		t.Fatalf("result=%+v err=%v delivery=%+v audit=%+v", result, err, delivery, auditor.events)
	}
}

func TestPublishFailureNacksAndDoesNotAudit(t *testing.T) {
	delivery := forwardedDelivery()
	auditor := &fakeAuditor{}
	_, err := (Service{SourceSubscription: "goatos-dev-domain-events", Publisher: &fakePublisher{err: errors.New("publish down")}, Auditor: auditor}).Handle(context.Background(), Request{
		Action: ActionReplay, TargetEventID: testEvent, ActorID: testActor, Reason: "repair",
	}, delivery)
	if err == nil || delivery.nacked != 1 || delivery.acked != 0 || len(auditor.events) != 0 {
		t.Fatalf("err=%v delivery=%+v audit=%+v", err, delivery, auditor.events)
	}
}

func TestAuditFailureNacksAfterPublish(t *testing.T) {
	delivery := forwardedDelivery()
	publisher := &fakePublisher{}
	_, err := (Service{SourceSubscription: "goatos-dev-domain-events", Publisher: publisher, Auditor: &fakeAuditor{err: errors.New("db down")}}).Handle(context.Background(), Request{
		Action: ActionReplay, TargetEventID: testEvent, ActorID: testActor, Reason: "repair",
	}, delivery)
	if err == nil || delivery.nacked != 1 || delivery.acked != 0 || len(publisher.messages) != 1 {
		t.Fatalf("err=%v delivery=%+v publish=%+v", err, delivery, publisher.messages)
	}
}

func TestAckFailureReturnsFailureWithoutSecondSettlement(t *testing.T) {
	delivery := forwardedDelivery()
	delivery.ackErr = errors.New("ack denied")
	_, err := (Service{SourceSubscription: "goatos-dev-domain-events", Auditor: &fakeAuditor{}}).Handle(context.Background(), Request{
		Action: ActionDiscard, TargetEventID: testEvent, ActorID: testActor, Reason: "repair",
	}, delivery)
	if err == nil || delivery.acked != 1 || delivery.nacked != 0 {
		t.Fatalf("err=%v delivery=%+v", err, delivery)
	}
}

func TestNackFailureIsNeverHidden(t *testing.T) {
	delivery := forwardedDelivery()
	delivery.nackErr = errors.New("nack denied")
	_, err := (Service{SourceSubscription: "goatos-dev-domain-events", Publisher: &fakePublisher{err: errors.New("publish down")}, Auditor: &fakeAuditor{}}).Handle(context.Background(), Request{
		Action: ActionReplay, TargetEventID: testEvent, ActorID: testActor, Reason: "repair",
	}, delivery)
	if err == nil || !strings.Contains(err.Error(), "publish down") || !strings.Contains(err.Error(), "nack denied") {
		t.Fatalf("err=%v", err)
	}
}

func TestSharedDLQFiltersOtherSourceAndListNacks(t *testing.T) {
	other := forwardedDelivery()
	other.message.Attributes["CloudPubSubDeadLetterSourceSubscription"] = "projects/goatos-dev/subscriptions/goatos-dev-analytics-export"
	result, err := (Service{SourceSubscription: "goatos-dev-domain-events"}).Handle(context.Background(), Request{Action: ActionList}, other)
	if err != nil || result.MatchedSource || other.nacked != 1 {
		t.Fatalf("result=%+v err=%v delivery=%+v", result, err, other)
	}
	delivery := forwardedDelivery()
	result, err = (Service{SourceSubscription: "goatos-dev-domain-events"}).Handle(context.Background(), Request{Action: ActionList}, delivery)
	if err != nil || !result.MatchedSource || delivery.nacked != 1 || delivery.acked != 0 {
		t.Fatalf("result=%+v err=%v delivery=%+v", result, err, delivery)
	}
}

func TestMutationValidation(t *testing.T) {
	for _, req := range []Request{
		{Action: ActionReplay, TargetEventID: "bad", ActorID: testActor, Reason: "x"},
		{Action: ActionReplay, TargetEventID: testEvent, ActorID: "bad", Reason: "x"},
		{Action: ActionDiscard, TargetEventID: testEvent, ActorID: testActor},
	} {
		delivery := forwardedDelivery()
		if _, err := (Service{}).Handle(context.Background(), req, delivery); err == nil || delivery.acked+delivery.nacked != 0 {
			t.Fatalf("req=%+v err=%v delivery=%+v", req, err, delivery)
		}
	}
}
