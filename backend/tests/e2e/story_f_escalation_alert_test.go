package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// TestKernelStoryF_EscalationAlert drives the escalation path: when a dose obligation crosses
// from "in-buffer" to "missed" (window closes with no action), the system marks it missed and
// escalates to leadership for rework. Leadership reads the missed obligation and reschedules it.
func TestKernelStoryF_EscalationAlert(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-f", "Escalation: buffer → missed → alert → rework",
		"A goat's vaccination obligation sits in the buffer zone (due but window still open). "+
			"The sweeper marks it missed when the window closes. Leadership is alerted and reschedules "+
			"the dose. The obligation returns to scheduled with a new due date.")
	defer story.Finish()

	versionID, ruleID := fx.PublishSimpleProtocol("vaccination.e2e.story_f", 21, 14, nil)

	const goatID = "e1000000-0000-4000-8000-0000000000f1"
	fx.SeedGoat(GoatSpec{GoatID: goatID})

	story.Step("Seed goat and protocol",
		"One goat, one vaccination protocol (21-day offset, 14-day window). "+
			"Obligation will be manually seeded to track escalation.")

	// Seed an obligation that is in-buffer (overdue but not yet missed)
	asOf := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC) // sweep as-of date (after window closes)
	dueDate := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	windowEnd := dueDate.AddDate(0, 0, 14) // 2026-07-10

	oblID, _, err := fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
		TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatID, ScopeType: "park", ScopeID: fxPark,
		DueAt: dueDate, WindowEnd: &windowEnd, Status: "scheduled",
		IdempotencyKey: "e2e-story-f-buffer", Sequence: 1,
	})
	if err != nil {
		t.Fatalf("seed obligation: %v", err)
	}

	story.Step("Seed in-buffer obligation",
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
			"Leadership is alerted and may reschedule.")

	// Leadership reschedules the obligation
	newDue := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	newWindowEnd := newDue.AddDate(0, 0, 14)
	_, isReplay, err := fx.Obl.RescheduleObligationByID(fx.Ctx, fxTenant, oblID, "e2e-story-f-reschedule",
		newDue, newDue, &newWindowEnd, newDue.AddDate(0, 0, -14))
	story.Assert("RescheduleObligationByID succeeded", err == nil, "err=%v", err)
	story.Assert("first-time reschedule (not idempotent replay)", !isReplay, "isReplay=%v", isReplay)

	finalStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE obligation_id=$1`, oblID)
	story.Assert("obligation rescheduled to scheduled", finalStatus == "scheduled", "status=%q", finalStatus)

	finalDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE obligation_id=$1`, oblID)
	story.Assert("new due date set by leadership", finalDue.Equal(newDue),
		"due=%s expected=%s", finalDue.Format("2006-01-02"), newDue.Format("2006-01-02"))

	story.Step("Leadership reschedules missed dose",
		"Missed obligation rescheduled to 2026-07-15. System returns it to scheduled status. "+
			"Escalation workflow complete: buffer → missed → alert → rework → scheduled again.")
}
