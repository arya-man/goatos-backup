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

// TestRescheduleObligationByIDCreatesNewObligationForMissed is the regression guard for the P0 kernel
// bug this file's other tests were written alongside: 'missed' is immutable closed history
// (docs/protocol-engine/state-machines.md, "Conventions" — completed/waived/canceled/superseded/missed
// rows are never rewritten; a later policy correction creates new work). Rescheduling a missed
// obligation must NOT flip its status back to 'scheduled' in place; it must insert a brand-new
// obligation for the new due date and leave the missed row's status/due_at/row_version untouched.
func TestRescheduleObligationByIDCreatesNewObligationForMissed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	originalDue := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC) // matches seed()'s DueAt
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='missed' WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, obA); err != nil {
		t.Fatalf("mark seed obligation missed: %v", err)
	}

	newDue := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	newID, isReplay, err := repo.RescheduleObligationByID(ctx, tenantID, obA, "resched-missed-1", newDue, newDue, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("reschedule missed obligation: %v", err)
	}
	if isReplay {
		t.Fatalf("first call must not be flagged as a replay")
	}
	if newID == "" || newID == obA {
		t.Fatalf("newID = %q, want a fresh id distinct from the missed obligation %q", newID, obA)
	}

	if got := scanStatus(t, ctx, pool, obA); got != "missed" {
		t.Fatalf("missed obligation status = %q, want unchanged 'missed'", got)
	}
	var origDueAt time.Time
	var origRowVersion int
	if err := pool.QueryRow(ctx, `SELECT due_at, row_version FROM obligation_instances WHERE obligation_id=$1`, obA).Scan(&origDueAt, &origRowVersion); err != nil {
		t.Fatalf("read missed row: %v", err)
	}
	if !origDueAt.Equal(originalDue) {
		t.Fatalf("missed obligation due_at = %s, want unchanged %s", origDueAt, originalDue)
	}
	if origRowVersion != 1 {
		t.Fatalf("missed obligation row_version = %d, want unchanged 1 (never mutated)", origRowVersion)
	}

	if got := scanStatus(t, ctx, pool, newID); got != "scheduled" {
		t.Fatalf("new obligation status = %q, want 'scheduled'", got)
	}
	var newDueAt time.Time
	if err := pool.QueryRow(ctx, `SELECT due_at FROM obligation_instances WHERE obligation_id=$1`, newID).Scan(&newDueAt); err != nil {
		t.Fatalf("read new obligation: %v", err)
	}
	if !newDueAt.Equal(newDue) {
		t.Fatalf("new obligation due_at = %s, want %s", newDueAt, newDue)
	}

	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='scheduled' AND payload->>'reason'='mobile_reschedule_of_missed' AND payload->>'superseded_missed_obligation_id'=$3`,
		tenantID, newID, obA); got != 1 {
		t.Fatalf("want 1 rework-scheduled event linking new obligation back to missed one, got %d", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2`,
		tenantID, obA); got != 0 {
		t.Fatalf("missed obligation must have zero new status events (never touched), got %d", got)
	}

	// Idempotent redelivery: same idempotency key + same payload must replay without a second insert.
	replayID, replayIsReplay, err := repo.RescheduleObligationByID(ctx, tenantID, obA, "resched-missed-1", newDue, newDue, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("replay reschedule missed obligation: %v", err)
	}
	if !replayIsReplay {
		t.Fatalf("replay call must be flagged as a replay")
	}
	if replayID != newID {
		t.Fatalf("replay id = %q, want the same new obligation id %q", replayID, newID)
	}
	if got := scanStatus(t, ctx, pool, obA); got != "missed" {
		t.Fatalf("missed obligation status after replay = %q, want unchanged 'missed'", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND protocol_version_id=(SELECT protocol_version_id FROM obligation_instances WHERE obligation_id=$2) AND rule_id=(SELECT rule_id FROM obligation_instances WHERE obligation_id=$2) AND target_id=(SELECT target_id FROM obligation_instances WHERE obligation_id=$2) AND due_at=$3`,
		tenantID, obA, newDue); got != 1 {
		t.Fatalf("want exactly 1 obligation at the new due date after replay (no duplicate insert), got %d", got)
	}
}

// TestRescheduleObligationByIDForMissedConvergesUnderDifferentIdempotencyKey is the regression guard for
// the P2 bug: RescheduleObligationByID's own idempotency reservation only dedups an EXACT key match, so a
// SECOND reschedule request for the SAME missed obligation to the SAME new due date, but under a
// DIFFERENT outer idempotency key (for example a retried mobile request that generated a fresh key),
// reaches insertReworkObligationForMissed's InsertObligationInstance a second time. That INSERT's own
// "WHERE NOT EXISTS" dedup guard then affects 0 rows because the first call's rework obligation is still
// open at the same logical target (protocol_version_id, rule_id, target, sequence, due_at) — this must be
// convergent (return the existing rework obligation as an idempotent success), not surface an internal
// error.
func TestRescheduleObligationByIDForMissedConvergesUnderDifferentIdempotencyKey(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='missed' WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, obA); err != nil {
		t.Fatalf("mark seed obligation missed: %v", err)
	}

	newDue := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	firstID, firstReplay, err := repo.RescheduleObligationByID(ctx, tenantID, obA, "resched-missed-conv-key-1", newDue, newDue, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("first reschedule of missed obligation: %v", err)
	}
	if firstReplay || firstID == "" || firstID == obA {
		t.Fatalf("first call: id=%q isReplay=%v, want a fresh id distinct from %q and isReplay=false", firstID, firstReplay, obA)
	}

	// Same missed obligation, same new due date, but a DIFFERENT request idempotency key -- must
	// converge on the existing rework obligation instead of erroring.
	secondID, secondReplay, err := repo.RescheduleObligationByID(ctx, tenantID, obA, "resched-missed-conv-key-2", newDue, newDue, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("second reschedule under a different idempotency key must converge, not error: %v", err)
	}
	if secondID != firstID {
		t.Fatalf("second call id = %q, want the same existing rework obligation %q (convergent no-op)", secondID, firstID)
	}
	// This is not an exact replay of the SAME outer key, so the reservation itself is new -- the
	// convergence happens one layer down, inside the rework insert.
	if secondReplay {
		t.Fatalf("second call used a different idempotency key, so it must not be flagged as an exact-key replay")
	}

	if got := scanStatus(t, ctx, pool, obA); got != "missed" {
		t.Fatalf("missed obligation status after second call = %q, want unchanged 'missed'", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND protocol_version_id=(SELECT protocol_version_id FROM obligation_instances WHERE obligation_id=$2) AND rule_id=(SELECT rule_id FROM obligation_instances WHERE obligation_id=$2) AND target_id=(SELECT target_id FROM obligation_instances WHERE obligation_id=$2) AND due_at=$3`,
		tenantID, obA, newDue); got != 1 {
		t.Fatalf("want exactly 1 obligation at the new due date after the second (different-key) call, got %d", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='scheduled' AND payload->>'reason'='mobile_reschedule_of_missed'`,
		tenantID, firstID); got != 1 {
		t.Fatalf("want exactly 1 rework-scheduled event on the existing obligation (no duplicate side effect), got %d", got)
	}
}
