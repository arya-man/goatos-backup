package observability

import (
	"context"
	"strings"
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
		ModelVersion:   "gemini-3.8-flash",
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

// TestAuditTraceSinkPopulatesRejectionReason (PR #318 R2-4): a verdict that
// carries fail reasons lands in the rejection_reason column the audit table is
// filtered on — the tenant-gate refusal shape the judge verified live, and an
// ordinary review failure — while a clean verdict leaves it empty.
func TestAuditTraceSinkPopulatesRejectionReason(t *testing.T) {
	store := NewMemoryTraceStore(8)
	sink := NewAuditTraceSink(store)
	ctx := context.Background()

	refused := ports.AuditRecord{
		RequestID: "req-gate", TenantID: "tA", Mode: domain.ModeRefused,
		Review: domain.ReviewVerdict{ScopeSafe: false, FailReasons: []string{"tenant_gate:compose:foreign"}},
		Steps:  []domain.StepTrace{{SubQuestionID: "tenant_gate", ToolName: "tenant_gate:compose", Err: "fact 0 belongs to another tenant"}},
	}
	if err := sink.Record(ctx, refused); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetTrace(ctx, "tA", "req-gate")
	if err != nil {
		t.Fatal(err)
	}
	if got.RejectionReason != "tenant_gate:compose:foreign" {
		t.Fatalf("rejection_reason = %q, want the tenant-gate reason", got.RejectionReason)
	}
	if got.Status != string(domain.ModeRefused) || got.ReviewVerdict == "" {
		t.Fatalf("status/verdict must still be carried: %+v", got)
	}

	failed := ports.AuditRecord{
		RequestID: "req-review", TenantID: "tA", Mode: domain.ModePartial,
		Review: domain.ReviewVerdict{Grounded: false, FailReasons: []string{"ungrounded number: 42", "critic: vague", "ungrounded number: 42"}},
	}
	if err := sink.Record(ctx, failed); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetTrace(ctx, "tA", "req-review")
	if got.RejectionReason != "ungrounded number: 42|critic: vague" {
		t.Fatalf("rejection_reason = %q, want the deduped joined reasons", got.RejectionReason)
	}

	clean := ports.AuditRecord{
		RequestID: "req-ok", TenantID: "tA", Mode: domain.ModePlanned,
		Review: domain.ReviewVerdict{Grounded: true, ScopeSafe: true, Complete: true},
	}
	if err := sink.Record(ctx, clean); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetTrace(ctx, "tA", "req-ok")
	if got.RejectionReason != "" {
		t.Fatalf("a clean verdict must leave rejection_reason empty, got %q", got.RejectionReason)
	}

	// Bounded: an absurd reason list is truncated, never rejected.
	long := strings.Repeat("x", 2*maxRejectionReasonBytes)
	if r := rejectionReason(domain.ReviewVerdict{FailReasons: []string{long}}); len(r) != maxRejectionReasonBytes {
		t.Fatalf("rejection reason must be bounded to %d bytes, got %d", maxRejectionReasonBytes, len(r))
	}
}
