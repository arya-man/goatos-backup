package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// WEIGHING PHASE 2 kernel proofs.
//
// Every date below is a FIXED Asia/Kolkata BUSINESS DATE. There is no
// `time.Now()`, no wall-clock offset, and no hour arithmetic anywhere in this
// file: the sweep is handed an instant inside a fixed business day and every
// assertion compares business DATES.
const (
	// kernelPlannedDate is the campaign's start business date in the shared
	// fixture, and therefore the planned business date of every bucket whose
	// greedy cap offset is 0.
	kernelPlannedDate = "2026-07-29"
	// kernelLaterDate is a fixed LATER business day, used to prove roll-forward
	// and delayed detection without any clock offset.
	kernelLaterDate  = "2026-08-05"
	kernelSecondOp   = "00000000-0000-4000-8000-000000000302"
	kernelExtraShedA = "00000000-0000-4000-8000-000000003101"
)

// businessInstant returns an instant inside the given Asia/Kolkata business day.
// It exists so a test can express "during business day D" without ever writing
// `now ± N hours`.
func businessInstant(t *testing.T, businessDate string) time.Time {
	t.Helper()
	day, err := time.ParseInLocation("2006-01-02", businessDate, biztime.DefaultLocation())
	if err != nil {
		t.Fatalf("parse business date %q: %v", businessDate, err)
	}
	instant := biztime.BusinessDayStart(day)
	if got := biztime.BusinessDate(instant); got != businessDate {
		t.Fatalf("business instant for %q resolved to %q", businessDate, got)
	}
	return instant
}

// publishFixtureCampaign drives the REAL production publish path (the same
// transaction the HTTP service calls), which is what materializes work items.
func publishFixtureCampaign(t *testing.T, ctx context.Context, repo *Repository, idempotencyKey string) {
	t.Helper()
	if _, err := repo.PublishCampaign(ctx, repoTenant, repoCampaign, repoVerifier, idempotencyKey); err != nil {
		t.Fatalf("publish campaign: %v", err)
	}
}

// addExtraBucket adds one more selected bucket (planner input, an external fact)
// so multi-operator and multi-chunk behaviour can be proven.
func addExtraBucket(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignShedID, locationID, label, operatorID string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', $3, $4::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, locationID, repoTenant, label, repoPark)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', $5, 'per_shed_partition', $6::uuid, 1)
ON CONFLICT (campaign_shed_id) DO NOTHING`,
		campaignShedID, repoCampaign, repoTenant, locationID, label, operatorID)
}

func countWorkItems(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM weighing_work_items WHERE tenant_id=$1::uuid`, repoTenant).Scan(&n); err != nil {
		t.Fatalf("count work items: %v", err)
	}
	return n
}

type workItemRow struct {
	state        string
	planned      string
	due          string
	surfacedOn   string
	rolledCount  int
	escalatedOn  string
	delayedSince string
	operatorID   string
}

