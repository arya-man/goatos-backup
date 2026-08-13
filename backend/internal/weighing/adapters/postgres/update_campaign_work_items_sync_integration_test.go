package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// TestUpdateCampaignAddsWorkItemsForNewShedsOnPublishedCampaign verifies BUG 1:
// adding a shed to an already-published campaign must create corresponding
// weighing_work_items rows, not leave them orphaned.
//
// Before the fix: adding a shed to a published campaign creates the
// weighing_campaign_sheds row but never calls createWorkItemsForPublishTx, so
// the kernel has no work to surface -- Calendar, Control Tower, and the
// operator see no task for it.
func TestUpdateCampaignAddsWorkItemsForNewShedsOnPublishedCampaign(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	repo := NewRepository(pool, 5*time.Second)

	// Publish the campaign: this creates work items for the two seeded buckets.
	publishFixtureCampaign(t, ctx, repo, "publish:work-items-base")
	if got := countWorkItems(t, ctx, pool); got != 2 {
		t.Fatalf("work items after initial publish=%d, want 2", got)
	}

	// Now add a NEW shed to the already-published campaign. The bucket upsert
	// succeeds (no duplicate guard), but without the fix, no work item is created
	// for the new bucket.
	newShedLocationID := "00000000-0000-4000-8000-000000003102"

	// Create the new shed location.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', 'Gandhi 2', $3::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, newShedLocationID, repoTenant, repoPark)

	// Update the campaign to add the new shed.
	updatedCampaign, err := repo.UpdateCampaign(ctx, repoCampaign, domain.UpdateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   "2026-07-27",
		PeriodEndDate:     "2026-08-02",
		StartBusinessDate: "2026-07-29",
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		Sheds: []domain.CreateCampaignShed{
			// Keep the two original sheds...
			{LocationID: repoExpectedShed, LocationType: "shed", DisplayName: "Gandhi 1 - Part 1", WeighingCategory: domain.CategoryIndividualAnimal},
			{LocationID: repoPerShed, LocationType: "shed", DisplayName: "Q1", WeighingCategory: domain.CategoryPerShedPartition},
			// ...and add the new one.
			{LocationID: newShedLocationID, LocationType: "shed", DisplayName: "Gandhi 2", WeighingCategory: domain.CategoryPerShedPartition},
		},
		IdempotencyKey: "update:add-shed-to-published",
	})
	if err != nil {
		t.Fatalf("update campaign to add shed: %v", err)
	}

	// The campaign now has three buckets.
	if got := len(updatedCampaign.Sheds); got != 3 {
		t.Fatalf("campaign sheds after update=%d, want 3", got)
	}

	// Find the new shed's campaign_shed_id.
	var newShedCampaignShedID string
	for _, shed := range updatedCampaign.Sheds {
		if shed.LocationID == newShedLocationID {
			newShedCampaignShedID = shed.CampaignShedID
			break
		}
	}
	if newShedCampaignShedID == "" {
		t.Fatal("new shed not found in updated campaign")
	}

	// BUG 1 PROOF: before the fix, countWorkItems returns 2 (the original two).
	// After the fix, it must return 3 (the new shed has a work item).
	workItemsAfterUpdate := countWorkItems(t, ctx, pool)
	if workItemsAfterUpdate != 3 {
		t.Fatalf("work items after adding shed to published campaign=%d, want 3 (new shed's work item missing)", workItemsAfterUpdate)
	}

	// Assert the new work item has the correct operator and other columns.
	newWorkItem := readWorkItem(t, ctx, pool, newShedCampaignShedID)
	if newWorkItem.state != domain.WorkStateScheduled {
		t.Fatalf("new work item state=%q, want scheduled", newWorkItem.state)
	}
	if newWorkItem.operatorID != repoOperator {
		t.Fatalf("new work item operator_user_id=%q, want %q", newWorkItem.operatorID, repoOperator)
	}
	if newWorkItem.planned != "2026-07-29" {
		t.Fatalf("new work item planned_business_date=%q, want 2026-07-29", newWorkItem.planned)
	}
}

