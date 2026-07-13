package e2e

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryY_CrossVaccineGap proves accepted PPR history created by SM-5 affects a later,
// independently published Goat Pox rule through the production generation compatibility policy.
func TestKernelStoryY_CrossVaccineGap(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-y", "Cross-vaccine gap: accepted PPR moves Goat Pox",
		"PPR is generated and accepted through the production kernel. A later Goat Pox protocol is "+
			"published with a live-to-live 28-day policy. SM-1 reads canonical accepted history and moves "+
			"Goat Pox later than its raw birth-age date.")
	story.Certify("backend kernel + SOP proof/submission/review")
	defer story.Finish()

	const (
		shedID  = "ea000000-0000-4000-8000-000000000001"
		stageID = "ea000000-0000-4000-8000-000000000002"
		goatID  = "ea000000-0000-4000-8000-000000000010"
	)
	fx.SeedShed(shedID, "E2E-Y", stageID)
	// One now-relative clock for every instant in this story. Acceptance stamps verified_at = now()
	// (SQL, uncontrollable), so DOB/administration/regeneration must be derived from the same wall
	// clock, not fixed calendar dates: with fixed dates the Goat Pox due window eventually falls into
	// the past, and applyMissedDosePolicy (asOf past businessDayStart(due)+DueWindowDays) would replace
	// the compatibility floor with a catch-up date. Anchoring keeps both pox due windows ~10-14 days in
	// the future on every run. Offsets preserve the original relationships: PPR administered 14d ago
	// (pprAt = dob+116, 4d past its dob+112 raw due, inside the 14d window); Goat Pox raw due = dob+140
	// = anchor+10; compatibility floor = pprAt+28 = anchor+14, which wins and stays future.
	anchor := biztime.BusinessDayStart(time.Now())
	dob := anchor.AddDate(0, 0, -130)
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob})

	pprEligibility := `{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"},"eligibility":{"animal_stage":"K1"}}`
	pprVersionID, pprRules := fx.PublishScheduleProtocol("vaccination.e2e.story_y.ppr", "{}", []RuleSpec{{
		DoseCode: "ppr", Sequence: 1, TriggerType: "birth_age", OffsetDays: 112,
		DueWindowDays: 14, EligibilityJSON: pprEligibility,
	}})

	story.Step("Generate and accept PPR through the kernel",
		"goat.created runs SM-1. SM-5 accepts the resulting obligation (administered 14 days ago) and writes canonical completion history.")
	pprAt := dob.AddDate(0, 0, 116)
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatID, pprAt.AddDate(0, 0, -1))
	pprID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3`, fxTenant, goatID, pprRules["ppr"])
	completeVaccinationObligationThroughSOP(t, fx, pprVersionID, pprID, shedID, []string{goatID}, pprAt, "story-y-ppr")

	poxEligibility := `{"vaccine":{"code":"Goat Pox","type":"live","pathogen_class":"viral"},"eligibility":{"animal_stage":"K1"}}`
	_, poxRules := fx.PublishScheduleProtocol("vaccination.e2e.story_y.pox",
		`{"compatibility_policy":{"live_to_live_gap_days":28}}`, []RuleSpec{{
			DoseCode: "goat_pox", Sequence: 1, TriggerType: "birth_age", OffsetDays: 140,
			DueWindowDays: 14, EligibilityJSON: poxEligibility,
		}})

	story.Step("Publish Goat Pox and regenerate from accepted history",
		"After the accepted completion's verification timestamp, a second goat.created delivery is idempotent for PPR and generates Goat Pox. Compatibility moves the raw birth-age date (dob+140) up to the live-to-live floor, PPR administration + 28 days.")
	// Regenerate strictly AFTER the PPR acceptance's verification instant. Acceptance stamps
	// vaccination_completions.verified_at = now() (wall clock), and the cross-vaccine history query
	// filters verified_at <= as_of; an as_of before that wall-clock verified_at would hide the accepted
	// PPR, so the live-to-live floor would never be applied. time.Now() is after the completion above
	// and, because DOB/pprAt are anchored to now, still comfortably before both Goat Pox due windows
	// (raw dob+140 = anchor+10, floor pprAt+28 = anchor+14), so missed-dose catch-up never triggers.
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatID, time.Now().UTC())
	wantDue := pprAt.AddDate(0, 0, 28)
	gotDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3`, fxTenant, goatID, poxRules["goat_pox"])
	story.Assert("Goat Pox due date honors the live-to-live floor", sameDay(gotDue, wantDue),
		"got=%s want=%s", gotDue.Format("2006-01-02"), wantDue.Format("2006-01-02"))
	rawDue := dob.AddDate(0, 0, 140)
	story.Assert("compatibility floor is later than raw birth-age scheduling", wantDue.After(rawDue),
		"floor=%s raw=%s", wantDue.Format("2006-01-02"), rawDue.Format("2006-01-02"))
	story.Assert("PPR completion remains canonical history",
		fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, pprID) == "completed",
		"status=%s", fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, pprID))
}