func readWorkItem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignShedID string) workItemRow {
	t.Helper()
	var row workItemRow
	if err := pool.QueryRow(ctx, `
SELECT work_state,
       planned_business_date::text,
       due_business_date::text,
       COALESCE(day_start_surfaced_on::text, ''),
       rolled_forward_count,
       COALESCE(escalated_on::text, ''),
       COALESCE(delayed_since_business_date::text, ''),
       operator_user_id::text
FROM weighing_work_items
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, campaignShedID).
		Scan(&row.state, &row.planned, &row.due, &row.surfacedOn, &row.rolledCount, &row.escalatedOn, &row.delayedSince, &row.operatorID); err != nil {
		t.Fatalf("read work item for bucket %s: %v", campaignShedID, err)
	}
	return row
}

// -----------------------------------------------------------------------------
// 1. PUBLISH CREATES WORK ITEMS EXACTLY ONCE (REPLAY-SAFE)
// -----------------------------------------------------------------------------

// Publishing must create exactly one durable work item per selected bucket, and
// an exact replay of the publish command must create ZERO duplicates. This is the
// invariant that lets the kernel sweep be at-least-once.
func TestWeighingPublishCreatesWorkItemsExactlyOnceOnReplay(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	repo := NewRepository(pool, 5*time.Second)

	publishFixtureCampaign(t, ctx, repo, "publish:work-items")
	if got := countWorkItems(t, ctx, pool); got != 2 {
		t.Fatalf("work items after publish=%d, want 2 (one per selected bucket)", got)
	}

	// Exact replay of the SAME publish command: the idempotency record short-circuits
	// and no second work item may appear.
	publishFixtureCampaign(t, ctx, repo, "publish:work-items")
	if got := countWorkItems(t, ctx, pool); got != 2 {
		t.Fatalf("work items after exact replay=%d, want 2 (replay must create no duplicates)", got)
	}

	// A DIFFERENT key against an already-published campaign is rejected as
	// immutable, and must also leave the work items untouched.
	if _, err := repo.PublishCampaign(ctx, repoTenant, repoCampaign, repoVerifier, "publish:work-items-second-key"); err == nil {
		t.Fatal("republish of a published campaign with a new key must be rejected")
	}
	if got := countWorkItems(t, ctx, pool); got != 2 {
		t.Fatalf("work items after rejected republish=%d, want 2", got)
	}

	// Grain proof: one row per bucket, each carrying its ONE assigned operator, and
	// the planned date is the campaign's start business date (cap offset 0).
	item := readWorkItem(t, ctx, pool, repoAnimalScope)
	if item.state != domain.WorkStateScheduled {
		t.Fatalf("published work item state=%q, want %q", item.state, domain.WorkStateScheduled)
	}
	if item.planned != kernelPlannedDate || item.due != kernelPlannedDate {
		t.Fatalf("published work item planned=%q due=%q, want both %q", item.planned, item.due, kernelPlannedDate)
	}
	if item.operatorID != repoOperator {
		t.Fatalf("published work item operator=%q, want %q", item.operatorID, repoOperator)
	}
}

// -----------------------------------------------------------------------------
// 2. DAY-START SELECTS ONLY TODAY'S OPEN WORK, FOR THE RIGHT OPERATOR
// -----------------------------------------------------------------------------

// Day-start must surface only work whose CURRENT due business date is today, and
// each operator must get an event naming only their own buckets.
func TestWeighingKernelDayStartSelectsOnlyTodaysOpenWorkForTheAssignedOperator(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	// A second bucket owned by a DIFFERENT operator, so a cross-operator leak would
	// show up as the wrong event count / wrong bucket list.
	addExtraBucket(t, ctx, pool, kernelExtraShedA, "00000000-0000-4000-8000-00000000a101", "Zulu 9", kernelSecondOp)
	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	repo := NewRepository(pool, 5*time.Second)
	publishFixtureCampaign(t, ctx, repo, "publish:day-start")

	// Push ONE bucket's due date to a later BUSINESS DAY so it must not be surfaced
	// today. This is a business-date write, not an hour offset.
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_work_items
SET planned_business_date=$3::date, due_business_date=$3::date
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoShedScope, kernelLaterDate)

	result, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{
		TenantID: repoTenant,
		AsOf:     businessInstant(t, kernelPlannedDate),
	})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if result.BusinessDate != kernelPlannedDate {
		t.Fatalf("sweep business date=%q, want %q", result.BusinessDate, kernelPlannedDate)
	}
	if result.DayStartSurfaced != 2 {
		t.Fatalf("day-start surfaced=%d, want 2 (the two buckets due today)", result.DayStartSurfaced)
	}
	if result.RolledForward != 0 || result.MarkedDelayed != 0 {
		t.Fatalf("on the planned day nothing may roll forward or go delayed: %+v", result)
	}

	if got := readWorkItem(t, ctx, pool, repoAnimalScope).surfacedOn; got != kernelPlannedDate {
		t.Fatalf("today's bucket surfaced_on=%q, want %q", got, kernelPlannedDate)
	}
	if got := readWorkItem(t, ctx, pool, kernelExtraShedA).surfacedOn; got != kernelPlannedDate {
		t.Fatalf("second operator's bucket surfaced_on=%q, want %q", got, kernelPlannedDate)
	}
	if got := readWorkItem(t, ctx, pool, repoShedScope).surfacedOn; got != "" {
		t.Fatalf("a bucket due on a LATER business day was surfaced today (surfaced_on=%q)", got)
	}

	// One event per (campaign, operator): two operators had work today.
	if got := countOutbox(t, ctx, pool, domain.EventWorkItemDayStart); got != 2 {
		t.Fatalf("day-start events=%d, want 2 (one per assigned operator)", got)
	}
	assertCadenceEventBuckets(t, ctx, pool, domain.EventWorkItemDayStart, repoOperator, []string{repoAnimalScope})
	assertCadenceEventBuckets(t, ctx, pool, domain.EventWorkItemDayStart, kernelSecondOp, []string{kernelExtraShedA})

	// Re-running the sweep on the SAME business date must not re-surface or
	// duplicate the push.
	again, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{TenantID: repoTenant, AsOf: businessInstant(t, kernelPlannedDate)})
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if again.DayStartSurfaced != 0 {
		t.Fatalf("second sweep on the same business date surfaced %d items, want 0", again.DayStartSurfaced)
	}
	if got := countOutbox(t, ctx, pool, domain.EventWorkItemDayStart); got != 2 {
		t.Fatalf("day-start events after replay=%d, want 2", got)
	}
}

// assertCadenceEventBuckets proves an operator's cadence event names EXACTLY their
// own buckets — never another operator's.
func assertCadenceEventBuckets(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventType, operatorID string, wantBuckets []string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT bucket->>'campaign_shed_id'
FROM outbox_messages,
     jsonb_array_elements(payload->'payload'->'buckets') AS bucket
WHERE tenant_id=$1::uuid
  AND event_type=$2
  AND payload->'payload'->>'operator_id' = $3
ORDER BY 1`, repoTenant, eventType, operatorID)
	if err != nil {
		t.Fatalf("read %s buckets for operator %s: %v", eventType, operatorID, err)
	}
	defer rows.Close()
	got := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan bucket: %v", err)
		}
		got = append(got, id)
	}
	if len(got) != len(wantBuckets) {
		t.Fatalf("%s operator %s buckets=%v, want %v", eventType, operatorID, got, wantBuckets)
	}
	for i := range got {
		if got[i] != wantBuckets[i] {
			t.Fatalf("%s operator %s buckets=%v, want %v", eventType, operatorID, got, wantBuckets)
		}
	}
}

