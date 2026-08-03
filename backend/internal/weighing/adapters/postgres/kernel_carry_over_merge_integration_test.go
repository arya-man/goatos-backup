package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// CARRY-OVER THAT LANDS ON A SHED ANOTHER TASK ALREADY COVERS (maintainer rule,
// 2026-08-03).
//
// Unfinished weighing rolls forward. If the day it rolls onto ALREADY has an open
// claim on the same (park, shed), two people would owe the same shed on the same
// real day. The rule: the task already planned for that day carries on, and the
// carried-over item is simply CLOSED and linked to it.
//
// NOTHING IS TRANSFERRED. No work item changes operator and no capture moves --
// whatever the closed task weighed stays its own history, and the surviving task
// does its own weighing (free-flow: the same tags again, or different ones).
//
// This proves all three cases in one fixture, because the interesting part is
// that they are the SAME rule:
//
//	case 1  collision, different operators -> slipped item closed, survivor untouched
//	case 2  NO collision                   -> ordinary roll-forward, nothing closed
//	case 3  collision, SAME operator       -> identical to case 1, no special case
func TestCarryOverMergesOnlyWhenTheNewDayAlreadyCoversThatShed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const (
		slippedShedA = "00000000-0000-4000-8000-000000004101" // collides, other operator
		survivorA    = "00000000-0000-4000-8000-000000004102"
		lonelyShed   = "00000000-0000-4000-8000-000000004103" // no collision
		slippedShedB = "00000000-0000-4000-8000-000000004104" // collides, same operator
		survivorB    = "00000000-0000-4000-8000-000000004105"
	)
	sheds := map[string]string{
		slippedShedA: "00000000-0000-4000-8000-000000004201",
		survivorA:    "00000000-0000-4000-8000-000000004201", // SAME physical shed as the slipped one
		lonelyShed:   "00000000-0000-4000-8000-000000004202",
		slippedShedB: "00000000-0000-4000-8000-000000004203",
		survivorB:    "00000000-0000-4000-8000-000000004203", // SAME physical shed
	}
	operators := map[string]string{
		slippedShedA: repoOperator,
		survivorA:    kernelSecondOp, // different person holds it that day
		lonelyShed:   repoOperator,
		slippedShedB: repoOperator,
		survivorB:    repoOperator, // same person: still no special case
	}
	// Yesterday's unfinished work, and today's already-planned work.
	dueDates := map[string]string{
		slippedShedA: kernelPlannedDate,
		survivorA:    kernelLaterDate,
		lonelyShed:   kernelPlannedDate,
		slippedShedB: kernelPlannedDate,
		survivorB:    kernelLaterDate,
	}

	// The collision is ACROSS TASKS: one campaign cannot hold the same shed twice
	// (weighing_campaign_sheds_campaign_location_uidx), which is exactly why the
	// day-level collision needs its own rule. The survivors therefore live in a
	// SECOND task, planned for the later day.
	const survivorCampaign = "00000000-0000-4000-8000-000000004900"
	lcpInsertCampaign(t, ctx, pool, survivorCampaign, repoPark, kernelLaterDate, domain.StatusPublished, kernelSecondOp)

	campaigns := map[string]string{
		slippedShedA: repoCampaign,
		survivorA:    survivorCampaign,
		lonelyShed:   repoCampaign,
		slippedShedB: repoCampaign,
		survivorB:    survivorCampaign,
	}

	for bucketID, shedID := range sheds {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', $3, $4::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, shedID, repoTenant, "carry-over shed", repoPark)
		lcpInsertBucket(t, ctx, pool, bucketID, campaigns[bucketID], shedID, domain.CategoryPerShedPartition, operators[bucketID], 1, "pending")
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO weighing_work_items (
  tenant_id, campaign_id, campaign_shed_id, park_id, operator_user_id, weighing_category,
  shed_label, shed_location_id, planned_business_date, due_business_date, work_state
)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'per_shed_partition',
        'carry-over bucket', $6::uuid, $7::date, $7::date, 'scheduled')
