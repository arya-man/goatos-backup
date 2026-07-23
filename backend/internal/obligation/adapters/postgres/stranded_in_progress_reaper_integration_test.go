package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// BUG-015 proof. MarkCompleted flips every still-open sibling on a batch to 'in_progress' the moment
// the FIRST animal in a drive is completed (see inprogress_integration_test.go). That in_progress
// state is deliberately EXEMPT from the ordinary missed sweep: MarkMissedBefore's candidate CTE
// carries `NOT (oi.status = 'in_progress' AND COALESCE(ob.status,'') = 'in_progress')`, so an
// obligation on a running drive is never marked missed while the drive is running.
//
// The defect: nothing ever ends a drive that is abandoned. If the operator's device dies after
// animal 1 of 60, the batch stays 'in_progress' and its 59 siblings stay 'in_progress' FOREVER --
// invisible to the missed sweep, invisible to escalation, and therefore silently un-worked. The fix
// is reapStrandedInProgress: a grace-window reaper, run from MarkMissedBefore, that marks stranded
// in_progress rows missed once nothing on the batch has been touched for 12h.
//
// These tests drive the REAL production entry point (MarkMissedBefore), never the private helper.

// ageDrive simulates abandonment: nothing on the batch or its obligations has been touched for the
// given interval. COALESCE(ob.updated_at, oi.updated_at) is what the reaper's grace window reads.
func ageDrive(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batchID, interval string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_batches SET updated_at = now() - $3::interval WHERE tenant_id=$1 AND batch_id=$2`,
		tenantID, batchID, interval); err != nil {
		t.Fatalf("age batch updated_at: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_instances SET updated_at = now() - $3::interval WHERE tenant_id=$1 AND batch_id=$2`,
		tenantID, batchID, interval); err != nil {
		t.Fatalf("age obligation updated_at: %v", err)
	}
}

// seedStrandedDrive builds the shared fixture: three obligations on one batch, animal 1 completed so
// the batch is genuinely 'in_progress' and obB/obC are genuinely stranded 'in_progress'.
func seedStrandedDrive(t *testing.T, ctx context.Context, repo *Repository, versionID, ruleID, session string, dueAt time.Time, keyPrefix string) (batchID, obA, obB, obC string) {
	t.Helper()
	insert := func(key string, offsetHours int, seq int32) string {
		t.Helper()
		id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
			DueAt: dueAt.Add(time.Duration(offsetHours) * time.Hour), Status: "scheduled",
			IdempotencyKey: key, Sequence: seq,
		})
		if err != nil || !applied {
			t.Fatalf("insert obligation %s: applied=%v err=%v", key, applied, err)
		}
		return id
	}
	obA = insert(keyPrefix+"-a", 0, 1)
	obB = insert(keyPrefix+"-b", 1, 2)
	obC = insert(keyPrefix+"-c", 2, 3)

	var err error
	batchID, err = repo.CreateBatch(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
		Session: session, Status: "planned", EstimatedTargets: 3, PlannedQuantity: "3", QuantityUnit: "dose",
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if attached, err := repo.AttachObligationsToBatch(ctx, tenantID, batchID, []string{obA, obB, obC}); err != nil || attached != 3 {
		t.Fatalf("attach batch: attached=%d err=%v", attached, err)
	}
	// Animal 1 completed -> batch 'in_progress', obB/obC stranded 'in_progress'.
	if ok, err := repo.MarkCompleted(ctx, tenantID, obA); err != nil || !ok {
		t.Fatalf("complete obA: ok=%v err=%v", ok, err)
	}
	return batchID, obA, obB, obC
}

// TestMarkMissedBeforeReapsStrandedInProgressAfterGraceWindow is the RED/GREEN anchor for BUG-015.
// The batch's last touch is aged past the 12h grace window (the operator never came back), so the
// production sweeper entry point must reap the stranded siblings to 'missed'.
func TestMarkMissedBeforeReapsStrandedInProgressAfterGraceWindow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	_ = seed(t, ctx, pool) // seeds tenant/party/park/goat/protocol/rule + one unrelated obligation
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	batchID, obA, obB, obC := seedStrandedDrive(t, ctx, repo, versionID, ruleID, "stranded-drive",
		time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), "obl-stranded")

	// Fixture invariants: the situation the reaper exists for actually exists.
	if got := scanTextObligation(t, ctx, pool,
		`SELECT status FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != "in_progress" {
		t.Fatalf("fixture invalid: batch status = %q, want in_progress", got)
	}
	for _, id := range []string{obB, obC} {
		if got := scanStatus(t, ctx, pool, id); got != "in_progress" {
			t.Fatalf("fixture invalid: sibling %s status = %s, want in_progress", id, got)
		}
	}
	if got := scanStatus(t, ctx, pool, obA); got != "completed" {
		t.Fatalf("fixture invalid: obA status = %s, want completed", got)
	}

	// The drive was abandoned: nothing on the batch or its rows has been touched for 48h.
	ageDrive(t, ctx, pool, batchID, "48 hours")

	// Production entry point. missedBefore = now, so the reaper's cutoff is now-12h.
	sweepAt := time.Now().UTC()
	if _, err := repo.MarkMissedBefore(ctx, tenantID, sweepAt, 1000); err != nil {
		t.Fatalf("MarkMissedBefore: %v", err)
	}

	for _, id := range []string{obB, obC} {
		if got := scanStatus(t, ctx, pool, id); got != "missed" {
			t.Fatalf("stranded sibling %s status = %s, want missed (an abandoned drive must not strand work in_progress forever)", id, got)
		}
	}
	if got := scanStatus(t, ctx, pool, obA); got != "completed" {
		t.Fatalf("completed obligation %s was mutated by the reaper: status = %s, want completed", obA, got)
	}

	// Attribution: these rows were reaped by the grace-window reaper, not incidentally by the
	// ordinary missed sweep (which is barred from touching in_progress rows on an in_progress batch).
	for _, id := range []string{obB, obC} {
		if got := countRows(t, ctx, pool,
			`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action='obligation.reaped_in_progress' AND resource_id=$2`,
			tenantID, id); got != 1 {
			t.Fatalf("reap audit rows for %s = %d, want 1", id, got)
		}
		if got := countRows(t, ctx, pool,
			`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='missed'`,
			tenantID, id); got != 1 {
			t.Fatalf("missed status events for %s = %d, want 1", id, got)
		}
		if got := countRows(t, ctx, pool,
			`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='obligation.missed' AND aggregate_id=$2`,
			tenantID, id); got != 1 {
			t.Fatalf("obligation.missed outbox rows for %s = %d, want 1", id, got)
		}
	}

	// Detachment parity with the ordinary missed path (repository.go MarkMissedBefore sets
	// batch_id = NULL on transition): a reaped obligation must not keep pointing at the dead drive,
	// otherwise re-planning would re-attach it to a batch that is never coming back.
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND obligation_id = ANY($2::uuid[]) AND batch_id IS NOT NULL`,
		tenantID, []string{obB, obC}); got != 0 {
		t.Fatalf("reaped obligations still attached to a batch: %d rows with batch_id, want 0", got)
	}

	// Replay safety: a second sweep must not re-emit events or re-audit.
	if _, err := repo.MarkMissedBefore(ctx, tenantID, time.Now().UTC(), 1000); err != nil {
		t.Fatalf("second MarkMissedBefore: %v", err)
	}
	for _, id := range []string{obB, obC} {
		if got := countRows(t, ctx, pool,
			`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='missed'`,
			tenantID, id); got != 1 {
			t.Fatalf("after replay: missed status events for %s = %d, want 1", id, got)
		}
	}
}

