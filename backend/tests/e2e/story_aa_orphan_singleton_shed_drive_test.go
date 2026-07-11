package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryAA_OrphanSingletonShedDrive drives Batch story B layer 3: when only one goat is
// due in a shed and no park merge partner exists, SM-4's shed fallback still creates a micro-drive.
func TestKernelStoryAA_OrphanSingletonShedDrive(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-aa", "Orphan singleton: shed micro-drive fallback",
		"Only one goat is due in its shed — below the minimum for a normal shed drive and with no "+
			"park partner to merge. SM-4 layer 3 must still batch it via the orphan singleton shed fallback.")
	defer story.Finish()

	const (
		shedID  = "ed000000-0000-4000-8000-000000000001"
		stageID = "ed000000-0000-4000-8000-000000000002"
		goatID  = "ed000000-0000-4000-8000-000000000010"
		itemID  = "ed000000-0000-4000-8000-000000000020"
	)

	fx.SeedShed(shedID, "E2E-AA", stageID)
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_aa", 21, 7, nil)

	dob := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob})
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-AA', 'E2E Story AA vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`,
		"ed000000-0000-4000-8000-000000000021", fxTenant, itemID, shedID)

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatID, due)

	story.Step("Sweep with park consolidation enabled but no merge partner",
		"Layer 1 skips the singleton; layer 2 finds no park merge; layer 3 creates a shed micro-drive.")
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, defaultParkSweepConfig(), time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("singleton goat batched", sweepRes.Obligations == 1, "obligations=%d", sweepRes.Obligations)

	batchID := fx.scanText(`SELECT COALESCE(batch_id::text, '') FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goatID)
	story.Assert("orphan goat is on a batch", batchID != "", "batch_id=%q", batchID)

	scopeType := fx.scanText(`SELECT scope_type FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2::uuid`, fxTenant, batchID)
	story.Assert("fallback drive is shed-scoped", scopeType == "shed", "scope_type=%q", scopeType)
}