// -----------------------------------------------------------------------------
// 3. ROLL-FORWARD PRESERVES THE ORIGINAL PLANNED DATE AND NEVER CANCELS
// -----------------------------------------------------------------------------

// Unfinished work at the business-day boundary stays EXECUTABLE: its due business
// date moves to today, its ORIGINAL planned business date is preserved for audit,
// and it is never auto-canceled because a date passed.
func TestWeighingKernelRollForwardPreservesOriginalPlannedDateAndNeverCancels(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	repo := NewRepository(pool, 5*time.Second)
	publishFixtureCampaign(t, ctx, repo, "publish:roll-forward")

	before := readWorkItem(t, ctx, pool, repoAnimalScope)
	if before.planned != kernelPlannedDate {
		t.Fatalf("planned date before sweep=%q, want %q", before.planned, kernelPlannedDate)
	}

	result, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{
		TenantID: repoTenant,
		AsOf:     businessInstant(t, kernelLaterDate),
	})
	if err != nil {
		t.Fatalf("sweep on a later business day: %v", err)
	}
	if result.RolledForward != 2 {
		t.Fatalf("rolled forward=%d, want 2", result.RolledForward)
	}

	after := readWorkItem(t, ctx, pool, repoAnimalScope)
	// The ORIGINAL plan survives.
	if after.planned != kernelPlannedDate {
		t.Fatalf("roll-forward rewrote planned_business_date to %q; the original plan %q must survive for audit", after.planned, kernelPlannedDate)
	}
	// The work is executable TODAY.
	if after.due != kernelLaterDate {
		t.Fatalf("rolled-forward due date=%q, want %q", after.due, kernelLaterDate)
	}
	if after.rolledCount != 1 {
		t.Fatalf("rolled_forward_count=%d, want 1", after.rolledCount)
	}
	// NEVER auto-canceled, and never terminal.
	if after.state == domain.WorkStateCanceled || after.state == domain.WorkStateClosed || after.state == domain.WorkStateCompleted {
		t.Fatalf("roll-forward moved work to terminal state %q; work must stay executable", after.state)
	}
	if after.state != domain.WorkStateDelayed {
		t.Fatalf("work past its planned date should read delayed, got %q", after.state)
	}
	// It rolled forward AND is surfaced as today's work in the same tick.
	if after.surfacedOn != kernelLaterDate {
		t.Fatalf("rolled-forward work was not surfaced for today (surfaced_on=%q)", after.surfacedOn)
	}
	if got := countOutbox(t, ctx, pool, domain.EventWorkItemRolledForward); got != 1 {
		t.Fatalf("rolled_forward events=%d, want 1 (one per campaign+operator group)", got)
	}
	// The rolled-forward event still names the ORIGINAL planned date, so the push
	// can say "first planned for <date>".
	var plannedInPayload string
	if err := pool.QueryRow(ctx, `
SELECT bucket->>'planned_business_date'
FROM outbox_messages,
     jsonb_array_elements(payload->'payload'->'buckets') AS bucket
WHERE tenant_id=$1::uuid AND event_type=$2
LIMIT 1`, repoTenant, domain.EventWorkItemRolledForward).Scan(&plannedInPayload); err != nil {
		t.Fatalf("read rolled-forward payload: %v", err)
	}
	if plannedInPayload != kernelPlannedDate {
		t.Fatalf("rolled-forward payload planned date=%q, want the original %q", plannedInPayload, kernelPlannedDate)
	}
}

