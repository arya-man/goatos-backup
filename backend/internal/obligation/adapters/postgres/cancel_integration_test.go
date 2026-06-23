package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestSM3CancelOpenForGoat(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool) // 'obl-1' scheduled for testGoatID
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	// A second obligation, then marked completed (must NOT be cancelled).
	obB, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: "obl-2", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert obB: applied=%v err=%v", applied, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='completed' WHERE obligation_id=$1`, obB); err != nil {
		t.Fatalf("complete obB: %v", err)
	}

	n, err := repo.CancelOpenForGoat(ctx, tenantID, testGoatID, "ineligible_after_exit")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 cancelled (obA), got %d", n)
	}

	if got := scanStatus(t, ctx, pool, obA); got != "canceled" {
		t.Fatalf("obA status: want canceled, got %s", got)
	}
	if got := scanStatus(t, ctx, pool, obB); got != "completed" {
		t.Fatalf("obB (completed) must be untouched, got %s", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='canceled'`,
		tenantID, obA); got != 1 {
		t.Fatalf("expected 1 canceled event for obA, got %d", got)
	}

	// Idempotent: re-run cancels nothing.
	n2, err := repo.CancelOpenForGoat(ctx, tenantID, testGoatID, "ineligible_after_exit")
	if err != nil {
		t.Fatalf("re-cancel: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("re-run should cancel 0, got %d", n2)
	}
}

func TestGoatExitedHandlerCancelsViaBus(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	bus := eventbus.NewInProcessBus()
	oblapp.NewGoatExitedHandler(repo).Register(bus)

	if err := bus.Publish(ctx, eventbus.Event{Type: oblapp.EventGoatExited, TenantID: tenantID, Key: testGoatID}); err != nil {
		t.Fatalf("publish goat.exited: %v", err)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "canceled" {
		t.Fatalf("obA should be canceled via bus, got %s", got)
	}
}

func scanStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(ctx, `SELECT status FROM obligation_instances WHERE obligation_id=$1`, id).Scan(&s); err != nil {
		t.Fatalf("scan status: %v", err)
	}
	return s
}
