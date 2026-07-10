package e2e

import (
	"testing"
	"time"

	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryK_ProcurementWarmup drives the procurement warm-up → intake → herd-schedule path
// through the REAL generation engine. Per docs/preventive-care-vaccination/vaccination-rules.md, an
// animal that arrives from a supplier/holding-farm gets a 7-day warm-up hold anchored to its
// farm-entry date: no vaccination during the hold, then the schedule resumes. This story publishes a
// post-arrival rule plus a 7-day warm-up procurement policy and shows:
//   - during warm-up, the on-arrival dose is generated but HELD (deferred, reason=warm-up),
//   - after the 7-day cool-off, the same dose is released to scheduled, due 7 days after farm-entry
//     (anchored to entry, honoring the warm-up floor, not the raw offset-0 arrival day).
//
// Uses the genuine GenerationService (post_arrival trigger + procurement_policy warm-up floor), not a
// reimplemented schedule.
func TestKernelStoryK_ProcurementWarmup(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-k", "Procurement warm-up → intake → herd schedule",
		"A goat arrives from a supplier holding farm on a known farm-entry date. The kernel applies a 7-day "+
			"warm-up hold anchored to farm entry: during the hold the on-arrival vaccination dose is generated "+
			"but held (deferred, warm-up reason), so nobody vaccinates a still-settling animal. After the 7-day "+
			"cool-off the same dose is released onto the herd schedule, due 7 days after farm entry.")
	defer story.Finish()

	const shedID = "eb000000-0000-4000-8000-000000000001"
	const stageID = "eb000000-0000-4000-8000-000000000002"
	fx.SeedAdultShed(shedID, "E2E-K", stageID, "A1")

	const goatID = "eb000000-0000-4000-8000-000000000010"
	entry := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	dob := entry.AddDate(-2, 0, 0)
	fx.SeedProcurementGoat(goatID, shedID, entry, "A1", dob)

	story.Step("Seed a procured goat and publish a post-arrival rule with a 7-day warm-up hold",
		"One adult goat arrived via procurement on 2026-06-01 (origin procured, farm-entry 2026-06-01). "+
			"The published protocol carries one post-arrival on-arrival dose (offset 0) plus a procurement "+
			"policy of a 7-day no-vaccination warm-up hold from farm entry.")

	_, ruleIDs := fx.PublishScheduleProtocol("vaccination.e2e.story_k",
		`{"procurement_policy":{"warmup_no_vaccination_days":7}}`,
		[]RuleSpec{{DoseCode: "on_arrival_ettt_ppr", Sequence: 1, TriggerType: "post_arrival", OffsetDays: 0, DueWindowDays: 14}})
	ruleID := ruleIDs["on_arrival_ettt_ppr"]

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	wantDue := entry.AddDate(0, 0, 7) // warm-up floor: 7 days from farm entry

	story.Step("During warm-up (day 3), the on-arrival dose is generated but HELD",
		"Run the real GenerationService.GenerateForGoat 3 days after arrival -- still inside the 7-day "+
			"warm-up. The on-arrival dose must be created but held (deferred, warm-up), and its due date "+
			"must be the warm-up floor: 7 days after farm entry, not arrival day.")
	duringWarmup := entry.AddDate(0, 0, 3)
	res1, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatID, duringWarmup)
	story.Assert("generation ran without error during warm-up", err == nil, "err=%v", err)
	story.Assert("the on-arrival dose was generated (held), not skipped", res1.Generated == 1, "generated=%d", res1.Generated)

	statusDuring := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3`, fxTenant, goatID, ruleID)
	story.Assert("the on-arrival dose is deferred (warm-up hold)", statusDuring == "deferred", "status=%q", statusDuring)
	dueDuring := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3`, fxTenant, goatID, ruleID)
	story.Assert("its due date is the 7-day warm-up floor from farm entry", sameDay(dueDuring, wantDue),
		"got_due=%s want_due=%s", dueDuring.Format("2006-01-02"), wantDue.Format("2006-01-02"))

	story.Step("After the 7-day cool-off, the dose is released to the herd schedule",
		"Re-run the real generation on day 7 (warm-up cleared). The held dose must be reopened to "+
			"scheduled -- the animal has cleared intake and entered the standard herd schedule -- still due "+
			"7 days after farm entry.")
	afterWarmup := entry.AddDate(0, 0, 7)
	res2, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatID, afterWarmup)
	story.Assert("generation ran without error after warm-up", err == nil, "err=%v", err)
	story.Assert("the held dose was reopened (released), not duplicated", res2.Reopened == 1 && res2.Generated == 0, "reopened=%d generated=%d", res2.Reopened, res2.Generated)

	statusAfter := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3`, fxTenant, goatID, ruleID)
	story.Assert("the on-arrival dose is now scheduled (on the herd schedule)", statusAfter == "scheduled", "status=%q", statusAfter)
	total := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goatID)
	story.Assert("exactly one obligation exists for the goat (no duplicate from re-generation)", total == 1, "count=%d", total)
	dueAfter := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3`, fxTenant, goatID, ruleID)
	story.Assert("released dose is still due 7 days after farm entry", sameDay(dueAfter, wantDue),
		"got_due=%s want_due=%s", dueAfter.Format("2006-01-02"), wantDue.Format("2006-01-02"))
}
