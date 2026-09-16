package pubsub

import (
	cloudpubsub "cloud.google.com/go/pubsub"
	"context"
	"errors"
	consumerapp "github.com/vgoats/goatos/backend/internal/domainconsumer/app"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"testing"
	"time"
)

func TestDispatchRecordsOnlyGenuinePublishLagAndPreservesDelivery(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer provider.Shutdown(context.Background())
	attempt := 2
	wantErr := errors.New("retry remains required")
	message := &cloudpubsub.Message{ID: "m", Data: []byte("payload"), Attributes: map[string]string{"occurred_at": "2000-01-01T00:00:00Z"}, DeliveryAttempt: &attempt, PublishTime: time.Now().Add(-3 * time.Second)}
	handler := func(ctx context.Context, m consumerapp.Message) error {
		if m.ID != "m" || string(m.Data) != "payload" || m.DeliveryAttempt != 2 {
			t.Fatalf("delivery changed: %+v", m)
		}
		return wantErr
	}
	if err := dispatchMessage(context.Background(), message, handler); !errors.Is(err, wantErr) {
		t.Fatalf("handler error changed: %v", err)
	}
	readLag := func() int64 {
		t.Helper()
		var data metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &data); err != nil {
			t.Fatal(err)
		}
		for _, scope := range data.ScopeMetrics {
			for _, m := range scope.Metrics {
				if m.Name == "kernel.consumer.lag" {
					if m.Unit != "s" {
						t.Fatal(m.Unit)
					}
					return m.Data.(metricdata.Gauge[int64]).DataPoints[0].Value
				}
			}
		}
		t.Fatal("lag metric missing")
		return -1
	}
	lag := readLag()
	if lag != 3 {
		t.Fatalf("publish lag=%d; must not use old occurred_at", lag)
	}
	for _, stamp := range []time.Time{{}, time.Now().Add(time.Hour)} {
		message.PublishTime = stamp
		_ = dispatchMessage(context.Background(), message, handler)
		if got := readLag(); got != lag {
			t.Fatalf("invalid publish time emitted fabricated lag %d", got)
		}
	}
}
