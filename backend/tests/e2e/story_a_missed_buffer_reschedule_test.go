package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// TestKernelStoryA_MissedBufferReschedule drives the obligation kernel's due-window arithmetic
// directly against the obligation repository/sweeper, the same way
// backend/internal/obligation/adapters/postgres/sweeper_integration_test.go and
// recovery_cancel_integration_test.go do, rather than through birth-age generation math.
//
// That choice is deliberate: GenerationService's missed-dose catch-up policy (see
// applyMissedDosePolicy in internal/vaccination/app/generation.go) re-dates a badly-overdue
// birth-age obligation to "today" (or defers it) the moment generation runs, precisely so a stale,
// still-"scheduled" obligation is never produced by the front door once its window has closed. The
// only way to exercise "an obligation sat scheduled past its due window and nobody acted on it" is
// to seed it directly and let the SWEEPER (not generation) discover it -- which is exactly what the
// obligation package's own sweeper tests do.
func TestKernelStoryA_MissedBufferReschedule(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-a", "Missed vs in-buffer vs reschedule",
		"Two goats share one PC vaccination rule with a 14-day due window. G-Missed's dose crossed its "+
			"due window with nobody acting on it, so the obligation sweeper must mark it missed. G-Buffer's "+
			"dose is overdue but still inside its window, so the sweeper must leave it alone. The missed "+
			"dose is then put back on the calendar.")
	defer story.Finish()

	versionID, ruleID := fx.PublishSimpleProtocol("vaccination.e2e.story_a", 21, 14, nil)

	const goatMissed = "e1000000-0000-4000-8000-0000000000a1"
	const goatBuffer = "e1000000-0000-4000-8000-0000000000a2"
	fx.SeedGoat(GoatSpec{GoatID: goatMissed})
	fx.SeedGoat(GoatSpec{GoatID: goatBuffer})

	story.Step("Seed one stale dose and one in-buffer dose",
		"G-Missed's dose was due 2026-05-22 with its 14-day window closing 2026-06-05 (long since crossed). "+
			"G-Buffer's dose was due 2026-06-26 with its window closing 2026-07-10 (overdue, but still open). "+
			"Both obligations are inserted directly against the obligation repository, mirroring "+
			"sweeper_integration_test.go's own seeding pattern.")

	missedDue := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	missedWindowEnd := missedDue.AddDate(0, 0, 14)
	missedObl, applied, err := fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
		TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatMissed, ScopeType: "park", ScopeID: fxPark,
		DueAt: missedDue, WindowEnd: &missedWindowEnd, Status: "scheduled",
		IdempotencyKey: "e2e-story-a-missed", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("seed missed obligation: applied=%v err=%v", applied, err)
	}

	bufferDue := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	bufferWindowEnd := bufferDue.AddDate(0, 0, 14)
	bufferObl, applied, err := fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
		TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatBuffer, ScopeType: "park", ScopeID: fxPark,
		DueAt: bufferDue, WindowEnd: &bufferWindowEnd, Status: "scheduled",
		IdempotencyKey: "e2e-story-a-buffer", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("seed buffer obligation: applied=%v err=%v", applied, err)
	}

	sweepAsOf := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	story.Step("Run the obligation sweeper's missed-dose pass",
		"Run the real oblapp.SweeperService.MarkMissed pass as of 2026-07-01: any obligation whose "+
			"window_end (falling back to due_at) is before that date is marked missed.")

	sweeper := oblapp.NewSweeperService(fx.Obl, nil, nil)
	n, err := sweeper.MarkMissed(fx.Ctx, fxTenant, sweepAsOf)
	story.Assert("sweeper's MarkMissed pass ran without error", err == nil, "err=%v", err)
	story.Assert("exactly one obligation crossed into missed", n == 1, "marked=%d", n)

	missedStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, missedObl)
	story.Assert("G-Missed's dose is now missed", missedStatus == "missed", "status=%q", missedStatus)

	bufferStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, bufferObl)
	story.Assert("G-Buffer's dose is NOT missed (still inside its due window)", bufferStatus == "scheduled", "status=%q", bufferStatus)
	story.Assert("G-Buffer's dose reads as overdue/due (due_at already passed, window still open)",
		bufferDue.Before(sweepAsOf) && bufferWindowEnd.After(sweepAsOf),
		"due_at=%s window_end=%s asOf=%s", bufferDue.Format("2006-01-02"), bufferWindowEnd.Format("2006-01-02"), sweepAsOf.Format("2006-01-02"))

	// --- Rework the missed dose onto a new calendar date. ---
	//
	// Kernel state-machine rule (docs/protocol-engine/state-machines.md, "Conventions"): missed is
	// immutable closed history, same as completed/waived/canceled/superseded. A later policy
	// correction must create new work or a rework/correction record -- it must never rewrite the
	// closed row. So RescheduleObligationByID on a 'missed' target does NOT flip the missed row back
	// to 'scheduled' in place: it inserts a brand-new obligation (fresh id, status 'scheduled', the
	// new due date, unbatched) via the same InsertObligationInstance path the SM-1 generator uses, and
	// leaves G-Missed's original row completely untouched (status, due_at, row_version, and its own
	// event history all unchanged). The new row's audit event carries
	// `superseded_missed_obligation_id` pointing back at G-Missed's obligation -- there is no
	// parent_obligation_id column on obligation_instances, so this JSONB back-reference is the
	// established traceability mechanism.
	newDue := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	newWindowEnd := newDue.AddDate(0, 0, 14)
	story.Step("Rework the missed dose onto a new calendar date",
		"Call the real RescheduleObligationByID repository method on G-Missed's obligation. It must "+
			"create a brand-new obligation for 2026-07-15 and leave the missed row untouched, never "+
			"mutate the missed row back to scheduled.")
	newObl, isReplay, err := fx.Obl.RescheduleObligationByID(fx.Ctx, fxTenant, missedObl, "e2e-story-a-reschedule", newDue, newDue, &newWindowEnd, newDue.AddDate(0, 0, -14))
	story.Assert("RescheduleObligationByID ran without error", err == nil, "err=%v", err)
	story.Assert("this was a first-time apply, not an idempotent replay", !isReplay, "isReplay=%v", isReplay)
	story.Assert("a brand-new obligation was created rather than the missed row being reused",
		newObl != "" && newObl != missedObl, "newObl=%q missedObl=%q", newObl, missedObl)

	missedStatusAfter := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, missedObl)
	story.Assert("G-Missed's original dose is STILL missed -- immutable closed history is never rewritten",
		missedStatusAfter == "missed", "status=%q", missedStatusAfter)

	missedDueAfter := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, missedObl)
	story.Assert("G-Missed's original due date is untouched", missedDueAfter.Equal(missedDue),
		"due_at=%s want=%s", missedDueAfter.Format("2006-01-02"), missedDue.Format("2006-01-02"))

	newStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, newObl)
	story.Assert("the new obligation is scheduled", newStatus == "scheduled", "status=%q", newStatus)

	newDueAt := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, newObl)
	story.Assert("the new obligation carries the new calendar date", newDueAt.Equal(newDue),
		"due_at=%s want=%s", newDueAt.Format("2006-01-02"), newDue.Format("2006-01-02"))

	// RescheduleObligationByID deliberately records event_type='scheduled' (not a new
	// 'rescheduled' enum value) on the NEW obligation -- obligation_status_events is a hot table and
	// validate-hot-index-migrations.sh rejects widening its CHECK constraint past the
	// enforcement floor; the "why" (and the back-reference to the missed row it reworks) lives in the
	// payload's reason/superseded_missed_obligation_id fields instead (see the doc comment above
	// insertReworkObligationForMissed in repository.go).
	events := fx.countRows(`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='scheduled' AND payload->>'reason'='mobile_reschedule_of_missed' AND payload->>'superseded_missed_obligation_id'=$3`,
		fxTenant, newObl, missedObl)
	story.Assert("a durable 'scheduled' audit event linking the new obligation back to the missed one was recorded",
		events == 1, "events=%d", events)

	missedOwnNewEvents := fx.countRows(`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='scheduled'`, fxTenant, missedObl)
	story.Assert("the missed row itself recorded no new 'scheduled' event -- it was never touched",
		missedOwnNewEvents == 0, "events=%d", missedOwnNewEvents)

	story.Step("Idempotent redelivery of the same reschedule request no-ops",
		"Calling RescheduleObligationByID again with the exact same idempotency key and payload must "+
			"replay the original result (same new obligation id) without creating a second obligation "+
			"or touching the missed row again.")
	replayObl, replayIsReplay, err := fx.Obl.RescheduleObligationByID(fx.Ctx, fxTenant, missedObl, "e2e-story-a-reschedule", newDue, newDue, &newWindowEnd, newDue.AddDate(0, 0, -14))
	story.Assert("replay ran without error", err == nil, "err=%v", err)
	story.Assert("replay is flagged as a replay, not a fresh apply", replayIsReplay, "isReplay=%v", replayIsReplay)
	story.Assert("replay returns the same new obligation id, not a second new one", replayObl == newObl,
		"replayObl=%q newObl=%q", replayObl, newObl)

	replayEvents := fx.countRows(`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='scheduled' AND payload->>'reason'='mobile_reschedule_of_missed'`, fxTenant, newObl)
	story.Assert("replay did not duplicate the scheduled audit event", replayEvents == 1, "events=%d", replayEvents)

	missedStatusAfterReplay := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, missedObl)
	story.Assert("G-Missed's original dose is still untouched after the idempotent replay",
		missedStatusAfterReplay == "missed", "status=%q", missedStatusAfterReplay)
}
