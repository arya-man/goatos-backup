package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// grantOperatorParkScope satisfies migration 000065's
// weighing_campaign_sheds_operator_park_bound trigger, which requires an
// ACTIVE user_scope_grants row before any weighing_campaign_sheds write can
// name repoOperator. This mirrors the identical grant already used by
// repository_lump_sum_resubmit_integration_test.go and
// close_campaign_notify_fanout_integration_test.go in this same package.
func grantOperatorParkScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`,
		repoTenant, repoOperator, repoPark)
}

// -----------------------------------------------------------------------------
// B09 — reopen/rework must not leave the kernel work item permanently terminal.
// -----------------------------------------------------------------------------
//
// This proves ReactivateWorkItemsForBucket (kernel.go) undoes
// reconcileTerminalWorkItems for exactly one bucket, atomically, and that
// WeighingProcessState (the Calendar/Control Tower binding) stops counting the
// bucket as finished once it is reactivated.
func TestReactivateWorkItemsForBucketUndoesTerminalReconciliation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	repo := NewRepository(pool, 5*time.Second)
	publishFixtureCampaign(t, ctx, repo, "publish:b09")

	// 1. Simulate the bucket completing (the real close/completion path sets
	// weighing_campaign_sheds.status='completed'; we drive that fact directly
	// so the test is scoped to the kernel reconcile/reactivate seam, not the
	// completion path).
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_campaign_sheds SET status='completed', completed_at=now(), updated_at=now()
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoShedScope)

	// 2. A kernel tick terminalizes the work item (production path: pass 1 of
	// SweepWorkItems).
	if _, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{
		TenantID: repoTenant, AsOf: businessInstant(t, kernelPlannedDate),
	}); err != nil {
		t.Fatalf("sweep to terminalize: %v", err)
	}
	terminal := readWorkItem(t, ctx, pool, repoShedScope)
	if terminal.state != domain.WorkStateCompleted {
		t.Fatalf("precondition: work item state=%q, want completed", terminal.state)
	}
	stateBefore, err := repo.WeighingProcessState(ctx, repoTenant, repoCampaign, kernelPlannedDate, kernelLaterDate)
	if err != nil {
		t.Fatalf("process state before reopen: %v", err)
	}
	if stateBefore.Summary.Completed < 1 {
		t.Fatalf("precondition: Control Tower summary must count the completed bucket, got %+v", stateBefore.Summary)
	}

	// 3. RED-then-GREEN: without calling ReactivateWorkItemsForBucket, a
	// rework/reopen transaction that only flips weighing_campaign_sheds back to
	// 'in_progress' (exactly what markObservationRework / ReopenScope already do
	// today) leaves the kernel work item stuck terminal. This is B09 verbatim:
	// no ReactivateWorkItemsForBucket call ⇒ still completed after the bucket
	// left its terminal status.
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE weighing_campaign_sheds SET status='in_progress', completed_at=NULL, updated_at=now()
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid AND status='completed'`, repoTenant, repoShedScope); err != nil {
		t.Fatalf("simulate bucket leaving terminal status: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit bucket-only transition: %v", err)
	}
	stillTerminal := readWorkItem(t, ctx, pool, repoShedScope)
	if stillTerminal.state != domain.WorkStateCompleted {
		t.Fatalf("RED case broke: work item state=%q after bucket-only reopen, want it to STILL be stuck at completed (proves the bug B09 describes)", stillTerminal.state)
	}

	// 4. GREEN: call ReactivateWorkItemsForBucket in the SAME transaction as the
	// bucket-status transition (this is the call-site contract documented on the
	// function — verification_verdict.go / repository.go must call it exactly
	// like this, right after their own bucket UPDATE).
	tx2, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin tx2: %v", err)
	}
	// bucket is already 'in_progress' from step 3; ReactivateWorkItemsForBucket
	// only depends on the WORK ITEM still being terminal, which it is.
	n, err := repo.ReactivateWorkItemsForBucket(ctx, tx2, repoTenant, repoShedScope)
	if err != nil {
		t.Fatalf("ReactivateWorkItemsForBucket: %v", err)
	}
	if n != 1 {
		t.Fatalf("ReactivateWorkItemsForBucket reactivated=%d, want 1", n)
	}
	if err := tx2.Commit(ctx); err != nil {
		t.Fatalf("commit reactivate: %v", err)
	}

	reopened := readWorkItem(t, ctx, pool, repoShedScope)
	if reopened.state != domain.WorkStateScheduled {
		t.Fatalf("after reactivate: work item state=%q, want scheduled", reopened.state)
	}
	var terminalAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT terminal_at FROM weighing_work_items WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`,
		repoTenant, repoShedScope).Scan(&terminalAt); err != nil {
		t.Fatalf("read terminal_at: %v", err)
	}
	if terminalAt != nil {
		t.Fatalf("after reactivate: terminal_at=%v, want NULL", *terminalAt)
	}

	stateAfter, err := repo.WeighingProcessState(ctx, repoTenant, repoCampaign, kernelPlannedDate, kernelLaterDate)
	if err != nil {
		t.Fatalf("process state after reopen: %v", err)
	}
	if stateAfter.Summary.Completed != stateBefore.Summary.Completed-1 {
		t.Fatalf("Control Tower summary completed=%d after reactivate, want %d (one fewer than before)",
			stateAfter.Summary.Completed, stateBefore.Summary.Completed-1)
	}
	if stateAfter.Summary.OpenTotal != stateBefore.Summary.OpenTotal+1 {
		t.Fatalf("Control Tower summary open_total=%d after reactivate, want %d (one more than before): the reopened item must count as open work again",
			stateAfter.Summary.OpenTotal, stateBefore.Summary.OpenTotal+1)
	}
}

// -----------------------------------------------------------------------------
// B10 — multi-chunk cadence must not silently drop notifications.
// -----------------------------------------------------------------------------
//
// Seeds ONE operator with 210 due buckets on ONE business date (>
// defaultKernelChunkSize=200 used implicitly here via an explicit ChunkSize),
// runs the day-start cadence pass in chunks of 200, and asserts EVERY bucket
// appears in SOME cadence event's payload while ALL 210 are marked surfaced.
// Before the fix (idem key without the bucket-set digest), the second chunk's
// event collided on ON CONFLICT DO NOTHING with the first chunk's identical
// (event_type, tenant, campaign, operator, date) key and its 10 buckets never
// reached any outbox payload, even though all 210 rows were marked surfaced.
func TestCadenceMultiChunkOperatorGetsEveryBucketInSomeEventPayload(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)

	const extra = 208 // + the 2 fixture buckets (both on repoOperator) = 210
	wantShedIDs := map[string]bool{repoAnimalScope: true, repoShedScope: true}
	for i := 0; i < extra; i++ {
		shedID := fmt.Sprintf("00000000-0000-4000-8000-000000092%03d", i)
		addExtraBucket(t, ctx, pool,
			shedID,
			fmt.Sprintf("00000000-0000-4000-8000-000000032%03d", i),
			fmt.Sprintf("B10 shed %03d", i),
			repoOperator)
		wantShedIDs[shedID] = true
	}
	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	// Raise the campaign's per-operator daily cap above the bucket count so the
	// greedy publish planner (kernel.go createWorkItemsForPublishTx) assigns
	// every bucket day_offset=0 -- i.e. the SAME due_business_date -- which is
	// what forces all 210 buckets into ONE operator's ONE business-date claim,
	// spread across multiple sweep chunks. This test is about the cadence
	// idempotency-key seam, not the day-offset spreading algorithm.
	execWeighingTestSQL(t, ctx, pool, `UPDATE weighing_campaigns SET planned_cap_per_day=1000 WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, repoTenant, repoCampaign)
	repo := NewRepository(pool, 5*time.Second)
	publishFixtureCampaign(t, ctx, repo, "publish:b10")
	if got := countWorkItems(t, ctx, pool); got != extra+2 {
		t.Fatalf("work items=%d, want %d", got, extra+2)
	}

	res, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{
		TenantID: repoTenant, AsOf: businessInstant(t, kernelPlannedDate), ChunkSize: 200, MaxChunks: 50,
	})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if res.DayStartSurfaced != extra+2 {
		t.Fatalf("DayStartSurfaced=%d, want %d", res.DayStartSurfaced, extra+2)
	}
	if res.Truncated {
		t.Fatal("sweep must fully drain within the given MaxChunks budget")
	}

	// Every row is marked surfaced regardless of the outbox outcome (this part
	// of the pipeline was already correct — the bug is in what got PUBLISHED).
	var unsurfaced int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM weighing_work_items
