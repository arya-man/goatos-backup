package pubsub

import (
	"context"
	"fmt"

	cloudpubsub "cloud.google.com/go/pubsub"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/vgoats/goatos/backend/internal/outbox/ports"
)

type GCPMessagePublisher struct {
	client *cloudpubsub.Client
}

func NewGCPMessagePublisher(ctx context.Context, projectID string) (*GCPMessagePublisher, error) {
	if projectID == "" {
		return nil, fmt.Errorf("pubsub project id is required")
	}
	client, err := cloudpubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("pubsub client: %w", err)
	}
	return &GCPMessagePublisher{client: client}, nil
}

func (p *GCPMessagePublisher) Close() error {
	if p == nil || p.client == nil {
		return nil
	}
	return p.client.Close()
}

func (p *GCPMessagePublisher) Publish(ctx context.Context, topicID string, data []byte, attributes map[string]string) (string, error) {
	if p == nil || p.client == nil {
		return "", ports.PermanentPublishError(fmt.Errorf("pubsub client is not configured"))
	}
	if topicID == "" {
		return "", ports.PermanentPublishError(fmt.Errorf("pubsub topic id is required"))
	}
	result := p.client.Topic(topicID).Publish(ctx, &cloudpubsub.Message{
		Data:       data,
		Attributes: attributes,
	})
	serverID, err := result.Get(ctx)
	if err == nil {
		return serverID, nil
	}
	switch status.Code(err) {
	case codes.InvalidArgument:
		// The ONLY genuinely permanent class: the message itself is malformed (too large, bad
		// attributes). Retrying the identical bytes cannot succeed, so failing fast is right.
		//
		// NotFound / PermissionDenied / Unauthenticated / FailedPrecondition used to land here
		// too, and that discarded live domain events on the FIRST attempt over conditions that
		// heal by themselves: a topic recreated by a deploy, IAM still propagating, credentials
		// mid-refresh. One such blip permanently dropped 16 events here -- including a
		// verification.verdict.rework, i.e. a verifier's rejection of an operator's weighing --
		// and requeuing them published all 17 with no other change, proving they were retryable
		// all along. They are environment faults, so they retry and, if the environment really
		// is broken, still terminate visibly via max_attempts_exhausted -> dead_letter, which is
		// recoverable and alertable rather than silently gone.
		return "", ports.PermanentPublishError(fmt.Errorf("pubsub publish topic %q: %w", topicID, err))
	default:
		return "", ports.RetryablePublishError(fmt.Errorf("pubsub publish topic %q: %w", topicID, err))
	}
}
