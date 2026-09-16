package observability

import (
	"context"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	dbconv "go.opentelemetry.io/otel/semconv/v1.40.0/dbconv"
	"testing"
)

func TestDatabaseDurationExportsSubsecondBuckets(t *testing.T) {
	ctx := context.Background()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader), sdkmetric.WithView(secondsHistogramView()))
	defer provider.Shutdown(ctx)
	// Use the same generated instrument constructor as otelpgx v0.11.1.
	h, err := dbconv.NewClientOperationDuration(provider.Meter("github.com/exaring/otelpgx"))
	if err != nil {
		t.Fatal(err)
	}
	h.Record(ctx, .0008, dbconv.SystemNameAttr("postgresql"))
	h.Record(ctx, .12, dbconv.SystemNameAttr("postgresql"))
	h.Record(ctx, .49, dbconv.SystemNameAttr("postgresql"))
	h.Record(ctx, 120, dbconv.SystemNameAttr("postgresql"))
	h.Record(ctx, 900, dbconv.SystemNameAttr("postgresql"))
	var data metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &data); err != nil {
		t.Fatal(err)
	}
	m := data.ScopeMetrics[0].Metrics[0]
	if m.Name != "db.client.operation.duration" || m.Unit != "s" {
		t.Fatalf("metric=%+v", m)
	}
	p := m.Data.(metricdata.Histogram[float64]).DataPoints[0]
	buckets := map[float64]uint64{}
	for i, bound := range p.Bounds {
		buckets[bound] = p.BucketCounts[i]
	}
	for _, bound := range []float64{.001, .15, .5, 120, 900} {
		if buckets[bound] != 1 {
			t.Fatalf("bound %g: %+v", bound, p)
		}
	}
	if p.Count != 5 || p.Sum < 1020.610799 || p.Sum > 1020.610801 {
		t.Fatalf("point=%+v", p)
	}
}
