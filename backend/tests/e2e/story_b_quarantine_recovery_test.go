package e2e

import (
	"testing"
	"time"

	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryB_QuarantineDeferRecovery drives the real SM-2 health-defer/recovery path end to
// end through the generation service, mirroring
// internal/vaccination/adapters/postgres/generation_integration_test.go's
// TestGoatRecheckDefersExistingScheduledObligation for the defer half, and adding the recovery half.
//
// The recovery recheck deliberately calls GenerateRecoveryRepairForGoat, NOT GenerateForGoat: the
// generation service exposes two distinct single-goat entrypoints (see generation.go) --
// GenerateForGoat (the plain goat.created path, generationOptions{}) and
// GenerateRecoveryRepairForGoat (generationOptions{healthRecoveryAlign: true}, the same option the
// real GoatRecheckHandler event consumer uses for goat.health.changed/goat.location.changed). Only
// the recovery-repair path computes a RecoveryReschedule and realigns due_at on reopen; calling
// GenerateForGoat here would still flip the obligation back to 'scheduled' correctly, but would
// silently leave due_at at its stale pre-quarantine value. GenerateRecoveryRepairForGoat is a real,
// wired production entrypoint (cmd/generate-vaccination-obligations/main.go's recovery-repair CLI
// path), not a test-only shortcut.
func TestKernelStoryB_QuarantineDeferRecovery(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-b", "Quarantine defer + health recovery reschedule",
		"A goat's PC vaccination dose is generated on schedule. The goat then enters quarantine, and the "+
			"next recheck must hold (defer) its open dose rather than let it slip past due. The goat "+
			"recovers, and the next recheck must reopen the held dose and realign its due date onto the "+
			"recovery-time calendar -- the real SM-2 health-recovery path, not a stand-in.")
	defer story.Finish()

	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_b", 21, 0, []string{"sick", "quarantine", "icu"})

	const goatID = "e2000000-0000-4000-8000-0000000000b1"
	dob := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatID, DOB: &dob})

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

	// --- Goat enters quarantine. ---
	story.Step("Goat enters quarantine; the next recheck defers the open dose",
		"Flip the goat's health_status to 'quarantine' and recheck generation the next day. The rule's "+
			"defer_states include quarantine, so the recheck must hold (defer) the already-open obligation "+
			"instead of leaving it due.")
	fx.exec("mark goat quarantine", `UPDATE goats SET health_status='quarantine' WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, goatID)

	deferAsOf := genAsOf.AddDate(0, 0, 1)
	res2, err := gen.GenerateForGoat(fx.Ctx, fxTenant, goatID, deferAsOf)
	story.Assert("recheck (quarantine) ran without error", err == nil, "err=%v", err)
	story.Assert("recheck deferred the open obligation", res2.Deferred == 1, "deferred=%d", res2.Deferred)

	deferredStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, obligationID)
	story.Assert("the obligation is now deferred", deferredStatus == "deferred", "status=%q", deferredStatus)
	deferredEvents := fx.countRows(`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='deferred'`, fxTenant, obligationID)
	story.Assert("a durable 'deferred' audit event was recorded", deferredEvents == 1, "events=%d", deferredEvents)

	// --- Goat recovers. ---
	story.Step("Goat recovers; the next recovery-repair recheck reopens and realigns the dose",
		"Flip the goat's health_status back to 'healthy' and run the real GenerateRecoveryRepairForGoat "+
			"recheck. It must call the real ReopenDeferredObligationByIdempotencyKey recovery path, flip "+
			"the obligation back to scheduled, and realign its due date onto the recovery-time calendar "+
			"(SM-2 health recovery) -- not leave it at its stale pre-quarantine date.")
	fx.exec("mark goat healthy", `UPDATE goats SET health_status='healthy' WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, goatID)

	recoverAsOf := deferAsOf.AddDate(0, 0, 3)
	res3, err := gen.GenerateRecoveryRepairForGoat(fx.Ctx, fxTenant, goatID, recoverAsOf)
	story.Assert("recheck (recovered) ran without error", err == nil, "err=%v", err)
	story.Assert("recheck reopened the deferred obligation", res3.Reopened == 1, "reopened=%d", res3.Reopened)

	recoveredStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, obligationID)
	story.Assert("the obligation is scheduled again", recoveredStatus == "scheduled", "status=%q", recoveredStatus)

	recoveredDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, obligationID)
	story.Assert("the due date was realigned onto the recovery-time calendar, not left at the stale pre-quarantine date",
		!recoveredDue.Equal(originalDue) && recoveredDue.After(originalDue),
		"original_due=%s recovered_due=%s recover_asOf=%s",
		originalDue.Format(time.RFC3339), recoveredDue.Format(time.RFC3339), recoverAsOf.Format(time.RFC3339))

	scheduledEvents := fx.countRows(`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='scheduled'`, fxTenant, obligationID)
	story.Assert("a durable 'scheduled' audit event was recorded for the reopen", scheduledEvents >= 1, "events=%d", scheduledEvents)
}