WHERE tenant_id=$1::uuid AND day_start_surfaced_on IS NULL`, repoTenant).Scan(&unsurfaced); err != nil {
		t.Fatalf("count unsurfaced: %v", err)
	}
	if unsurfaced != 0 {
		t.Fatalf("%d work items never marked surfaced", unsurfaced)
	}

	seenShedIDs := collectDayStartPayloadBucketIDs(t, ctx, pool)
	var missing []string
	for want := range wantShedIDs {
		if !seenShedIDs[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("B10: %d of %d buckets were marked surfaced but never appeared in ANY day_start outbox event payload (dropped by an idempotency-key collision across chunks): %v",
			len(missing), len(wantShedIDs), missing)
	}
}

// collectDayStartPayloadBucketIDs reads every weighing.work_item.day_start
// outbox row for repoTenant/repoCampaign and unions the campaign_shed_id of
// every bucket across every event's payload, so the test can prove ALL claimed
// buckets reached SOME event — not just count the number of events.
func collectDayStartPayloadBucketIDs(t *testing.T, ctx context.Context, pool *pgxpool.Pool) map[string]bool {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT payload FROM outbox_messages
WHERE tenant_id=$1::uuid AND event_type=$2 AND aggregate_id=$3::uuid`,
		repoTenant, domain.EventWorkItemDayStart, repoCampaign)
	if err != nil {
		t.Fatalf("query outbox_messages: %v", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan outbox payload: %v", err)
		}
		var envelope struct {
			Payload struct {
				Buckets []struct {
					CampaignShedID string `json:"campaign_shed_id"`
				} `json:"buckets"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatalf("unmarshal outbox payload: %v", err)
		}
		for _, b := range envelope.Payload.Buckets {
			seen[b.CampaignShedID] = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate outbox rows: %v", err)
	}
	return seen
}
