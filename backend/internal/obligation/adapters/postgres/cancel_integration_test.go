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
	obC, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: "obl-missed-before-exit", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert obC: applied=%v err=%v", applied, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='missed' WHERE obligation_id=$1`, obC); err != nil {
		t.Fatalf("miss obC: %v", err)
	}

	n, err := repo.CancelOpenForGoat(ctx, tenantID, testGoatID, "ineligible_after_exit")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 cancelled (obA + missed obC), got %d", n)
	}

	if got := scanStatus(t, ctx, pool, obA); got != "canceled" {
		t.Fatalf("obA status: want canceled, got %s", got)
	}
	if got := scanStatus(t, ctx, pool, obB); got != "completed" {
		t.Fatalf("obB (completed) must be untouched, got %s", got)
	}
	if got := scanStatus(t, ctx, pool, obC); got != "canceled" {
		t.Fatalf("obC (missed before exit) must be canceled, got %s", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='canceled'`,
		tenantID, obA); got != 1 {
		t.Fatalf("expected 1 canceled event for obA, got %d", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='canceled'`,
		tenantID, obC); got != 1 {
		t.Fatalf("expected 1 canceled event for obC, got %d", got)
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

	occurredAt := time.Date(2026, time.July, 9, 10, 11, 12, 0, time.UTC)
	if err := bus.Publish(ctx, eventbus.Event{ID: "event-exit-1", Type: oblapp.EventGoatExited, TenantID: tenantID, Key: testGoatID, OccurredAt: occurredAt}); err != nil {
		t.Fatalf("publish goat.exited: %v", err)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "canceled" {
		t.Fatalf("obA should be canceled via bus, got %s", got)
	}
	if got := scanEventOccurredAt(t, ctx, pool, obA); !got.Equal(occurredAt) {
		t.Fatalf("canceled event occurred_at = %s, want %s", got, occurredAt)
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

func scanEventOccurredAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, obligationID string) time.Time {
	t.Helper()
	var occurredAt time.Time
	if err := pool.QueryRow(ctx, `
SELECT occurred_at
FROM obligation_status_events
WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='canceled'
ORDER BY occurred_at DESC
LIMIT 1`, tenantID, obligationID).Scan(&occurredAt); err != nil {
		t.Fatalf("scan canceled occurred_at: %v", err)
	}
	return occurredAt.UTC()
}
