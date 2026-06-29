package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
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

func TestDeferOpenObligationSkipsMissedRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	if n, err := repo.MarkMissedBefore(ctx, tenantID, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), 100); err != nil || n != 1 {
		t.Fatalf("mark missed: n=%d err=%v", n, err)
	}

	id, changed, err := repo.DeferOpenObligationByIdempotencyKey(ctx, tenantID, "obl-1", "sick", time.Now().UTC())
	if err != nil {
		t.Fatalf("defer missed row: %v", err)
	}
	if changed || id != obA {
		t.Fatalf("defer missed row changed=%v id=%q, want no-op for %s", changed, id, obA)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "missed" {
		t.Fatalf("missed obligation status = %s, want missed", got)
	}
	if got := countRows(t, ctx, pool, `
SELECT count(*)
FROM obligation_status_events
WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='deferred'`, tenantID, obA); got != 0 {
		t.Fatalf("defer missed row wrote deferred events=%d, want 0", got)
	}
}

func TestReopenDeferredObligationClearsStaleBatch(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	batchID, err := repo.CreateBatch(ctx, domain.NewBatch{
		TenantID:          tenantID,
		ProtocolVersionID: mustVersionOf(t, ctx, pool),
		ScopeType:         "park",
		ScopeID:           cbePark,
		Session:           "morning",
		Status:            "in_progress",
		EstimatedTargets:  1,
		PlannedQuantity:   "0",
		QuantityUnit:      "dose",
	})
	if err != nil {
		t.Fatalf("create in-progress batch: %v", err)
	}
	if attached, err := repo.AttachObligationsToBatch(ctx, tenantID, batchID, []string{obA}); err != nil || attached != 1 {
		t.Fatalf("attach stale batch precondition: attached=%d err=%v", attached, err)
	}
	if _, changed, err := repo.DeferOpenObligationByIdempotencyKey(ctx, tenantID, "obl-1", "sick", time.Now().UTC()); err != nil || !changed {
		t.Fatalf("defer held obligation: changed=%v err=%v", changed, err)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2 AND status='deferred' AND batch_id=$3`,
		tenantID, obA, batchID); got != 1 {
		t.Fatalf("precondition: deferred obligation should still carry non-planned batch_id, got %d", got)
	}

	id, changed, err := repo.ReopenDeferredObligationByIdempotencyKey(ctx, tenantID, "obl-1", time.Now().UTC())
	if err != nil || !changed || id != obA {
		t.Fatalf("reopen: id=%q changed=%v err=%v", id, changed, err)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2 AND status='scheduled' AND batch_id IS NULL`,
		tenantID, obA); got != 1 {
		t.Fatalf("reopened obligation must be unbatched and schedulable, got %d", got)
	}
}

func TestReopenDeferredObligationSkipsProcurementExcludedVaccination(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	if _, changed, err := repo.DeferOpenObligationByIdempotencyKey(ctx, tenantID, "obl-1", "sick", time.Now().UTC()); err != nil || !changed {
		t.Fatalf("defer held obligation: changed=%v err=%v", changed, err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE goats
SET lifecycle_status='dead'
WHERE tenant_id=$1 AND goat_id=$2`, tenantID, testGoatID); err != nil {
		t.Fatalf("mark excluded goat dead: %v", err)
	}

	id, changed, err := repo.ReopenDeferredObligationByIdempotencyKey(ctx, tenantID, "obl-1", time.Now().UTC())
	if err != nil {
		t.Fatalf("reopen excluded vaccination obligation: %v", err)
	}
	if changed || id != "" {
		t.Fatalf("excluded reopen changed=%v id=%q, want no-op", changed, id)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "deferred" {
		t.Fatalf("excluded deferred obligation status = %s, want deferred", got)
	}
}
