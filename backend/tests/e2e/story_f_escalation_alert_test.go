package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryF_EscalationAlert drives the escalation path: when a dose obligation crosses
// from "in-buffer" to "missed" (window closes with no action), the system marks it missed and
// escalates to leadership for rework. Leadership reads the missed obligation and reworks it onto a
// new obligation.
//
// Kernel state-machine rule (docs/protocol-engine/state-machines.md, "Conventions"): missed is
// immutable closed history, same as completed/waived/canceled/superseded — a later policy correction
// creates new work, it never rewrites the closed row. So leadership's rework does NOT flip the missed
// obligation back to 'scheduled' in place; it creates a brand-new obligation for the new due date and
// leaves the missed row's status/due_at untouched.
func TestKernelStoryF_EscalationAlert(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-f", "Escalation: buffer → missed → alert → rework",
		"A goat's vaccination obligation sits in the buffer zone (due but window still open). "+
			"The sweeper marks it missed when the window closes. Leadership is alerted and reworks "+
			"the dose onto a new obligation with a new due date; the missed obligation stays missed.")
	defer story.Finish()

	fx.PublishSimpleProtocol("vaccination.e2e.story_f", 21, 14, nil)

	const goatID = "e1000000-0000-4000-8000-0000000000f1"
	dob := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatID, DOB: &dob})

	story.Step("Create source fixtures and publish the protocol",
		"One goat, one vaccination protocol (21-day offset, 14-day window). "+
			"The goat.created consumer creates the obligation while its due window is open.")

	// Generate an obligation while its due window is open, then fast-forward past the window.
	asOf := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC) // sweep as-of date (after window closes)
	dueDate := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatID, dueDate)
	oblID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goatID)

	story.Step("Generate the in-buffer obligation through goat.created",
		"Obligation due 2026-06-26, window 2026-07-10. As-of date 2026-07-11 (after window closes), "+
			"the obligation is overdue and its window is closed (now eligible to be marked missed).")

	status := fx.scanText(`SELECT status FROM obligation_instances WHERE obligation_id=$1`, oblID)
	story.Assert("obligation is scheduled (not yet marked missed)", status == "scheduled", "status=%q", status)

	// Run the sweeper to mark it missed (asOf > windowEnd)
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, nil)
	n, err := sweeper.MarkMissed(fx.Ctx, fxTenant, asOf)
	story.Assert("sweeper.MarkMissed ran without error", err == nil, "err=%v", err)
	story.Assert("one obligation marked missed", n == 1, "marked=%d", n)

	updatedStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE obligation_id=$1`, oblID)
	story.Assert("obligation now marked missed", updatedStatus == "missed", "status=%q", updatedStatus)

	story.Step("Run sweeper to escalate to missed",
		"Obligation window closed (2026-07-10), sweeper marks it missed (as-of 2026-07-11). "+
			"Leadership is alerted and may rework it onto a new obligation.")

	// Leadership reworks the missed obligation onto a new one.
	newDue := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	newWindowEnd := newDue.AddDate(0, 0, 14)
	newOblID, isReplay, err := fx.Obl.RescheduleObligationByID(fx.Ctx, fxTenant, oblID, "e2e-story-f-reschedule",
		newDue, newDue, &newWindowEnd, newDue.AddDate(0, 0, -14))
	story.Assert("RescheduleObligationByID succeeded", err == nil, "err=%v", err)
	story.Assert("first-time reschedule (not idempotent replay)", !isReplay, "isReplay=%v", isReplay)
	story.Assert("a brand-new obligation was created rather than the missed row being reused",
		newOblID != "" && newOblID != oblID, "newOblID=%q oblID=%q", newOblID, oblID)

	missedStatusAfter := fx.scanText(`SELECT status FROM obligation_instances WHERE obligation_id=$1`, oblID)
	story.Assert("the original obligation stays missed -- immutable closed history is never rewritten",
		missedStatusAfter == "missed", "status=%q", missedStatusAfter)

	finalStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE obligation_id=$1`, newOblID)
	story.Assert("the new obligation is scheduled", finalStatus == "scheduled", "status=%q", finalStatus)

	finalDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE obligation_id=$1`, newOblID)
	story.Assert("new due date set by leadership", sameDay(finalDue, newDue),
		"due=%s expected=%s", finalDue.Format("2006-01-02"), newDue.Format("2006-01-02"))

	story.Step("Leadership reworks missed dose onto a new obligation",
		"Missed obligation reworked onto a new obligation due 2026-07-15; the original missed row is "+
			"untouched. Escalation workflow complete: buffer → missed → alert → rework → new scheduled obligation.")
}
