package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestSM3CancelIncludesDeferredWork proves a goat held (deferred) for sick/ICU/quarantine and then
// exited (death/sale) does not strand its held obligation: SM-3 cancellation includes 'deferred'.
func TestSM3CancelIncludesDeferredWork(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	if _, changed, err := repo.DeferOpenObligationByIdempotencyKey(ctx, tenantID, "obl-1", "sick", time.Now().UTC()); err != nil || !changed {
		t.Fatalf("defer held obligation: changed=%v err=%v", changed, err)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "deferred" {
		t.Fatalf("precondition: want deferred, got %s", got)
	}

	n, err := repo.CancelOpenForGoat(ctx, tenantID, testGoatID, "ineligible_after_exit")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 canceled (the held obligation), got %d", n)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "canceled" {
		t.Fatalf("deferred work must be canceled on exit, got %s", got)
	}
}

// TestReopenDeferredObligationOnRecovery proves a recovered goat's held obligation is reopened to
// 'scheduled' (with a 'scheduled' status event) and that a replay after recovery is a safe no-op.
func TestReopenDeferredObligationOnRecovery(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	if _, changed, err := repo.DeferOpenObligationByIdempotencyKey(ctx, tenantID, "obl-1", "sick", time.Now().UTC()); err != nil || !changed {
		t.Fatalf("defer held obligation: changed=%v err=%v", changed, err)
	}

	id, changed, err := repo.ReopenDeferredObligationByIdempotencyKey(ctx, tenantID, "obl-1", time.Now().UTC())
	if err != nil || !changed || id != obA {
		t.Fatalf("reopen: id=%q changed=%v err=%v", id, changed, err)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "scheduled" {
		t.Fatalf("recovered obligation must be scheduled, got %s", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='scheduled'`,
		tenantID, obA); got != 1 {
		t.Fatalf("want 1 scheduled (reopen) event, got %d", got)
	}

	// Idempotent replay: already schedulable -> no-op, no second event.
	if _, changed2, err := repo.ReopenDeferredObligationByIdempotencyKey(ctx, tenantID, "obl-1", time.Now().UTC()); err != nil {
		t.Fatalf("reopen replay: %v", err)
	} else if changed2 {
		t.Fatalf("reopen replay should be a no-op")
	}
}