// TestUpdateCampaignSyncWorkItemsOnOperatorChange verifies BUG 2: reassigning
// a shed's operator must update the work_item's operator_user_id and park_id,
// not leave them stale.
//
// Before the fix: updating a published campaign to reassign a shed's operator
// updates the weighing_campaign_sheds row but the weighing_work_items row
// keeps its original operator. Cadence passes group by OperatorUserID read
// from the work item, so the OLD operator keeps getting nudged while the NEW
// operator sees nothing.
func TestUpdateCampaignSyncWorkItemsOnOperatorChange(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	repo := NewRepository(pool, 5*time.Second)

	// Publish the campaign.
	publishFixtureCampaign(t, ctx, repo, "publish:operator-reassign-base")

	// Read the work item for the lump-sum (per_shed_partition) bucket before the edit.
	originalWorkItem := readWorkItem(t, ctx, pool, repoShedScope)
	if originalWorkItem.operatorID != repoOperator {
		t.Fatalf("precondition: original work item operator=%q, want %q", originalWorkItem.operatorID, repoOperator)
	}

	// Now reassign the shed to a different operator via UpdateCampaign.
	_, err := repo.UpdateCampaign(ctx, repoCampaign, domain.UpdateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   "2026-07-27",
		PeriodEndDate:     "2026-08-02",
		StartBusinessDate: "2026-07-29",
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator, // Campaign-level operator stays the same
		CreatedBy:         repoOperator,
		Sheds: []domain.CreateCampaignShed{
			{LocationID: repoExpectedShed, LocationType: "shed", DisplayName: "Gandhi 1 - Part 1", WeighingCategory: domain.CategoryIndividualAnimal, OperatorUserID: repoOperator},
			// Reassign the per_shed bucket to a different operator.
			{LocationID: repoPerShed, LocationType: "shed", DisplayName: "Q1", WeighingCategory: domain.CategoryPerShedPartition, OperatorUserID: repoOtherOp},
		},
		IdempotencyKey: "update:reassign-operator",
	})
	if err != nil {
		t.Fatalf("update campaign with operator reassignment: %v", err)
	}

	// Verify the bucket's operator changed in weighing_campaign_sheds.
	var bucketOperatorAfter string
	if err := pool.QueryRow(ctx, `
SELECT operator_user_id::text
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoShedScope).
		Scan(&bucketOperatorAfter); err != nil {
		t.Fatalf("read updated bucket operator: %v", err)
	}
	if bucketOperatorAfter != repoOtherOp {
		t.Fatalf("bucket operator after update=%q, want %q", bucketOperatorAfter, repoOtherOp)
	}

	// BUG 2 PROOF: before the fix, the work_item's operator_user_id stays at
	// repoOperator. After the fix, it must be repoOtherOp.
	updatedWorkItem := readWorkItem(t, ctx, pool, repoShedScope)
	if updatedWorkItem.operatorID != repoOtherOp {
		t.Fatalf("work item operator_user_id after reassign=%q, want %q (operator not synced)", updatedWorkItem.operatorID, repoOtherOp)
	}
	// Other columns should stay the same.
	if updatedWorkItem.state != originalWorkItem.state {
		t.Fatalf("work item state changed after operator reassign: %q -> %q", originalWorkItem.state, updatedWorkItem.state)
	}
	if updatedWorkItem.planned != originalWorkItem.planned {
		t.Fatalf("work item planned date changed after operator reassign: %q -> %q", originalWorkItem.planned, updatedWorkItem.planned)
	}
}

// TestUpdateCampaignDoesNotSyncTerminalWorkItems verifies that terminal work
// items (completed/closed) are NOT rewritten, even if the bucket is reassigned.
// Only non-terminal items (scheduled/delayed) are synced.
func TestUpdateCampaignDoesNotSyncTerminalWorkItems(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	grantOperatorParkScope(t, ctx, pool)
	seedWeighingObservationFixture(t, ctx, pool)
	setCampaignStatus(t, ctx, pool, domain.StatusDraft)
	repo := NewRepository(pool, 5*time.Second)

	// Publish the campaign.
	publishFixtureCampaign(t, ctx, repo, "publish:terminal-no-sync-base")

	// Manually terminate the work item for one bucket (as if a sweep pass had
	// reconciled it after the bucket was completed).
	execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_work_items
SET work_state='completed', terminal_at=now(), updated_at=now()
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoShedScope)

	// Verify the work item is now completed.
	terminalBefore := readWorkItem(t, ctx, pool, repoShedScope)
	if terminalBefore.state != domain.WorkStateCompleted {
		t.Fatalf("precondition: work item not terminalized, state=%q", terminalBefore.state)
	}

	// Now reassign that shed's operator via UpdateCampaign.
	_, err := repo.UpdateCampaign(ctx, repoCampaign, domain.UpdateCampaign{
		TenantID:          repoTenant,
		ParkID:            repoPark,
		PeriodStartDate:   "2026-07-27",
		PeriodEndDate:     "2026-08-02",
		StartBusinessDate: "2026-07-29",
		PlannedCapPerDay:  100,
		OperatorUserID:    repoOperator,
		CreatedBy:         repoOperator,
		Sheds: []domain.CreateCampaignShed{
			{LocationID: repoExpectedShed, LocationType: "shed", DisplayName: "Gandhi 1 - Part 1", WeighingCategory: domain.CategoryIndividualAnimal},
			// Reassign the terminal bucket.
			{LocationID: repoPerShed, LocationType: "shed", DisplayName: "Q1", WeighingCategory: domain.CategoryPerShedPartition, OperatorUserID: repoOtherOp},
		},
		IdempotencyKey: "update:terminal-no-sync",
	})
	if err != nil {
		t.Fatalf("update campaign with operator reassignment: %v", err)
	}

	// The terminal work item must NOT be rewritten. Its operator and status stay unchanged.
	terminalAfter := readWorkItem(t, ctx, pool, repoShedScope)
	if terminalAfter.state != domain.WorkStateCompleted {
		t.Fatalf("terminal work item state changed after update: %q (should stay completed)", terminalAfter.state)
	}
	if terminalAfter.operatorID != repoOperator {
		t.Fatalf("terminal work item operator changed to %q (should NOT be rewritten)", terminalAfter.operatorID)
	}
}
