package publisher

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher/logging"
	"github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher/pubsub"
)

type noopBroker struct{}

func (noopBroker) Publish(_ context.Context, _ string, _ []byte, _ map[string]string) (string, error) {
	return "srv-1", nil
}

func TestSelectDefaultsToLogging(t *testing.T) {
	p := Select("", nil, nil)
	if _, ok := p.(*logging.Publisher); !ok {
		t.Fatalf("expected logging publisher by default, got %T", p)
	}
}

func TestSelectPubSubWithClient(t *testing.T) {
	p := Select(KindPubSub, nil, noopBroker{})
	if _, ok := p.(*pubsub.Publisher); !ok {
		t.Fatalf("expected pubsub publisher, got %T", p)
	}
}

func TestSelectPubSubWithoutClientFallsBackToLogging(t *testing.T) {
	p := Select(KindPubSub, nil, nil)
	if _, ok := p.(*logging.Publisher); !ok {
		t.Fatalf("expected logging fallback when no broker client, got %T", p)
	}
	if !WantsPubSub(KindPubSub) {
		t.Fatal("WantsPubSub should report true for the pubsub kind")
	}
}
