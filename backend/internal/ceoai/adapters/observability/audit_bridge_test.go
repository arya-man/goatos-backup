package observability

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// TestAuditTraceSinkPersistsReadableTrace proves the orchestrator's audit port
// is bridged to the trace store the admin endpoint reads: a per-request
// AuditRecord written through the sink is retrievable as a TraceRecord for the
// same (tenant, request_id), with routes/tools/steps carried across and actor
// identity dropped.
func TestAuditTraceSinkPersistsReadableTrace(t *testing.T) {
	store := NewMemoryTraceStore(8)
	sink := NewAuditTraceSink(store)

	rec := ports.AuditRecord{
		RequestID:      "req-42",
		TenantID:       "tA",
		ActorID:        "user-ceo", // sensitive: must NOT surface as a role
		ConversationID: "conv-1",
		Mode:           domain.ModePlanned,
		Routes:         []domain.Route{domain.RouteCube, domain.RouteAPI, domain.RouteCube},
		ToolsCalled:    []string{"vaccination_overdue", "active_animals"},
		RowCount:       7,
		LatencyMS:      123,
		ModelVersion:   "gemini-3.5-flash-lite",
		PromptVersion:  "v1",
		Review:         domain.ReviewVerdict{Grounded: true, ScopeSafe: true, Complete: true},
		Steps: []domain.StepTrace{
			{SubQuestionID: "0", Route: domain.RouteCube, ToolName: "vaccination_overdue", RowCount: 3, DurationMS: 12},
		},
	}
	if err := sink.Record(context.Background(), rec); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := store.GetTrace(context.Background(), "tA", "req-42")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if got.RouteTier != "cube,api" {
		t.Fatalf("routes not deduped/joined: %q", got.RouteTier)
	}
	if got.ToolCalled != "vaccination_overdue,active_animals" {
		t.Fatalf("tools not joined: %q", got.ToolCalled)
	}
	if got.Status != string(domain.ModePlanned) {
		t.Fatalf("status = %q, want %q", got.Status, domain.ModePlanned)
	}
	if got.RowCount != 7 || got.LatencyMS != 123 {
		t.Fatalf("row/latency lost: rows=%d latency=%d", got.RowCount, got.LatencyMS)
	}
	if len(got.Steps) != 1 || got.Steps[0].ToolName != "vaccination_overdue" {
		t.Fatalf("step trace not carried: %+v", got.Steps)
	}
	if got.ActorRole != "" {
		t.Fatalf("actor identity must not be persisted as a role, got %q", got.ActorRole)
	}
}

// TestAuditTraceSinkNilStoreIsNoop guards the degraded-boot path.
func TestAuditTraceSinkNilStoreIsNoop(t *testing.T) {
	var sink *AuditTraceSink
	if err := sink.Record(context.Background(), ports.AuditRecord{RequestID: "x"}); err != nil {
		t.Fatalf("nil sink must be a no-op, got %v", err)
	}
	if err := NewAuditTraceSink(nil).Record(context.Background(), ports.AuditRecord{RequestID: "x"}); err != nil {
		t.Fatalf("nil store must be a no-op, got %v", err)
	}
}
