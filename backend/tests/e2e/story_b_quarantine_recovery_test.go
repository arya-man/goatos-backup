package e2e

import (
	"testing"
	"time"

	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryB_ClinicalHoldRecovery drives the real SM-2 health-defer/recovery path through
// Identity commands, their durable outbox envelopes, and the production vaccination recheck
// consumer. The recovery consumer invokes the recovery-repair generation mode, which realigns the
// reopened obligation's due date instead of leaving its pre-hold date in place.
func TestKernelStoryB_ClinicalHoldRecovery(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-b", "Clinical defer + health recovery reschedule",
		"A goat's PC vaccination dose is generated on schedule. The goat then becomes sick, and the "+
			"next recheck must hold (defer) its open dose rather than let it slip past due. The goat "+
			"recovers, and the next recheck must reopen the held dose and realign its due date onto the "+
			"recovery-time calendar -- the real SM-2 health-recovery path, not a stand-in.")
	defer story.Finish()
	story.Certify("backend kernel")

	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_b", 21, 0, []string{"sick", "under_treatment", "recovering", "quarantine", "icu"})

	const goatID = "e2000000-0000-4000-8000-0000000000b1"
	const shedID = "e2000000-0000-4000-8000-0000000000b2"
	const stageID = "e2000000-0000-4000-8000-0000000000b3"
	dob := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	fx.SeedShed(shedID, "E2E-B", stageID)
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob})

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)

	story.Step("Generate the goat's primary dose",
		"Generate obligations for this goat as of 2026-06-23 (53 days after birth, 21-day-offset rule): "+
			"one scheduled obligation due 2026-05-22.")
	genAsOf := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	res1, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatID, genAsOf)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("exactly one obligation was generated", res1.Generated == 1, "generated=%d", res1.Generated)

	obligationID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND protocol_version_id=$3`,
		fxTenant, goatID, versionID)
	originalDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, obligationID)
	originalStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, obligationID)
	story.Assert("the generated obligation starts scheduled", originalStatus == "scheduled", "status=%q", originalStatus)

	// --- Goat becomes sick. ---
	story.Step("Goat becomes sick; the identity event recheck defers the open dose",
		"The production identity health command commits sick, emits goat.health.changed, and the registered vaccination recheck consumer holds the open obligation.")
	deferAsOf := genAsOf.AddDate(0, 0, 1)
	fx.ChangeGoatHealth(goatID, "sick", "story-b-sick", deferAsOf)

	deferredStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, obligationID)
	story.Assert("the obligation is now deferred", deferredStatus == "deferred", "status=%q", deferredStatus)
	deferredEvents := fx.countRows(`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='deferred'`, fxTenant, obligationID)
	story.Assert("a durable 'deferred' audit event was recorded", deferredEvents == 1, "events=%d", deferredEvents)

	// --- Goat recovers. ---
	story.Step("Goat recovers; the identity event reopens and realigns the dose",
		"The production healthy transition emits goat.health.changed. Its recovery recheck must call the real ReopenDeferredObligationByIdempotencyKey path, flip "+
			"the obligation back to scheduled, and realign its due date onto the recovery-time calendar "+
			"(SM-2 health recovery) -- not leave it at its stale pre-hold date.")
	recoverAsOf := deferAsOf.AddDate(0, 0, 3)
	fx.ChangeGoatHealth(goatID, "healthy", "story-b-recover", recoverAsOf)

	recoveredStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, obligationID)
	story.Assert("the obligation is scheduled again", recoveredStatus == "scheduled", "status=%q", recoveredStatus)

	recoveredDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, obligationID)
	story.Assert("the due date was realigned onto the recovery-time calendar, not left at the stale pre-hold date",
		!recoveredDue.Equal(originalDue) && recoveredDue.After(originalDue),
		"original_due=%s recovered_due=%s recover_asOf=%s",
		originalDue.Format(time.RFC3339), recoveredDue.Format(time.RFC3339), recoverAsOf.Format(time.RFC3339))

	scheduledEvents := fx.countRows(`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='scheduled'`, fxTenant, obligationID)
	story.Assert("a durable 'scheduled' audit event was recorded for the reopen", scheduledEvents >= 1, "events=%d", scheduledEvents)
}
