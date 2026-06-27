package pubsub

import (
	"context"
	"fmt"

	cloudpubsub "cloud.google.com/go/pubsub"

	consumerapp "github.com/vgoats/goatos/backend/internal/domainconsumer/app"
)

type GCPSubscriber struct {
	client *cloudpubsub.Client
}

func NewGCPSubscriber(ctx context.Context, projectID string) (*GCPSubscriber, error) {
	if projectID == "" {
		return nil, fmt.Errorf("pubsub project id is required")
	}
	client, err := cloudpubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("pubsub client: %w", err)
	}
	return &GCPSubscriber{client: client}, nil
}

func (s *GCPSubscriber) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *GCPSubscriber) Receive(ctx context.Context, subscriptionID string, handler consumerapp.Handler) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("pubsub subscriber client is not configured")
	}
	if subscriptionID == "" {
		return fmt.Errorf("pubsub subscription id is required")
	}
	subscription := s.client.Subscription(subscriptionID)
	return subscription.Receive(ctx, func(ctx context.Context, message *cloudpubsub.Message) {
		deliveryAttempt := 0
		if message.DeliveryAttempt != nil {
			deliveryAttempt = *message.DeliveryAttempt
		}
		err := handler(ctx, consumerapp.Message{
			ID:              message.ID,
			Data:            message.Data,
			Attributes:      message.Attributes,
			DeliveryAttempt: deliveryAttempt,
		})
		if err != nil {
			message.Nack()
			return
		}
		message.Ack()
	})
}