// -----------------------------------------------------------------------------
// 4. DELAYED DETECTION + ESCALATION
// -----------------------------------------------------------------------------

// Work past its ORIGINAL planned business date becomes visibly delayed and
// escalates upward exactly once.
func TestWeighingKernelDelayedDetectionEscalatesPastPlannedBusinessDate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	repo := NewRepository(pool, 5*time.Second)
	publishFixtureCampaign(t, ctx, repo, "publish:delayed")

	// On the planned day nothing is delayed.
	onPlan, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{TenantID: repoTenant, AsOf: businessInstant(t, kernelPlannedDate)})
	if err != nil {
		t.Fatalf("sweep on planned day: %v", err)
	}
	if onPlan.MarkedDelayed != 0 {
		t.Fatalf("marked delayed on the planned business day=%d, want 0", onPlan.MarkedDelayed)
	}
	if got := countOutbox(t, ctx, pool, domain.EventWorkItemDelayed); got != 0 {
		t.Fatalf("delayed escalations on the planned day=%d, want 0", got)
	}

	late, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{TenantID: repoTenant, AsOf: businessInstant(t, kernelLaterDate)})
	if err != nil {
		t.Fatalf("sweep on a later business day: %v", err)
	}
	if late.MarkedDelayed != 2 {
		t.Fatalf("marked delayed=%d, want 2", late.MarkedDelayed)
	}
	item := readWorkItem(t, ctx, pool, repoAnimalScope)
	if item.state != domain.WorkStateDelayed {
		t.Fatalf("work state=%q, want %q", item.state, domain.WorkStateDelayed)
	}
	if item.delayedSince != kernelLaterDate || item.escalatedOn != kernelLaterDate {
		t.Fatalf("delayed_since=%q escalated_on=%q, want both %q", item.delayedSince, item.escalatedOn, kernelLaterDate)
	}
	if got := countOutbox(t, ctx, pool, domain.EventWorkItemDelayed); got != 1 {
		t.Fatalf("delayed escalation events=%d, want 1", got)
	}

	// Escalation is once per transition, not once per tick.
	if _, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{TenantID: repoTenant, AsOf: businessInstant(t, kernelLaterDate)}); err != nil {
		t.Fatalf("repeat sweep: %v", err)
	}
	if got := countOutbox(t, ctx, pool, domain.EventWorkItemDelayed); got != 1 {
		t.Fatalf("delayed escalation events after repeat sweep=%d, want 1", got)
	}
}

// -----------------------------------------------------------------------------
// 5. TERMINAL RECONCILIATION
// -----------------------------------------------------------------------------

