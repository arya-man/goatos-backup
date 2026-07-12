package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryX_MissingEntryDateDefer drives scenario 8: a procured adult without a farm-entry
// date gets a visible deferred gap (missing_entry_date) and is excluded from shed drive batching.
func TestKernelStoryX_MissingEntryDateDefer(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-x", "Missing entry date: visible defer, no drive batching",
		"A procured adult arrives without a farm-entry date. Generation must create a deferred "+
			"missing-entry-date obligation, not a schedulable post-arrival dose. The sweeper must not batch it.")
	defer story.Finish()
	story.Certify("backend kernel")

	const (
		shedID  = "e9000000-0000-4000-8000-000000000001"
		stageID = "e9000000-0000-4000-8000-000000000002"
		goatID  = "e9000000-0000-4000-8000-000000000010"
		itemID  = "e9000000-0000-4000-8000-000000000020"
	)

	fx.SeedAdultShed(shedID, "E2E-X", stageID, "A1")
	dob := time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob, NoEntryDate: true, Stage: "A1"})

	versionID, _ := fx.PublishScheduleProtocol("vaccination.e2e.story_x", "{}",
		[]RuleSpec{{DoseCode: "on_arrival", Sequence: 1, TriggerType: "post_arrival", OffsetDays: 0, DueWindowDays: 14}})

	story.Step("Generate without farm-entry date",
		"The real generation engine must defer the procured adult with a missing-entry-date gap.")
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	asOf := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	res, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatID, asOf)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("one visible deferred gap created", res.Deferred == 1 && res.Generated == 1, "deferred=%d generated=%d", res.Deferred, res.Generated)

	status := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goatID)
	story.Assert("obligation is deferred (missing entry date)", status == "deferred", "status=%q", status)

	story.Step("Sweeper must skip the deferred gap",
		"Deferred rows are excluded from SM-4; no batch is formed for this goat.")
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-X', 'E2E Story X vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{VaccineItemID: itemID, DosesPerGoat: 1}, asOf.AddDate(0, 0, 30))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("no drive formed for missing-entry-date gap", sweepRes.Batches == 0, "batches=%d", sweepRes.Batches)

	deferredAfterSweep := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='deferred'`, fxTenant, goatID)
	story.Assert("missing-entry-date gap stays deferred through sweep", deferredAfterSweep == 1, "deferred=%d", deferredAfterSweep)
}
