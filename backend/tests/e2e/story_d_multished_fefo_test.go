package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// TestKernelStoryD_MultiShedFEFO proves real generation, park consolidation, stock reservation,
// completion, and FEFO consumption across two singleton sheds.
func TestKernelStoryD_MultiShedFEFO(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-d", "Multi-shed drive with FEFO dose consumption",
		"Two singleton sheds generate vaccination work. SM-4 consolidates them into a park drive, reserves "+
			"the earliest-expiry stock lot, and SM-5 consumes that lot when both doses are accepted; the later "+
			"lot remains untouched.")
	defer story.Finish()

	const (
		shedAID  = "e1000000-0000-4000-8000-0000000000d1"
		shedBID  = "e1000000-0000-4000-8000-0000000000d2"
		stageA   = "e1000000-0000-4000-8000-0000000000da"
		stageB   = "e1000000-0000-4000-8000-0000000000db"
		goatA    = "e1000000-0000-4000-8000-0000000000d3"
		goatB    = "e1000000-0000-4000-8000-0000000000d4"
		itemID   = "e1000000-0000-4000-8000-0000000000d5"
		earlyLot = "e1000000-0000-4000-8000-0000000000d6"
		lateLot  = "e1000000-0000-4000-8000-0000000000d7"
	)

	fx.SeedShed(shedAID, "E2E-D-A", stageA)
	fx.SeedAdultShed(shedBID, "E2E-D-B", stageB, "K1-DB")
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_d", 21, 14, nil)
	due := time.Now().UTC().AddDate(0, 0, 10)
	dob := due.AddDate(0, 0, -21)
	fx.SeedGoat(GoatSpec{GoatID: goatA, ShedID: shedAID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: goatB, ShedID: shedBID, DOB: &dob})
	fx.exec("vaccine item", `INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		VALUES ($1, $2, 'VAC-E2E-D', 'E2E Story D vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("early FEFO lot", `INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		VALUES ($1, $2, $3, $4, 10, 0, 'dose', CURRENT_DATE + INTERVAL '30 days')`, earlyLot, fxTenant, itemID, fxPark)
	fx.exec("later FEFO lot", `INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		VALUES ($1, $2, $3, $4, 10, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`, lateLot, fxTenant, itemID, fxPark)

	story.Step("Generate one obligation per shed through goat.created",
		"The production SM-1 consumer creates both obligations at the goats' actual shed scopes.")
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatA, due)
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatB, due)
	story.Assert("two generated obligations exist", fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND protocol_version_id=$2`, fxTenant, versionID) == 2,
		"count=%d", fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND protocol_version_id=$2`, fxTenant, versionID))

	story.Step("Consolidate both singleton sheds and reserve FEFO stock",
		"SM-4 forms one park-scoped drive and the inventory reservation path must choose the earliest-expiry eligible lot.")
	cfg := defaultParkSweepConfig()
	cfg.VaccineItemID = itemID
	cfg.DosesPerGoat = 1
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	result, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, cfg, due.AddDate(0, 0, 1))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("both obligations joined one drive", result.Batches == 1 && result.Obligations == 2, "batches=%d obligations=%d", result.Batches, result.Obligations)
	batchID := fx.scanText(`SELECT batch_id::text FROM obligation_batches WHERE tenant_id=$1 AND protocol_version_id=$2`, fxTenant, versionID)
	reservedLot := fx.scanText(`SELECT lot_id::text FROM inventory_stock_movements WHERE tenant_id=$1 AND batch_id=$2 AND movement_type='reserve' ORDER BY occurred_at, movement_id LIMIT 1`, fxTenant, batchID)
	story.Assert("earliest-expiry lot was reserved", reservedLot == earlyLot, "reserved=%s want=%s", reservedLot, earlyLot)

	story.Step("Record and accept both doses through SM-5",
		"The real completion service accepts both drive records and consumes the reserved FEFO lot.")
	service := vaccapp.NewService(fx.Vacc)
	completion := vaccapp.NewCompletionService(service, fx.Obl, fx.Inv)
	doses := int32(1)
	for _, row := range []struct{ goatID, key string }{{goatA, "story-d-a"}, {goatB, "story-d-b"}} {
		obligationID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, row.goatID)
		batch, lot := batchID, reservedLot
		completionID, applied, recordErr := service.RecordCompletion(fx.Ctx, vaccdomain.NewCompletion{
			TenantID: fxTenant, ObligationID: obligationID, GoatID: row.goatID, BatchID: &batch,
			VaccineInventoryLotID: &lot, Doses: &doses, RouteSite: "subcutaneous",
			AdministeredAt: due, ColdChainVerified: true, Status: "recorded", IdempotencyKey: row.key,
		})
		if recordErr != nil || !applied {
			t.Fatalf("record %s: applied=%v err=%v", row.goatID, applied, recordErr)
		}
		accepted, acceptErr := completion.AcceptExisting(fx.Ctx, vaccapp.AcceptExistingInput{TenantID: fxTenant, CompletionID: completionID})
		story.Assert("accepted dose for "+row.goatID, acceptErr == nil && accepted.Completed, "result=%+v err=%v", accepted, acceptErr)
	}

	earlyRemaining := fx.scanText(`SELECT quantity_in_stock::text FROM inventory_stock WHERE tenant_id=$1 AND stock_id=$2`, fxTenant, earlyLot)
	lateRemaining := fx.scanText(`SELECT quantity_in_stock::text FROM inventory_stock WHERE tenant_id=$1 AND stock_id=$2`, fxTenant, lateLot)
	story.Assert("two doses were consumed from the early lot", earlyRemaining == "8", "remaining=%s", earlyRemaining)
	story.Assert("later-expiry lot remains untouched", lateRemaining == "10", "remaining=%s", lateRemaining)
}
