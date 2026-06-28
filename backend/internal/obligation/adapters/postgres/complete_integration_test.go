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
	obC, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC), Status: "in_progress",
		IdempotencyKey: "obl-missed-in-progress-control", Sequence: 2,
	})
	if err != nil || !applied {
		t.Fatalf("insert obC: applied=%v err=%v", applied, err)
	}

	n, err := repo.MarkMissedBefore(ctx, tenantID, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), 100)
	if err != nil {
		t.Fatalf("mark missed: %v", err)
	}
	if n != 2 {
		t.Fatalf("marked missed = %d, want 2", n)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "missed" {
		t.Fatalf("obA status: want missed, got %s", got)
	}
	if got := scanStatus(t, ctx, pool, obB); got != "completed" {
		t.Fatalf("obB status: want completed, got %s", got)
	}
	if got := scanStatus(t, ctx, pool, obC); got != "missed" {
		t.Fatalf("obC status: want missed, got %s", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='missed'`,
		tenantID, obA); got != 1 {
		t.Fatalf("expected 1 missed event, got %d", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='obligation.missed'`,
		tenantID, obA); got != 1 {
		t.Fatalf("expected 1 missed outbox event, got %d", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='obligation.missed'`,
		tenantID, obA); got != 1 {
		t.Fatalf("expected 1 missed audit event, got %d", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='obligation.missed'`,
		tenantID, obC); got != 1 {
		t.Fatalf("expected 1 in-progress missed audit event, got %d", got)
	}

	replay, err := repo.MarkMissedBefore(ctx, tenantID, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), 100)
	if err != nil {
		t.Fatalf("replay mark missed: %v", err)
	}
	if replay != 0 {
		t.Fatalf("replay marked missed = %d, want 0", replay)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='obligation.missed'`,
		tenantID, obA); got != 1 {
		t.Fatalf("replay missed audit event count = %d, want 1", got)
	}
}

func TestMarkCompletedAllowsLateMissedObligation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	if n, err := repo.MarkMissedBefore(ctx, tenantID, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), 100); err != nil || n != 1 {
		t.Fatalf("mark missed: n=%d err=%v", n, err)
	}

	ok, err := repo.MarkCompleted(ctx, tenantID, obA)
	if err != nil || !ok {
		t.Fatalf("late complete missed obligation: ok=%v err=%v", ok, err)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "completed" {
		t.Fatalf("status: want completed, got %s", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='missed'`,
		tenantID, obA); got != 1 {
		t.Fatalf("missed event count = %d, want 1", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='completed'`,
		tenantID, obA); got != 1 {
		t.Fatalf("completed event count = %d, want 1", got)
	}
}

func TestMarkMissedBeforeSkipsDeferredObligations(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='deferred' WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, obA); err != nil {
		t.Fatalf("defer obligation: %v", err)
	}
	repo := NewRepository(pool, 5*time.Second)
	n, err := repo.MarkMissedBefore(ctx, tenantID, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), 100)
	if err != nil {
		t.Fatalf("mark missed: %v", err)
	}
	if n != 0 {
		t.Fatalf("marked deferred obligation missed: n=%d, want 0", n)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "deferred" {
		t.Fatalf("deferred obligation status = %s, want deferred", got)
	}
}

func TestCancelOpenForGoatCancelsInProgress(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='in_progress' WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, obA); err != nil {
		t.Fatalf("set in_progress: %v", err)
	}
	repo := NewRepository(pool, 5*time.Second)
	n, err := repo.CancelOpenForGoat(ctx, tenantID, testGoatID, "dead")
	if err != nil {
		t.Fatalf("cancel open: %v", err)
	}
	if n != 1 {
		t.Fatalf("canceled = %d, want 1", n)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "canceled" {
		t.Fatalf("status: want canceled, got %s", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='canceled'`,
		tenantID, obA); got != 1 {
		t.Fatalf("canceled event count = %d, want 1", got)
	}
}
