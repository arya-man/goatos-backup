package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// PEND-1 REDESIGN: an earlier submit-time obligation.MarkInProgress writer (called from
// sopbridge.OnTaskSubmitted) was UNREACHABLE in its intended shape and has been removed along with
// the standalone MarkInProgress method. The reachable in_progress trigger now lives entirely inside
// MarkCompleted: the FIRST real completion in a multi-obligation drive flips its still-open
// (scheduled/due) siblings on the same batch to in_progress, in the same transaction as the
// completion. This file proves that design end to end against real Postgres.

// insertSiblingObligation inserts one more obligation for the given goat/version/rule with a
// distinct due_at + idempotency key, mirroring seed()'s shape. Reusing the same goat as seed() is
// fine: the obligation_instances_dup_guard unique constraint is (tenant, protocol_version_id,
// rule_id, target_type, target_id, due_at), not per-goat exclusivity.
func insertSiblingObligation(t *testing.T, ctx context.Context, repo *Repository, versionID, ruleID, idemKey string, dueAt time.Time, sequence int32) string {
	t.Helper()
	id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: dueAt, Status: "scheduled", IdempotencyKey: idemKey, Sequence: sequence,
	})
	if err != nil || !applied {
		t.Fatalf("insert sibling obligation %s: applied=%v err=%v", idemKey, applied, err)
	}
	return id
}

// TestMarkCompletedFlipsOpenSiblingsToInProgressAndSparesThemFromMissedSweep is the core PEND-1
// proof: (a) a 3-obligation batch, completing the first obligation moves the batch to in_progress
// AND flips its 2 still-open siblings to in_progress, one obligation.in_progress event + outbox row
// each; (b) a MarkMissedBefore sweep past the due cutoff SPARES those in_progress siblings but
// STILL misses a due obligation sitting on a separate, still-planned (not-in_progress) batch; (c)
// completing the remaining two obligations closes the batch out to completed; (e) an idempotent
// replay of the first completion does not re-emit sibling in_progress events.
func TestMarkCompletedFlipsOpenSiblingsToInProgressAndSparesThemFromMissedSweep(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool) // scheduled, due 2026-08-01 (see seed())
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	ruleID := mustRuleOf(t, ctx, pool)

	obB := insertSiblingObligation(t, ctx, repo, versionID, ruleID, "obl-sibling-b", time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC), 2)
	obC := insertSiblingObligation(t, ctx, repo, versionID, ruleID, "obl-sibling-c", time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC), 3)

	batchID, err := repo.CreateBatch(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
		Status: "planned", EstimatedTargets: 3, PlannedQuantity: "3", QuantityUnit: "dose",
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if attached, err := repo.AttachObligationsToBatch(ctx, tenantID, batchID, []string{obA, obB, obC}); err != nil || attached != 3 {
		t.Fatalf("attach: attached=%d err=%v", attached, err)
	}

	// A second, independent planned batch with its own due obligation on a DIFFERENT goat/target --
	// used in step (b) below to prove MarkMissedBefore still misses ordinary due work on a batch that
	// never started, while sparing the in_progress siblings on the drive that DID start.
	const otherGoat = "10000000-0000-4000-8000-0000000000bb"
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id)
		 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4)`, otherGoat, tenantID, meshaParty, cbePark); err != nil {
		t.Fatalf("seed other goat: %v", err)
	}
	obD, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: otherGoat, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled",
		IdempotencyKey: "obl-other-batch-due", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert obD: applied=%v err=%v", applied, err)
	}
	// Distinct Session so this second planned batch on the same (version, scope) does not collide
	// with the first batch under obligation_batches_unfinalized_planned_unique_idx (unique on
	// tenant/version/scope/session/planned_date/window while status='planned' and unfinalized).
	otherBatchID, err := repo.CreateBatch(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
		Session: "other-batch",
		Status:  "planned", EstimatedTargets: 1, PlannedQuantity: "1", QuantityUnit: "dose",
	})
	if err != nil {
		t.Fatalf("create other batch: %v", err)
	}
	if attached, err := repo.AttachObligationsToBatch(ctx, tenantID, otherBatchID, []string{obD}); err != nil || attached != 1 {
		t.Fatalf("attach other batch: attached=%d err=%v", attached, err)
	}

	// --- (a) Complete the first obligation: batch -> in_progress, both open siblings -> in_progress.
	ok, err := repo.MarkCompleted(ctx, tenantID, obA)
	if err != nil || !ok {
		t.Fatalf("complete obA: ok=%v err=%v", ok, err)
	}
	if got := scanTextObligation(t, ctx, pool,
		`SELECT status FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != "in_progress" {
		t.Fatalf("batch status after first complete = %q, want in_progress", got)
	}
	if got := scanStatus(t, ctx, pool, obB); got != "in_progress" {
		t.Fatalf("obB status = %s, want in_progress", got)
	}
	if got := scanStatus(t, ctx, pool, obC); got != "in_progress" {
		t.Fatalf("obC status = %s, want in_progress", got)
	}
	for _, ob := range []string{obB, obC} {
		if got := countRows(t, ctx, pool,
			`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='in_progress'`,
			tenantID, ob); got != 1 {
			t.Fatalf("in_progress event count for %s = %d, want 1", ob, got)
		}
		if got := countRows(t, ctx, pool,
			`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='obligation.in_progress'`,
			tenantID, ob); got != 1 {
			t.Fatalf("in_progress outbox count for %s = %d, want 1", ob, got)
		}
		if got := countRows(t, ctx, pool,
			`SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND resource_id=$2 AND action='obligation.in_progress'`,
			tenantID, ob); got != 1 {
			t.Fatalf("in_progress audit count for %s = %d, want 1", ob, got)
		}
	}
	// obA itself only ever gets a 'completed' event, never 'in_progress' -- it is excluded from its
	// own sibling UPDATE because MarkObligationCompleted already moved it to 'completed' first.
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='in_progress'`,
		tenantID, obA); got != 0 {
		t.Fatalf("in_progress event count for completed obA = %d, want 0", got)
	}

	// --- (e) Idempotent replay of the same completion must not re-emit sibling in_progress events.
	ok2, err := repo.MarkCompleted(ctx, tenantID, obA)
	if err != nil {
		t.Fatalf("replay complete obA: %v", err)
	}
	if ok2 {
		t.Fatalf("replay complete obA should be a no-op (already terminal)")
	}
	for _, ob := range []string{obB, obC} {
		if got := countRows(t, ctx, pool,
			`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='in_progress'`,
			tenantID, ob); got != 1 {
			t.Fatalf("replay: in_progress event count for %s = %d, want 1 (no duplicate)", ob, got)
		}
		if got := countRows(t, ctx, pool,
			`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='obligation.in_progress'`,
			tenantID, ob); got != 1 {
			t.Fatalf("replay: in_progress outbox count for %s = %d, want 1 (no duplicate)", ob, got)
		}
	}

	// --- (b) MarkMissedBefore, well past every due date above, must SPARE obB/obC (in_progress on an
	// in_progress batch) but STILL miss obD (due, on a batch that never started / stays planned).
	n, err := repo.MarkMissedBefore(ctx, tenantID, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), 100)
	if err != nil {
		t.Fatalf("mark missed: %v", err)
	}
	if n != 1 {
		t.Fatalf("marked missed = %d, want 1 (only obD)", n)
	}
	if got := scanStatus(t, ctx, pool, obB); got != "in_progress" {
		t.Fatalf("obB status after missed sweep = %s, want in_progress (protected)", got)
	}
	if got := scanStatus(t, ctx, pool, obC); got != "in_progress" {
		t.Fatalf("obC status after missed sweep = %s, want in_progress (protected)", got)
	}
	if got := scanStatus(t, ctx, pool, obD); got != "missed" {
		t.Fatalf("obD status after missed sweep = %s, want missed (planned batch, not protected)", got)
	}
	if got := scanTextObligation(t, ctx, pool,
		`SELECT status FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, tenantID, otherBatchID); got != "planned" {
		t.Fatalf("other batch status = %q, want planned (never started)", got)
	}

	// --- (c) Complete the remaining two obligations: the batch closes out to completed. The second
	// completion's sibling UPDATE must be a no-op (both siblings already in_progress/handled) -- no
	// duplicate in_progress events, proving the O(N)-not-O(N^2) design.
	okB, err := repo.MarkCompleted(ctx, tenantID, obB)
	if err != nil || !okB {
		t.Fatalf("complete obB: ok=%v err=%v", okB, err)
	}
	if got := scanTextObligation(t, ctx, pool,
		`SELECT status FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != "in_progress" {
		t.Fatalf("batch status after second complete = %q, want in_progress (obC still open)", got)
	}
	okC, err := repo.MarkCompleted(ctx, tenantID, obC)
	if err != nil || !okC {
		t.Fatalf("complete obC: ok=%v err=%v", okC, err)
	}
	if got := scanTextObligation(t, ctx, pool,
		`SELECT status FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != "completed" {
		t.Fatalf("batch status after last complete = %q, want completed", got)
	}
	for _, ob := range []string{obB, obC} {
		if got := countRows(t, ctx, pool,
			`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='in_progress'`,
			tenantID, ob); got != 1 {
			t.Fatalf("final: in_progress event count for %s = %d, want 1 (no duplicate from later completions)", ob, got)
		}
	}
}

