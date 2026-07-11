package repair

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	platformaudit "github.com/vgoats/goatos/backend/internal/platform/audit"
)

const deadLetterAttributePrefix = "CloudPubSubDeadLetterSource"

type Message struct {
	BrokerID    string
	Data        []byte
	Attributes  map[string]string
	OrderingKey string
}

type Delivery interface {
	Message() Message
	Ack(context.Context) error
	Nack(context.Context) error
}

type Publisher interface {
	Publish(context.Context, Message) error
}

type Auditor interface {
	Record(context.Context, platformaudit.Event) error
}

type Action string

const (
	ActionList    Action = "list"
	ActionReplay  Action = "replay"
	ActionDiscard Action = "discard"
)

type Request struct {
	Action        Action
	TargetEventID string
	ActorID       string
	Reason        string
}

type Result struct {
	MatchedSource bool
	MatchedTarget bool
	Settled       bool
	EventID       string
	BrokerID      string
	Attributes    map[string]string
}

type Service struct {
	SourceSubscription string
	Publisher          Publisher
	Auditor            Auditor
}

func (s Service) Handle(ctx context.Context, req Request, delivery Delivery) (Result, error) {
	if delivery == nil {
		return Result{}, errors.New("domain consumer DLQ repair: delivery is required")
	}
	if err := ValidateRequest(req); err != nil {
		return Result{}, err
	}
	message := delivery.Message()
	result := Result{
		BrokerID:   message.BrokerID,
		EventID:    message.Attributes["event_id"],
		Attributes: originalAttributes(message.Attributes),
	}
	if !sameSubscription(message.Attributes[deadLetterAttributePrefix+"Subscription"], s.SourceSubscription) {
		return result, nackWithCause(ctx, delivery, nil)
	}
	result.MatchedSource = true
	if req.Action == ActionList {
		return result, nackWithCause(ctx, delivery, nil)
	}
	if result.EventID != req.TargetEventID {
		return result, nackWithCause(ctx, delivery, nil)
	}
	result.MatchedTarget = true

	original := Message{
		BrokerID:    message.BrokerID,
		Data:        append([]byte(nil), message.Data...),
		Attributes:  result.Attributes,
		OrderingKey: message.OrderingKey,
	}
	if req.Action == ActionReplay {
		if s.Publisher == nil {
			return result, nackWithCause(ctx, delivery, errors.New("source-topic publisher is not configured"))
		}
		if err := s.Publisher.Publish(ctx, original); err != nil {
			return result, nackWithCause(ctx, delivery, fmt.Errorf("publish replay: %w", err))
		}
	}
	if s.Auditor == nil {
		return result, nackWithCause(ctx, delivery, errors.New("audit recorder is not configured"))
	}
	if err := s.Auditor.Record(ctx, auditEvent(req, message, original)); err != nil {
		return result, nackWithCause(ctx, delivery, fmt.Errorf("record repair audit: %w", err))
	}
	if err := delivery.Ack(ctx); err != nil {
		return result, fmt.Errorf("ack repaired dead letter: %w", err)
	}
	result.Settled = true
	return result, nil
}

func ValidateRequest(req Request) error {
	switch req.Action {
	case ActionList:
		return nil
	case ActionReplay, ActionDiscard:
	default:
		return fmt.Errorf("domain consumer DLQ repair: unsupported action %q", req.Action)
	}
	if _, err := uuid.Parse(req.TargetEventID); err != nil {
		return errors.New("domain consumer DLQ repair: target event id must be a UUID")
	}
	if _, err := uuid.Parse(req.ActorID); err != nil {
		return errors.New("domain consumer DLQ repair: actor id must be a UUID")
	}
	if strings.TrimSpace(req.Reason) == "" {
		return errors.New("domain consumer DLQ repair: non-empty reason is required")
	}
	return nil
}

func originalAttributes(attributes map[string]string) map[string]string {
	original := make(map[string]string, len(attributes))
	for key, value := range attributes {
		if strings.HasPrefix(key, deadLetterAttributePrefix) {
			continue
		}
		original[key] = value
	}
	return original
}

func sameSubscription(got, want string) bool {
	got = strings.TrimSpace(got)
	want = strings.TrimSpace(want)
	if got == "" || want == "" {
		return false
	}
	return got == want || strings.HasSuffix(got, "/subscriptions/"+want)
}

func nackWithCause(ctx context.Context, delivery Delivery, cause error) error {
	if err := delivery.Nack(ctx); err != nil {
		if cause == nil {
			return fmt.Errorf("nack dead letter: %w", err)
		}
		return errors.Join(cause, fmt.Errorf("nack dead letter: %w", err))
	}
	return cause
}

func auditEvent(req Request, forwarded Message, original Message) platformaudit.Event {
	return platformaudit.Event{
		TenantID:     original.Attributes["tenant_id"],
		ActorID:      req.ActorID,
		ActorType:    "operator",
		Action:       "domain_consumer.dlq_" + string(req.Action),
		ResourceType: "domain_event",
		ResourceID:   req.TargetEventID,
		Metadata: map[string]any{
			"reason":              strings.TrimSpace(req.Reason),
			"broker_message_id":   forwarded.BrokerID,
			"event_id":            req.TargetEventID,
			"event_type":          original.Attributes["event_type"],
			"source_subscription": forwarded.Attributes[deadLetterAttributePrefix+"Subscription"],
			"delivery_count":      forwarded.Attributes[deadLetterAttributePrefix+"DeliveryCount"],
			"result":              string(req.Action) + "ed",
		},
		TraceID: original.Attributes["trace_id"],
	}
}
