package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryI_DeathAutoCancel proves the full production path: goat.created generates a
// four-dose course, SM-5 accepts the completed historical dose, the identity critical-death command
// emits goat.exited, and SM-3 cancels only the dead goat's remaining open work.
func TestKernelStoryI_DeathAutoCancel(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-i", "Death mid-course: future doses auto-cancelled",
		"A goat is partway through a real generated vaccination course with one accepted historical dose "+
			"and three future doses. The production identity death command emits goat.exited; SM-3 cancels "+
			"only future work, preserves history, and is idempotent under event replay.")
	story.Certify("backend kernel + SOP proof/submission/review + durable exit consumer")
	defer story.Finish()

	versionID, ruleIDs := fx.PublishScheduleProtocol("vaccination.e2e.story_i", "{}", []RuleSpec{
		{DoseCode: "dose_1", Sequence: 1, TriggerType: "birth_age", OffsetDays: 0, DueWindowDays: 14},
		{DoseCode: "dose_2", Sequence: 2, TriggerType: "birth_age", OffsetDays: 56, DueWindowDays: 14},
		{DoseCode: "dose_3", Sequence: 3, TriggerType: "birth_age", OffsetDays: 84, DueWindowDays: 14},
		{DoseCode: "dose_4", Sequence: 4, TriggerType: "birth_age", OffsetDays: 112, DueWindowDays: 14},
	})

	const goatDies = "e9000000-0000-4000-8000-000000000001"
	const goatLives = "e9000000-0000-4000-8000-000000000002"
	const shedID = "e9000000-0000-4000-8000-000000000003"
	const stageID = "e9000000-0000-4000-8000-000000000004"
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	dob := base.AddDate(0, 0, -28)
	livesDOB := base
	fx.SeedShed(shedID, "E2E-I", stageID)
	fx.SeedGoat(GoatSpec{GoatID: goatDies, ShedID: shedID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: goatLives, ShedID: shedID, DOB: &livesDOB})

	story.Step("Generate both courses through goat.created",
		"The real event consumer runs SM-1 for both goats. Each receives the four authored obligations; no obligation rows are inserted by the story.")
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatDies, dob)
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatLives, dob)
	story.Assert("both goats received four generated obligations",
		fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id IN ($2,$3)`, fxTenant, goatDies, goatLives) == 8,
		"total=%d", fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id IN ($2,$3)`, fxTenant, goatDies, goatLives))

	story.Step("Accept dose 1 through SM-5",
		"The first dose is recorded and accepted through CompletionService. This creates durable completed history and vaccination.completed outbox state.")
	completedObl := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3`,
		fxTenant, goatDies, ruleIDs["dose_1"])
	completeVaccinationObligationThroughSOP(t, fx, versionID, completedObl, shedID, []string{goatDies}, dob.AddDate(0, 0, 2), "story-i-completed")
	openBefore := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`, fxTenant, goatDies)
	story.Assert("dying goat starts with three open generated doses", openBefore == 3, "open=%d", openBefore)

	story.Step("Exit through the production critical-death command",
		"Identity validates and commits the critical death, emits a durable goat.exited envelope, and the production SM-3 consumer cancels the open vaccination work.")
	exitAt := base.AddDate(0, 0, 10)
	fx.ExitGoat(goatDies, "dead", "story-i-exit", exitAt)
	openAfter := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`, fxTenant, goatDies)
	canceled := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='canceled'`, fxTenant, goatDies)
	story.Assert("no open doses remain for the dead goat", openAfter == 0, "open=%d", openAfter)
	story.Assert("all three future doses were canceled", canceled == 3, "canceled=%d", canceled)

	story.Step("History survives and another goat is untouched",
		"The accepted dose remains completed, while all four open obligations belonging to the living bystander remain scheduled.")
	stillCompleted := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, completedObl)
	bystanderOpen := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`, fxTenant, goatLives)
	story.Assert("completed history remains completed", stillCompleted == "completed", "status=%q", stillCompleted)
	story.Assert("bystander's four obligations are untouched", bystanderOpen == 4, "open=%d", bystanderOpen)

	story.Step("Replay the durable exit envelope",
		"At-least-once delivery replays the same identity outbox event through SM-3 without duplicating cancellation or corrupting history.")
	fx.dispatchIdentityOutbox(goatDies, oblapp.EventGoatExited)
	canceledReplay := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='canceled'`, fxTenant, goatDies)
	story.Assert("replay remains exactly three canceled doses", canceledReplay == 3, "canceled=%d", canceledReplay)
	processedStatus := fx.scanText(`
SELECT p.status
FROM domain_event_processed_events p
JOIN outbox_messages o
  ON o.tenant_id = p.tenant_id
 AND o.payload->>'event_id' = p.event_id
WHERE o.tenant_id=$1 AND o.aggregate_id=$2 AND o.event_type=$3 AND p.subscription_id='direct'
ORDER BY o.created_at DESC
LIMIT 1`, fxTenant, goatDies, oblapp.EventGoatExited)
	story.Assert("consumer replay is deduplicated by a durable processed-event record",
		processedStatus == "processed", "processed_event_status=%q", processedStatus)
}
