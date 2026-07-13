package e2e

import (
	"testing"
	"time"

	calendardomain "github.com/vgoats/goatos/backend/internal/calendar/domain"
	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryAG_RecurringCalendarLifecycle proves the missing recurring lifecycle without
// pre-seeding any derived output: generation -> accepted history -> vaccination.completed -> next
// yearly obligation -> Calendar history/marker/future event -> culled exit cancellation.
func TestKernelStoryAG_RecurringCalendarLifecycle(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-ag", "Accepted adult history creates the next Calendar cycle until exit",
		"An adult procurement goat receives a generated yearly vaccination late. SM-5 records accepted "+
			"history, SM-7 schedules exactly one next cycle from the actual administration date, Calendar "+
			"shows both the past completed marker and future work, and a production culled exit removes only "+
			"the future obligation while retaining history.")
	story.Certify("backend kernel + SOP proof/submission/review + durable outbox envelope + domain consumer")
	defer story.Finish()

	const (
		shedID  = "af000000-0000-4000-8000-000000000001"
		stageID = "af000000-0000-4000-8000-000000000002"
		goatID  = "af000000-0000-4000-8000-000000000010"
	)
	fx.SeedAdultShed(shedID, "E2E-AG", stageID, "A1")
	entry := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	dob := entry.AddDate(-2, 0, 0)
	fx.SeedProcurementGoat(goatID, shedID, entry, "A1", dob)
	versionID, ruleIDs := fx.PublishScheduleProtocol("vaccination.e2e.story_ag", "{}", []RuleSpec{{
		DoseCode: "adult_yearly", Sequence: 1, TriggerType: "post_arrival", OffsetDays: 0,
		DueWindowDays: 30, Repeat: "yearly", CatchUp: "next_cycle",
	}})

	story.Step("Generate the adult obligation through goat.created",
		"The identity event reaches the real adult-procurement schedule path; no obligation is inserted by the story.")
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, goatID, entry)
	currentID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3 AND status='scheduled'`,
		fxTenant, goatID, ruleIDs["adult_yearly"])
	story.Assert("one current obligation was generated", currentID != "", "obligation_id=%s", currentID)

	story.Step("Accept it late and dispatch vaccination.completed",
		"SM-5 accepts the dose on January 10. SM-7 must anchor the yearly recurrence to that actual administration date, not the January 1 due date.")
	administeredAt := time.Date(2025, 1, 10, 9, 30, 0, 0, time.UTC)
	completeVaccinationObligationThroughSOP(t, fx, versionID, currentID, shedID, []string{goatID}, administeredAt, "story-ag-current")
	fx.DispatchVaccinationCompleted(currentID)
	nextID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND rule_id=$3 AND status='scheduled'`,
		fxTenant, goatID, ruleIDs["adult_yearly"])
	nextDue := fx.scanTime(`SELECT due_at FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, nextID)
	wantNextDue := administeredAt.AddDate(1, 0, 0)
	story.Assert("exactly one future recurrence exists",
		fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`, fxTenant, goatID) == 1,
		"count=%d", fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`, fxTenant, goatID))
	story.Assert("next yearly due date uses administered_at", sameDay(nextDue, wantNextDue), "got=%s want=%s", nextDue.Format("2006-01-02"), wantNextDue.Format("2006-01-02"))

	story.Step("Project and read past history plus the future cycle through Calendar",
		"Calendar reads accepted history from canonical completion state and projects only the live future obligation. Date markers distinguish completed history from open work.")
	// Drive BOTH real calendar projectors the reads are gated on: the UPCOMING projection
	// (RefreshVaccinationProjection / calendar_projection_state) for the future recurrence, and the
	// HISTORY projection (RecomputeVaccinationHistoryProjection / calendar_history_projection_state)
	// for the completed dose. Each refresh window projects one day BEYOND the read's inclusive DateTo,
	// because the freshness gate compares the projection's date_to against the read's EXCLUSIVE upper
	// bound (read DateTo + 24h); a window that only reaches the inclusive DateTo reads back as stale.
	if _, err := fx.Calendar.RefreshVaccinationProjection(fx.Ctx, calendarports.RefreshVaccinationProjection{
		TenantID: fxTenant, DateFrom: wantNextDue.AddDate(0, 0, -2), DateTo: wantNextDue.AddDate(0, 0, 3), Limit: 100,
	}); err != nil {
		t.Fatalf("refresh future Calendar projection: %v", err)
	}
	if _, err := fx.CalendarRepo.RecomputeVaccinationHistoryProjection(fx.Ctx, calendarports.RefreshVaccinationHistoryProjection{
		TenantID: fxTenant, DateFrom: administeredAt.AddDate(0, 0, -2), DateTo: administeredAt.AddDate(0, 0, 3), Limit: 100,
	}); err != nil {
		t.Fatalf("refresh history Calendar projection: %v", err)
	}
	completed := calendardomain.StatusCompleted
	history, err := fx.Calendar.ListEvents(fx.Ctx, calendardomain.Query{
		TenantID: fxTenant, OwnerKey: calendardomain.OwnerAll, Status: &completed,
		DateFrom: administeredAt.AddDate(0, 0, -1), DateTo: administeredAt.AddDate(0, 0, 1),
		Limit: 20, IncludeDateMarkers: true, Scope: calendardomain.ScopeFilter{TenantWide: true},
	})
	story.Assert("Calendar history read succeeded", err == nil, "err=%v", err)
	story.Assert("past accepted dose appears as completed history", err == nil && len(history.Items) == 1 && history.Items[0].EventType == calendardomain.EventVaccinationHistory,
		"items=%d", len(history.Items))
	historyCompleted, historyOpen := 0, 0
	if len(history.DateMarkers) > 0 {
		historyCompleted = history.DateMarkers[0].CompletedCount
		historyOpen = history.DateMarkers[0].OpenCount
	}
	story.Assert("past date marker is completed-only", err == nil && len(history.DateMarkers) == 1 && history.DateMarkers[0].CompletedCount == 1 && history.DateMarkers[0].OpenCount == 0,
		"markers=%d completed=%d open=%d", len(history.DateMarkers), historyCompleted, historyOpen)

	future, err := fx.Calendar.ListEvents(fx.Ctx, calendardomain.Query{
		TenantID: fxTenant, OwnerKey: calendardomain.OwnerAll,
		DateFrom: wantNextDue.AddDate(0, 0, -1), DateTo: wantNextDue.AddDate(0, 0, 1),
		Limit: 20, IncludeDateMarkers: true, Scope: calendardomain.ScopeFilter{TenantWide: true},
	})
	story.Assert("Calendar future read succeeded", err == nil, "err=%v", err)
	futureStatus := ""
	if len(future.Items) > 0 {
		futureStatus = future.Items[0].Status
	}
	story.Assert("future recurrence appears as open Calendar work", err == nil && len(future.Items) == 1 && future.Items[0].Status != calendardomain.StatusCompleted,
		"items=%d status=%q", len(future.Items), futureStatus)
	futureOpen := 0
	if len(future.DateMarkers) > 0 {
		futureOpen = future.DateMarkers[0].OpenCount
	}
	story.Assert("future marker counts open work", err == nil && len(future.DateMarkers) == 1 && future.DateMarkers[0].OpenCount == 1,
		"markers=%d open=%d", len(future.DateMarkers), futureOpen)

	story.Step("Cull the goat through identity and refresh Calendar",
		"The production culled exit emits goat.exited and SM-3 cancels the next obligation. Refresh tombstones it from Calendar while accepted history remains queryable.")
	fx.ExitGoat(goatID, "culled", "story-ag-culled", time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC))
	if _, err := fx.Calendar.RefreshVaccinationProjection(fx.Ctx, calendarports.RefreshVaccinationProjection{
		TenantID: fxTenant, DateFrom: wantNextDue.AddDate(0, 0, -2), DateTo: wantNextDue.AddDate(0, 0, 3), Limit: 100,
	}); err != nil {
		t.Fatalf("refresh Calendar after exit: %v", err)
	}
	// Accepted history is unchanged by the exit, but re-drive the history projector too so the
	// post-exit history read is served by a freshly-rebuilt (not just still-within-TTL) projection.
	if _, err := fx.CalendarRepo.RecomputeVaccinationHistoryProjection(fx.Ctx, calendarports.RefreshVaccinationHistoryProjection{
		TenantID: fxTenant, DateFrom: administeredAt.AddDate(0, 0, -2), DateTo: administeredAt.AddDate(0, 0, 3), Limit: 100,
	}); err != nil {
		t.Fatalf("refresh history Calendar after exit: %v", err)
	}
	futureAfterExit, err := fx.Calendar.ListEvents(fx.Ctx, calendardomain.Query{
		TenantID: fxTenant, OwnerKey: calendardomain.OwnerAll,
		DateFrom: wantNextDue.AddDate(0, 0, -1), DateTo: wantNextDue.AddDate(0, 0, 1),
		Limit: 20, IncludeDateMarkers: true, Scope: calendardomain.ScopeFilter{TenantWide: true},
	})
	story.Assert("future Calendar work disappears after exit", err == nil && len(futureAfterExit.Items) == 0, "items=%d err=%v", len(futureAfterExit.Items), err)
	historyAfterExit, err := fx.Calendar.ListEvents(fx.Ctx, calendardomain.Query{
		TenantID: fxTenant, OwnerKey: calendardomain.OwnerAll, Status: &completed,
		DateFrom: administeredAt.AddDate(0, 0, -1), DateTo: administeredAt.AddDate(0, 0, 1),
		Limit: 20, IncludeDateMarkers: true, Scope: calendardomain.ScopeFilter{TenantWide: true},
	})
	story.Assert("accepted history remains after exit", err == nil && len(historyAfterExit.Items) == 1, "items=%d err=%v", len(historyAfterExit.Items), err)
}