// A bucket that reached a terminal status stops being open work, so it can never
// roll forward or escalate forever.
func TestWeighingKernelStopsSweepingTerminalBuckets(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	repo := NewRepository(pool, 5*time.Second)
	publishFixtureCampaign(t, ctx, repo, "publish:terminal")

	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_campaign_sheds SET status='completed', updated_at=now()
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoAnimalScope)

	result, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{TenantID: repoTenant, AsOf: businessInstant(t, kernelLaterDate)})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if result.ReconciledTerminal != 1 {
		t.Fatalf("reconciled terminal=%d, want 1", result.ReconciledTerminal)
	}
	if got := readWorkItem(t, ctx, pool, repoAnimalScope).state; got != domain.WorkStateCompleted {
		t.Fatalf("terminal bucket work state=%q, want %q", got, domain.WorkStateCompleted)
	}
	if result.RolledForward != 1 {
		t.Fatalf("rolled forward=%d, want 1 (only the still-open bucket)", result.RolledForward)
	}
}

// -----------------------------------------------------------------------------
// 6. THE SWEEP IS CHUNKED AND TERMINATES
// -----------------------------------------------------------------------------

// The claim is keyset-chunked; a tick with a small chunk size must still drain
// every row and must terminate. A tick with an exhausted chunk budget must stop,
// report Truncated, and resume on the next tick — never spin.
func TestWeighingKernelSweepClaimIsChunkedAndTerminates(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	const extra = 10
	for i := 0; i < extra; i++ {
		addExtraBucket(t, ctx, pool,
			fmt.Sprintf("00000000-0000-4000-8000-0000000091%02d", 10+i),
			fmt.Sprintf("00000000-0000-4000-8000-0000000031%02d", 10+i),
			fmt.Sprintf("Chunk shed %02d", i),
			repoOperator)
	}
	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	repo := NewRepository(pool, 5*time.Second)
	publishFixtureCampaign(t, ctx, repo, "publish:chunked")
	if got := countWorkItems(t, ctx, pool); got != extra+2 {
		t.Fatalf("work items=%d, want %d", got, extra+2)
	}

	// Chunk budget exhausted after one chunk: bounded, truncated, resumable.
	first, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{
		TenantID: repoTenant, AsOf: businessInstant(t, kernelPlannedDate), ChunkSize: 3, MaxChunks: 1,
	})
	if err != nil {
		t.Fatalf("bounded sweep: %v", err)
	}
	if first.DayStartSurfaced != 3 {
		t.Fatalf("bounded sweep surfaced=%d, want exactly one chunk of 3", first.DayStartSurfaced)
	}
	if !first.Truncated {
		t.Fatal("bounded sweep must report Truncated when its chunk budget is spent")
	}

	// Enough chunks: drains the rest and terminates on its own.
	done := make(chan domain.KernelSweepResult, 1)
	errs := make(chan error, 1)
	go func() {
		res, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{
			TenantID: repoTenant, AsOf: businessInstant(t, kernelPlannedDate), ChunkSize: 3, MaxChunks: 50,
		})
		if err != nil {
			errs <- err
			return
		}
		done <- res
	}()
	select {
	case err := <-errs:
		t.Fatalf("draining sweep: %v", err)
	case res := <-done:
		if res.DayStartSurfaced != extra+2-3 {
			t.Fatalf("draining sweep surfaced=%d, want %d", res.DayStartSurfaced, extra+2-3)
		}
		if res.Truncated {
			t.Fatal("draining sweep must not report Truncated once the pass is drained")
		}
	case <-time.After(60 * time.Second):
		t.Fatal("sweep did not terminate: the keyset claim loop is not making forward progress")
	}

	var unsurfaced int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM weighing_work_items
