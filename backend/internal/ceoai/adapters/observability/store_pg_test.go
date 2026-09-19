package observability

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestPostgresTraceStoreRejectionReasonRoundTrip (PR #318 R2-4, Postgres
// gated): the tenant-gate refusal the orchestrator audits lands with a
// non-empty rejection_reason in ceo_ai_assistant_audit, so the column an
// operator filters on actually finds it; GetTrace reads the same value back.
func TestPostgresTraceStoreRejectionReasonRoundTrip(t *testing.T) {
	ctx := context.Background()
	pgtest.SkipIfNoDocker(t)
	pool := pgtest.StartPostgres(t, ctx)
	var tenant string
	if err := pool.QueryRow(ctx,
		`INSERT INTO tenants (tenant_id, name, status) VALUES (gen_random_uuid(), 'Audit Tenant', 'active') RETURNING tenant_id::text`,
	).Scan(&tenant); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	sink := NewAuditTraceSink(NewPostgresTraceStore(pool, 5*time.Second))
	if err := sink.Record(ctx, ports.AuditRecord{
		RequestID: "req-gate-pg", TenantID: tenant, Mode: domain.ModeRefused,
		Routes: []domain.Route{domain.RouteSQL}, ToolsCalled: []string{"sql_fallback"},
		Review: domain.ReviewVerdict{ScopeSafe: false, FailReasons: []string{"tenant_gate:compose:foreign"}},
		Steps:  []domain.StepTrace{{SubQuestionID: "tenant_gate", ToolName: "tenant_gate:compose", Err: "fact belongs to another tenant"}},
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM ceo_ai_assistant_audit WHERE tenant_id = $1::uuid AND rejection_reason = 'tenant_gate:compose:foreign'`,
		tenant).Scan(&n); err != nil {
		t.Fatalf("filter by rejection_reason: %v", err)
	}
	if n != 1 {
		t.Fatalf("filtering the audit table by rejection_reason must find the refusal, got %d rows", n)
	}
	got, err := NewPostgresTraceStore(pool, 5*time.Second).GetTrace(ctx, tenant, "req-gate-pg")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if got.RejectionReason != "tenant_gate:compose:foreign" || got.Status != string(domain.ModeRefused) {
		t.Fatalf("round trip lost the reason: %+v", got)
	}
}
