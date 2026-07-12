package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryP_StockBlockedDrive proves SM-4 marks a drive stock-blocked when FEFO cannot cover
// the required dose count — obligations stay attached but execution must wait for stock.
func TestKernelStoryP_StockBlockedDrive(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-p", "Stock-blocked drive when FEFO cannot reserve",
		"Two goats are due in one shed but only one vaccine dose is in stock. The sweeper forms the "+
			"drive but marks it stock-blocked — no partial reserve, no silent execution.")
	defer story.Finish()
	story.Certify("backend kernel")

	const (
		shedID  = "f1000000-0000-4000-8000-000000000001"
		stageID = "f1000000-0000-4000-8000-000000000002"
		goat1   = "f1000000-0000-4000-8000-000000000010"
		goat2   = "f1000000-0000-4000-8000-000000000011"
		itemID  = "f1000000-0000-4000-8000-000000000020"
		lotID   = "f1000000-0000-4000-8000-000000000021"
	)

	fx.SeedShed(shedID, "E2E-P", stageID)
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_p", 21, 0, nil)

	now := time.Now().UTC()
	dob := now.AddDate(0, 0, -53)
	fx.SeedGoat(GoatSpec{GoatID: goat1, ShedID: shedID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: goat2, ShedID: shedID, DOB: &dob})
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-P', 'E2E Story P vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock (insufficient)",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 1, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`, lotID, fxTenant, itemID, shedID)

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	if _, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionID, now); err != nil {
		t.Fatalf("generate: %v", err)
	}

	story.Step("Sweep with insufficient stock",
		"Two goats need two doses but only one is available — the batch must be stock-blocked.")
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	_, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{VaccineItemID: itemID, DosesPerGoat: 1}, now.AddDate(0, 0, 1))
	story.Assert("sweep completed (blocked batch, not a hard error)", err == nil, "err=%v", err)

	blocked := fx.countRows(`SELECT count(*) FROM obligation_batches WHERE tenant_id=$1 AND context ? 'stock_block'`, fxTenant)
	story.Assert("one stock-blocked batch recorded", blocked == 1, "blocked_batches=%d", blocked)

	reserved := fx.scanText(`SELECT quantity_reserved::text FROM inventory_stock WHERE stock_id=$1`, lotID)
	story.Assert("no stock was reserved on a blocked drive", reserved == "0", "reserved=%q", reserved)

	movements := fx.countRows(`SELECT count(*) FROM inventory_stock_movements WHERE tenant_id=$1 AND movement_type='reserve'`, fxTenant)
	story.Assert("no reserve movement on blocked drive", movements == 0, "movements=%d", movements)
}
