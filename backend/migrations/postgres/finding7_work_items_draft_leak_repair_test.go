package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// FINDING 7, part (b): the 000072 forward-repair migration must delete
// weighing_work_items rows whose PARENT campaign is still 'draft' today, and
// must leave alone weighing_work_items rows belonging to a campaign that is
// currently published (or any other non-draft status) -- exactly the "safe
// to keep" case where a campaign was draft when 000069 ran but has SINCE been
// published for real by createWorkItemsForPublishTx.
func TestWorkItemsDraftLeakRepairDeletesOnlyStillDraftCampaigns(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)

	const tenantID = "d0000000-0000-4000-8000-000000000001"
	const parkID = "d0000000-0000-4000-8000-000000000002"
	const shedLocID = "d0000000-0000-4000-8000-000000000003"
	const draftCampaignID = "d0000000-0000-4000-8000-000000000101"
	const draftShedID = "d0000000-0000-4000-8000-000000000102"
	const draftWorkItemID = "d0000000-0000-4000-8000-000000000103"
	const publishedCampaignID = "d0000000-0000-4000-8000-000000000201"
	const publishedShedID = "d0000000-0000-4000-8000-000000000202"
	const publishedWorkItemID = "d0000000-0000-4000-8000-000000000203"
	const operatorID = "d0000000-0000-4000-8000-000000000301"

	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Finding7 Tenant', 'active')`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, name, status)
VALUES ($1::uuid, $2::uuid, 'park', 'Finding7 Park', 'active'),
       ($3::uuid, $2::uuid, 'shed', 'Finding7 Shed', 'active')`,
		parkID, tenantID, shedLocID); err != nil {
		t.Fatalf("seed locations: %v", err)
	}
	// weighing_campaign_sheds enforces single-park operator scoping via a
	// trigger reading user_scope_grants (000065). Seed the grant this fixture
	// needs.
	if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, scope_type, scope_id, role, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'park', $3::uuid, 'operator', 'active', now())`,
		tenantID, operatorID, parkID); err != nil {
		t.Fatalf("seed operator scope grant: %v", err)
	}

	// Campaign A: STILL DRAFT today. Its work item must be deleted - it can
	// only exist because of the 000069 leak, since createWorkItemsForPublishTx
	// has never run for a campaign that never published.
	if _, err := pool.Exec(ctx, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-07-27', '2026-08-02', '2026-07-29', 'draft', 100, $4::uuid, $4::uuid)`,
		draftCampaignID, tenantID, parkID, operatorID); err != nil {
		t.Fatalf("seed draft campaign: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Draft Leak Shed', 'individual_animal', $5::uuid, 1)`,
		draftShedID, draftCampaignID, tenantID, shedLocID, operatorID); err != nil {
		t.Fatalf("seed draft campaign shed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO weighing_work_items (work_item_id, tenant_id, campaign_id, campaign_shed_id, park_id, operator_user_id, weighing_category, shed_label, shed_location_id, planned_business_date, due_business_date, work_state)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::uuid, 'individual_animal', 'Draft Leak Shed', $7::uuid, '2026-07-29', '2026-07-29', 'scheduled')`,
		draftWorkItemID, tenantID, draftCampaignID, draftShedID, parkID, operatorID, shedLocID); err != nil {
		t.Fatalf("seed leaked draft work item: %v", err)
	}

	// Campaign B: WAS draft when a hypothetical 000069 run happened, but has
	// SINCE been published for real. Its work item must survive untouched.
	if _, err := pool.Exec(ctx, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, '2026-08-03', '2026-08-09', '2026-08-05', 'published', 100, $4::uuid, $4::uuid)`,
		publishedCampaignID, tenantID, parkID, operatorID); err != nil {
		t.Fatalf("seed published campaign: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', 'Legit Published Shed', 'individual_animal', $5::uuid, 1)`,
		publishedShedID, publishedCampaignID, tenantID, shedLocID, operatorID); err != nil {
		t.Fatalf("seed published campaign shed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO weighing_work_items (work_item_id, tenant_id, campaign_id, campaign_shed_id, park_id, operator_user_id, weighing_category, shed_label, shed_location_id, planned_business_date, due_business_date, work_state)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6::uuid, 'individual_animal', 'Legit Published Shed', $7::uuid, '2026-07-29', '2026-07-29', 'scheduled')`,
		publishedWorkItemID, tenantID, publishedCampaignID, publishedShedID, parkID, operatorID, shedLocID); err != nil {
		t.Fatalf("seed legitimate published work item: %v", err)
	}

	migration, err := os.ReadFile("000072_weighing_backfill_work_items_draft_leak_repair.sql")
	if err != nil {
		t.Fatalf("read repair migration: %v", err)
	}
	if _, err := pool.Exec(ctx, migrationUp(string(migration))); err != nil {
		t.Fatalf("apply repair migration: %v", err)
	}

	var draftRowExists, publishedRowExists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM weighing_work_items WHERE work_item_id=$1::uuid)`, draftWorkItemID).Scan(&draftRowExists); err != nil {
		t.Fatalf("check draft work item: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM weighing_work_items WHERE work_item_id=$1::uuid)`, publishedWorkItemID).Scan(&publishedRowExists); err != nil {
		t.Fatalf("check published work item: %v", err)
	}
	if draftRowExists {
		t.Fatalf("work item under a still-draft campaign survived the repair migration, want deleted")
	}
	if !publishedRowExists {
		t.Fatalf("work item under a since-published campaign was incorrectly deleted by the repair migration")
	}

	// Re-run: idempotent, deletes nothing more, still leaves the published row.
	if _, err := pool.Exec(ctx, migrationUp(string(migration))); err != nil {
		t.Fatalf("re-apply repair migration: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM weighing_work_items WHERE work_item_id=$1::uuid)`, publishedWorkItemID).Scan(&publishedRowExists); err != nil {
		t.Fatalf("check published work item after re-run: %v", err)
	}
	if !publishedRowExists {
		t.Fatalf("re-running the repair migration incorrectly deleted the legitimate published-campaign work item")
	}
}
