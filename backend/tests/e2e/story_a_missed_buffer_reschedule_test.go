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

	// --- Reschedule the missed dose back onto the calendar. ---
	//
	// The parallel worktree fix mentioned in this task's brief -- a real RescheduleObligationByID
	// repository method for the mobile "reschedule an overdue/missed obligation" write path, meant to
	// replace internal/vaccinationexecution/adapters/http/handler.go's RescheduleObligation handler
	// (which previously misused ReopenDeferredObligationByIdempotencyKey, a defer-recovery method, for
	// this) -- landed in this same worktree while this test suite was being written. It had NOT landed
	// yet when Story A was first implemented (repository.go had no method whose name contained
	// "Reschedule" at that point, only the HTTP route registration), so this call replaced an earlier
	// raw-SQL + RecordStatusEvent stand-in once RescheduleObligationByID appeared. It targets the
	// obligation directly by id, explicitly supports rescheduling a 'missed' row back to 'scheduled',
	// and writes its own durable 'rescheduled' audit event.
	newDue := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	newWindowEnd := newDue.AddDate(0, 0, 14)
	story.Step("Reschedule the missed dose",
		"Call the real RescheduleObligationByID repository method to put G-Missed's dose back on the "+
			"calendar for 2026-07-15.")
	_, isReplay, err := fx.Obl.RescheduleObligationByID(fx.Ctx, fxTenant, missedObl, "e2e-story-a-reschedule", newDue, newDue, &newWindowEnd, newDue.AddDate(0, 0, -14))
	story.Assert("RescheduleObligationByID ran without error", err == nil, "err=%v", err)
	story.Assert("this was a first-time apply, not an idempotent replay", !isReplay, "isReplay=%v", isReplay)

	rescheduledStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, missedObl)
	story.Assert("G-Missed's dose is scheduled again", rescheduledStatus == "scheduled", "status=%q", rescheduledStatus)

	rescheduledDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, missedObl)
	story.Assert("its due date moved to the new calendar date", rescheduledDue.Equal(newDue), "due_at=%s want=%s", rescheduledDue.Format("2006-01-02"), newDue.Format("2006-01-02"))

	// RescheduleObligationByID deliberately records event_type='scheduled' (not a new
	// 'rescheduled' enum value) — obligation_status_events is a hot table and
	// validate-hot-index-migrations.sh rejects widening its CHECK constraint past the
	// enforcement floor; the "why" lives in the payload's reason field instead (see the
	// doc comment above InsertObligationStatusEvent's call site in repository.go).
	events := fx.countRows(`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='scheduled' AND payload->>'reason'='mobile_reschedule'`, fxTenant, missedObl)
	story.Assert("a durable 'scheduled' audit event (reason=mobile_reschedule) was recorded", events == 1, "events=%d", events)
}
