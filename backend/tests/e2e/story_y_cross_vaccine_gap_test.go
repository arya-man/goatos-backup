package e2e

import (
	"testing"
	"time"

	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryY_CrossVaccineGap drives scenario 12: after PPR is accepted, Goat Pox scheduling
// must honor the live-to-live cross-vaccine gap floor (28 days), not only the raw birth-age offset.
func TestKernelStoryY_CrossVaccineGap(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-y", "Cross-vaccine gap: PPR then Goat Pox floor",
		"A kid already received PPR. When Goat Pox is generated, the compatibility policy must push "+
			"the due date to at least 28 days after PPR — even when the raw birth-age offset would be sooner.")
	defer story.Finish()

	const (
		shedID = "ea000000-0000-4000-8000-000000000001"
		stageID = "ea000000-0000-4000-8000-000000000002"
		goatID = "ea000000-0000-4000-8000-000000000010"
	)

	fx.SeedShed(shedID, "E2E-Y", stageID)
	dob := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	fx.exec("kid goat",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, health_status, species, custodian_party_id, sex,
		    current_location_id, park_id, shed_id, management_stage, dob, origin_type)
		 VALUES ($1, $2, 'alive', 'healthy', 'goat', $3, 'female', $4, $5, $4, 'K1', $6::date, 'birth')`,
		goatID, fxTenant, fxParty, shedID, fxPark, dob)

	pprEligibility := `{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"},"eligibility":{"animal_stage":"K1"}}`
	poxEligibility := `{"vaccine":{"code":"Goat Pox","type":"live","pathogen_class":"viral"},"eligibility":{"animal_stage":"K1"}}`
	versionID, ruleIDs := fx.PublishScheduleProtocol("vaccination.e2e.story_y",
		`{"compatibility_policy":{"live_to_live_gap_days":28}}`,
		[]RuleSpec{
			{DoseCode: "ppr", Sequence: 1, TriggerType: "birth_age", OffsetDays: 112, DueWindowDays: 14, EligibilityJSON: pprEligibility},
			{DoseCode: "goat_pox", Sequence: 2, TriggerType: "birth_age", OffsetDays: 140, DueWindowDays: 14, EligibilityJSON: poxEligibility},
		})

	story.Step("Seed accepted PPR history late in the kid course",
		"PPR was given on 2026-06-25 — after the raw 140-day pox offset would allow scheduling without a gap floor.")
	pprAt := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	fx.SeedAcceptedCompletion(versionID, ruleIDs["ppr"], goatID, "story-y-ppr", pprAt)

	story.Step("Generate Goat Pox with cross-vaccine gap enforcement",
		"The real generation engine must schedule pox at PPR+28d (2026-07-23), not the raw DOB+140d date (2026-07-19).")
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	asOf := pprAt.AddDate(0, 0, 5)
	res, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatID, asOf)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("PPR suppressed, pox generated once", res.SuppressedByTrustedHistory == 1 && res.Generated == 1,
		"suppressed=%d generated=%d", res.SuppressedByTrustedHistory, res.Generated)

	wantDue := pprAt.AddDate(0, 0, 28)
	gotDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3`,
		fxTenant, goatID, ruleIDs["goat_pox"])
	story.Assert("goat pox due at live-to-live gap floor after PPR", sameDay(gotDue, wantDue),
		"got_due=%s want_due=%s", gotDue.Format("2006-01-02"), wantDue.Format("2006-01-02"))

	rawBirthAgeDue := dob.AddDate(0, 0, 140)
	story.Assert("gap floor is later than raw birth-age offset in this scenario", wantDue.After(rawBirthAgeDue),
		"gap_due=%s raw_due=%s", wantDue.Format("2006-01-02"), rawBirthAgeDue.Format("2006-01-02"))
}
