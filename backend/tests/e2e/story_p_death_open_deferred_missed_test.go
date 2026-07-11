package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryP_DeathCancelsScheduledDeferredMissed builds every status through production
// transitions before proving goat.exited cancels all still-open variants and preserves completion.
func TestKernelStoryP_DeathCancelsScheduledDeferredMissed(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-p-exit-statuses", "Exit cancels generated scheduled, deferred, and missed work",
		"SM-1 generates a four-rule course. SM-5 completes one dose, MarkMissed closes one window, a "+
			"production health-change recheck defers one rule, and SM-4 batches open work. A production sold "+
			"exit then cancels every remaining open status while preserving accepted history.")
	defer story.Finish()

	const (
		shedID    = "eb000000-0000-4000-8000-000000000001"
		stageID   = "eb000000-0000-4000-8000-000000000002"
		goatDies  = "eb000000-0000-4000-8000-000000000010"
		goatLives = "eb000000-0000-4000-8000-000000000011"
	)
	fx.SeedShed(shedID, "E2E-P-EXIT", stageID)
	versionID, ruleIDs := fx.PublishScheduleProtocol("vaccination.e2e.story_p_exit", "{}", []RuleSpec{
		{DoseCode: "missed", Sequence: 1, TriggerType: "birth_age", OffsetDays: 0, DueWindowDays: 14},
		{DoseCode: "completed", Sequence: 2, TriggerType: "birth_age", OffsetDays: 10, DueWindowDays: 14},
		{DoseCode: "deferred", Sequence: 3, TriggerType: "birth_age", OffsetDays: 100, DueWindowDays: 14, EligibilityJSON: `{"defer_states":["sick"]}`},
		{DoseCode: "scheduled", Sequence: 4, TriggerType: "birth_age", OffsetDays: 110, DueWindowDays: 14},
	})
	dob := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatDies, ShedID: shedID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: goatLives, ShedID: shedID, DOB: &dob})

	story.Step("Generate both four-rule courses through goat.created",
		"No obligation rows are seeded: the production event consumer materializes every authored rule.")
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatDies, dob)
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatLives, dob)
	story.Assert("dying goat has four generated obligations",
		fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goatDies) == 4,
		"count=%d", fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goatDies))

	story.Step("Create completed and missed states through their owners",
		"SM-5 accepts the completed rule; MarkMissed advances only the oldest window.")
	completedID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3`, fxTenant, goatDies, ruleIDs["completed"])
	fx.AcceptObligation(completedID, goatDies, "story-p-completed", dob.AddDate(0, 0, 11))
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, nil)
	marked, err := sweeper.MarkMissed(fx.Ctx, fxTenant, dob.AddDate(0, 0, 20))
	story.Assert("only the oldest generated rule became missed", err == nil && marked == 2, "marked=%d err=%v", marked, err)
	story.Step("Batch open work through SM-4",
		"While the goat is still healthy, the sweeper attaches eligible scheduled/missed work to real planned batches.")
	_, err = sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, defaultParkSweepConfig(), dob.AddDate(0, 0, 111))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	batched := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND batch_id IS NOT NULL`, fxTenant, goatDies)
	story.Assert("at least one open row was batched by SM-4", batched > 0, "batched=%d", batched)

	story.Step("Defer the authored sick rule through identity and recheck",
		"The production health command emits goat.health.changed. Generation recheck moves the matching open rule to deferred and safely detaches it from a planned batch.")
	fx.ChangeGoatHealth(goatDies, "sick", "story-p-sick", dob.AddDate(0, 0, 111))
	deferredStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3`, fxTenant, goatDies, ruleIDs["deferred"])
	story.Assert("sick-authored rule was deferred by recheck", deferredStatus == "deferred", "status=%q", deferredStatus)

	story.Step("Exit through identity and SM-3",
		"A sold exit is validated and committed by identity, emitted through outbox, and consumed by the production cancellation handler.")
	exitAt := dob.AddDate(0, 0, 111)
	fx.ExitGoat(goatDies, "sold", "story-p-sold", exitAt)
	canceled := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='canceled'`, fxTenant, goatDies)
	story.Assert("all three non-completed rows are canceled", canceled == 3, "canceled=%d", canceled)
	story.Assert("accepted history remains completed",
		fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, completedID) == "completed",
		"status=%s", fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, completedID))
	bystanderCanceled := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='canceled'`, fxTenant, goatLives)
	story.Assert("living bystander has no canceled obligations", bystanderCanceled == 0, "canceled=%d", bystanderCanceled)
}
