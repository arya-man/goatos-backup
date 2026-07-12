package e2e

import (
	"testing"
	"time"

	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryR_PregnancyAndMilkingDefer drives the two reproductive-state dynamic defers
// documented in docs/preventive-care-vaccination/vaccination-rules.md ("vaccines must not be
// scheduled in pregnancy months 4 and 5" / "do not vaccinate milking-department animals during
// milking") through the REAL generation engine's pregnancyDeferReason and milkingDeferReason
// (backend/internal/vaccination/app/schedule_policy.go) -- no prior story exercised either path.
//
// Two independent goats, one published version carrying a pregnancy_policy
// (skip_from_pregnancy_month=4, skip_through_pregnancy_month=5):
//   - G-Pregnant: reproductive_status='pregnant' with a breeding_date landing in month 4 defers
//     ("late_pregnancy_hold"); once the pregnancy month passes the configured window (month 6, past
//     skip_through), the next recheck resumes normal scheduling on its own -- pregnancy reuses the
//     exact same defer/reopen machinery health recovery uses, not a separate code path.
//   - G-Milking: reproductive_status='milking' defers ("milking_window_hold") regardless of the
//     pregnancy policy; leaving the milking state resumes scheduling.
func TestKernelStoryR_PregnancyAndMilkingDefer(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-r", "Dynamic defer: pregnancy month 4-5 hold and milking-window hold",
		"Two goats hit the two reproductive-state defers. G-Pregnant enters pregnancy month 4 (inside "+
			"the configured months 4-5 hold) -- the recheck must defer its open dose with reason "+
			"'late_pregnancy_hold'; once the pregnancy month passes the hold window, scheduling resumes "+
			"on its own. G-Milking enters the milking reproductive state -- the recheck must defer its "+
			"open dose with reason 'milking_window_hold', regardless of the pregnancy policy; leaving "+
			"milking resumes scheduling.")
	defer story.Finish()
	story.Certify("backend kernel")

	const pregnancyRuleDSL = `{"pregnancy_policy":{"skip_from_pregnancy_month":4,"skip_through_pregnancy_month":5}}`
	versionID, ruleIDs := fx.PublishScheduleProtocol("vaccination.e2e.story_r", pregnancyRuleDSL,
		[]RuleSpec{{DoseCode: "primary", Sequence: 1, TriggerType: "birth_age", OffsetDays: 21, DueWindowDays: 0}})
	primaryRuleID := ruleIDs["primary"]
	_ = primaryRuleID

	const shedID = "e5000000-0000-4000-8000-000000000001"
	const stageID = "e5000000-0000-4000-8000-00000000000a"
	fx.SeedShed(shedID, "E2E-R", stageID)

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)

	// ---- G-Pregnant: pregnancy month 4-5 hold, then resume once past the window. ----
	const goatPregnant = "e5000000-0000-4000-8000-000000000010"
	dobPregnant := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatPregnant, ShedID: shedID, DOB: &dobPregnant})

	story.Step("G-Pregnant: generate the primary dose before pregnancy is recorded",
		"Generate obligations for G-Pregnant as of 2025-10-22 (21 days after birth). One scheduled "+
			"obligation, reproductive_status not yet pregnant.")
	genAsOf := dobPregnant.AddDate(0, 0, 21)
	pregGen1, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatPregnant, genAsOf)
	story.Assert("G-Pregnant generation ran without error", err == nil, "err=%v", err)
	story.Assert("G-Pregnant: exactly one obligation was generated", pregGen1.Generated == 1, "generated=%d", pregGen1.Generated)

	pregOblID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3`,
		fxTenant, goatPregnant, versionID)
	pregOriginalDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, pregOblID)

	story.Step("G-Pregnant enters pregnancy month 4; the recheck defers with late_pregnancy_hold",
		"Set reproductive_status='pregnant' with a breeding_date exactly 100 days before the recheck "+
			"date -- pregnancyMonth(breeding_date, asOf) computes to month 4, inside the configured "+
			"skip_from=4/skip_through=5 window. The recheck must defer the open dose.")
	checkAsOf1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	breedingDate := checkAsOf1.AddDate(0, 0, -100) // wholeDaysBetween = 100 -> month = 100/30 + 1 = 4
	fx.ChangeGoatReproductive(goatPregnant, "pregnant", "story-r-pregnant", checkAsOf1, &breedingDate)

	pregDeferredStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, pregOblID)
	story.Assert("G-Pregnant's obligation is now deferred", pregDeferredStatus == "deferred", "status=%q", pregDeferredStatus)
	pregDeferReason := fx.scanText(`SELECT payload->>'defer_status' FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='deferred'`, fxTenant, pregOblID)
	story.Assert("the durable defer event records defer_status='late_pregnancy_hold'", pregDeferReason == "late_pregnancy_hold", "defer_status=%q", pregDeferReason)

	story.Step("G-Pregnant's pregnancy month passes the hold window; recovery-repair resumes scheduling",
		"Recheck 51 days later (breeding_date + 151 days => pregnancy month 6, past skip_through=5). "+
			"With no more hold in force, the recovery-repair recheck must reopen the dose and realign "+
			"due_at forward -- pregnancy resuming reuses the exact same reopen path as a health recovery.")
	checkAsOf2 := breedingDate.AddDate(0, 0, 151) // month = 151/30 + 1 = 6, past skip_through
	pregGen3, err := gen.GenerateRecoveryRepairForGoat(fx.Ctx, fxTenant, goatPregnant, checkAsOf2)
	story.Assert("G-Pregnant recovery recheck ran without error", err == nil, "err=%v", err)
	story.Assert("G-Pregnant recovery recheck reopened the obligation", pregGen3.Reopened == 1, "reopened=%d", pregGen3.Reopened)
	pregRecoveredStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, pregOblID)
	story.Assert("G-Pregnant's obligation is scheduled again", pregRecoveredStatus == "scheduled", "status=%q", pregRecoveredStatus)
	pregRecoveredDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, pregOblID)
	story.Assert("G-Pregnant's due date was realigned forward, not left stale",
		pregRecoveredDue.After(pregOriginalDue), "original_due=%s recovered_due=%s", pregOriginalDue.Format("2006-01-02"), pregRecoveredDue.Format("2006-01-02"))

	// ---- G-Milking: milking-window hold, independent of the pregnancy policy. ----
	const goatMilking = "e5000000-0000-4000-8000-000000000020"
	dobMilking := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatMilking, ShedID: shedID, DOB: &dobMilking})

	story.Step("G-Milking: generate the primary dose before entering the milking state",
		"Generate obligations for G-Milking as of 2025-10-22, same rule. One scheduled obligation, "+
			"reproductive_status not yet milking.")
	milkGen1, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatMilking, genAsOf)
	story.Assert("G-Milking generation ran without error", err == nil, "err=%v", err)
	story.Assert("G-Milking: exactly one obligation was generated", milkGen1.Generated == 1, "generated=%d", milkGen1.Generated)

	milkOblID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3`,
		fxTenant, goatMilking, versionID)
	milkOriginalDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, milkOblID)

	story.Step("G-Milking enters the milking reproductive state; the recheck defers with milking_window_hold",
		"Set reproductive_status='milking'. The recheck must defer the open dose with reason "+
			"'milking_window_hold' -- this fires independently of the pregnancy_policy configured on "+
			"this same version, per vaccination-rules.md's 'do not vaccinate milking-department animals "+
			"during milking'.")
	milkCheckAsOf1 := genAsOf.AddDate(0, 0, 40)
	fx.ChangeGoatReproductive(goatMilking, "milking", "story-r-milking", milkCheckAsOf1, nil)

	milkDeferredStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, milkOblID)
	story.Assert("G-Milking's obligation is now deferred", milkDeferredStatus == "deferred", "status=%q", milkDeferredStatus)
	milkDeferReason := fx.scanText(`SELECT payload->>'defer_status' FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='deferred'`, fxTenant, milkOblID)
	story.Assert("the durable defer event records defer_status='milking_window_hold'", milkDeferReason == "milking_window_hold", "defer_status=%q", milkDeferReason)

	story.Step("G-Milking leaves the milking state; recovery-repair resumes scheduling",
		"Set reproductive_status to the governed non_pregnant value through identity. The emitted recheck must reopen "+
			"the dose and realign due_at forward.")
	milkCheckAsOf2 := milkCheckAsOf1.AddDate(0, 0, 5)
	fx.ChangeGoatReproductive(goatMilking, "non_pregnant", "story-r-nonpregnant", milkCheckAsOf2, nil)
	milkRecoveredStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, milkOblID)
	story.Assert("G-Milking's obligation is scheduled again", milkRecoveredStatus == "scheduled", "status=%q", milkRecoveredStatus)
	milkRecoveredDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, milkOblID)
	story.Assert("G-Milking's due date was realigned forward, not left stale",
		milkRecoveredDue.After(milkOriginalDue), "original_due=%s recovered_due=%s", milkOriginalDue.Format("2006-01-02"), milkRecoveredDue.Format("2006-01-02"))
}