WHERE tenant_id=$1::uuid AND day_start_surfaced_on IS NULL`, repoTenant).Scan(&unsurfaced); err != nil {
		t.Fatalf("count unsurfaced: %v", err)
	}
	if unsurfaced != 0 {
		t.Fatalf("%d work items were never surfaced; the chunked claim skipped rows", unsurfaced)
	}
}

// -----------------------------------------------------------------------------
// 7. CALENDAR / CONTROL TOWER READ MODEL
// -----------------------------------------------------------------------------

// The Control Tower summary is a WHOLE-FILTER aggregate at the declared
// `weighing_work_item` grain, with DISJOINT buckets, and it is page-size
// independent: with 12 work items it reports 12 even though every operator list in
// the product pages at ~20 and this read has no page parameter at all.
func TestWeighingProcessStateSummaryIsDisjointWholeFilterAndPageSizeIndependent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	const extra = 10
	for i := 0; i < extra; i++ {
		addExtraBucket(t, ctx, pool,
			fmt.Sprintf("00000000-0000-4000-8000-0000000092%02d", 10+i),
			fmt.Sprintf("00000000-0000-4000-8000-0000000032%02d", 10+i),
			fmt.Sprintf("Summary shed %02d", i),
			repoOperator)
	}
	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	repo := NewRepository(pool, 5*time.Second)
	publishFixtureCampaign(t, ctx, repo, "publish:process-state")

	// One bucket goes terminal so more than one disjoint bucket is populated.
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_campaign_sheds SET status='closed', updated_at=now()
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoShedScope)
	if _, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{TenantID: repoTenant, AsOf: businessInstant(t, kernelPlannedDate)}); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	state, err := repo.WeighingProcessState(ctx, repoTenant, repoCampaign, "2026-07-27", "2026-08-02")
	if err != nil {
		t.Fatalf("process state: %v", err)
	}
	if state.Grain != domain.ProcessStateGrain {
		t.Fatalf("declared grain=%q, want %q", state.Grain, domain.ProcessStateGrain)
	}
	total := extra + 2
	if state.Summary.Total != total {
		t.Fatalf("summary total=%d, want %d (whole-filter, not one page of ~20)", state.Summary.Total, total)
	}
	if state.Summary.Closed != 1 {
		t.Fatalf("summary closed=%d, want 1", state.Summary.Closed)
	}
	if state.Summary.Scheduled != total-1 {
		t.Fatalf("summary scheduled=%d, want %d", state.Summary.Scheduled, total-1)
	}
	// Disjointness: the five state buckets sum EXACTLY to the total, so a UI may add
	// them without double counting.
	sum := state.Summary.Scheduled + state.Summary.Delayed + state.Summary.Completed + state.Summary.Closed + state.Summary.Canceled
	if sum != state.Summary.Total {
		t.Fatalf("disjoint buckets sum to %d but total is %d", sum, state.Summary.Total)
	}
	if state.Summary.OpenTotal != state.Summary.Scheduled+state.Summary.Delayed {
		t.Fatalf("open_total=%d, want scheduled+delayed=%d", state.Summary.OpenTotal, state.Summary.Scheduled+state.Summary.Delayed)
	}

	// Day markers are dot-grain: one row per business day carrying counts, never the
	// day's work items.
	if len(state.DayMarkers) != 1 {
		t.Fatalf("day markers=%+v, want exactly one business day", state.DayMarkers)
	}
	if state.DayMarkers[0].BusinessDate != kernelPlannedDate || state.DayMarkers[0].OpenCount != total-1 {
		t.Fatalf("day marker=%+v, want %s with %d open", state.DayMarkers[0], kernelPlannedDate, total-1)
	}

	// Narrowing the window changes ROWS only; the summary is unchanged, because the
	// summary is a whole-filter aggregate and not a page rollup.
	narrow, err := repo.WeighingProcessState(ctx, repoTenant, repoCampaign, "2026-08-01", "2026-08-02")
	if err != nil {
		t.Fatalf("narrow process state: %v", err)
	}
	if len(narrow.DayMarkers) != 0 {
		t.Fatalf("narrow window day markers=%+v, want none", narrow.DayMarkers)
	}
	if narrow.Summary != state.Summary {
		t.Fatalf("narrow window summary %+v != whole summary %+v; the summary must not be window/page local", narrow.Summary, state.Summary)
	}
}

// -----------------------------------------------------------------------------
// 8. COMPOSITE (DATE, WORK_ITEM_ID) KEYSET CORRECTNESS ACROSS MIXED DUE DATES
// -----------------------------------------------------------------------------

// TestWeighingKernelRollForwardKeysetSpansMultipleDueDatesAcrossChunks is the
// regression for the scale-shape defect where the roll-forward/delayed/
// day-start claims ORDER BY work_item_id while their supporting indexes are
// (tenant_id, work_state, due_business_date/planned_business_date,
// work_item_id) — a date-then-id index cannot serve a plain work_item_id
// ORDER BY. This fixture forces MULTIPLE DISTINCT stale due business dates
// (never just one shared date, which the original chunk test already covered)
// spread across several small keyset chunks, so a claim loop that lost rows or
// stalled between date groups would leave some items un-rolled-forward or
// would fail to terminate.
func TestWeighingKernelRollForwardKeysetSpansMultipleDueDatesAcrossChunks(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	const extra = 9
	for i := 0; i < extra; i++ {
		addExtraBucket(t, ctx, pool,
			fmt.Sprintf("00000000-0000-4000-8000-0000000092%02d", 10+i),
			fmt.Sprintf("00000000-0000-4000-8000-0000000032%02d", 10+i),
			fmt.Sprintf("Multi-date shed %02d", i),
			repoOperator)
	}
	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	repo := NewRepository(pool, 5*time.Second)
	publishFixtureCampaign(t, ctx, repo, "publish:multi-date-keyset")
	total := countWorkItems(t, ctx, pool)
	if total != extra+2 {
		t.Fatalf("work items=%d, want %d", total, extra+2)
	}

	// Stagger every work item onto one of THREE distinct stale due business
	// dates (all strictly before kernelLaterDate), interleaved by work_item_id
	// so the row-value-equivalent keyset must cross date boundaries mid-chunk,
	// not just once at the end.
	staleDates := []string{"2026-07-30", "2026-07-31", "2026-08-01"}
	rows, err := pool.Query(ctx, `SELECT work_item_id::text FROM weighing_work_items WHERE tenant_id=$1::uuid ORDER BY work_item_id`, repoTenant)
	if err != nil {
		t.Fatalf("list work items: %v", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan work item id: %v", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) != total {
		t.Fatalf("listed %d work item ids, want %d", len(ids), total)
	}
	for i, id := range ids {
		due := staleDates[i%len(staleDates)]
		if _, err := pool.Exec(ctx, `UPDATE weighing_work_items SET due_business_date=$2::date WHERE work_item_id=$1::uuid`, id, due); err != nil {
			t.Fatalf("stagger due date for %s: %v", id, err)
		}
	}

	// Small chunk size relative to the row count and date spread forces
	// several chunks, each of which must cross at least one date boundary.
	done := make(chan domain.KernelSweepResult, 1)
	errs := make(chan error, 1)
	go func() {
		res, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{
			TenantID: repoTenant, AsOf: businessInstant(t, kernelLaterDate), ChunkSize: 2, MaxChunks: 50,
		})
		if err != nil {
			errs <- err
			return
		}
		done <- res
	}()
	select {
	case err := <-errs:
		t.Fatalf("multi-date roll-forward sweep: %v", err)
	case res := <-done:
		if res.RolledForward != total {
			t.Fatalf("rolled forward=%d, want all %d work items across every stale due date", res.RolledForward, total)
		}
		if res.Truncated {
			t.Fatal("multi-date sweep must drain fully (not report Truncated) once every chunk budget is sufficient")
		}
	case <-time.After(60 * time.Second):
		t.Fatal("multi-date roll-forward sweep did not terminate: the composite keyset is not making forward progress across a date boundary")
	}

	var stillStale int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM weighing_work_items
WHERE tenant_id=$1::uuid AND due_business_date < $2::date`, repoTenant, kernelLaterDate).Scan(&stillStale); err != nil {
		t.Fatalf("count still-stale work items: %v", err)
	}
	if stillStale != 0 {
		t.Fatalf("%d work items were left with a stale due_business_date; the multi-date keyset skipped rows", stillStale)
	}

	var todayCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM weighing_work_items
WHERE tenant_id=$1::uuid AND due_business_date = $2::date`, repoTenant, kernelLaterDate).Scan(&todayCount); err != nil {
		t.Fatalf("count rolled-forward-to-today work items: %v", err)
	}
	if todayCount != total {
		t.Fatalf("only %d of %d work items rolled forward to %q", todayCount, total, kernelLaterDate)
	}
}