// TestMarkMissedBeforeSparesActivelyWorkedInProgressDrive is the safety direction: a drive that IS
// being worked (batch touched minutes ago) must survive the sweep untouched. If this fails the
// reaper is destroying live operator work.
func TestMarkMissedBeforeSparesActivelyWorkedInProgressDrive(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	batchID, _, obB, obC := seedStrandedDrive(t, ctx, repo, versionID, ruleID, "active-drive",
		time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC), "obl-active")

	// Age the rows deep into the past on the DUE axis but keep the batch freshly touched: this is an
	// overdue drive that an operator is working RIGHT NOW. Only the grace window may protect it.
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_instances
		 SET due_at = now() - interval '30 days' + (sequence * interval '1 hour'), window_end = NULL
		 WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); err != nil {
		t.Fatalf("age due_at: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_batches SET updated_at = now() WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); err != nil {
		t.Fatalf("touch batch: %v", err)
	}

	if _, err := repo.MarkMissedBefore(ctx, tenantID, time.Now().UTC(), 1000); err != nil {
		t.Fatalf("MarkMissedBefore: %v", err)
	}

	for _, id := range []string{obB, obC} {
		if got := scanStatus(t, ctx, pool, id); got != "in_progress" {
			t.Fatalf("actively worked obligation %s status = %s, want in_progress (the reaper must not steal live work)", id, got)
		}
		if got := countRows(t, ctx, pool,
			`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action='obligation.reaped_in_progress' AND resource_id=$2`,
			tenantID, id); got != 0 {
			t.Fatalf("actively worked obligation %s was reaped (%d audit rows)", id, got)
		}
	}
	if got := scanTextObligation(t, ctx, pool,
		`SELECT status FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != "in_progress" {
		t.Fatalf("active batch status = %q, want in_progress", got)
	}
}

// TestMarkMissedBeforeReapGraceIsAnchoredToWallClockNotMissedBefore pins the defect found while
// proving BUG-015: the reaper's first implementation derived its staleness cutoff from the caller's
// missedBefore (reapBefore = missedBefore - 12h). missedBefore is a DUE cutoff, not "now", and
// sweepers routinely pass a forward-dated one to close out a whole drive window. With that anchor,
// a cutoff more than 12h in the future made every in_progress row look stale and reaped drives an
// operator was working seconds earlier -- silently deleting live work and breaking the PEND-1
// protection in TestMarkCompletedFlipsOpenSiblingsToInProgressAndSparesThemFromMissedSweep.
// Staleness is elapsed real time since the last completion, so the cutoff is now()-12h.
func TestMarkMissedBeforeReapGraceIsAnchoredToWallClockNotMissedBefore(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	batchID, _, obB, obC := seedStrandedDrive(t, ctx, repo, versionID, ruleID, "future-cutoff-drive",
		time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC), "obl-futurecutoff")

	// The drive was touched RIGHT NOW (seedStrandedDrive just completed animal 1), but the sweep is
	// asked to close out everything due through 30 days from now.
	farFutureCutoff := time.Now().UTC().AddDate(0, 0, 30)
	if _, err := repo.MarkMissedBefore(ctx, tenantID, farFutureCutoff, 1000); err != nil {
		t.Fatalf("MarkMissedBefore: %v", err)
	}

	for _, id := range []string{obB, obC} {
		if got := countRows(t, ctx, pool,
			`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND action='obligation.reaped_in_progress' AND resource_id=$2`,
			tenantID, id); got != 0 {
			t.Fatalf("obligation %s was reaped by a future-dated missedBefore (%d reap audit rows): the grace window must be anchored to wall-clock now, not to the caller's due cutoff", id, got)
		}
	}
	if got := scanTextObligation(t, ctx, pool,
		`SELECT status FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != "in_progress" {
		t.Fatalf("batch status = %q after future-dated sweep, want in_progress", got)
	}
}
