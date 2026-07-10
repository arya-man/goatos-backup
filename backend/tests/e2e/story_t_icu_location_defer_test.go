package e2e

import (
	"testing"
	"time"

	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryT_ICULocationDefer drives scenario 3: a goat in an ICU shed location is held
// (icu defer) and never batched into a vaccination drive while in ICU.
func TestKernelStoryT_ICULocationDefer(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-t", "ICU location defer",
		"A kid lives in an ICU-marked shed. Generation must defer its dose for ICU/clinical hold — "+
			"the animal is not eligible for shed drive batching until it leaves ICU care.")
	defer story.Finish()

	const (
		shedID  = "f5000000-0000-4000-8000-000000000001"
		stageID = "f5000000-0000-4000-8000-000000000002"
		goatID  = "f5000000-0000-4000-8000-000000000010"
	)

	fx.SeedICUShed(shedID, "E2E-T-ICU", stageID)
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_t", 21, 14, []string{"icu", "quarantine", "sick"})

	dob := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob, OriginType: "birth"})

	story.Step("Generate for goat in ICU shed",
		"Health is healthy but location is ICU — the dose must defer, not schedule for a drive.")
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	asOf := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	res, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatID, asOf)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("dose generated but held for ICU", res.Generated == 1 && res.Deferred == 1, "generated=%d deferred=%d", res.Generated, res.Deferred)

	status := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goatID)
	story.Assert("obligation is deferred", status == "deferred", "status=%q", status)

	batched := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND batch_id IS NOT NULL`, fxTenant, goatID)
	story.Assert("ICU-held goat is not on any batch", batched == 0, "batched=%d", batched)
	_ = versionID
}
