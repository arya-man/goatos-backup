package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryZ_MissedCatchUpBatch generates all work through goat.created, fast-forwards one
// row through the missed sweeper, then proves SM-4 includes it in a healthy catch-up drive.
func TestKernelStoryZ_MissedCatchUpBatch(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-z", "Missed obligation catch-up batching",
		"Three healthy goats receive generated work. The oldest window crosses missed while two remain "+
			"scheduled. The real sweeper batches all three into the same catch-up drive.")
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
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_z", 21, 14, nil)
	now := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	missedDue := now.AddDate(0, 0, -20)
	healthyDue := now.AddDate(0, 0, -10)
	missedDOB := missedDue.AddDate(0, 0, -21)
	healthyDOB := healthyDue.AddDate(0, 0, -21)
	fx.SeedGoat(GoatSpec{GoatID: missedID, ShedID: shedID, DOB: &missedDOB})
	fx.SeedGoat(GoatSpec{GoatID: healthy1, ShedID: shedID, DOB: &healthyDOB})
	fx.SeedGoat(GoatSpec{GoatID: healthy2, ShedID: shedID, DOB: &healthyDOB})
	fx.exec("vaccine item", `INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		VALUES ($1, $2, 'VAC-E2E-Z', 'E2E Story Z vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock", `INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		VALUES ($1, $2, $3, $4, 20, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`,
		"ec000000-0000-4000-8000-000000000021", fxTenant, itemID, shedID)

	story.Step("Generate three obligations from identity events",
		"The missed candidate is generated earlier; the two healthy shed-mates are generated later while all windows are valid.")
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, missedID, missedDue)
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, healthy1, healthyDue)
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, healthy2, healthyDue)

	story.Step("Fast-forward only the oldest row into missed",
		"The real MarkMissed worker evaluates authored window_end values and closes exactly one obligation.")
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	marked, err := sweeper.MarkMissed(fx.Ctx, fxTenant, now)
	story.Assert("exactly one obligation became missed", err == nil && marked == 1, "marked=%d err=%v", marked, err)

	story.Step("Sweep all three into date-correct catch-up drives",
		"SM-4 treats a clinically clear missed row as batchable, while preserving separate drive dates for the older missed row and the newer scheduled rows.")
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{VaccineItemID: itemID, DosesPerGoat: 1}, now.AddDate(0, 0, 1))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("two due-date drives formed", sweepRes.Batches == 2, "batches=%d", sweepRes.Batches)
	story.Assert("all three obligations batched", sweepRes.Obligations == 3, "obligations=%d", sweepRes.Obligations)
	missedBatch := fx.scanText(`SELECT COALESCE(batch_id::text, '') FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, missedID)
	story.Assert("missed goat is on the catch-up batch", missedBatch != "", "batch_id=%q", missedBatch)
}
