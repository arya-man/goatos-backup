package e2e

import (
	"fmt"
	"testing"
	"time"

	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryO_BirthTaggingBuffer drives the birth-triggered predefined event through the REAL
// generation engine (the same shape as Story G's kid bundle) and asserts the operational "24-hour
// tagging buffer" invariant documented in docs/architecture/event-driven-kernel.md: a newborn kid
// cannot be RFID-tagged and enrolled until at least 24 hours after birth, so no generated obligation
// may ever be due before DOB + 24h.
//
// Today the generation engine has no explicit due_at >= DOB + 24h floor -- the buffer holds only
// because every kid rule's minimum offset (28 days, ET+TT #1) is far larger than 24 hours. This
// story proves that invariant against the real, currently-published schedule (it does not add a new
// clamp to the kernel), and exercises the edge case of generation running almost immediately after
// birth (asOf = DOB + a few hours, i.e. the tagging/intake moment itself) to show the buffer holds
// even at the earliest possible generation time, not just eventually.
func TestKernelStoryO_BirthTaggingBuffer(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-o", "Birth: full predefined schedule + 24-hour tagging buffer",
		"A kid is born and registered almost immediately (generation runs the same day, a few hours "+
			"after birth -- the intake/tagging moment). The generation engine must materialize the full "+
			"standard kid schedule (4w/7w/12w/16w/20w) from DOB, and every generated dose's due date must "+
			"respect the 24-hour tagging buffer: no dose may ever be due before DOB + 24h, since a kid "+
			"cannot be RFID-tagged and enrolled until a day after birth.")
	defer story.Finish()
	story.Certify("backend kernel")

	const shedID = "ef000000-0000-4000-8000-000000000001"
	const stageID = "ef000000-0000-4000-8000-000000000002"
	fx.SeedShed(shedID, "E2E-O", stageID)

	// Kid born today at 06:00 UTC; origin_type='birth' keeps it on the kid schedule path.
	const kidID = "ef000000-0000-4000-8000-000000000010"
	dob := time.Date(2026, 4, 1, 6, 0, 0, 0, time.UTC)
	fx.exec("kid goat",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, health_status, species, custodian_party_id, sex,
		    current_location_id, park_id, shed_id, management_stage, dob, origin_type)
		 VALUES ($1, $2, 'alive', 'healthy', 'goat', $3, 'female', $4, $5, $4, 'K1', $6::date, 'birth')`,
		kidID, fxTenant, fxParty, shedID, fxPark, dob)

	story.Step("Register the newborn kid and publish the full kid schedule",
		"One kid born 2026-04-01. The published protocol version carries the same five birth-age rows "+
			"as the standard kid bundle: 28d (ET+TT #1, 4w), 49d (ET+TT #2, 7w), 84d (FMD/HS, 12w), 112d "+
			"(PPR, 16w), and 140d (Goat Pox, 20w).")

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
	_, ruleIDs := fx.PublishScheduleProtocol("vaccination.e2e.story_o", "{}", specs)

	story.Step("Generate the schedule at the tagging/intake moment (DOB + a few hours)",
		"Run the real GenerationService.GenerateForGoat at DOB + 3 hours -- as early as generation could "+
			"plausibly run (intake/tagging time), not days later. All five doses must still materialize.")
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	asOf := dob.Add(3 * time.Hour)
	res, err := gen.GenerateForGoat(fx.Ctx, fxTenant, kidID, asOf)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("all five kid doses were generated at intake time", res.Generated == 5, "generated=%d", res.Generated)

	total := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`, fxTenant, kidID)
	story.Assert("exactly five scheduled obligations for the kid", total == 5, "count=%d", total)

	story.Step("Every generated dose respects the 24-hour tagging buffer",
		"For every kid row, the generated due date must be at DOB + the row's offset, and -- the buffer "+
			"invariant -- strictly no earlier than DOB + 24h. The earliest row (ET+TT #1, 28d) clears the "+
			"buffer by 27 full days, proving the floor holds with wide margin under the current schedule.")
	const taggingBuffer = 24 * time.Hour
	var earliestDue time.Time
	for i, d := range bundle {
		ruleID := ruleIDs[d.code]
		wantDue := dob.AddDate(0, 0, int(d.offset))
		gotDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3`, fxTenant, kidID, ruleID)
		story.Assert(
			fmt.Sprintf("%s: due at DOB+%dd (%dw)", d.code, d.offset, d.weeks),
			sameDay(gotDue, wantDue),
			"got_due=%s want_due=%s", gotDue.Format("2006-01-02"), wantDue.Format("2006-01-02"))
		story.Assert(
			fmt.Sprintf("%s: due date is not before DOB + 24h tagging buffer", d.code),
			gotDue.Sub(dob) >= taggingBuffer,
			"due-DOB=%s buffer=%s", gotDue.Sub(dob), taggingBuffer)
		if i == 0 || gotDue.Before(earliestDue) {
			earliestDue = gotDue
		}
	}

	story.Step("The earliest obligation overall is not before DOB + 24h",
		"Across the whole generated schedule, the single earliest due date (the first ET+TT dose) is the "+
			"binding case for the buffer invariant -- if it clears 24h, every later dose clears it too.")
	story.Assert("earliest dose across the full schedule clears the 24h buffer",
		earliestDue.Sub(dob) >= taggingBuffer,
		"earliest_due=%s dob=%s margin=%s", earliestDue.Format(time.RFC3339), dob.Format(time.RFC3339), earliestDue.Sub(dob))
	story.Assert("the buffer is cleared with wide margin (>=27 days), not a near-miss",
		earliestDue.Sub(dob) >= 27*24*time.Hour,
		"margin=%s", earliestDue.Sub(dob))
}
