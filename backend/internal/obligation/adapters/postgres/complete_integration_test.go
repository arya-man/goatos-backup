package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestSM5aMarkCompleted(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool) // scheduled obligation for testGoatID
	repo := NewRepository(pool, 5*time.Second)

	ok, err := repo.MarkCompleted(ctx, tenantID, obA)
	if err != nil || !ok {
		t.Fatalf("mark completed: ok=%v err=%v", ok, err)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "completed" {
		t.Fatalf("status: want completed, got %s", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='completed'`,
		tenantID, obA); got != 1 {
		t.Fatalf("expected 1 completed event, got %d", got)
	}

	ok2, err := repo.MarkCompleted(ctx, tenantID, obA)
	if err != nil {
		t.Fatalf("re-complete: %v", err)
	}
	if ok2 {
		t.Fatalf("re-complete should be a no-op (already terminal)")
	}
}

func TestMarkMissedBeforeMaterializesCanonicalStatus(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	obB, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled",
		IdempotencyKey: "obl-missed-terminal-control", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert obB: applied=%v err=%v", applied, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='completed' WHERE obligation_id=$1`, obB); err != nil {
		t.Fatalf("complete obB: %v", err)
	}

	n, err := repo.MarkMissedBefore(ctx, tenantID, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), 100)
	if err != nil {
		t.Fatalf("mark missed: %v", err)
	}
	if n != 1 {
		t.Fatalf("marked missed = %d, want 1", n)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "missed" {
		t.Fatalf("obA status: want missed, got %s", got)
	}
	if got := scanStatus(t, ctx, pool, obB); got != "completed" {
		t.Fatalf("obB status: want completed, got %s", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='missed'`,
		tenantID, obA); got != 1 {
		t.Fatalf("expected 1 missed event, got %d", got)
	}

	replay, err := repo.MarkMissedBefore(ctx, tenantID, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), 100)
	if err != nil {
		t.Fatalf("replay mark missed: %v", err)
	}
	if replay != 0 {
		t.Fatalf("replay marked missed = %d, want 0", replay)
	}
}
