package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestRescheduleObligationByIDMovesOpenObligation proves the mobile "reschedule this obligation" write
// path moves an open (here: 'due', overdue) obligation to a new future due date and flips it back to
// 'scheduled'.
func TestRescheduleObligationByIDMovesOpenObligation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='due' WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, obA); err != nil {
		t.Fatalf("mark seed obligation due: %v", err)
	}

	newDue := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	id, isReplay, err := repo.RescheduleObligationByID(ctx, tenantID, obA, "resched-key-1", newDue, newDue, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("reschedule: %v", err)
	}
	if id != obA || isReplay {
		t.Fatalf("reschedule id=%q isReplay=%v, want id=%q isReplay=false", id, isReplay, obA)
	}

	var status string
	var dueAt time.Time
	var rowVersion int
	if err := pool.QueryRow(ctx, `SELECT status, due_at, row_version FROM obligation_instances WHERE obligation_id=$1`, obA).Scan(&status, &dueAt, &rowVersion); err != nil {
		t.Fatalf("read rescheduled row: %v", err)
	}
	if status != "scheduled" {
		t.Fatalf("status = %q want scheduled", status)
	}
	if !dueAt.Equal(newDue) {
		t.Fatalf("due_at = %s want %s", dueAt, newDue)
	}
	if rowVersion != 2 {
		t.Fatalf("row_version = %d want 2 (incremented once)", rowVersion)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='scheduled' AND idempotency_key LIKE '%:rescheduled:%'`,
		tenantID, obA); got != 1 {
		t.Fatalf("want 1 rescheduled status event, got %d", got)
	}
}

// TestRescheduleObligationByIDDetachesPlannedBatch proves an obligation still attached to a 'planned'
// batch is detached (batch_id cleared, estimated_targets decremented) on reschedule, mirroring
// DeferOpenObligationByIdempotencyKey's detachPlannedBatch pattern — a moved due date may no longer
// belong to that drive.
func TestRescheduleObligationByIDDetachesPlannedBatch(t *testing.T) {
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
		Status:            "planned",
		EstimatedTargets:  1,
		PlannedQuantity:   "0",
		QuantityUnit:      "dose",
	})
	if err != nil {
		t.Fatalf("create planned batch: %v", err)
	}
	if attached, err := repo.AttachObligationsToBatch(ctx, tenantID, batchID, []string{obA}); err != nil || attached != 1 {
		t.Fatalf("attach precondition: attached=%d err=%v", attached, err)
	}

	newDue := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	id, isReplay, err := repo.RescheduleObligationByID(ctx, tenantID, obA, "resched-batch-1", newDue, newDue, nil, time.Now().UTC())
	if err != nil || id != obA || isReplay {
		t.Fatalf("reschedule: id=%q isReplay=%v err=%v", id, isReplay, err)
	}

	var storedBatchID *string
	if err := pool.QueryRow(ctx, `SELECT batch_id::text FROM obligation_instances WHERE obligation_id=$1`, obA).Scan(&storedBatchID); err != nil {
		t.Fatalf("read batch_id: %v", err)
	}
	if storedBatchID != nil {
		t.Fatalf("batch_id = %v, want NULL (detached from planned batch)", *storedBatchID)
	}
	var estimatedTargets int
	if err := pool.QueryRow(ctx, `SELECT estimated_targets FROM obligation_batches WHERE batch_id=$1`, batchID).Scan(&estimatedTargets); err != nil {
		t.Fatalf("read estimated_targets: %v", err)
	}
	if estimatedTargets != 0 {
		t.Fatalf("estimated_targets = %d want 0 (decremented)", estimatedTargets)
	}
}

// TestRescheduleObligationByIDExactReplayIsNoOp proves calling with the SAME idempotency key and SAME
// payload returns the original result without a second mutation or a second status event.
func TestRescheduleObligationByIDExactReplayIsNoOp(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	newDue := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	firstID, firstReplay, err := repo.RescheduleObligationByID(ctx, tenantID, obA, "resched-replay-1", newDue, newDue, nil, time.Now().UTC())
	if err != nil || firstID != obA || firstReplay {
		t.Fatalf("first call: id=%q isReplay=%v err=%v", firstID, firstReplay, err)
	}
	if got := countRows(t, ctx, pool, `SELECT row_version FROM obligation_instances WHERE obligation_id=$1`, obA); got != 2 {
		t.Fatalf("row_version after first call = %d want 2", got)
	}

	secondID, secondReplay, err := repo.RescheduleObligationByID(ctx, tenantID, obA, "resched-replay-1", newDue, newDue, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("replay call: %v", err)
	}
	if secondID != obA || !secondReplay {
		t.Fatalf("replay call: id=%q isReplay=%v, want id=%q isReplay=true", secondID, secondReplay, obA)
	}
	if got := countRows(t, ctx, pool, `SELECT row_version FROM obligation_instances WHERE obligation_id=$1`, obA); got != 2 {
		t.Fatalf("row_version after replay = %d want unchanged at 2 (no double-mutation)", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='scheduled' AND idempotency_key LIKE '%:rescheduled:%'`,
		tenantID, obA); got != 1 {
		t.Fatalf("want exactly 1 rescheduled status event after replay, got %d", got)
	}
}

