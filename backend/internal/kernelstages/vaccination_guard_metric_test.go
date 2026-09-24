package kernelstages

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/kmetrics"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestVaccinationGuardRejectedMetricIsEmitted(t *testing.T) {
	ctx := context.Background()
	kmetrics.RecordVaccinationGuardRejected(ctx, 0)
	kmetrics.RecordVaccinationGuardRejected(ctx, 2)
	var data metricdata.ResourceMetrics
	if err := stageMetricReader.Collect(ctx, &data); err != nil {
		t.Fatal(err)
	}
	for _, scope := range data.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "kernel.vaccination_generation.guard_rejected" {
				continue
			}
			var total int64
			for _, p := range m.Data.(metricdata.Sum[int64]).DataPoints {
				total += p.Value
			}
			if total >= 2 {
				return
			}
			t.Fatalf("guard_rejected total=%d, want >= 2", total)
		}
	}
	t.Fatal("guard_rejected metric not emitted")
}
