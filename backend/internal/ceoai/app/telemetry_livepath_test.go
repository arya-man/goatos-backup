package app_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/vgoats/goatos/backend/internal/ceoai/adapters/observability"
	"github.com/vgoats/goatos/backend/internal/ceoai/app"
	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

// livePlanner is the deterministic provider standing in for Vertex so the test
// drives the REAL orchestrator path end to end without a model.
type livePlanner struct{ plan domain.Plan }

func (p *livePlanner) Plan(context.Context, domain.Question, []domain.ResolvedEntities, []ports.ToolSpec) (domain.Plan, error) {
	return p.plan, nil
}
func (p *livePlanner) PlannedByModel() bool { return true }

type liveExec struct{ spec ports.ToolSpec }

func (e *liveExec) Spec() ports.ToolSpec { return e.spec }
func (e *liveExec) Execute(context.Context, domain.Actor, domain.SubQuestion) (domain.ToolResult, error) {
	return domain.ToolResult{
		Route: domain.RouteAPI, ToolName: e.spec.Name, Surface: "Mesha · counts",
		Facts: []domain.Fact{{Label: "active", Value: "1234"}},
	}, nil
}

// TestObservabilityMetricsEmittedOnLiveAsk is the OBS-RUN-1 root-cause proof:
// the REAL observability.Metrics facade, injected as the orchestrator's
// Telemetry port, must actually fire assistant_* instruments when a leadership
// question is answered through Assistant.Ask. Before the wiring fix the
// orchestrator had no telemetry seam at all, so nothing was ever recorded on a
// live request; this test collects the emitted metric set and asserts the
// terminal counter/histogram + the row histogram are present.
func TestObservabilityMetricsEmittedOnLiveAsk(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(prev) })

	// NewMetrics binds its instruments to the reader installed above; injecting
	// it as the Telemetry port is exactly what bootstrap does on the live server.
	metrics := observability.NewMetrics()

	exec := &liveExec{spec: ports.ToolSpec{Name: "active_animals", Route: domain.RouteAPI}}
	reg := app.NewRegistry(nil, nil, nil)
	reg.Register(exec)
	prov := &livePlanner{plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "active_animals", Route: domain.RouteAPI},
	}}}

	a := app.NewAssistant(app.Config{}, app.Deps{
		Provider:  prov,
		Registry:  reg,
		Telemetry: metrics,
	})

	actor := domain.Actor{TenantID: "t1", UserID: "u1", Role: permissions.RoleCEOInternal}
	if _, err := a.Ask(context.Background(), domain.Question{Actor: actor, Text: "how many active animals"}); err != nil {
		t.Fatalf("Ask: %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}

	// The terminal request counter and the tool-rows histogram must both have
	// fired on this single live Ask.
	for _, want := range []string{"assistant_requests_total", "assistant_tool_rows_returned"} {
		if !metricEmitted(rm, want) {
			t.Fatalf("expected metric %q to be emitted on a live /ask; none found — telemetry not wired", want)
		}
	}

	// The request counter must carry the bounded tool+status labels (not empty).
	status, tool := requestLabels(t, rm)
	if status != "ok" {
		t.Fatalf("expected status=ok on a grounded answer, got %q", status)
	}
	if tool != string(domain.RouteAPI) {
		t.Fatalf("expected tool=%q, got %q", domain.RouteAPI, tool)
	}
}

func metricEmitted(rm metricdata.ResourceMetrics, name string) bool {
	for _, sm := range rm.ScopeMetrics {
		for _, md := range sm.Metrics {
			if md.Name == name {
				return true
			}
		}
	}
	return false
}

// requestLabels returns the (status, tool) attribute pair on the single
// assistant_requests_total data point.
func requestLabels(t *testing.T, rm metricdata.ResourceMetrics) (status, tool string) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, md := range sm.Metrics {
			if md.Name != "assistant_requests_total" {
				continue
			}
			sum, ok := md.Data.(metricdata.Sum[int64])
			if !ok || len(sum.DataPoints) == 0 {
				t.Fatalf("assistant_requests_total has no int64 sum data points")
			}
			for _, kv := range sum.DataPoints[0].Attributes.ToSlice() {
				switch kv.Key {
				case "status":
					status = kv.Value.AsString()
				case "tool":
					tool = kv.Value.AsString()
				}
			}
			return status, tool
		}
	}
	t.Fatalf("assistant_requests_total not found")
	return "", ""
}