ON CONFLICT (tenant_id, campaign_shed_id) DO NOTHING`,
			repoTenant, campaigns[bucketID], bucketID, repoPark, operators[bucketID], shedID, dueDates[bucketID])
	}

	result, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{
		TenantID: repoTenant,
		AsOf:     businessInstant(t, kernelLaterDate),
	})
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if result.MergedOnCarryOver != 2 {
		t.Fatalf("merged_on_carry_over=%d, want exactly the 2 colliding carry-overs", result.MergedOnCarryOver)
	}

	// CASE 1 — collision, different operators.
	slippedA := readWorkItem(t, ctx, pool, slippedShedA)
	if slippedA.state != "closed" {
		t.Fatalf("colliding carry-over state=%q, want closed -- two people must never owe one shed on one day", slippedA.state)
	}
	if slippedA.operatorID != repoOperator {
		t.Fatalf("closed item operator=%q, want it UNCHANGED (%q): closing is not a re-assignment", slippedA.operatorID, repoOperator)
	}
	survivorRowA := readWorkItem(t, ctx, pool, survivorA)
	if survivorRowA.state != "scheduled" || survivorRowA.operatorID != kernelSecondOp {
		t.Fatalf("survivor=%+v, want it untouched and still %s's work", survivorRowA, kernelSecondOp)
	}

	// The closed item says WHERE the shed went, so the operator's carry-over is
	// answerable from one row rather than silently gone.
	var mergedInto, closedReason string
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(merged_into_work_item_id::text, ''), COALESCE(closed_reason, '')
FROM weighing_work_items WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`,
		repoTenant, slippedShedA).Scan(&mergedInto, &closedReason); err != nil {
		t.Fatalf("read merge link: %v", err)
	}
	if mergedInto == "" || closedReason != "merged_on_carry_over" {
		t.Fatalf("merged_into=%q reason=%q, want a link to the surviving item and the machine reason", mergedInto, closedReason)
	}

	// THE BUCKET CLOSES TOO. This is the half that matters to the person holding the
	// phone: weighing_work_items has no operator-facing reader, so a bucket left
	// 'pending' would keep the shed on their task and invite them to walk to a shed
	// somebody else is standing at -- while the kernel, having closed the work item,
	// no longer chases it at all.
	assertScopeStatus(t, ctx, pool, slippedShedA, "closed")
	assertScopeStatus(t, ctx, pool, slippedShedB, "closed")
	// The surviving task's bucket is untouched: it is still today's work.
	assertScopeStatus(t, ctx, pool, survivorA, "pending")
	assertScopeStatus(t, ctx, pool, survivorB, "pending")

	// CASE 2 — no collision: ordinary roll-forward, untouched by this rule.
	assertScopeStatus(t, ctx, pool, lonelyShed, "pending") // still real work, nobody else has it
	lonely := readWorkItem(t, ctx, pool, lonelyShed)
	if lonely.state == "closed" {
		t.Fatal("a carry-over with NO collision was closed; only a shed another task already covers may merge")
	}
	if lonely.due != kernelLaterDate || lonely.rolledCount != 1 {
		t.Fatalf("non-colliding carry-over=%+v, want it rolled forward to %s exactly once", lonely, kernelLaterDate)
	}
	if lonely.planned != kernelPlannedDate {
		t.Fatalf("planned date=%q, want the original %q preserved for audit", lonely.planned, kernelPlannedDate)
	}

	// CASE 3 — collision, SAME operator: identical outcome, no special case.
	slippedB := readWorkItem(t, ctx, pool, slippedShedB)
	if slippedB.state != "closed" {
		t.Fatalf("same-operator collision state=%q, want closed -- 'it was mine anyway' is not an exception", slippedB.state)
	}
	if survivorRow := readWorkItem(t, ctx, pool, survivorB); survivorRow.state != "scheduled" {
		t.Fatalf("same-operator survivor=%+v, want it carrying on", survivorRow)
	}

	// Idempotent: a second sweep on the same business day merges nothing new.
	again, err := repo.SweepWorkItems(ctx, domain.KernelSweepParams{
		TenantID: repoTenant,
		AsOf:     businessInstant(t, kernelLaterDate),
	})
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if again.MergedOnCarryOver != 0 {
		t.Fatalf("second sweep merged %d, want 0 -- the pass must be replay-safe", again.MergedOnCarryOver)
	}
}
