package e2e

import (
	"fmt"
	"testing"
	"time"

	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryH_AdultSchedule drives the adult procurement course through the REAL generation
// engine: an adult goat that arrived on a known farm-entry date, a published version carrying the
// adult course rows (D0, ET+TT dose 2 at D0+21d, and pox at D0+28d). ET+TT uses the
// 21-day course gap for both kid and adult schedules; pox keeps the 28-day live-to-live spacing after PPR.
//
// origin_type='procured' + a farm entry_date put the goat on the adult_procurement schedule path, so
// the post_arrival rows fire (ruleMatchesSchedulePath only fires post_arrival rows for the
// adult_procurement path). No warm-up policy is authored here, so D0 anchors on the entry date -- the
// procurement warm-up hold is exercised separately in Story K.
func TestKernelStoryH_AdultSchedule(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-h", "Adult schedule: ET+TT dose 2 at 21 days, pox at 28 days",
		"An adult goat arrives on a known farm-entry date. The published adult course carries two "+
			"post-arrival follow-ups after the first dose: ET+TT dose 2 at 21 days and pox at 28 days. "+
			"The generation engine must materialize each dose at the correct date.")
	defer story.Finish()
	story.Certify("backend kernel")

	const shedID = "e8000000-0000-4000-8000-000000000001"
	const stageID = "e8000000-0000-4000-8000-000000000002"
	fx.SeedAdultShed(shedID, "E2E-H", stageID, "A1")

	// Adult arrived 2026-06-01; DOB two years earlier so it is unambiguously past the kid cutoff.
	const adultID = "e8000000-0000-4000-8000-000000000010"
	entry := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	dob := entry.AddDate(-2, 0, 0)
	fx.SeedProcurementGoat(adultID, shedID, entry, "A1", dob)

	story.Step("Seed the adult and publish the D0 + 21d + 28d course",
		"One adult goat, arrived 2026-06-01 (origin procured, adult stage). The published protocol "+
			"carries three post-arrival rows: D0 (offset 0), ET+TT dose 2 (offset 21), and pox "+
			"(offset 28), each with a 14-day window.")

	type dose struct {
		code   string
		seq    int32
		offset int32
	}
	course := []dose{
		{"d0_ettt_ppr", 1, 0},
		{"d21_ettt_booster", 2, 21},
		{"d28_goatpox", 3, 28},
	}
	specs := make([]RuleSpec, 0, len(course))
	for _, d := range course {
		specs = append(specs, RuleSpec{DoseCode: d.code, Sequence: d.seq, TriggerType: "post_arrival", OffsetDays: d.offset, DueWindowDays: 14})
	}
	versionID, ruleIDs := fx.PublishScheduleProtocol("vaccination.e2e.story_h", "{}", specs)

	story.Step("Generate the adult course via the real generation engine",
		"Run the real GenerationService.GenerateForVersion as of arrival. It must produce the entry dose, "+
			"ET+TT dose 2, and the pox dose -- one scheduled obligation per post-arrival row.")
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	asOf := entry
	res, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionID, asOf)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("all adult course doses were generated", res.Generated == 3, "generated=%d", res.Generated)

	total := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`, fxTenant, adultID)
	story.Assert("exactly three scheduled obligations for the adult", total == 3, "count=%d", total)

	story.Step("Assert ET+TT dose 2 and pox dates",
		"D0 must be due on the farm-entry date; ET+TT dose 2 must be due 21 days later; pox must be due "+
			"28 days later, preserving the live-to-live spacing rule.")
	dueByCode := map[string]time.Time{}
	for _, d := range course {
		ruleID := ruleIDs[d.code]
		wantDue := entry.AddDate(0, 0, int(d.offset))
		gotDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3`, fxTenant, adultID, ruleID)
		dueByCode[d.code] = gotDue
		story.Assert(
			fmt.Sprintf("%s: due at %s", d.code, wantDue.Format("2006-01-02")),
			sameDay(gotDue, wantDue),
			"got_due=%s want_due=%s", gotDue.Format("2006-01-02"), wantDue.Format("2006-01-02"))
	}
	etttGapDays := int(dueByCode["d21_ettt_booster"].Sub(dueByCode["d0_ettt_ppr"]).Hours() / 24)
	story.Assert("the adult ET+TT booster is exactly 3 weeks (21 days) after dose 1", etttGapDays == 21, "gap_days=%d", etttGapDays)
	poxGapDays := int(dueByCode["d28_goatpox"].Sub(dueByCode["d0_ettt_ppr"]).Hours() / 24)
	story.Assert("the adult pox dose stays exactly 4 weeks (28 days) after PPR/live dose", poxGapDays == 28, "gap_days=%d", poxGapDays)
}
