package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryX_MissingEntryDateRoutesToAdultCatchUp drives scenario 8 per the 2026-07-14
// vaccination seed/schedule fix plan (Fix Plan B2/B3): a procured adult arriving without a
// farm-entry date, with no vaccination history for this vaccine, must NOT get a fabricated
// "missing_due_date" deferred blocker — a missing entry-date anchor is never itself a clinical
// defer reason. It routes to the adult catch-up path at the next compatible drive: a normal
// scheduled obligation due today, which the sweeper batches like any other due work.
func TestKernelStoryX_MissingEntryDateRoutesToAdultCatchUp(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-x", "Missing entry date, no history: adult catch-up, not a deferred gap",
		"A procured adult arrives without a farm-entry date and has never received this vaccine. "+
			"Generation must never defer merely because entry_date is unknown (Fix Plan B2/B3) — it "+
			"materializes a normal scheduled catch-up obligation due today, and the sweeper batches it.")
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
		"The real generation engine must route the never-received vaccine to the adult catch-up "+
			"path (due today) instead of a fabricated missing-entry-date deferral.")
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	asOf := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	res, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatID, asOf)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("one scheduled catch-up obligation, never a deferred gap", res.Generated == 1 && res.Deferred == 0, "deferred=%d generated=%d", res.Deferred, res.Generated)

	status := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goatID)
	story.Assert("obligation is scheduled, not deferred (no entry date is not a clinical defer)", status == "scheduled", "status=%q", status)

	story.Step("Sweeper batches the catch-up obligation",
		"A missing-entry-date catch-up is normal due work, not an excluded defer row — SM-4 batches it.")
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-X', 'E2E Story X vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{VaccineItemID: itemID, DosesPerGoat: 1}, asOf.AddDate(0, 0, 30))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("a drive is formed for the catch-up obligation", sweepRes.Batches == 1, "batches=%d", sweepRes.Batches)

	scheduledAfterSweep := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled' AND batch_id IS NOT NULL`, fxTenant, goatID)
	story.Assert("catch-up obligation is batched, not stuck deferred", scheduledAfterSweep == 1, "batched_scheduled=%d", scheduledAfterSweep)
}
