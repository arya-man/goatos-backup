package e2e

import (
	"testing"
	"time"

	calendardomain "github.com/vgoats/goatos/backend/internal/calendar/domain"
	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryAK_DriveClubbingWithinBuffer is the hard vaccination scheduling rule:
// the algorithm must maximize useful drive output inside the authored safe window. It must not
// blindly materialize one micro-drive per animal due date.
func TestKernelStoryAK_DriveClubbingWithinBuffer(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-ak", "Drive clubbing: nearby due animals use one buffered drive",
		"Two kids in the same shed become due one day apart under a seven-day vaccination window. "+
			"Generation must create individual obligations, but SM-4 must plan one larger drive on the "+
			"later day and Calendar must show one two-animal drive, not two micro-drives.")
	defer story.Finish()
	story.Certify("backend kernel + Calendar projection")

	const (
		shedID  = "a1000000-0000-4000-8000-000000000001"
		stageID = "a1000000-0000-4000-8000-000000000002"
		goatA   = "a1000000-0000-4000-8000-000000000010"
		goatB   = "a1000000-0000-4000-8000-000000000011"
	)
	fx.SeedShed(shedID, "E2E-AK", stageID)
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_ak", 21, 7, nil)

	dueA := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	dueB := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	dobA := dueA.AddDate(0, 0, -21)
	dobB := dueB.AddDate(0, 0, -21)
	fx.SeedGoat(GoatSpec{GoatID: goatA, ShedID: shedID, DOB: &dobA})
	fx.SeedGoat(GoatSpec{GoatID: goatB, ShedID: shedID, DOB: &dobB})

	story.Step("Generate individual due obligations",
		"Goat.created events run the production generation handler. The algorithm may keep per-animal "+
			"obligation truth, but it must not turn that into per-animal drive execution.")
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatA, dueA)
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatB, dueB)
	generated := fx.countRows(`
SELECT count(*) FROM obligation_instances
WHERE tenant_id=$1 AND protocol_version_id=$2 AND status='scheduled'`, fxTenant, versionID)
	story.Assert("two animal obligations were generated", generated == 2, "generated=%d", generated)

	story.Step("Sweep on the later due date",
		"The Aug 19 animal is still inside its seven-day window, so SM-4 should delay it by one day "+
			"and club it with the Aug 20 animal for one higher-output shed drive.")
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, nil)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, defaultParkSweepConfig(), dueB)
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("one drive batch was created", sweepRes.Batches == 1, "batches=%d", sweepRes.Batches)
	story.Assert("both obligations were attached", sweepRes.Obligations == 2, "obligations=%d", sweepRes.Obligations)

	batchID := fx.scanText(`
SELECT batch_id::text FROM obligation_batches
WHERE tenant_id=$1 AND protocol_version_id=$2`, fxTenant, versionID)
	expectedPlannedDate := biztime.BusinessDayStart(dueB).Format("2006-01-02")
	plannedDate := fx.scanText(`
SELECT to_char(planned_date, 'YYYY-MM-DD') FROM obligation_batches
WHERE tenant_id=$1 AND batch_id=$2`, fxTenant, batchID)
	story.Assert("the one drive is planned on the later due business day", plannedDate == expectedPlannedDate,
		"planned_date=%s want_storage_date=%s", plannedDate, expectedPlannedDate)
	attached := fx.countRows(`
SELECT count(*)
FROM obligation_instances oi
WHERE oi.tenant_id=$1 AND oi.protocol_version_id=$2 AND oi.batch_id=$3`, fxTenant, versionID, batchID)
	story.Assert("the planned drive contains both animals", attached == 2, "attached=%d", attached)
	held := fx.countRows(`
SELECT count(*) FROM obligation_instances
WHERE tenant_id=$1 AND protocol_version_id=$2 AND target_id=$3 AND batching_hold_count=1`, fxTenant, versionID, goatA)
	story.Assert("the earlier due animal records one intentional batching hold", held == 1, "held=%d", held)

	story.Step("Project to Calendar",
		"Calendar is only a lens over the backend batch. It must show one two-animal drive event, not "+
			"separate per-animal or per-date micro-drive events.")
	events, err := fx.Calendar.ListEvents(fx.Ctx, calendardomain.Query{
		TenantID: fxTenant, OwnerKey: calendardomain.OwnerAll,
		DateFrom: dueA.AddDate(0, 0, -1), DateTo: dueB.AddDate(0, 0, 1),
		Limit: 20, Scope: calendardomain.ScopeFilter{TenantWide: true},
	})
	story.Assert("Calendar read succeeded", err == nil, "err=%v", err)
	story.Assert("Calendar shows one drive", err == nil && len(events.Items) == 1, "items=%d", len(events.Items))
	targetCount := 0
	eventDate := ""
	windowStartDate := ""
	if err == nil && len(events.Items) == 1 {
		targetCount = events.Items[0].TargetCount
		eventDate = biztime.BusinessDate(events.Items[0].DueAt)
		if events.Items[0].WindowStart != nil {
			windowStartDate = biztime.BusinessDate(*events.Items[0].WindowStart)
		}
	}
	story.Assert("Calendar drive target count is two animals", targetCount == 2, "target_count=%d", targetCount)
	expectedEventDate := biztime.BusinessDate(dueB)
	story.Assert("Calendar drive renders on the planned execution date", eventDate == expectedEventDate,
		"event_date=%s want=%s", eventDate, expectedEventDate)
	story.Assert("Calendar does not render the clubbed drive on the earlier due date", eventDate != biztime.BusinessDate(dueA),
		"event_date=%s earlier_due_date=%s", eventDate, biztime.BusinessDate(dueA))
	story.Assert("Calendar drive window starts on the planned execution date", windowStartDate == expectedEventDate,
		"window_start=%s want=%s", windowStartDate, expectedEventDate)
}
