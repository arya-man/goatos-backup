package httpmiddleware

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestHTTPDurationExportsSubsecondBucketsInSeconds(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer provider.Shutdown(context.Background())
	histogram := mustFloat64Histogram(provider.Meter(metricsMeterName), "http.server.request.duration", "s", "HTTP latency")
	for _, duration := range []time.Duration{120 * time.Millisecond, 300 * time.Millisecond, 490 * time.Millisecond, 1500 * time.Millisecond} {
		recordHistogram(context.Background(), histogram, duration.Seconds())
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatal(err)
	}
	if len(data.ScopeMetrics) != 1 || len(data.ScopeMetrics[0].Metrics) != 1 {
		t.Fatalf("unexpected metrics: %+v", data)
	}
	exported := data.ScopeMetrics[0].Metrics[0]
	if exported.Unit != "s" {
		t.Fatalf("unit=%q", exported.Unit)
	}
	points := exported.Data.(metricdata.Histogram[float64]).DataPoints
	if len(points) != 1 || points[0].Count != 4 {
		t.Fatalf("points=%+v", points)
	}
	p := points[0]
	buckets := map[float64]uint64{}
	for i, bound := range p.Bounds {
		buckets[bound] = p.BucketCounts[i]
	}
	for _, bound := range []float64{0.15, 0.3, 0.5, 1.5} {
		if buckets[bound] != 1 {
			t.Fatalf("bound %g: count=%d, bounds=%v counts=%v", bound, buckets[bound], p.Bounds, p.BucketCounts)
		}
	}
	if p.Sum < 2.409999 || p.Sum > 2.410001 {
		t.Fatalf("expected2.41 seconds, got%g", p.Sum)
	}
}