// TestMarkCompletedSingleObligationBatchNeverPassesThroughInProgress is scenario (d): a
// single-obligation batch completes straight to 'completed' and never visits 'in_progress' at all --
// there is no sibling to protect, so the sibling UPDATE matches zero rows.
func TestMarkCompletedSingleObligationBatchNeverPassesThroughInProgress(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	obA := seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)

	batchID, err := repo.CreateBatch(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: versionID, ScopeType: "park", ScopeID: cbePark,
		Status: "planned", EstimatedTargets: 1, PlannedQuantity: "1", QuantityUnit: "dose",
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if attached, err := repo.AttachObligationsToBatch(ctx, tenantID, batchID, []string{obA}); err != nil || attached != 1 {
		t.Fatalf("attach: attached=%d err=%v", attached, err)
	}

	ok, err := repo.MarkCompleted(ctx, tenantID, obA)
	if err != nil || !ok {
		t.Fatalf("complete obA: ok=%v err=%v", ok, err)
	}
	if got := scanTextObligation(t, ctx, pool,
		`SELECT status FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, tenantID, batchID); got != "completed" {
		t.Fatalf("batch status = %q, want completed (single-obligation batch closes directly)", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='in_progress'`,
		tenantID, obA); got != 0 {
		t.Fatalf("in_progress event count = %d, want 0 (never passes through in_progress)", got)
	}
	if got := countRows(t, ctx, pool,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='obligation.in_progress'`,
		tenantID, obA); got != 0 {
		t.Fatalf("in_progress outbox count = %d, want 0", got)
	}
}
