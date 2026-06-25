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
	case codes.InvalidArgument, codes.NotFound, codes.PermissionDenied, codes.Unauthenticated, codes.FailedPrecondition:
		return "", ports.PermanentPublishError(fmt.Errorf("pubsub publish topic %q: %w", topicID, err))
	default:
		return "", ports.RetryablePublishError(fmt.Errorf("pubsub publish topic %q: %w", topicID, err))
	}
}
