package e2e

import (
	"fmt"
	"testing"
	"time"

	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryG_KidDoseBundle drives the full kid dose bundle through the REAL generation
// service: one kid born on a known date, one published protocol version carrying every kid
// birth-age row (4w/7w/12w/16w/20w = 28/49/84/112/140 days from DOB per
// docs/preventive-care-vaccination/vaccination-rules.md's V1 effective-days column), and asserts
// the generation engine materializes exactly one scheduled obligation per row, each due at DOB +
// the row's offset with the row's due window.
//
// The rows model the goat kid schedule: ET+TT #1 (28d, 4w), ET+TT #2 (49d, 7w), FMD/HS (84d, 12w),
// PPR (112d, 16w), Goat Pox (140d, 20w). origin_type='birth' locks the kid schedule path so every
// birth_age row fires (ruleMatchesSchedulePath in schedule_policy.go only fires birth_age rules for
// the kid path). No compatibility policy is authored, so each row schedules at its raw birth-age
// offset -- exactly the source schedule, which is what this story asserts.
func TestKernelStoryG_KidDoseBundle(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-g", "Kid dose bundle: 4w / 7w / 12w / 16w / 20w",
		"A single kid is born on a known date. The published PC vaccination protocol carries the full kid "+
			"schedule -- five birth-age rows at 4, 7, 12, 16, and 20 weeks. The generation engine must "+
			"materialize one scheduled dose per row, each due at the kid's birth date plus the row's "+
			"age offset, with the correct due window. This is the standard kid course every kid inherits.")
	defer story.Finish()

	const shedID = "e7000000-0000-4000-8000-000000000001"
	const stageID = "e7000000-0000-4000-8000-000000000002"
	fx.SeedShed(shedID, "E2E-G", stageID)

	// Kid born 2026-03-01; origin_type='birth' keeps it on the kid schedule path.
	const kidID = "e7000000-0000-4000-8000-000000000010"
	dob := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	fx.exec("kid goat",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, health_status, species, custodian_party_id, sex,
		    current_location_id, park_id, shed_id, management_stage, dob, origin_type)
		 VALUES ($1, $2, 'alive', 'healthy', 'goat', $3, 'female', $4, $5, $4, 'K1', $6::date, 'birth')`,
		kidID, fxTenant, fxParty, shedID, fxPark, dob)

	story.Step("Seed the kid and publish the full kid schedule",
		"One kid born 2026-03-01. The published protocol version carries five birth-age rows at "+
			"28d (ET+TT #1, 4w), 49d (ET+TT #2, 7w), 84d (FMD/HS, 12w), 112d (PPR, 16w), and 140d "+
			"(Goat Pox, 20w), each with a 14-day due window.")

	// Full kid bundle, grounded in vaccination-rules.md V1 effective days.
	type dose struct {
		code   string
		seq    int32
		offset int32
		weeks  int
	}
	bundle := []dose{
		{"ettt_1", 1, 28, 4},
		{"ettt_2", 2, 49, 7},
		{"fmd_hs", 3, 84, 12},
		{"ppr", 4, 112, 16},
		{"goat_pox", 5, 140, 20},
	}
	specs := make([]RuleSpec, 0, len(bundle))
	for _, d := range bundle {
		specs = append(specs, RuleSpec{DoseCode: d.code, Sequence: d.seq, TriggerType: "birth_age", OffsetDays: d.offset, DueWindowDays: 14})
	}
	versionID, ruleIDs := fx.PublishScheduleProtocol("vaccination.e2e.story_g", "{}", specs)

	story.Step("Generate the kid's schedule via the real generation engine",
		"Run the real GenerationService.GenerateForVersion. It must produce exactly five scheduled "+
			"obligations -- one per birth-age row.")
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	asOf := dob.AddDate(0, 0, 21) // early in the kid's life; generation schedules the whole bundle forward
	res, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionID, asOf)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("all five kid doses were generated", res.Generated == 5, "generated=%d", res.Generated)

	total := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`, fxTenant, kidID)
	story.Assert("exactly five scheduled obligations for the kid", total == 5, "count=%d", total)

	story.Step("Assert each dose is due at the correct birth-age offset with its window",
		"For every kid row, the generated obligation must be due at DOB + offset days and carry a "+
			"window_end at due + 14 days -- the exact source schedule, no drift.")
	for _, d := range bundle {
		ruleID := ruleIDs[d.code]
		wantDue := dob.AddDate(0, 0, int(d.offset))
		wantWindowEnd := wantDue.AddDate(0, 0, 14)
		gotDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3`, fxTenant, kidID, ruleID)
		gotWindowEnd := fx.scanTime(`SELECT window_end FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3`, fxTenant, kidID, ruleID)
		story.Assert(
			fmt.Sprintf("%s: due at %s (%dw)", d.code, wantDue.Format("2006-01-02"), d.weeks),
			sameDay(gotDue, wantDue),
			"got_due=%s want_due=%s", gotDue.Format("2006-01-02"), wantDue.Format("2006-01-02"))
		story.Assert(
			fmt.Sprintf("%s: 14-day due window ends %s", d.code, wantWindowEnd.Format("2006-01-02")),
			sameDay(gotWindowEnd, wantWindowEnd),
			"got_window_end=%s want_window_end=%s", gotWindowEnd.Format("2006-01-02"), wantWindowEnd.Format("2006-01-02"))
	}
}
