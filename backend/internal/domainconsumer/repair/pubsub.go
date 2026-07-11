package repair

import (
	"context"
	"errors"
	"fmt"
	"sync"

	cloudpubsub "cloud.google.com/go/pubsub"
)

type GCPClient struct {
	client *cloudpubsub.Client
	topic  *cloudpubsub.Topic
}

func NewGCPClient(ctx context.Context, projectID, sourceTopicID string) (*GCPClient, error) {
	if projectID == "" {
		return nil, errors.New("domain consumer DLQ repair: project id is required")
	}
	client, err := cloudpubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("domain consumer DLQ repair pubsub client: %w", err)
	}
	gcp := &GCPClient{client: client}
	if sourceTopicID != "" {
		gcp.topic = client.Topic(sourceTopicID)
	}
	return gcp, nil
}

func (c *GCPClient) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	if c.topic != nil {
		c.topic.Stop()
	}
	return c.client.Close()
}

func (c *GCPClient) Publish(ctx context.Context, message Message) error {
	if c == nil || c.topic == nil {
		return errors.New("domain consumer DLQ repair: source topic is not configured")
	}
	result := c.topic.Publish(ctx, &cloudpubsub.Message{
		Data:        append([]byte(nil), message.Data...),
		Attributes:  cloneAttributes(message.Attributes),
		OrderingKey: message.OrderingKey,
	})
	if _, err := result.Get(ctx); err != nil {
		return fmt.Errorf("publish source topic: %w", err)
	}
	return nil
}

// Receive serializes deliveries so a targeted operator repair never races multiple ACK/NACK
// decisions. The callback returns stop=true only after the requested list limit or target settles.
func (c *GCPClient) Receive(ctx context.Context, subscriptionID string, handle func(context.Context, Delivery) (bool, error)) error {
	if c == nil || c.client == nil {
		return errors.New("domain consumer DLQ repair: pubsub client is not configured")
	}
	if subscriptionID == "" {
		return errors.New("domain consumer DLQ repair: inspection subscription is required")
	}
	receiveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	subscription := c.client.Subscription(subscriptionID)
	subscription.ReceiveSettings.Synchronous = true
	subscription.ReceiveSettings.NumGoroutines = 1
	subscription.ReceiveSettings.MaxOutstandingMessages = 1

	var mu sync.Mutex
	var callbackErr error
	err := subscription.Receive(receiveCtx, func(callbackCtx context.Context, message *cloudpubsub.Message) {
		stop, handleErr := handle(callbackCtx, &gcpDelivery{message: message})
		if handleErr != nil {
			mu.Lock()
			callbackErr = errors.Join(callbackErr, handleErr)
			mu.Unlock()
			cancel()
			return
		}
		if stop {
			cancel()
		}
	})
	mu.Lock()
	defer mu.Unlock()
	if callbackErr != nil {
		return callbackErr
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

type gcpDelivery struct {
	message *cloudpubsub.Message
}

func (d *gcpDelivery) Message() Message {
	return Message{
		BrokerID:    d.message.ID,
		Data:        append([]byte(nil), d.message.Data...),
		Attributes:  cloneAttributes(d.message.Attributes),
		OrderingKey: d.message.OrderingKey,
	}
}

func (d *gcpDelivery) Ack(ctx context.Context) error {
	status, err := d.message.AckWithResult().Get(ctx)
	if err != nil {
		return err
	}
	if status != cloudpubsub.AcknowledgeStatusSuccess {
		return fmt.Errorf("pubsub ack status %v", status)
	}
	return nil
}

func (d *gcpDelivery) Nack(ctx context.Context) error {
	status, err := d.message.NackWithResult().Get(ctx)
	if err != nil {
		return err
	}
	if status != cloudpubsub.AcknowledgeStatusSuccess {
		return fmt.Errorf("pubsub nack status %v", status)
	}
	return nil
}

func cloneAttributes(attributes map[string]string) map[string]string {
	cloned := make(map[string]string, len(attributes))
	for key, value := range attributes {
		cloned[key] = value
	}
	return cloned
}
