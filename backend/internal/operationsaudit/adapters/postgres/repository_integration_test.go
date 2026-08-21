package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/operationsaudit/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const auditTestTenant = "00000000-0000-4000-8000-000000000001"

func TestOperationsAuditRepositoryPaginationFiltersAndAnomalies(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	base := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	insertAuditRow(t, ctx, pool, auditSeed{
		AuditID:    "90000000-0000-4000-8000-000000000001",
		RecordedAt: base.Add(time.Minute),
		Action:     "sop.task.rework",
		Metadata: map[string]any{
			"domain": "pc", "module": "vaccination", "category": "rework", "result": "rework", "status": "rework",
		},
	})
	insertAuditRow(t, ctx, pool, auditSeed{
		AuditID:    "90000000-0000-4000-8000-000000000002",
		RecordedAt: base.Add(2 * time.Minute),
		Action:     "goat.created",
		Metadata: map[string]any{
			"domain": "procurement", "module": "source_entry", "category": "accepted_intake", "result": "queued", "status": "queued",
		},
	})
	insertAuditRow(t, ctx, pool, auditSeed{
		AuditID:    "90000000-0000-4000-8000-000000000003",
		RecordedAt: base.Add(3 * time.Minute),
		Action:     "inventory.stock_mismatch",
		Metadata: map[string]any{
			"domain": "pc", "module": "vaccination", "category": "stock_mismatch", "result": "mismatch", "status": "mismatch",
		},
	})
	insertAuditRow(t, ctx, pool, auditSeed{
		AuditID:    "90000000-0000-4000-8000-000000000004",
		RecordedAt: base.Add(4 * time.Minute),
		Action:     "sop.task.submit",
		Metadata: map[string]any{
			"domain": "pc", "module": "vaccination", "category": "proof", "result": "completed", "status": "completed", "proof_id": "proof-1",
		},
	})
	insertAuditRow(t, ctx, pool, auditSeed{
		AuditID:    "90000000-0000-4000-8000-000000000005",
		RecordedAt: base.Add(5 * time.Minute),
		Action:     "vaccination.verification.requested",
		Metadata: map[string]any{
			"domain": "pc", "module": "vaccination", "category": "verification", "result": "verification_pending", "status": "verification_pending",
		},
	})

	from := base.Add(-time.Hour)
	to := base.Add(time.Hour)
	page1, next, err := repo.List(ctx, domain.Query{TenantID: auditTestTenant, From: &from, To: &to, Limit: 2})
	if err != nil {
		t.Fatalf("page1 list: %v", err)
	}
	if len(page1) != 2 || page1[0].AuditID != "90000000-0000-4000-8000-000000000005" || page1[1].AuditID != "90000000-0000-4000-8000-000000000004" {
		t.Fatalf("page1 rows=%#v", page1)
	}
	if next == nil || next.AuditID != page1[1].AuditID || !next.RecordedAt.Equal(page1[1].RecordedAt) {
		t.Fatalf("next cursor=%#v want last returned row", next)
	}
	page2, _, err := repo.List(ctx, domain.Query{TenantID: auditTestTenant, From: &from, To: &to, Limit: 2, Cursor: next})
	if err != nil {
		t.Fatalf("page2 list: %v", err)
	}
	if len(page2) != 2 || page2[0].AuditID != "90000000-0000-4000-8000-000000000003" {
		t.Fatalf("page2 rows=%#v; boundary row was skipped", page2)
	}

	domainFilter := "vaccination"
	moduleFilter := "vaccination"
	categoryFilter := "stock_mismatch"
	filtered, _, err := repo.List(ctx, domain.Query{
		TenantID: auditTestTenant,
		From:     &from,
		To:       &to,
		Domain:   &domainFilter,
		Module:   &moduleFilter,
		Category: &categoryFilter,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("filtered list: %v", err)
	}
	if len(filtered) != 1 || filtered[0].AuditID != "90000000-0000-4000-8000-000000000003" {
		t.Fatalf("filtered rows=%#v", filtered)
	}

	anomalies, _, err := repo.List(ctx, domain.Query{TenantID: auditTestTenant, From: &from, To: &to, AnomaliesOnly: true, Limit: 10})
	if err != nil {
		t.Fatalf("anomaly list: %v", err)
	}
	if len(anomalies) != 2 || anomalies[0].AuditID != "90000000-0000-4000-8000-000000000003" || anomalies[1].AuditID != "90000000-0000-4000-8000-000000000001" {
		t.Fatalf("anomalies=%#v", anomalies)
	}

	search := "verification"
	searched, _, err := repo.List(ctx, domain.Query{TenantID: auditTestTenant, From: &from, To: &to, Search: &search, Limit: 10})
	if err != nil {
		t.Fatalf("search list: %v", err)
	}
	if len(searched) != 1 || searched[0].AuditID != "90000000-0000-4000-8000-000000000005" {
		t.Fatalf("searched=%#v", searched)
	}

	proofGaps, _, err := repo.List(ctx, domain.Query{TenantID: auditTestTenant, From: &from, To: &to, ProofGapsOnly: true, Limit: 10})
	if err != nil {
		t.Fatalf("proof gap list: %v", err)
	}
	if len(proofGaps) != 2 || proofGaps[0].AuditID != "90000000-0000-4000-8000-000000000005" || proofGaps[1].AuditID != "90000000-0000-4000-8000-000000000001" {
		t.Fatalf("proofGaps=%#v", proofGaps)
	}

	summary, err := repo.Summary(ctx, domain.Query{TenantID: auditTestTenant, From: &from, To: &to, Domain: &domainFilter, Module: &moduleFilter})
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.Actions != 4 || summary.AwaitingVerification != 1 || summary.ProofEvents != 2 || summary.ProofCoveragePercent != 50 || summary.Rework != 1 || summary.Anomalies != 2 {
		t.Fatalf("summary=%#v", summary)
	}
}

func TestOperationsAuditRepositoryDomainFallbacks(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	base := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	insertAuditRow(t, ctx, pool, auditSeed{
		AuditID:      "90000000-0000-4000-8000-000000000006",
		RecordedAt:   base.Add(6 * time.Minute),
		Action:       "feed.packing.pending_verification",
		ResourceType: "feed_packing_completion",
		Metadata: map[string]any{
			"result": "verification_pending", "status": "verification_pending",
		},
	})
	insertAuditRow(t, ctx, pool, auditSeed{
		AuditID:      "90000000-0000-4000-8000-000000000007",
		RecordedAt:   base.Add(7 * time.Minute),
		Action:       "weighing.observation_accepted",
		ResourceType: "weighing_observation",
		Metadata: map[string]any{
			"result": "accepted", "status": "accepted",
		},
	})
	insertAuditRow(t, ctx, pool, auditSeed{
		AuditID:      "90000000-0000-4000-8000-000000000008",
		RecordedAt:   base.Add(8 * time.Minute),
		Action:       "vaccination.completed",
		ResourceType: "obligation_instance",
		Metadata: map[string]any{
			"domain": "pc", "module": "vaccination", "result": "completed", "status": "completed",
		},
	})
	insertAuditRow(t, ctx, pool, auditSeed{
		AuditID:      "90000000-0000-4000-8000-000000000009",
		RecordedAt:   base.Add(9 * time.Minute),
		Action:       "goat.created",
		ResourceType: "goat",
		Metadata: map[string]any{
			"result": "recorded", "status": "recorded",
		},
	})
	insertAuditRow(t, ctx, pool, auditSeed{
		AuditID:      "90000000-0000-4000-8000-000000000010",
		RecordedAt:   base.Add(10 * time.Minute),
		Action:       "inventory.stock_mismatch",
		ResourceType: "inventory_adjustment",
		Metadata: map[string]any{
			"result": "mismatch", "status": "mismatch",
		},
	})

	from := base.Add(-time.Hour)
	to := base.Add(time.Hour)

	feedDomain := "feed"
	feedModule := "feed_packing"
	feedRows, _, err := repo.List(ctx, domain.Query{TenantID: auditTestTenant, From: &from, To: &to, Domain: &feedDomain, Module: &feedModule, Limit: 10})
	if err != nil {
		t.Fatalf("fallback feed list: %v", err)
	}
	if len(feedRows) != 1 || feedRows[0].AuditID != "90000000-0000-4000-8000-000000000006" || feedRows[0].Metadata["domain"] != "feed" || feedRows[0].Metadata["module"] != "feed_packing" {
		t.Fatalf("fallback feed rows=%#v", feedRows)
	}

	weighingDomain := "weighing"
	weighingSummary, err := repo.Summary(ctx, domain.Query{TenantID: auditTestTenant, From: &from, To: &to, Domain: &weighingDomain})
	if err != nil {
		t.Fatalf("fallback weighing summary: %v", err)
	}
	if weighingSummary.Actions != 1 {
		t.Fatalf("weighingSummary.Actions=%d want 1", weighingSummary.Actions)
	}

	vaccinationDomain := "vaccination"
	vaccinationRows, _, err := repo.List(ctx, domain.Query{TenantID: auditTestTenant, From: &from, To: &to, Domain: &vaccinationDomain, Limit: 10})
	if err != nil {
		t.Fatalf("fallback vaccination list: %v", err)
	}
	if len(vaccinationRows) != 1 || vaccinationRows[0].AuditID != "90000000-0000-4000-8000-000000000008" || vaccinationRows[0].Metadata["domain"] != "vaccination" {
		t.Fatalf("fallback vaccination rows=%#v", vaccinationRows)
	}

	procurementDomain := "procurement"
	procurementRows, _, err := repo.List(ctx, domain.Query{TenantID: auditTestTenant, From: &from, To: &to, Domain: &procurementDomain, Limit: 10})
	if err != nil {
		t.Fatalf("fallback procurement list: %v", err)
	}
	if len(procurementRows) != 1 || procurementRows[0].AuditID != "90000000-0000-4000-8000-000000000009" || procurementRows[0].Metadata["module"] != "source_entry" {
		t.Fatalf("fallback procurement rows=%#v", procurementRows)
	}

	otherDomain := "other"
	otherRows, _, err := repo.List(ctx, domain.Query{TenantID: auditTestTenant, From: &from, To: &to, Domain: &otherDomain, Limit: 10})
	if err != nil {
		t.Fatalf("fallback other list: %v", err)
	}
	if len(otherRows) != 1 || otherRows[0].AuditID != "90000000-0000-4000-8000-000000000010" || otherRows[0].Metadata["domain"] != "other" {
		t.Fatalf("fallback other rows=%#v", otherRows)
	}
}

type auditSeed struct {
	AuditID      string
	RecordedAt   time.Time
	Action       string
	ResourceType string
	Metadata     map[string]any
}

func insertAuditRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, seed auditSeed) {
	t.Helper()
	metadata, err := json.Marshal(seed.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	resourceType := seed.ResourceType
	if resourceType == "" {
		resourceType = "goat"
	}
	_, err = pool.Exec(ctx, `
INSERT INTO audit_log (
  audit_id,
  tenant_id,
  actor_type,
  actor_id,
  action,
  resource_type,
  resource_id,
  scope_type,
  scope_id,
  metadata,
  trace_id,
  recorded_at
) VALUES (
  $1::uuid,
  $2::uuid,
  'human',
  '10000000-0000-4000-8000-000000000001'::uuid,
  $3,
  $4,
  '71000000-0000-4000-8000-000000000001'::uuid,
  'shed',
  '71000000-0000-4000-8000-000000000002'::uuid,
  $5::jsonb,
  'test-trace',
  $6::timestamptz
)`, seed.AuditID, auditTestTenant, seed.Action, resourceType, metadata, seed.RecordedAt)
	if err != nil {
		t.Fatalf("insert audit row %s: %v", seed.AuditID, err)
	}
}
