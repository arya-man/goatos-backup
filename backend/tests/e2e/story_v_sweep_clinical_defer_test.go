package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryV_SweepClinicalDefer drives the SM-4 pre-batch clinical recheck: a goat that became
// sick after its dose was scheduled must be deferred at sweep time and excluded from the shed drive.
func TestKernelStoryV_SweepClinicalDefer(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-v", "SM-4 sweep clinical recheck defers sick goat",
		"Three goats share a shed with scheduled doses. One turns sick before the sweeper runs. "+
			"SM-4's pre-batch clinical recheck must defer that goat and batch only the two healthy animals.")
	defer story.Finish()
	story.Certify("backend kernel")

	const (
		shedID   = "f7000000-0000-4000-8000-000000000001"
		stageID  = "f7000000-0000-4000-8000-000000000002"
		sickID   = "f7000000-0000-4000-8000-000000000010"
		healthy1 = "f7000000-0000-4000-8000-000000000011"
		healthy2 = "f7000000-0000-4000-8000-000000000012"
		itemID   = "f7000000-0000-4000-8000-000000000020"
	)

	fx.SeedShed(shedID, "E2E-V", stageID)
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_v", 21, 0, []string{"sick", "quarantine", "icu"})

	now := time.Now().UTC()
	dob := now.AddDate(0, 0, -53)
	fx.SeedGoat(GoatSpec{GoatID: sickID, ShedID: shedID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: healthy1, ShedID: shedID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: healthy2, ShedID: shedID, DOB: &dob})
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-V', 'E2E Story V vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 20, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`,
		"f7000000-0000-4000-8000-000000000021", fxTenant, itemID, shedID)

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	if _, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionID, now); err != nil {
		t.Fatalf("generate: %v", err)
	}

	story.Step("Goat turns sick after doses were scheduled",
		"The production identity command emits goat.health.changed; its vaccination consumer defers the open dose before SM-4 can batch it.")
	fx.ChangeGoatHealth(sickID, "sick", "story-v-sick", now)
	preStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, sickID)
	story.Assert("identity event recheck already deferred the obligation", preStatus == "deferred", "status=%q", preStatus)

	story.Step("Sweep defers sick goat and batches only healthy shed-mates",
		"deferBlockedSweepCandidates runs before batching; sick scheduled rows must not enter the drive.")
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{VaccineItemID: itemID, DosesPerGoat: 1}, now.AddDate(0, 0, 1))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("only two healthy goats batched", sweepRes.Obligations == 2, "obligations=%d", sweepRes.Obligations)

	postStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, sickID)
	story.Assert("sick goat remains deferred after sweep", postStatus == "deferred", "status=%q", postStatus)
	sickBatch := fx.scanText(`SELECT COALESCE(batch_id::text, '') FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, sickID)
	story.Assert("deferred sick goat is not on the batch", sickBatch == "", "batch_id=%q", sickBatch)
}
