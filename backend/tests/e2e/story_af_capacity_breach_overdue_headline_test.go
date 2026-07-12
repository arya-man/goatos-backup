package e2e

import (
	"testing"
	"time"

	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccexecdomain "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// TestKernelStoryAF_CapacityBreachKeepsOverdueHeadline proves the rare collision explicitly:
// a shed can be in capacity_breach on the capacity axis while still showing Overdue as the merged
// CEO status headline when any animal is late. Needs review remains discoverable through the
// capacity filter, but it no longer hides late animals in the Status filter.
func TestKernelStoryAF_CapacityBreachKeepsOverdueHeadline(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-af", "Capacity breach keeps Overdue as the shed headline",
		"A shed with more open vaccination cells than the safe window can carry is still an overdue "+
			"shed when any animal is late. The merged Status chip must show Overdue; the separate "+
			"Capacity chip must still show Needs review so managers can find the capacity breach.")
	defer story.Finish()
	story.Certify("backend kernel")

	fx.exec("tight capacity for collision proof",
		`UPDATE vaccination_capacity_config
		   SET max_per_day = 2, max_buffer_days = 1, capacity_scope = 'tenant',
		       overflow_policy = 'split_within_safe_window_then_mark_needs_review',
		       row_version = row_version + 1, updated_at = now()
		 WHERE tenant_id = $1`, fxTenant)

	const (
		shedID  = "af000000-0000-4000-8000-000000000001"
		stageID = "af000000-0000-4000-8000-00000000000a"
		goatA   = "af000000-0000-4000-8000-000000000010"
		goatB   = "af000000-0000-4000-8000-000000000011"
		goatC   = "af000000-0000-4000-8000-000000000012"
	)
	fx.SeedShed(shedID, "E2E-AF", stageID)

	versionFMD, _ := fx.PublishScheduleProtocol("vaccination.e2e.story_af.fmd", "{}",
		[]RuleSpec{{DoseCode: "FMD", Sequence: 1, TriggerType: "birth_age", OffsetDays: 0, DueWindowDays: 7}})
	versionHS, _ := fx.PublishScheduleProtocol("vaccination.e2e.story_af.hs", "{}",
		[]RuleSpec{{DoseCode: "HS", Sequence: 1, TriggerType: "birth_age", OffsetDays: 0, DueWindowDays: 7}})

	dueDay := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatA, ShedID: shedID, DOB: &dueDay})
	fx.SeedGoat(GoatSpec{GoatID: goatB, ShedID: shedID, DOB: &dueDay})
	fx.SeedGoat(GoatSpec{GoatID: goatC, ShedID: shedID, DOB: &dueDay})

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	story.Step("Generate six due vaccination cells",
		"Three goats each need FMD and HS. With the tenant cap tightened to 2/day and a 1-day buffer, "+
			"six open cells require three sessions, which exceeds the two-day safe window.")
	fmdRes, errF := gen.GenerateForVersion(fx.Ctx, fxTenant, versionFMD, dueDay)
	hsRes, errH := gen.GenerateForVersion(fx.Ctx, fxTenant, versionHS, dueDay)
	story.Assert("both generation passes succeeded", errF == nil && errH == nil, "errF=%v errH=%v", errF, errH)
	story.Assert("six open cells generated", fmdRes.Generated+hsRes.Generated == 6, "fmd=%d hs=%d", fmdRes.Generated, hsRes.Generated)

	lateAsOf := dueDay.AddDate(0, 0, 1)
	story.Step("Read shed summary after the due day",
		"On the next business day at least one animal is late. The capacity axis must remain "+
			"capacity_breach, but the merged Status headline must be overdue.")
	row := shedRow(fx, story, shedID, lateAsOf)
	story.Assert("open_cells still counts all six due vaccination cells", row.OpenCells == 6, "open_cells=%d", row.OpenCells)
	story.Assert("six cells at 2/day need three sessions", row.Sessions == 3, "sessions=%d", row.Sessions)
	story.Assert("capacity axis is Needs review", row.Capacity == vaccexecdomain.CapacityBreach, "capacity=%q", row.Capacity)
	story.Assert("merged shed status is Overdue", row.Status == vaccexecdomain.ShedStatusOverdue, "status=%q", row.Status)

	story.Step("Filter axes stay independent",
		"Status filtering follows the merged headline, while capacity filtering follows the capacity "+
			"machine state. This keeps overdue visible without losing the Needs review capacity queue.")
	overdueStatus := vaccexecdomain.ShedStatusOverdue
	overdueRows, err := fx.VaccExec.ShedSummary(fx.Ctx, vaccexecdomain.ShedSummaryQuery{
		TenantID: fxTenant, ShedID: ptrString(shedID), AsOf: lateAsOf, Status: &overdueStatus,
	})
	story.Assert("status=overdue includes the collision shed", err == nil && len(overdueRows) == 1, "err=%v rows=%d", err, len(overdueRows))

	needsReviewStatus := vaccexecdomain.ShedStatusNeedsReview
	needsRows, err := fx.VaccExec.ShedSummary(fx.Ctx, vaccexecdomain.ShedSummaryQuery{
		TenantID: fxTenant, ShedID: ptrString(shedID), AsOf: lateAsOf, Status: &needsReviewStatus,
	})
	story.Assert("status=needs_review does not steal the overdue shed", err == nil && len(needsRows) == 0, "err=%v rows=%d", err, len(needsRows))

	breachCapacity := vaccexecdomain.CapacityBreach
	breachRows, err := fx.VaccExec.ShedSummary(fx.Ctx, vaccexecdomain.ShedSummaryQuery{
		TenantID: fxTenant, ShedID: ptrString(shedID), AsOf: lateAsOf, Capacity: &breachCapacity,
	})
	story.Assert("capacity=capacity_breach still includes the shed", err == nil && len(breachRows) == 1, "err=%v rows=%d", err, len(breachRows))
}

func ptrString(s string) *string {
	return &s
}
