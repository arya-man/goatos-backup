package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryR_MissingDOBDefer drives scenario 4: a kid with no DOB gets a visible deferred gap
// (missing_dob) and is excluded from shed drive batching until DOB is backfilled.
func TestKernelStoryR_MissingDOBDefer(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-r", "Missing DOB: visible defer, no drive batching",
		"A kid is registered without a date of birth. Generation must create a deferred missing-DOB "+
			"obligation (visible process gap), not a schedulable dose. The sweeper must not batch it.")
	defer story.Finish()
	story.Certify("backend kernel")

	const (
		shedID  = "f3000000-0000-4000-8000-000000000001"
		stageID = "f3000000-0000-4000-8000-000000000002"
		goatID  = "f3000000-0000-4000-8000-000000000010"
		itemID  = "f3000000-0000-4000-8000-000000000020"
	)

	fx.SeedShed(shedID, "E2E-R", stageID)
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_r", 21, 14, nil)
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, NoDOB: true, OriginType: "birth"})

	story.Step("Generate without DOB",
		"The real generation engine must defer the kid with a missing-DOB gap, not schedule a dose date.")
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	asOf := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	res, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatID, asOf)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("one visible deferred gap created", res.Deferred == 1 && res.Generated == 1, "deferred=%d generated=%d", res.Deferred, res.Generated)

	status := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goatID)
	story.Assert("obligation is deferred (missing DOB)", status == "deferred", "status=%q", status)

	story.Step("Sweeper must skip the deferred gap",
		"Deferred rows are excluded from SM-4; no batch is formed for this goat.")
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-R', 'E2E Story R vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{VaccineItemID: itemID, DosesPerGoat: 1}, asOf.AddDate(0, 0, 30))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("no drive formed for missing-DOB gap", sweepRes.Batches == 0, "batches=%d", sweepRes.Batches)

	deferredAfterSweep := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='deferred'`, fxTenant, goatID)
	story.Assert("missing-DOB gap stays deferred through sweep", deferredAfterSweep == 1, "deferred=%d", deferredAfterSweep)
}
