package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// TestKernelStoryZ_MissedCatchUpBatch drives Batch story G: a missed obligation remains eligible
// for SM-4 catch-up batching alongside healthy shed-mates (when the goat is clinically clear).
func TestKernelStoryZ_MissedCatchUpBatch(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-z", "Missed obligation catch-up batching",
		"A healthy goat's dose is already missed while two shed-mates are still schedulable. The "+
			"sweeper must include the missed row in the same shed catch-up drive — missed status is "+
			"batchable when the goat is not clinically blocked.")
	defer story.Finish()

	const (
		shedID   = "ec000000-0000-4000-8000-000000000001"
		stageID  = "ec000000-0000-4000-8000-000000000002"
		missedID = "ec000000-0000-4000-8000-000000000010"
		healthy1 = "ec000000-0000-4000-8000-000000000011"
		healthy2 = "ec000000-0000-4000-8000-000000000012"
		itemID   = "ec000000-0000-4000-8000-000000000020"
	)

	fx.SeedShed(shedID, "E2E-Z", stageID)
	versionID, ruleID := fx.PublishSimpleProtocol("vaccination.e2e.story_z", 21, 14, nil)

	now := time.Now().UTC()
	dob := now.AddDate(0, 0, -53)
	fx.SeedGoat(GoatSpec{GoatID: missedID, ShedID: shedID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: healthy1, ShedID: shedID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: healthy2, ShedID: shedID, DOB: &dob})
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-Z', 'E2E Story Z vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 20, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`,
		"ec000000-0000-4000-8000-000000000021", fxTenant, itemID, shedID)

	story.Step("Seed one missed and two scheduled obligations in the same shed",
		"All three goats share one rule/window; only the missed goat crossed its grace window.")
	due := now.AddDate(0, 0, -10)
	win := due.AddDate(0, 0, 14)
	for _, row := range []struct {
		goatID, key, status string
	}{
		{missedID, "e2e-story-z-missed", "missed"},
		{healthy1, "e2e-story-z-h1", "scheduled"},
		{healthy2, "e2e-story-z-h2", "scheduled"},
	} {
		_, applied, err := fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
			TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: row.goatID, ScopeType: "shed", ScopeID: shedID,
			DueAt: due, WindowEnd: &win, Status: row.status,
			IdempotencyKey: row.key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("seed %s: applied=%v err=%v", row.key, applied, err)
		}
	}

	story.Step("Sweep must batch all three goats into one shed catch-up drive",
		"Missed rows are included in the SM-4 unbatched query alongside scheduled/due rows.")
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{VaccineItemID: itemID, DosesPerGoat: 1}, now.AddDate(0, 0, 1))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("one shed drive formed", sweepRes.Batches == 1, "batches=%d", sweepRes.Batches)
	story.Assert("all three obligations batched", sweepRes.Obligations == 3, "obligations=%d", sweepRes.Obligations)

	missedBatch := fx.scanText(`SELECT COALESCE(batch_id::text, '') FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, missedID)
	story.Assert("missed goat is on the catch-up batch", missedBatch != "", "batch_id=%q", missedBatch)
}
