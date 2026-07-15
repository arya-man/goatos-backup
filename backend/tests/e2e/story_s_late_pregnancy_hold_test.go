package e2e

import (
	"testing"
	"time"

	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryS_LatePregnancyHold drives scenario 5: pregnant goats in months 4–5 are held
// (late_pregnancy_hold) and excluded from drive batching until the window passes.
func TestKernelStoryS_LatePregnancyHold(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-s", "Late pregnancy hold (months 4–5)",
		"A pregnant doe in her late-pregnancy window (months 4–5) must have doses held with "+
			"late_pregnancy_hold — not scheduled into an active shed drive.")
	defer story.Finish()
	story.Certify("backend kernel")

	const (
		shedID  = "f4000000-0000-4000-8000-000000000001"
		stageID = "f4000000-0000-4000-8000-000000000002"
		goatID  = "f4000000-0000-4000-8000-000000000010"
	)

	fx.SeedShed(shedID, "E2E-S", stageID)
	pregnancyDSL := `{"pregnancy_policy":{"allow_until_pregnancy_month":3,"skip_from_pregnancy_month":4,"skip_through_pregnancy_month":5,"post_delivery_catch_up_days":14}}`
	// A pregnant doe is an ADULT, so her vaccination obligation comes from the adult (post_arrival)
	// path, not a kid (birth_age) dose — a 2.5-year-old must never be routed to kid vaccines
	// (VACC-RULE-02). The late-pregnancy hold applies to that adult obligation.
	_, ruleIDs := fx.PublishScheduleProtocol("vaccination.e2e.story_s", pregnancyDSL,
		[]RuleSpec{{DoseCode: "primary", Sequence: 1, TriggerType: "post_arrival", OffsetDays: 21, DueWindowDays: 14}})
	ruleID := ruleIDs["primary"]

	dob := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	entry := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	breeding := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC) // month ~5 at asOf below
	asOf := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{
		GoatID: goatID, ShedID: shedID, DOB: &dob, EntryDate: &entry, Stage: "adult",
		ReproductiveStatus: "pregnant", BreedingDate: &breeding, OriginType: "procured",
	})

	story.Step("Generate during late pregnancy",
		"Breeding date puts the doe in pregnancy month 5 — generation must defer with late_pregnancy_hold.")
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	res, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatID, asOf)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("dose was generated but held", res.Generated == 1 && res.Deferred == 1, "generated=%d deferred=%d", res.Generated, res.Deferred)

	status := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3`, fxTenant, goatID, ruleID)
	story.Assert("obligation is deferred (late pregnancy)", status == "deferred", "status=%q", status)

	deferredEvents := fx.countRows(`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id IN (
		SELECT obligation_id FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2)`, fxTenant, goatID)
	story.Assert("durable defer audit recorded", deferredEvents >= 1, "events=%d", deferredEvents)
}
