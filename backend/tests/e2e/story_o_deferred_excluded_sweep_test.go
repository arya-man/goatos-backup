package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryO_DeferredExcludedFromSweep proves deferred obligations never enter SM-4 batching:
// a clinically held goat stays out of the shed drive while healthy shed-mates batch normally.
func TestKernelStoryO_DeferredExcludedFromSweep(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-o", "Deferred goat excluded from shed drive sweep",
		"Three goats share a shed. One is held after becoming sick; two are healthy and due. "+
			"The sweeper must batch only the two healthy goats — the deferred dose must stay unbatched.")
	defer story.Finish()

	const (
		shedID   = "f0000000-0000-4000-8000-000000000001"
		stageID  = "f0000000-0000-4000-8000-000000000002"
		held     = "f0000000-0000-4000-8000-000000000010"
		healthy1 = "f0000000-0000-4000-8000-000000000011"
		healthy2 = "f0000000-0000-4000-8000-000000000012"
		itemID   = "f0000000-0000-4000-8000-000000000020"
	)

	fx.SeedShed(shedID, "E2E-O", stageID)
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_o", 21, 0, []string{"sick", "quarantine", "icu"})

	now := time.Now().UTC()
	dob := now.AddDate(0, 0, -53)
	fx.SeedGoat(GoatSpec{GoatID: held, ShedID: shedID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: healthy1, ShedID: shedID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: healthy2, ShedID: shedID, DOB: &dob})
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-O', 'E2E Story O vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 20, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`,
		"f0000000-0000-4000-8000-000000000021", fxTenant, itemID, shedID)

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	res, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionID, now)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("three doses generated", res.Generated == 3, "generated=%d", res.Generated)

	story.Step("Hold one goat through the identity health path",
		"The goat becomes sick through the production command; goat.health.changed reaches the vaccination recheck and keeps it out of drive batching.")
	fx.ChangeGoatHealth(held, "sick", "story-o-sick", now.Add(24*time.Hour))

	heldStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, held)
	story.Assert("held goat obligation is deferred", heldStatus == "deferred", "status=%q", heldStatus)

	story.Step("Sweep the shed drive",
		"SM-4 must attach only the two healthy goats; the deferred obligation keeps batch_id NULL.")
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{VaccineItemID: itemID, DosesPerGoat: 1}, now.AddDate(0, 0, 1))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("exactly one shed drive formed", sweepRes.Batches == 1, "batches=%d", sweepRes.Batches)
	story.Assert("only two healthy goats batched", sweepRes.Obligations == 2, "obligations=%d", sweepRes.Obligations)

	heldBatch := fx.scanText(`SELECT COALESCE(batch_id::text, '') FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, held)
	story.Assert("deferred goat is not on any batch", heldBatch == "", "batch_id=%q", heldBatch)
}
