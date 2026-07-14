package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryR_MissingDOBRoutesToAdultCatchUp drives scenario 4 per the 2026-07-14 vaccination
// seed/schedule fix plan (docs/runbooks/vaccination-seed-audit-and-fix-plan-2026-07-14.md, Fix Plan
// B2/B3): a goat registered without a date of birth, and with no vaccination history for this
// vaccine, must NOT get a fabricated "missing_due_date" deferred blocker (that read as a clinical
// defer/"Deferred" gap and was confirmed bug #3 — 531 animals, ~1,994 doses wrongly deferred).
// Instead it is routed to the adult catch-up/primary path at the next compatible drive: a normal
// SCHEDULED obligation due today, which the sweeper CAN batch like any other due work.
func TestKernelStoryR_MissingDOBRoutesToAdultCatchUp(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-r", "Missing DOB, no history: adult catch-up, not a deferred gap",
		"A goat is registered without a date of birth and has never received this vaccine. Generation "+
			"must never defer merely because DOB is unknown (Fix Plan B2/B3) — it materializes a normal "+
			"scheduled catch-up obligation due today, and the sweeper batches it like any other due work.")
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
		"The real generation engine must route the never-received vaccine to the adult catch-up path "+
			"(due today) instead of a fabricated missing-DOB deferral.")
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	asOf := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	res, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatID, asOf)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("one scheduled catch-up obligation, never a deferred gap", res.Generated == 1 && res.Deferred == 0, "deferred=%d generated=%d", res.Deferred, res.Generated)

	status := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goatID)
	story.Assert("obligation is scheduled, not deferred (no DOB is not a clinical defer)", status == "scheduled", "status=%q", status)

	story.Step("Sweeper batches the catch-up obligation",
		"A missing-DOB catch-up is normal due work, not an excluded defer row — SM-4 batches it.")
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-R', 'E2E Story R vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{VaccineItemID: itemID, DosesPerGoat: 1}, asOf.AddDate(0, 0, 30))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("a drive is formed for the catch-up obligation", sweepRes.Batches == 1, "batches=%d", sweepRes.Batches)

	scheduledAfterSweep := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled' AND batch_id IS NOT NULL`, fxTenant, goatID)
	story.Assert("catch-up obligation is batched, not stuck deferred", scheduledAfterSweep == 1, "batched_scheduled=%d", scheduledAfterSweep)
}