// TestRescheduleObligationByIDSameKeyDifferentPayloadConflicts proves a same-key/different-payload
// replay is rejected with ports.ErrIdempotencyConflict and the row is left untouched.
func TestRescheduleObligationByIDSameKeyDifferentPayloadConflicts(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	firstDue := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	if _, isReplay, err := repo.RescheduleObligationByID(ctx, tenantID, obA, "resched-conflict-1", firstDue, firstDue, nil, time.Now().UTC()); err != nil || isReplay {
		t.Fatalf("first call: isReplay=%v err=%v", isReplay, err)
	}

	differentDue := time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC)
	if _, _, err := repo.RescheduleObligationByID(ctx, tenantID, obA, "resched-conflict-1", differentDue, differentDue, nil, time.Now().UTC()); err == nil {
		t.Fatalf("expected ErrIdempotencyConflict, got nil error")
	} else if err != ports.ErrIdempotencyConflict {
		t.Fatalf("err = %v, want ports.ErrIdempotencyConflict", err)
	}

	var status string
	var dueAt time.Time
	var rowVersion int
	if err := pool.QueryRow(ctx, `SELECT status, due_at, row_version FROM obligation_instances WHERE obligation_id=$1`, obA).Scan(&status, &dueAt, &rowVersion); err != nil {
		t.Fatalf("read row after conflict: %v", err)
	}
	if !dueAt.Equal(firstDue) {
		t.Fatalf("due_at after conflicting replay = %s, want unchanged at %s", dueAt, firstDue)
	}
	if rowVersion != 2 {
		t.Fatalf("row_version after conflicting replay = %d, want unchanged at 2", rowVersion)
	}
}

// TestRescheduleObligationByIDRejectsDeferredObligation is a regression guard for the original bug: a
// 'deferred' (health-held) obligation must NOT be reschedulable through this new obligation_id-scoped
// path. This proves the SM-2 health-recovery reopen mechanism
// (ReopenDeferredObligationByIdempotencyKey) remains the ONLY way to reopen a deferred row — this path
// was not accidentally widened to also cover it.
func TestRescheduleObligationByIDRejectsDeferredObligation(t *testing.T) {
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

	newDue := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	id, isReplay, err := repo.RescheduleObligationByID(ctx, tenantID, obA, "resched-deferred-1", newDue, newDue, nil, time.Now().UTC())
	if err == nil {
		t.Fatalf("expected ErrNotFound for a deferred obligation, got id=%q isReplay=%v", id, isReplay)
	}
	if err != ports.ErrNotFound {
		t.Fatalf("err = %v, want ports.ErrNotFound", err)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "deferred" {
		t.Fatalf("deferred obligation must stay deferred, got %s", got)
	}
}
