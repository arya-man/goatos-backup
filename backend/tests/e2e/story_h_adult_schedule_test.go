package e2e

import (
	"fmt"
	"testing"
	"time"

	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryH_AdultSchedule drives the adult procurement course through the REAL generation
// engine: an adult goat that arrived on a known farm-entry date, a published version carrying the
// two post-arrival rows of the adult course (D0 and D0+4w, per
// docs/preventive-care-vaccination/vaccination-rules.md's "New-animal procurement schedule" -- ET+TT
// + PPR first, then Goat Pox + ET+TT booster after 4 weeks, the 4-week wait honoring live→live
// spacing), and asserts both doses are generated at the right dates with the 28-day gap between them.
//
// origin_type='procured' + a farm entry_date put the goat on the adult_procurement schedule path, so
// the post_arrival rows fire (ruleMatchesSchedulePath only fires post_arrival rows for the
// adult_procurement path). No warm-up policy is authored here, so D0 anchors on the entry date -- the
// procurement warm-up hold is exercised separately in Story K.
func TestKernelStoryH_AdultSchedule(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-h", "Adult schedule: D0 + 4-week course",
		"An adult goat arrives on a known farm-entry date. The published adult course carries two "+
			"post-arrival rows: the D0 dose (ET+TT + PPR) at arrival and the D0+4w dose (Goat Pox + ET+TT "+
			"booster) four weeks later -- the 4-week gap honoring the live→live spacing rule. The generation "+
			"engine must materialize both doses at the correct dates.")
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

	story.Step("Seed the adult and publish the D0 + 4w course",
		"One adult goat, arrived 2026-06-01 (origin procured, adult stage). The published protocol "+
			"carries two post-arrival rows: D0 (offset 0) and D0+4w (offset 28), each with a 14-day window.")

	type dose struct {
		code   string
		seq    int32
		offset int32
	}
	course := []dose{
		{"d0_ettt_ppr", 1, 0},
		{"d28_goatpox_booster", 2, 28},
	}
	specs := make([]RuleSpec, 0, len(course))
	for _, d := range course {
		specs = append(specs, RuleSpec{DoseCode: d.code, Sequence: d.seq, TriggerType: "post_arrival", OffsetDays: d.offset, DueWindowDays: 14})
	}
	versionID, ruleIDs := fx.PublishScheduleProtocol("vaccination.e2e.story_h", "{}", specs)

	story.Step("Generate the adult course via the real generation engine",
		"Run the real GenerationService.GenerateForVersion as of arrival. It must produce exactly two "+
			"scheduled obligations -- one per post-arrival row.")
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	asOf := entry
	res, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionID, asOf)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("both adult doses were generated", res.Generated == 2, "generated=%d", res.Generated)

	total := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`, fxTenant, adultID)
	story.Assert("exactly two scheduled obligations for the adult", total == 2, "count=%d", total)

	story.Step("Assert the two doses and the 4-week gap",
		"D0 must be due on the farm-entry date; D0+4w must be due 28 days later. The gap between the "+
			"two administered doses must be exactly 4 weeks, honoring the live→live spacing rule.")
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
	gapDays := int(dueByCode["d28_goatpox_booster"].Sub(dueByCode["d0_ettt_ppr"]).Hours() / 24)
	story.Assert("the two adult doses are exactly 4 weeks (28 days) apart", gapDays == 28, "gap_days=%d", gapDays)
}
