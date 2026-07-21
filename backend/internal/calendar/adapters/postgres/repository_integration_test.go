package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	testTenantID    = "00000000-0000-4000-8000-000000000001"
	testCustodianID = "00000000-0000-4000-8000-000000001001"
	testActorID     = "86000000-0000-4000-8000-000000009999"
	testParkA       = "86000000-0000-4000-8000-000000000701"
	testParkB       = "86000000-0000-4000-8000-000000000702"
	testShedA       = "86000000-0000-4000-8000-000000000711"
	testShedB       = "86000000-0000-4000-8000-000000000712"
)

func testCalendarActorGrants() []ports.ActorGrant {
	return []ports.ActorGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: testTenantID}}
}

func TestCalendarAcceptedHistoryRangePlanUsesPartialIndex(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `EXPLAIN (COSTS OFF)
SELECT obligation_id
FROM vaccination_completions
WHERE tenant_id = $1::uuid
  AND status = 'accepted'
  AND administered_at >= $2::timestamptz
  AND administered_at < $3::timestamptz
ORDER BY administered_at, obligation_id
LIMIT 200`, testTenantID, time.Now().Add(-45*24*time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	plan := strings.Join(lines, "\n")
	if !strings.Contains(plan, "vaccination_completions_accepted_history_calendar_idx") {
		t.Fatalf("accepted Calendar history plan missed bounded partial index:\n%s", plan)
	}
	if strings.Contains(plan, "Seq Scan on vaccination_completions") {
		t.Fatalf("accepted Calendar history plan used sequential scan:\n%s", plan)
	}
}

func TestCalendarPostgresListDetailActionsAndHistory(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	protocolID := "86000000-0000-4000-8000-000000001101"
	versionID := "86000000-0000-4000-8000-000000001102"
	ruleID := "86000000-0000-4000-8000-000000001103"
	obligationID := "86000000-0000-4000-8000-000000001104"
	batchID := "86000000-0000-4000-8000-000000001105"
	reminderObligationID := "86000000-0000-4000-8000-000000001106"
	reminderBatchID := "86000000-0000-4000-8000-000000001107"
	// Real canonical batches (5k-50k envelope: canonical is the only serving path), one per park so
	// the park-day drive grouping keeps them as two distinct list items.
	dueAt := time.Now().UTC().Add(2 * time.Hour)
	reminderDueAt := time.Now().UTC().Add(30 * time.Minute)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, dueAt)
	seedVaccinationBatchForShed(t, ctx, pool, batchID, versionID, testParkA, testShedA, dueAt, obligationID)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, reminderObligationID, reminderDueAt)
	seedVaccinationBatchForShed(t, ctx, pool, reminderBatchID, versionID, testParkB, testShedB, reminderDueAt, reminderObligationID)
	// CR-002/CR-003: the stable park-drive identity (parkdrive:park:...:date:...), not the
	// underlying batch's own batch:<id>.
	testCalendarEvent := parkDriveEventID(testParkA, dueAt)
	testReminderEvent := parkDriveEventID(testParkB, reminderDueAt)

	repo := NewRepository(pool, 5*time.Second)
	from := time.Now().UTC().Add(-24 * time.Hour)
	to := time.Now().UTC().Add(44 * 24 * time.Hour)
	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: from,
		DateTo:   to,
		Limit:    200,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("list items = %d, want 2 drive events", len(list.Items))
	}
	for _, item := range list.Items {
		if item.EventType != domain.EventVaccinationDrive {
			t.Fatalf("list item type = %s, want vaccination_drive", item.EventType)
		}
	}
	detail, err := repo.GetEventDetail(ctx, domain.EventQuery{TenantID: testTenantID, EventID: testCalendarEvent, Scope: domain.ScopeFilter{TenantWide: true}})
	if err != nil {
		t.Fatalf("GetEventDetail: %v", err)
	}
	if detail.Event.EventID != testCalendarEvent || len(detail.NotificationChannels) != 1 {
		t.Fatalf("detail = %#v channels=%#v", detail.Event, detail.NotificationChannels)
	}

	first, err := repo.SendNudge(ctx, ports.SendNudge{
		TenantID: testTenantID, EventID: testCalendarEvent, ActorID: testActorID,
		TraceID: "trace-nudge", IdempotencyKey: "calendar-nudge-key", Channel: "slack",
		Message: "Please handle this dose", Reason: "CEO follow-up", Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("SendNudge first: %v", err)
	}
	replay, err := repo.SendNudge(ctx, ports.SendNudge{
		TenantID: testTenantID, EventID: testCalendarEvent, ActorID: testActorID,
		TraceID: "trace-nudge", IdempotencyKey: "calendar-nudge-key", Channel: "slack",
		Message: "Please handle this dose", Reason: "CEO follow-up", Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("SendNudge replay: %v", err)
	}
	if replay.ActionID != first.ActionID || !replay.IdempotentReplay {
		t.Fatalf("nudge replay = %#v, want same action replay", replay)
	}
	if _, err := repo.SendNudge(ctx, ports.SendNudge{
		TenantID: testTenantID, EventID: testCalendarEvent, ActorID: testActorID,
		IdempotencyKey: "calendar-nudge-key", Channel: "email", Message: "different", Scope: domain.ScopeFilter{TenantWide: true},
	}); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("SendNudge conflict err = %v, want ErrIdempotencyConflict", err)
	}
	assertCount(t, ctx, pool, "nudge notifications", `SELECT count(*) FROM notification_requests WHERE tenant_id=$1 AND calendar_event_id=$2 AND notification_type='nudge'`, 1, testTenantID, testCalendarEvent)
	assertCount(t, ctx, pool, "nudge outbox", `SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_type='calendar_notification' AND aggregate_id=$2::uuid`, 1, testTenantID, first.ActionID)
	assertOutboxEnvelope(t, ctx, pool, "nudge outbox envelope", "calendar_notification", first.ActionID, "calendar.nudge.requested", testCalendarEvent)
	assertCount(t, ctx, pool, "nudge audit", `SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND metadata->>'calendar_event_id'=$2 AND action='calendar.nudge.sent'`, 1, testTenantID, testCalendarEvent)

	snoozeUntil := time.Now().UTC().Add(6 * time.Hour)
	snooze, err := repo.Snooze(ctx, ports.Snooze{
		TenantID: testTenantID, EventID: testCalendarEvent, ActorID: testActorID,
		TraceID: "trace-snooze", IdempotencyKey: "calendar-snooze-key",
		SnoozeUntil: snoozeUntil, Reason: "wait for morning round", Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("Snooze first: %v", err)
	}
	snoozeReplay, err := repo.Snooze(ctx, ports.Snooze{
		TenantID: testTenantID, EventID: testCalendarEvent, ActorID: testActorID,
		TraceID: "trace-snooze", IdempotencyKey: "calendar-snooze-key",
		SnoozeUntil: snoozeUntil, Reason: "wait for morning round", Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("Snooze replay: %v", err)
	}
	if snoozeReplay.ActionID != snooze.ActionID || !snoozeReplay.IdempotentReplay {
		t.Fatalf("snooze replay = %#v, want same action replay", snoozeReplay)
	}
	if _, err := repo.Snooze(ctx, ports.Snooze{
		TenantID: testTenantID, EventID: testCalendarEvent, ActorID: testActorID,
		IdempotencyKey: "calendar-snooze-key", SnoozeUntil: snoozeUntil.Add(time.Hour), Reason: "different", Scope: domain.ScopeFilter{TenantWide: true},
	}); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("Snooze conflict err = %v, want ErrIdempotencyConflict", err)
	}
	if _, err := repo.Snooze(ctx, ports.Snooze{
		TenantID: testTenantID, EventID: testCalendarEvent, ActorID: testActorID,
		IdempotencyKey: "calendar-snooze-second", SnoozeUntil: snoozeUntil.Add(time.Hour), Reason: "second active", Scope: domain.ScopeFilter{TenantWide: true},
	}); !errors.Is(err, ports.ErrActiveSnoozeExists) {
		t.Fatalf("second active snooze err = %v, want ErrActiveSnoozeExists", err)
	}
	assertCount(t, ctx, pool, "active snoozes", `SELECT count(*) FROM calendar_snoozes WHERE tenant_id=$1 AND calendar_event_id=$2 AND status='active'`, 1, testTenantID, testCalendarEvent)
	assertOutboxEnvelope(t, ctx, pool, "snooze outbox envelope", "calendar_snooze", snooze.ActionID, "calendar.snooze.recorded", testCalendarEvent)

	history, err := repo.History(ctx, domain.HistoryQuery{TenantID: testTenantID, EventID: testCalendarEvent, Limit: 20, Scope: domain.ScopeFilter{TenantWide: true}})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history.Items) < 4 {
		t.Fatalf("history items = %d, want nudge/snooze/audit rows", len(history.Items))
	}

	queued, err := repo.SweepDueReminders(ctx, testTenantID, 10)
	if err != nil {
		t.Fatalf("SweepDueReminders: %v", err)
	}
	if queued != 1 {
		t.Fatalf("queued reminders = %d, want 1", queued)
	}
	assertCount(t, ctx, pool, "reminder requests", `SELECT count(*) FROM notification_requests WHERE tenant_id=$1 AND calendar_event_id=$2 AND notification_type='reminder'`, 1, testTenantID, testReminderEvent)
	var reminderID string
	if err := pool.QueryRow(ctx, `SELECT notification_request_id::text FROM notification_requests WHERE tenant_id=$1 AND calendar_event_id=$2 AND notification_type='reminder'`, testTenantID, testReminderEvent).Scan(&reminderID); err != nil {
		t.Fatalf("reminder id query: %v", err)
	}
	assertOutboxEnvelope(t, ctx, pool, "reminder outbox envelope", "calendar_notification", reminderID, "calendar.reminder.queued", testReminderEvent)
}

func TestCalendarReminderSweepRearmsOpenWorkDaily(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolID := "86000000-0000-4000-8000-000000001211"
	versionID := "86000000-0000-4000-8000-000000001212"
	ruleID := "86000000-0000-4000-8000-000000001213"
	obligationID := "86000000-0000-4000-8000-000000001214"
	batchID := "86000000-0000-4000-8000-000000001215"
	dueAt := time.Now().UTC().Add(30 * time.Minute)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, dueAt)
	seedVaccinationBatchForShed(t, ctx, pool, batchID, versionID, testParkA, testShedA, dueAt, obligationID)
	// CR-002/CR-003: the stable park-drive identity, not batch:<id>.
	eventID := parkDriveEventID(testParkA, dueAt)
	yesterdayKey := testTenantID + ":calendar.reminder:" + eventID + ":" + calendarBusinessDate(time.Now().In(biztime.DefaultLocation()).Add(-24*time.Hour))
	if _, err := pool.Exec(ctx, `
INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, notification_type, channel,
  title, body, status, idempotency_key, request_fingerprint, context
) VALUES (
  $1::uuid, $2, 'calendar_event', 'reminder', 'local-stub',
  'Yesterday reminder', 'kept as reminder history', 'sent',
  $3, $3 || ':fingerprint', '{}'::jsonb
)`, testTenantID, eventID, yesterdayKey); err != nil {
		t.Fatalf("seed old reminder: %v", err)
	}

	queued, err := repo.SweepDueReminders(ctx, testTenantID, 10)
	if err != nil {
		t.Fatalf("SweepDueReminders rearm: %v", err)
	}
	if queued != 1 {
		t.Fatalf("queued reminders = %d, want daily rearm", queued)
	}
	todayKey := testTenantID + ":calendar.reminder:" + eventID + ":" + calendarBusinessDate(time.Now().In(biztime.DefaultLocation()))
	assertCount(t, ctx, pool, "daily reminder key", `
SELECT count(*)
FROM notification_requests
WHERE tenant_id=$1::uuid
  AND calendar_event_id=$2
  AND notification_type='reminder'
  AND idempotency_key=$3`, 1, testTenantID, eventID, todayKey)
	replay, err := repo.SweepDueReminders(ctx, testTenantID, 10)
	if err != nil {
		t.Fatalf("SweepDueReminders same-day replay: %v", err)
	}
	if replay != 0 {
		t.Fatalf("same-day replay queued %d reminders, want 0", replay)
	}
	assertCount(t, ctx, pool, "daily reminder history", `
SELECT count(*)
FROM notification_requests
WHERE tenant_id=$1::uuid
  AND calendar_event_id=$2
  AND notification_type='reminder'`, 2, testTenantID, eventID)
}

func TestCalendarPostgresAppliesParkShedScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolID := "86000000-0000-4000-8000-000000001111"
	versionID := "86000000-0000-4000-8000-000000001112"
	ruleID := "86000000-0000-4000-8000-000000001113"
	obligationA := "86000000-0000-4000-8000-00000000a101"
	batchA := "86000000-0000-4000-8000-000000001121"
	obligationB := "86000000-0000-4000-8000-00000000a102"
	batchB := "86000000-0000-4000-8000-000000001122"
	dueAt := time.Now().UTC().Add(2 * time.Hour)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationA, dueAt)
	seedVaccinationBatchForShed(t, ctx, pool, batchA, versionID, testParkA, testShedA, dueAt, obligationA)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, obligationB, dueAt)
	seedVaccinationBatchForShed(t, ctx, pool, batchB, versionID, testParkB, testShedB, dueAt, obligationB)
	// CR-002/CR-003: the stable park-drive identity, not batch:<id>.
	eventA := parkDriveEventID(testParkA, dueAt)
	eventB := parkDriveEventID(testParkB, dueAt)

	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID:             testTenantID,
		OwnerKey:             domain.OwnerAll,
		DateFrom:             time.Now().UTC().Add(-24 * time.Hour),
		DateTo:               time.Now().UTC().Add(24 * time.Hour),
		Limit:                20,
		Scope:                domain.ScopeFilter{ParkIDs: []string{testParkA}},
		IncludeFilterOptions: true,
	})
	if err != nil {
		t.Fatalf("ListEvents park scope: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].EventID != eventA {
		t.Fatalf("park scoped list = %#v, want only %s", list.Items, eventA)
	}
	if list.FilterOptions == nil {
		t.Fatal("park scoped filter options are nil")
	}
	if len(list.FilterOptions.Parks) != 1 || list.FilterOptions.Parks[0].Value != testParkA {
		t.Fatalf("park scoped options = %#v, want only %s", list.FilterOptions.Parks, testParkA)
	}
	if len(list.FilterOptions.Sheds) != 1 ||
		list.FilterOptions.Sheds[0].Value != testShedA ||
		list.FilterOptions.Sheds[0].ParentValue == nil ||
		*list.FilterOptions.Sheds[0].ParentValue != testParkA {
		t.Fatalf("shed scoped options = %#v, want only %s under %s", list.FilterOptions.Sheds, testShedA, testParkA)
	}
	vaccine := "Projection Test Vaccine"
	vaccineList, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		Vaccine:  &vaccine,
		DateFrom: time.Now().UTC().Add(-24 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    20,
		Scope:    domain.ScopeFilter{ParkIDs: []string{testParkA}},
	})
	if err != nil {
		t.Fatalf("ListEvents vaccine filter: %v", err)
	}
	if len(vaccineList.Items) != 1 || vaccineList.Items[0].EventID != eventA {
		t.Fatalf("vaccine scoped list = %#v, want only %s", vaccineList.Items, eventA)
	}
	unknownVaccine := "Not a published vaccine"
	emptyList, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		Vaccine:  &unknownVaccine,
		DateFrom: time.Now().UTC().Add(-24 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    20,
		Scope:    domain.ScopeFilter{ParkIDs: []string{testParkA}},
	})
	if err != nil {
		t.Fatalf("ListEvents unknown vaccine filter: %v", err)
	}
	if len(emptyList.Items) != 0 {
		t.Fatalf("unknown vaccine list = %#v, want empty", emptyList.Items)
	}

	firstPage, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: time.Now().UTC().Add(-24 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    1,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents first page: %v", err)
	}
	if len(firstPage.Items) != 1 || firstPage.NextCursor == nil {
		t.Fatalf("first page items=%d next=%v, want one row and continuation", len(firstPage.Items), firstPage.NextCursor)
	}
	cursor, err := domain.DecodeCalendarCursor(*firstPage.NextCursor)
	if err != nil {
		t.Fatalf("decode first page cursor: %v", err)
	}
	secondPage, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: time.Now().UTC().Add(-24 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Cursor:   &cursor,
		Limit:    1,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents second page: %v", err)
	}
	if len(secondPage.Items) != 1 || secondPage.Items[0].EventID == firstPage.Items[0].EventID {
		t.Fatalf("second page = %#v, want a distinct continuation row", secondPage.Items)
	}
	if _, err := repo.GetEventDetail(ctx, domain.EventQuery{
		TenantID: testTenantID,
		EventID:  eventB,
		Scope:    domain.ScopeFilter{ParkIDs: []string{testParkA}},
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("cross-park detail err = %v, want ErrNotFound", err)
	}
	if _, err := repo.SendNudge(ctx, ports.SendNudge{
		TenantID: testTenantID, EventID: eventB, ActorID: testActorID,
		IdempotencyKey: "scoped-nudge-cross-park", Scope: domain.ScopeFilter{ParkIDs: []string{testParkA}},
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("cross-park nudge err = %v, want ErrNotFound", err)
	}

	// vaccination_drive list/detail rows are park-level aggregates -- park_drive_events always emits
	// shed_id = NULL (even for a single-shed, drive_count=1 pass-through), so shed-level ScopeFilter
	// enforcement can't be proven against a batch/drive event at the list level. Prove it instead
	// against an individual, unbatched obligation, which DOES carry a real shed_id (obligation_events'
	// own location join). This is necessarily a detail-only lookup: obligation-typed events are
	// deliberately excluded from ListEvents (event_type <> 'vaccination_dose_due').
	shedObligationID := "86000000-0000-4000-8000-00000000a103"
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, shedObligationID, dueAt)
	attachObligationToGoatScope(t, ctx, pool, shedObligationID, shedObligationID, "shed", testShedB)
	shedEventID := "obligation:" + shedObligationID
	if _, err := repo.GetEventDetail(ctx, domain.EventQuery{
		TenantID: testTenantID, EventID: shedEventID, Scope: domain.ScopeFilter{ShedIDs: []string{testShedB}},
	}); err != nil {
		t.Fatalf("GetEventDetail shed scope: %v", err)
	}
	if _, err := repo.GetEventDetail(ctx, domain.EventQuery{
		TenantID: testTenantID, EventID: shedEventID, Scope: domain.ScopeFilter{ShedIDs: []string{testShedA}},
	}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("cross-shed detail err = %v, want ErrNotFound", err)
	}
}

func TestCalendarHistoryVaccineOneToManyStatusBucketsStayCardinalitySafe(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	const (
		protocolID   = "86000000-0000-4000-8000-00000000f101"
		versionID    = "86000000-0000-4000-8000-00000000f102"
		ruleID       = "86000000-0000-4000-8000-00000000f103"
		obligationID = "86000000-0000-4000-8000-00000000f104"
		goatID       = "86000000-0000-4000-8000-00000000f105"
		completionID = "86000000-0000-4000-8000-00000000f106"
	)
	dueAt := time.Now().UTC()
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, dueAt)
	attachObligationToGoatScope(t, ctx, pool, obligationID, goatID, "shed", testShedA)
	seedVaccinationCompletion(t, ctx, pool, obligationID, goatID, completionID)
	seedProtocolRuleDimension(t, ctx, pool, versionID, ruleID, "fmd", "FMD")
	seedProtocolRuleDimension(t, ctx, pool, versionID, ruleID, "ppr", "PPR")

	vaccine := "FMD"
	status := "completed"
	response, err := repo.ListEvents(ctx, domain.Query{
		TenantID:           testTenantID,
		OwnerKey:           domain.OwnerAll,
		Status:             &status,
		Vaccine:            &vaccine,
		DateFrom:           dueAt.Add(-24 * time.Hour),
		DateTo:             dueAt.Add(24 * time.Hour),
		Limit:              20,
		Scope:              domain.ScopeFilter{TenantWide: true},
		MarkersOnly:        true,
		IncludeDateMarkers: true,
	})
	if err != nil {
		t.Fatalf("ListEvents completed FMD markers: %v", err)
	}
	var eventCount, completedCount int
	for _, marker := range response.DateMarkers {
		eventCount += marker.EventCount
		completedCount += marker.CompletedCount
	}
	if eventCount != 1 || completedCount != 1 {
		t.Fatalf(
			"two rule dimensions fanned one completion into markers: events=%d completed=%d markers=%#v",
			eventCount,
			completedCount,
			response.DateMarkers,
		)
	}
}

func TestCalendarListExcludesClosedEventsByDefault(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolID := "86000000-0000-4000-8000-000000000791"
	versionID := "86000000-0000-4000-8000-000000000792"
	ruleID := "86000000-0000-4000-8000-000000000793"
	dueAt := time.Now().UTC().Add(2 * time.Hour)
	activeObligation := "86000000-0000-4000-8000-000000000794"
	completedObligation := "86000000-0000-4000-8000-000000000795"
	canceledObligation := "86000000-0000-4000-8000-000000000796"
	activeBatch := "86000000-0000-4000-8000-000000000797"
	completedBatch := "86000000-0000-4000-8000-000000000798"
	canceledBatch := "86000000-0000-4000-8000-000000000799"
	completedParkID := "86000000-0000-4000-8000-0000000007a1"
	completedShedID := "86000000-0000-4000-8000-0000000007a2"
	canceledParkID := "86000000-0000-4000-8000-0000000007a3"
	canceledShedID := "86000000-0000-4000-8000-0000000007a4"

	// Three real canonical batches, each in its own park (so the park-day grouping keeps them as
	// separate list items): one left open (planned/scheduled), one marked completed, one marked
	// superseded (canceled). batch_events maps ob.status='planned'->'scheduled', 'completed'->
	// 'completed', and excludes 'superseded'/'canceled' batches from source_events entirely -- so the
	// canceled batch never reaches canonical_selected, by default or under any status filter.
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, activeObligation, dueAt)
	seedVaccinationBatchForShed(t, ctx, pool, activeBatch, versionID, testParkA, testShedA, dueAt, activeObligation)
	// CR-002/CR-003: the stable park-drive identity, not batch:<id>.
	activeID := parkDriveEventID(testParkA, dueAt)

	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, completedObligation, dueAt.Add(time.Minute))
	seedVaccinationBatchForShed(t, ctx, pool, completedBatch, versionID, completedParkID, completedShedID, dueAt.Add(time.Minute), completedObligation)
	completedID := parkDriveEventID(completedParkID, dueAt.Add(time.Minute))
	if _, err := pool.Exec(ctx, `
UPDATE obligation_batches SET status='completed', updated_at=now()
WHERE tenant_id=$1::uuid AND batch_id=$2::uuid`, testTenantID, completedBatch); err != nil {
		t.Fatalf("mark batch completed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances SET status='completed', completed_at=now(), updated_at=now()
WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid`, testTenantID, completedObligation); err != nil {
		t.Fatalf("mark obligation completed: %v", err)
	}

	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, canceledObligation, dueAt.Add(2*time.Minute))
	seedVaccinationBatchForShed(t, ctx, pool, canceledBatch, versionID, canceledParkID, canceledShedID, dueAt.Add(2*time.Minute), canceledObligation)
	if _, err := pool.Exec(ctx, `
UPDATE obligation_batches SET status='superseded', updated_at=now()
WHERE tenant_id=$1::uuid AND batch_id=$2::uuid`, testTenantID, canceledBatch); err != nil {
		t.Fatalf("mark batch canceled: %v", err)
	}

	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		// R50-012: a batch's due_at is now derived from planned_date via an explicit
		// `AT TIME ZONE 'Asia/Kolkata'` conversion (midnight of the Asia/Kolkata business
		// day), never a session-timezone-dependent bare cast. That instant can legitimately
		// fall up to ~24h before this test's dueAt (a business day starting near midnight
		// Kolkata for a dueAt scheduled late in that same Kolkata day), so the lower window
		// bound must cover a full day, not a narrow "-1h" buffer tied to wall-clock "now".
		DateFrom: time.Now().UTC().Add(-25 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    20,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents default: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].EventID != activeID {
		t.Fatalf("default list items = %#v, want only active event", list.Items)
	}
	completed := domain.StatusCompleted
	list, err = repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		Status:   &completed,
		DateFrom: time.Now().UTC().Add(-25 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    20,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents completed: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].EventID != completedID {
		t.Fatalf("completed list items = %#v, want completed event", list.Items)
	}
}

func TestCalendarVaccinationProjectionRefreshBackfillsObligations(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolID := "86000000-0000-4000-8000-000000000801"
	versionID := "86000000-0000-4000-8000-000000000802"
	ruleID := "86000000-0000-4000-8000-000000000803"
	obligationID := "86000000-0000-4000-8000-000000000804"
	dueAt := time.Now().UTC().Add(4 * time.Hour)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, dueAt)
	projectionFrom := time.Now().UTC().Add(-time.Hour)
	projectionTo := time.Now().UTC().Add(48 * time.Hour)

	// 5k-50k envelope: canonical is the only serving path -- no refresh step. The individual
	// obligation resolves directly by event_id (GetEventDetail bypasses the list-only
	// vaccination_dose_due exclusion)...
	detail, err := repo.GetEventDetail(ctx, domain.EventQuery{
		TenantID: testTenantID, EventID: "obligation:" + obligationID, Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("GetEventDetail obligation: %v", err)
	}
	if detail.Event.EventID != "obligation:"+obligationID || !detail.Event.SourceBacked {
		t.Fatalf("obligation detail event_id=%s source_backed=%t", detail.Event.EventID, detail.Event.SourceBacked)
	}
	// ...while the list view groups it into a visible catch-up drive summary.
	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: projectionFrom,
		DateTo:   projectionTo.Add(-24 * time.Hour),
		Limit:    20,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	// CR-002/CR-003: the stable tenant-wide park-drive identity, not catchup:tenant:...
	if len(list.Items) != 1 || list.Items[0].EventID != parkDriveTenantEventID(testTenantID, dueAt) || list.Items[0].EventType != domain.EventVaccinationDrive {
		t.Fatalf("list items=%#v, want one visible catch-up drive", list.Items)
	}
}

func TestCalendarVaccinationProjectionCollapsesBatchedGoatDosesToDrive(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolID := "86000000-0000-4000-8000-000000000a01"
	versionID := "86000000-0000-4000-8000-000000000a02"
	ruleID := "86000000-0000-4000-8000-000000000a03"
	batchID := "86000000-0000-4000-8000-000000000a04"
	obligationIDs := []string{
		"86000000-0000-4000-8000-000000000a05",
		"86000000-0000-4000-8000-000000000a06",
	}
	dueAt := time.Now().UTC().Add(4 * time.Hour)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationIDs[0], dueAt)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, obligationIDs[1], dueAt)
	for _, obligationID := range obligationIDs {
		attachObligationToGoatScope(t, ctx, pool, obligationID, obligationID, "shed", testShedA)
	}

	// Pre-batch: canonical is live, no refresh -- both obligations resolve individually (unbatched,
	// batch_id IS NULL keeps them in obligation_events).
	for _, obligationID := range obligationIDs {
		detail, err := repo.GetEventDetail(ctx, domain.EventQuery{
			TenantID: testTenantID, EventID: "obligation:" + obligationID, Scope: domain.ScopeFilter{TenantWide: true},
		})
		if err != nil {
			t.Fatalf("GetEventDetail pre-batch obligation %s: %v", obligationID, err)
		}
		if detail.Event.EventID != "obligation:"+obligationID {
			t.Fatalf("pre-batch obligation detail=%#v", detail.Event)
		}
	}

	seedVaccinationBatch(t, ctx, pool, batchID, versionID, dueAt, obligationIDs...)

	// Post-batch: batch_id is no longer NULL, so obligation_events excludes both rows -- they now
	// surface only via the single collapsed batch drive.
	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll,
		DateFrom: dueAt.Add(-time.Hour), DateTo: dueAt.Add(24 * time.Hour), Limit: 20,
		Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents after batch: %v", err)
	}
	// CR-002/CR-003: the stable park-drive identity, not batch:<id>.
	wantID := parkDriveEventID(testParkA, dueAt)
	if len(list.Items) != 1 || list.Items[0].EventID != wantID ||
		list.Items[0].EventType != domain.EventVaccinationDrive || list.Items[0].TargetCount != len(obligationIDs) {
		t.Fatalf("list after batch=%#v, want one batch drive with %d goats", list.Items, len(obligationIDs))
	}
	assertDriveTargets(t, ctx, repo, wantID, obligationIDs)
}

func TestCalendarVaccinationProjectionGroupsMultipleShedsAndVaccinesIntoOneAllDayParkDrive(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	dueAt := stableSameLocalDayDueAt(time.Now().In(biztime.DefaultLocation()))
	const (
		protocolA   = "86000000-0000-4000-8000-00000000c101"
		versionA    = "86000000-0000-4000-8000-00000000c102"
		ruleA       = "86000000-0000-4000-8000-00000000c103"
		batchA      = "86000000-0000-4000-8000-00000000c104"
		obligationA = "86000000-0000-4000-8000-00000000c105"
		animalA     = "86000000-0000-4000-8000-00000000c106"
		protocolB   = "86000000-0000-4000-8000-00000000c201"
		versionB    = "86000000-0000-4000-8000-00000000c202"
		ruleB       = "86000000-0000-4000-8000-00000000c203"
		batchB      = "86000000-0000-4000-8000-00000000c204"
		obligationB = "86000000-0000-4000-8000-00000000c205"
		animalB     = "86000000-0000-4000-8000-00000000c206"
	)
	seedVaccinationObligation(t, ctx, pool, protocolA, versionA, ruleA, obligationA, dueAt)
	attachObligationToGoatScope(t, ctx, pool, obligationA, animalA, "shed", testShedA)
	setCalendarGoatCurrentShed(t, ctx, pool, animalA, testParkA, testShedA)
	seedProtocolRuleVaccineName(t, ctx, pool, versionA, ruleA, "ET+TT")
	seedVaccinationBatchForShed(t, ctx, pool, batchA, versionA, testParkA, testShedA, dueAt, obligationA)

	// CR-002/CR-003 (calendar-canonical-5k50k review): a solo source for the park/day now ALREADY
	// carries the STABLE parkdrive:park:...:date:... identity -- never the underlying batch's own
	// batch:<id> -- so the identity never mutates once a second source joins the same park+day below,
	// and existing snooze/reminder/escalation state keyed on it never detaches (see
	// TestCalendarParkDriveIdentityAndSnoozeStateSurviveMembershipChange for the state-survival case).
	wantID := parkDriveEventID(testParkA, dueAt)
	soloList, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll, DateFrom: dueAt.Add(-time.Hour), DateTo: dueAt.Add(24 * time.Hour), Limit: 20,
		Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents single batch: %v", err)
	}
	if len(soloList.Items) != 1 || soloList.Items[0].EventID != wantID || soloList.Items[0].Status == domain.StatusCanceled {
		t.Fatalf("ListEvents single batch=%#v, want stable park-drive event %s", soloList.Items, wantID)
	}

	// CR-002: the stable park-drive id must resolve on BOTH GetEventDetail and ListDriveTargets --
	// not just ListEvents -- even for a solo (drive_count = 1) drive.
	soloDetail, err := repo.GetEventDetail(ctx, domain.EventQuery{
		TenantID: testTenantID, EventID: wantID, Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("GetEventDetail solo park drive: %v", err)
	}
	if soloDetail.Event.EventID != wantID || soloDetail.Event.TargetCount != 1 {
		t.Fatalf("GetEventDetail solo park drive=%#v, want id=%s targets=1", soloDetail.Event, wantID)
	}
	assertDriveTargets(t, ctx, repo, wantID, []string{animalA})
	assertDriveTargetSheds(t, ctx, repo, wantID, map[string]string{
		animalA: "Test Shed 0711",
	})

	seedVaccinationObligation(t, ctx, pool, protocolB, versionB, ruleB, obligationB, dueAt.Add(10*time.Minute))
	attachObligationToGoatScope(t, ctx, pool, obligationB, animalB, "shed", testShedB)
	seedProtocolRuleVaccineName(t, ctx, pool, versionB, ruleB, "PPR")
	seedVaccinationBatchForShed(t, ctx, pool, batchB, versionB, testParkA, testShedB, dueAt.Add(10*time.Minute), obligationB)
	// Real seeded park-drive rows can have no goat.current_location_id/shed_id and a stale/park
	// obligation scope after batching. The roster still must display the animal's shed from the
	// member batch, not a dash.
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET scope_type = 'park',
    scope_id = $3::uuid,
    updated_at = now()
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`,
		testTenantID, obligationB, testParkA); err != nil {
		t.Fatalf("drift obligation scope after batching: %v", err)
	}

	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll, DateFrom: dueAt.Add(-time.Hour), DateTo: dueAt.Add(24 * time.Hour), Limit: 20,
		Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents grouped park drive: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("ListEvents grouped park drive items=%#v", list.Items)
	}
	got := list.Items[0]
	if got.EventID != wantID || got.TargetCount != 2 || got.ShedCount != 2 || got.VaccineCount != 2 || got.DriveCount != 2 {
		t.Fatalf("grouped drive=%#v, want id=%s targets=2 sheds=2 vaccines=2 packets=2", got, wantID)
	}
	if !got.AllDay || !got.Aggregated {
		t.Fatalf("ListEvents summary=%#v, want all-day aggregated park drive", got)
	}
	if !slices.Equal(got.ShedLabels, []string{"Test Shed 0711", "Test Shed 0712"}) || !slices.Equal(got.VaccineLabels, []string{"ET+TT", "PPR"}) {
		t.Fatalf("grouped labels sheds=%v vaccines=%v", got.ShedLabels, got.VaccineLabels)
	}

	// CR-002 (the primary multi-shed/multi-vaccine flow): once a second batch aggregates onto the
	// SAME park-drive identity, GetEventDetail and ListDriveTargets must both still succeed (not
	// 400 invalid_event_id) against that generated id, and the roster must span EVERY member batch.
	detail, err := repo.GetEventDetail(ctx, domain.EventQuery{
		TenantID: testTenantID, EventID: wantID, Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("GetEventDetail aggregated park drive: %v", err)
	}
	if detail.Event.EventID != wantID || detail.Event.TargetCount != 2 || detail.Event.ShedCount != 2 {
		t.Fatalf("GetEventDetail aggregated park drive=%#v, want id=%s targets=2 sheds=2", detail.Event, wantID)
	}
	if detail.Event.DriveSummary == nil || detail.Event.DriveSummary.TotalCount != 2 || len(detail.Event.DriveSummary.Sheds) != 2 {
		t.Fatalf("GetEventDetail aggregated park drive_summary=%#v, want total=2 sheds=2", detail.Event.DriveSummary)
	}
	assertDriveTargets(t, ctx, repo, wantID, []string{animalA, animalB})
	assertDriveTargetSheds(t, ctx, repo, wantID, map[string]string{
		animalA: "Test Shed 0711",
		animalB: "Test Shed 0712",
	})
}

func TestCalendarParkDriveTargetsIncludeParkScopedBatchMembers(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	dueAt := time.Date(2026, 8, 2, 6, 0, 0, 0, time.UTC)
	const (
		protocolID  = "86000000-0000-4000-8000-00000000d201"
		versionID   = "86000000-0000-4000-8000-00000000d202"
		ruleID      = "86000000-0000-4000-8000-00000000d203"
		obligationA = "86000000-0000-4000-8000-00000000d204"
		obligationB = "86000000-0000-4000-8000-00000000d205"
		animalA     = "86000000-0000-4000-8000-00000000d206"
		animalB     = "86000000-0000-4000-8000-00000000d207"
		batchID     = "86000000-0000-4000-8000-00000000d208"
	)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationA, dueAt)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, obligationB, dueAt)
	attachObligationToGoatScope(t, ctx, pool, obligationA, animalA, "shed", testShedA)
	attachObligationToGoatScope(t, ctx, pool, obligationB, animalB, "shed", testShedB)
	setCalendarGoatCurrentShed(t, ctx, pool, animalA, testParkA, testShedA)
	setCalendarGoatCurrentShed(t, ctx, pool, animalB, testParkA, testShedB)
	seedVaccinationBatchForPark(t, ctx, pool, batchID, versionID, testParkA, dueAt, obligationA, obligationB)

	eventID := parkDriveEventID(testParkA, dueAt)
	assertDriveTargets(t, ctx, repo, eventID, []string{animalA, animalB})
	assertDriveTargetSheds(t, ctx, repo, eventID, map[string]string{
		animalA: "Test Shed 0711",
		animalB: "Test Shed 0712",
	})
}

func TestCalendarDefaultListKeepsPlannedDriveWhenSameDayCatchupDeferred(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	driveDate := time.Date(2026, 8, 5, 6, 0, 0, 0, time.UTC)
	const (
		protocolID  = "86000000-0000-4000-8000-00000000d301"
		versionID   = "86000000-0000-4000-8000-00000000d302"
		ruleID      = "86000000-0000-4000-8000-00000000d303"
		batchedOb   = "86000000-0000-4000-8000-00000000d304"
		deferredOb  = "86000000-0000-4000-8000-00000000d305"
		batchedGoat = "86000000-0000-4000-8000-00000000d306"
		deferGoat   = "86000000-0000-4000-8000-00000000d307"
		batchID     = "86000000-0000-4000-8000-00000000d308"
	)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, batchedOb, driveDate)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, deferredOb, driveDate)
	attachObligationToGoatScope(t, ctx, pool, batchedOb, batchedGoat, "shed", testShedA)
	attachObligationToGoatScope(t, ctx, pool, deferredOb, deferGoat, "shed", testShedA)
	seedVaccinationBatchForPark(t, ctx, pool, batchID, versionID, testParkA, driveDate, batchedOb)
	setDriveObligationStatus(t, ctx, pool, deferredOb, "deferred")

	resp, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: driveDate.Add(-time.Hour),
		DateTo:   driveDate.Add(24 * time.Hour),
		Limit:    20,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	var drive *domain.CalendarEvent
	for i := range resp.Items {
		if resp.Items[i].EventType == domain.EventVaccinationDrive {
			drive = &resp.Items[i]
			break
		}
	}
	if drive == nil {
		t.Fatalf("planned drive disappeared from default list; items=%#v", resp.Items)
	}
	if drive.Status != domain.StatusScheduled {
		t.Fatalf("drive status=%s, want scheduled while same-day deferred_count remains summary-only", drive.Status)
	}
	if drive.ScheduledCount != 1 || drive.DriveSummary == nil || drive.DriveSummary.DeferredCount != 1 {
		t.Fatalf("drive scheduled_count=%d summary=%#v, want scheduled_count=1 deferred_count=1", drive.ScheduledCount, drive.DriveSummary)
	}
}

func TestCalendarVaccinationProjectionDoesNotReclassifyDeferredCatchupAsOverdue(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	dueAt := stableSameLocalDayDueAt(time.Now().In(biztime.DefaultLocation())).Add(-2 * time.Hour)
	const (
		protocolID   = "86000000-0000-4000-8000-00000000c301"
		versionID    = "86000000-0000-4000-8000-00000000c302"
		ruleID       = "86000000-0000-4000-8000-00000000c303"
		obligationID = "86000000-0000-4000-8000-00000000c304"
	)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, dueAt)
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET status='deferred', updated_at=now()
WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid`, testTenantID, obligationID); err != nil {
		t.Fatalf("defer obligation: %v", err)
	}
	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll,
		DateFrom: dueAt.Add(-time.Hour), DateTo: dueAt.Add(24 * time.Hour), Limit: 20,
		Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	var drives []domain.CalendarEvent
	for _, item := range list.Items {
		if item.EventType == domain.EventVaccinationDrive {
			drives = append(drives, item)
		}
	}
	if len(drives) != 1 || drives[0].Status != domain.StatusDeferred {
		t.Fatalf("drives=%#v, want exactly one deferred drive (never reclassified overdue)", drives)
	}
}

func TestCalendarVaccinationProjectionCollapsesMultipleRulesIntoSingleCatchupDrive(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolIDs := []string{
		"86000000-0000-4000-8000-000000000a21",
		"86000000-0000-4000-8000-000000000a27",
	}
	versionIDs := []string{
		"86000000-0000-4000-8000-000000000a22",
		"86000000-0000-4000-8000-000000000a28",
	}
	ruleIDs := []string{
		"86000000-0000-4000-8000-000000000a23",
		"86000000-0000-4000-8000-000000000a24",
	}
	obligationIDs := []string{
		"86000000-0000-4000-8000-000000000a25",
		"86000000-0000-4000-8000-000000000a26",
	}
	dueAt := stableSameLocalDayDueAt(time.Now().In(biztime.DefaultLocation()))
	seedVaccinationObligation(t, ctx, pool, protocolIDs[0], versionIDs[0], ruleIDs[0], obligationIDs[0], dueAt)
	seedVaccinationObligation(t, ctx, pool, protocolIDs[1], versionIDs[1], ruleIDs[1], obligationIDs[1], dueAt.Add(15*time.Minute))
	for _, obligationID := range obligationIDs {
		attachObligationToGoatScope(t, ctx, pool, obligationID, obligationID, "tenant", testTenantID)
	}

	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll,
		DateFrom: dueAt.Add(-time.Hour), DateTo: dueAt.Add(24 * time.Hour), Limit: 20,
		Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	// CR-002/CR-003: the stable tenant-wide park-drive identity, not catchup:tenant:...
	if len(list.Items) != 1 || list.Items[0].EventID != parkDriveTenantEventID(testTenantID, dueAt) ||
		list.Items[0].EventType != domain.EventVaccinationDrive || list.Items[0].Status == domain.StatusCanceled ||
		list.Items[0].TargetCount != 2 {
		t.Fatalf("list=%#v, want one multi-rule catchup drive with target_count=2", list.Items)
	}
}

func TestCalendarCatchupDriveTargetsUseVaccinationAnimalAndQueueSemantics(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	dueAt := stableSameLocalDayDueAt(time.Now().In(biztime.DefaultLocation()))
	vaccinatedGoatID := "86000000-0000-4000-8000-000000000b01"
	feedGoatID := "86000000-0000-4000-8000-000000000b02"
	obligationIDs := []string{
		"86000000-0000-4000-8000-000000000b11",
		"86000000-0000-4000-8000-000000000b12",
	}
	seedVaccinationObligation(t, ctx, pool,
		"86000000-0000-4000-8000-000000000b21",
		"86000000-0000-4000-8000-000000000b22",
		"86000000-0000-4000-8000-000000000b23",
		obligationIDs[0],
		dueAt,
	)
	seedVaccinationObligation(t, ctx, pool,
		"86000000-0000-4000-8000-000000000b31",
		"86000000-0000-4000-8000-000000000b32",
		"86000000-0000-4000-8000-000000000b33",
		obligationIDs[1],
		dueAt.Add(15*time.Minute),
	)
	for _, obligationID := range obligationIDs {
		attachObligationToGoatScope(t, ctx, pool, obligationID, vaccinatedGoatID, "park", testParkA)
	}
	seedNonVaccinationGoatObligation(t, ctx, pool,
		"86000000-0000-4000-8000-000000000b41",
		"86000000-0000-4000-8000-000000000b42",
		"86000000-0000-4000-8000-000000000b43",
		"86000000-0000-4000-8000-000000000b44",
		feedGoatID,
		testParkA,
		dueAt.Add(30*time.Minute),
	)

	// CR-002/CR-003: the stable park-drive identity, not catchup:park:...
	catchupID := parkDriveEventID(testParkA, dueAt)
	detail, err := repo.GetEventDetail(ctx, domain.EventQuery{
		TenantID: testTenantID, EventID: catchupID, Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("GetEventDetail catch-up: %v", err)
	}
	var summary map[string]any
	if err := json.Unmarshal(detail.Summary, &summary); err != nil {
		t.Fatalf("decode catch-up summary: %v", err)
	}
	var notificationPolicy map[string]any
	if err := json.Unmarshal(detail.NotificationPolicy, &notificationPolicy); err != nil {
		t.Fatalf("decode catch-up notification policy: %v", err)
	}
	queueCount, _ := summary["queue_count"].(float64)
	nudgeAllowed, _ := notificationPolicy["nudge_allowed"].(bool)
	if detail.Event.TargetCount != 1 || int(queueCount) != 2 || nudgeAllowed {
		t.Fatalf("catch-up detail=%#v summary=%v policy=%v, want 1 animal, 2 queues, read-only", detail.Event, summary, notificationPolicy)
	}
	targets, err := repo.ListDriveTargets(ctx, domain.DriveTargetQuery{
		TenantID: testTenantID,
		EventID:  catchupID,
		Scope:    domain.ScopeFilter{TenantWide: true},
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListDriveTargets: %v", err)
	}
	if len(targets.Items) != 1 || targets.Items[0].AnimalID != vaccinatedGoatID {
		t.Fatalf("targets=%#v, want one vaccinated goat and no feed-direction obligation", targets.Items)
	}
	if _, err := repo.SendNudge(ctx, ports.SendNudge{
		TenantID: testTenantID, EventID: catchupID, ActorID: testActorID,
		IdempotencyKey: "calendar-catchup-nudge-blocked", Channel: "local-stub",
		Message: "direct catch-up nudge should not queue", Scope: domain.ScopeFilter{TenantWide: true},
	}); !errors.Is(err, ports.ErrEventNotActionable) {
		t.Fatalf("SendNudge catch-up err = %v, want ErrEventNotActionable", err)
	}
	if _, err := repo.Snooze(ctx, ports.Snooze{
		TenantID: testTenantID, EventID: catchupID, ActorID: testActorID,
		IdempotencyKey: "calendar-catchup-snooze-blocked",
		SnoozeUntil:    time.Now().UTC().Add(2 * time.Hour),
		Reason:         "direct catch-up snooze should not queue",
		Scope:          domain.ScopeFilter{TenantWide: true},
	}); !errors.Is(err, ports.ErrEventNotActionable) {
		t.Fatalf("Snooze catch-up err = %v, want ErrEventNotActionable", err)
	}
}

func TestCalendarReminderSweepTargetsCatchupSummaryNotHiddenDose(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	obligationID := "86000000-0000-4000-8000-000000000b51"
	dueAt := time.Now().UTC().Add(30 * time.Minute)
	seedVaccinationObligation(t, ctx, pool,
		"86000000-0000-4000-8000-000000000b52",
		"86000000-0000-4000-8000-000000000b53",
		"86000000-0000-4000-8000-000000000b54",
		obligationID,
		dueAt,
	)
	attachObligationToGoatScope(t, ctx, pool, obligationID, "86000000-0000-4000-8000-000000000b55", "tenant", testTenantID)
	queued, err := repo.SweepDueReminders(ctx, testTenantID, 10)
	if err != nil {
		t.Fatalf("SweepDueReminders: %v", err)
	}
	if queued != 1 {
		t.Fatalf("queued reminders = %d, want visible catch-up summary only", queued)
	}
	// CR-002/CR-003: the stable tenant-wide park-drive identity, not catchup:tenant:...
	catchupID := parkDriveTenantEventID(testTenantID, dueAt)
	assertCount(t, ctx, pool, "catch-up reminder request", `
SELECT count(*)
FROM notification_requests
WHERE tenant_id=$1::uuid
  AND calendar_event_id=$2
  AND notification_type='reminder'`, 1, testTenantID, catchupID)
	assertCount(t, ctx, pool, "hidden dose reminder skipped", `
SELECT count(*)
FROM notification_requests
WHERE tenant_id=$1::uuid
  AND calendar_event_id=$2
  AND notification_type='reminder'`, 0, testTenantID, "obligation:"+obligationID)
}

func TestCalendarVaccinationProjectionCollapsesMultipleRulesInBatchIntoSingleDrive(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolIDs := []string{
		"86000000-0000-4000-8000-000000000a31",
		"86000000-0000-4000-8000-000000000a38",
	}
	versionIDs := []string{
		"86000000-0000-4000-8000-000000000a32",
		"86000000-0000-4000-8000-000000000a39",
	}
	batchID := "86000000-0000-4000-8000-000000000a33"
	ruleIDs := []string{
		"86000000-0000-4000-8000-000000000a34",
		"86000000-0000-4000-8000-000000000a35",
	}
	obligationIDs := []string{
		"86000000-0000-4000-8000-000000000a36",
		"86000000-0000-4000-8000-000000000a37",
	}
	dueAt := stableSameLocalDayDueAt(time.Now().In(biztime.DefaultLocation()))
	seedVaccinationObligation(t, ctx, pool, protocolIDs[0], versionIDs[0], ruleIDs[0], obligationIDs[0], dueAt)
	seedVaccinationObligation(t, ctx, pool, protocolIDs[1], versionIDs[1], ruleIDs[1], obligationIDs[1], dueAt.Add(15*time.Minute))
	seedVaccinationBatch(t, ctx, pool, batchID, versionIDs[0], dueAt, obligationIDs...)

	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll,
		DateFrom: dueAt.Add(-time.Hour), DateTo: dueAt.Add(24 * time.Hour), Limit: 20,
		Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	// CR-002/CR-003: the stable park-drive identity, not batch:<id>.
	if len(list.Items) != 1 || list.Items[0].EventID != parkDriveEventID(testParkA, dueAt) ||
		list.Items[0].EventType != domain.EventVaccinationDrive || list.Items[0].Status == domain.StatusCanceled ||
		list.Items[0].TargetCount != 2 {
		t.Fatalf("list=%#v, want one multi-rule batch drive with target_count=2", list.Items)
	}
}

func TestCalendarVaccinationProjectionPreservesMissedStatus(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolID := "86000000-0000-4000-8000-000000000871"
	versionID := "86000000-0000-4000-8000-000000000872"
	ruleID := "86000000-0000-4000-8000-000000000873"
	obligationID := "86000000-0000-4000-8000-000000000874"
	dueAt := time.Now().UTC().Add(-48 * time.Hour)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, dueAt)
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET status = 'missed', updated_at = now()
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`, testTenantID, obligationID); err != nil {
		t.Fatalf("mark obligation missed: %v", err)
	}

	detail, err := repo.GetEventDetail(ctx, domain.EventQuery{
		TenantID: testTenantID, EventID: "obligation:" + obligationID, Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("GetEventDetail: %v", err)
	}
	if detail.Event.Status != domain.StatusMissed {
		t.Fatalf("obligation detail status=%s, want missed", detail.Event.Status)
	}
}

// TestCalendarProjectionAndListIncludeOldMissedExceptions is a canonical-read regression: the
// pre-cutover projector kept an old missed obligation's catch-up drive visible on the Calendar list
// no matter how far its own due_at had drifted behind the requested [dateFrom, dateTo) window (the
// obligation sweeper's 15-minute stage marks it missed once, and it stays a visible open exception
// until resolved, not just for the day it was originally due). catchup_drive_events'/obligation_events'
// own WHERE clauses already bypass the window for status IN ('missed','in_progress','deferred'), but
// canonical_selected's outer due_at window filter used to re-apply the window to EVERY event_type
// uniformly, silently dropping an old catch-up drive the moment "today" moved past its due date. Fixed
// by mirroring the same missed/in_progress/deferred/overdue bypass onto canonical_selected's own
// window predicate for vaccination_drive rows (the only event_type park_drive_events ever projects with
// those aggregated statuses).
func TestCalendarProjectionAndListIncludeOldMissedExceptions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolID := "86000000-0000-4000-8000-000000000881"
	versionID := "86000000-0000-4000-8000-000000000882"
	ruleID := "86000000-0000-4000-8000-000000000883"
	obligationID := "86000000-0000-4000-8000-000000000884"

	oldDueAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, oldDueAt)
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET status = 'missed', updated_at = now()
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`, testTenantID, obligationID); err != nil {
		t.Fatalf("mark obligation missed: %v", err)
	}

	// The requested window is the current day only -- it does NOT include oldDueAt (10 days behind).
	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: time.Now().UTC().Add(-24 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    20,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	// CR-002/CR-003: the stable tenant-wide park-drive identity, not catchup:tenant:...
	eventID := parkDriveTenantEventID(testTenantID, oldDueAt)
	var found *domain.CalendarEvent
	for i := range list.Items {
		if list.Items[i].EventID == eventID {
			found = &list.Items[i]
		}
	}
	if found == nil {
		t.Fatalf("old missed obligation (due %s) not visible in current-window list=%#v, want catch-up drive %s", oldDueAt, list.Items, eventID)
	}
	if found.Status != domain.StatusMissed {
		t.Fatalf("old missed catch-up drive status=%s, want missed", found.Status)
	}
}

// TestCalendarListSurfacesPastDueOpenExceptions covers the sibling out-of-window case: an obligation
// left in its normal open status (never explicitly marked missed) whose due_at has simply fallen far
// behind "now" is still overdue open work -- catchup_drive_events' own
// `(oi.status IN ('scheduled','due') AND oi.due_at < now())` bypass includes it, and the aggregated
// park_drive_events status resolves to 'overdue'. Same canonical_selected fix as
// TestCalendarProjectionAndListIncludeOldMissedExceptions keeps it visible outside the requested
// window instead of only on the day it was originally due.
func TestCalendarListSurfacesPastDueOpenExceptions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolID := "86000000-0000-4000-8000-000000000891"
	versionID := "86000000-0000-4000-8000-000000000892"
	ruleID := "86000000-0000-4000-8000-000000000893"
	obligationID := "86000000-0000-4000-8000-000000000894"

	oldDueAt := time.Now().UTC().Add(-10 * 24 * time.Hour)
	// Left at its natural 'scheduled' status -- due_at < now() makes it overdue open work, no manual
	// status flip needed.
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, oldDueAt)

	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: time.Now().UTC().Add(-24 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    20,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	// CR-002/CR-003: the stable tenant-wide park-drive identity, not catchup:tenant:...
	eventID := parkDriveTenantEventID(testTenantID, oldDueAt)
	var found *domain.CalendarEvent
	for i := range list.Items {
		if list.Items[i].EventID == eventID {
			found = &list.Items[i]
		}
	}
	if found == nil {
		t.Fatalf("past-due open obligation (due %s) not visible in current-window list=%#v, want catch-up drive %s", oldDueAt, list.Items, eventID)
	}
	if found.Status != domain.StatusOverdue {
		t.Fatalf("past-due open catch-up drive status=%s, want overdue", found.Status)
	}
}

func TestCalendarEscalationSweepQueuesNotificationAndObligationEscalation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolID := "86000000-0000-4000-8000-000000000851"
	versionID := "86000000-0000-4000-8000-000000000852"
	ruleID := "86000000-0000-4000-8000-000000000853"
	obligationID := "86000000-0000-4000-8000-000000000854"
	dueAt := time.Now().UTC().Add(-2 * time.Hour)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, dueAt)
	queued, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:    testTenantID,
		Limit:       10,
		Now:         time.Now().In(biztime.DefaultLocation()),
		Level1After: 0,
		Level2After: 4 * time.Hour,
		Level3After: 24 * time.Hour,
		Level4After: 48 * time.Hour,
	})
	if err != nil {
		t.Fatalf("SweepEscalations: %v", err)
	}
	if queued != 1 {
		t.Fatalf("queued escalations = %d, want 1", queued)
	}
	// CR-002/CR-003: the stable tenant-wide park-drive identity, not catchup:tenant:...
	eventID := parkDriveTenantEventID(testTenantID, dueAt)
	assertCount(t, ctx, pool, "escalation notifications", `
SELECT count(*)
FROM notification_requests
WHERE tenant_id=$1::uuid
  AND calendar_event_id=$2
  AND target_type='catchup'
  AND target_id=$3::uuid
  AND notification_type='escalation'
  AND channel='local-stub'
  AND status='queued'`, 1, testTenantID, eventID, testTenantID)
	assertCount(t, ctx, pool, "hidden obligation escalation skipped for catch-up summary", `
SELECT count(*)
FROM obligation_escalations
WHERE tenant_id=$1::uuid
  AND obligation_id=$2::uuid
  AND level=1`, 0, testTenantID, obligationID)

	// status/reminder_state/escalation_state are all read-derived now (no projection row to persist
	// them): overall status comes off the live list item; reminder/escalation labels off the rail.
	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll,
		DateFrom: dueAt.Add(-time.Hour), DateTo: time.Now().UTC().Add(24 * time.Hour),
		Limit: 20, IncludeReminderRail: true, Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].EventID != eventID || list.Items[0].Status != domain.StatusOverdue {
		t.Fatalf("list=%#v, want overdue catch-up drive %s", list.Items, eventID)
	}
	if list.ReminderRail == nil {
		t.Fatalf("reminder rail missing")
	}
	var railItem *domain.CalendarReminderRailItem
	for i := range list.ReminderRail.Items {
		if list.ReminderRail.Items[i].EventID == eventID {
			railItem = &list.ReminderRail.Items[i]
		}
	}
	if railItem == nil || railItem.ReminderLabel != "Reminder escalated" || railItem.EscalationLabel != "Escalated · L1" {
		t.Fatalf("reminder rail item=%#v, want Reminder escalated / Escalated L1", railItem)
	}

	again, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:    testTenantID,
		Limit:       10,
		Now:         time.Now().In(biztime.DefaultLocation()),
		Level1After: 0,
		Level2After: 4 * time.Hour,
		Level3After: 24 * time.Hour,
		Level4After: 48 * time.Hour,
	})
	if err != nil {
		t.Fatalf("SweepEscalations replay: %v", err)
	}
	if again != 0 {
		t.Fatalf("replay queued escalations = %d, want 0", again)
	}
}

// TestCalendarReminderRailDerivesFromCanonicalNotificationsNotProjectionColumns is the U5 (ADR
// operational-kernel-5k-50k-scale-envelope step 5) regression: the reminder rail must derive
// reminder/escalation display state at read time from notification_requests/calendar_snoozes/
// obligation_escalations, never from any persisted reminder_state/escalation_state column --
// calendar_event_projections and its columns are gone entirely (U7), so there is nothing left to
// persist or corrupt; this proves the read-derived labels are correct for each write path.
//
// This does not exercise the obligation_escalations-derived branch of calendarReminderRailSQL
// (the "acknowledged"/individually-dosed obligation path): every 'obligation:'-prefixed event row is
// event_type='vaccination_dose_due' (see the obligation_events CTE), which the rail's own candidate
// filter (event_type <> 'vaccination_dose_due') has always excluded -- and AcknowledgeEscalation/
// ResolveEscalation only ever act on an obligation-typed target (applyEscalationAction's
// target.TargetType != "obligation" guard), so no event that can be acknowledged/resolved was ever
// rail-visible, before or after this change. The obligation_escalations JOIN in the new SQL is
// therefore defensive/future-proofing (in case that coupling changes), not a reachable path today;
// TestCalendarEscalationAcknowledgeAndResolveWorkflow already covers the acknowledge/resolve write
// path itself. The catch-up (composite) escalation case below IS rail-visible and IS the real proof
// that escalation display is derived, not stored.
func TestCalendarReminderRailDerivesFromCanonicalNotificationsNotProjectionColumns(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	findRailItem := func(rail domain.CalendarReminderRail, eventID string) domain.CalendarReminderRailItem {
		t.Helper()
		for _, item := range rail.Items {
			if item.EventID == eventID {
				return item
			}
		}
		t.Fatalf("reminder rail missing event %s, items=%#v", eventID, rail.Items)
		return domain.CalendarReminderRailItem{}
	}

	protocolID := "86000000-0000-4000-8000-000000009011"
	versionID := "86000000-0000-4000-8000-000000009012"
	ruleID := "86000000-0000-4000-8000-000000009013"

	// Queued reminder: derived live from notification_requests(notification_type='reminder'). Each
	// scenario gets its own park so the park-day grouping keeps it independently addressable.
	reminderObligation := "86000000-0000-4000-8000-000000009021"
	reminderBatch := "86000000-0000-4000-8000-000000009022"
	reminderParkID := "86000000-0000-4000-8000-000000009023"
	reminderShedID := "86000000-0000-4000-8000-000000009024"
	reminderDueAt := time.Now().UTC().Add(30 * time.Minute)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, reminderObligation, reminderDueAt)
	seedVaccinationBatchForShed(t, ctx, pool, reminderBatch, versionID, reminderParkID, reminderShedID, reminderDueAt, reminderObligation)
	// CR-002/CR-003: the stable park-drive identity, not batch:<id>.
	reminderEvent := parkDriveEventID(reminderParkID, reminderDueAt)
	if queued, err := repo.SweepDueReminders(ctx, testTenantID, 10); err != nil {
		t.Fatalf("SweepDueReminders: %v", err)
	} else if queued != 1 {
		t.Fatalf("queued reminders = %d, want 1", queued)
	}

	// Nudge overrides the reminder label: derived from notification_requests(notification_type='nudge').
	nudgeObligation := "86000000-0000-4000-8000-000000009031"
	nudgeBatch := "86000000-0000-4000-8000-000000009032"
	nudgeParkID := "86000000-0000-4000-8000-000000009033"
	nudgeShedID := "86000000-0000-4000-8000-000000009034"
	nudgeDueAt := time.Now().UTC().Add(45 * time.Minute)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, nudgeObligation, nudgeDueAt)
	seedVaccinationBatchForShed(t, ctx, pool, nudgeBatch, versionID, nudgeParkID, nudgeShedID, nudgeDueAt, nudgeObligation)
	// CR-002/CR-003: the stable park-drive identity, not batch:<id>.
	nudgeEvent := parkDriveEventID(nudgeParkID, nudgeDueAt)
	if _, err := repo.SendNudge(ctx, ports.SendNudge{
		TenantID: testTenantID, EventID: nudgeEvent, ActorID: testActorID,
		IdempotencyKey: "rail-nudge-key", Channel: "local-stub", Message: "please act",
		Scope: domain.ScopeFilter{TenantWide: true},
	}); err != nil {
		t.Fatalf("SendNudge: %v", err)
	}

	// Snooze overrides the reminder label: derived purely from an active calendar_snoozes row.
	snoozeObligation := "86000000-0000-4000-8000-000000009041"
	snoozeBatch := "86000000-0000-4000-8000-000000009042"
	snoozeParkID := "86000000-0000-4000-8000-000000009043"
	snoozeShedID := "86000000-0000-4000-8000-000000009044"
	snoozeDueAt := time.Now().UTC().Add(50 * time.Minute)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, snoozeObligation, snoozeDueAt)
	seedVaccinationBatchForShed(t, ctx, pool, snoozeBatch, versionID, snoozeParkID, snoozeShedID, snoozeDueAt, snoozeObligation)
	// CR-002/CR-003: the stable park-drive identity, not batch:<id>.
	snoozeEvent := parkDriveEventID(snoozeParkID, snoozeDueAt)
	if _, err := repo.Snooze(ctx, ports.Snooze{
		TenantID: testTenantID, EventID: snoozeEvent, ActorID: testActorID,
		IdempotencyKey: "rail-snooze-key", SnoozeUntil: time.Now().UTC().Add(2 * time.Hour),
		Reason: "waiting on stock", Scope: domain.ScopeFilter{TenantWide: true},
	}); err != nil {
		t.Fatalf("Snooze: %v", err)
	}

	// Composite (catch-up summary) escalation: no obligation_escalations row is ever written for a
	// catch-up event (see TestCalendarEscalationSweepQueuesNotificationAndObligationEscalation), so
	// this must be derived purely from the notification_requests escalation row.
	catchupProtocolID := "86000000-0000-4000-8000-000000009201"
	catchupVersionID := "86000000-0000-4000-8000-000000009202"
	catchupRuleID := "86000000-0000-4000-8000-000000009203"
	catchupObligationID := "86000000-0000-4000-8000-000000009204"
	catchupDueAt := time.Now().UTC().Add(-2 * time.Hour)
	seedVaccinationObligation(t, ctx, pool, catchupProtocolID, catchupVersionID, catchupRuleID, catchupObligationID, catchupDueAt)
	if _, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID: testTenantID, Limit: 10, Now: time.Now().In(biztime.DefaultLocation()),
		Level1After: 0, Level2After: 4 * time.Hour, Level3After: 24 * time.Hour, Level4After: 48 * time.Hour,
	}); err != nil {
		t.Fatalf("SweepEscalations (catch-up): %v", err)
	}
	catchupEvent := parkDriveTenantEventID(testTenantID, catchupDueAt)
	assertCount(t, ctx, pool, "no canonical escalation ladder for catch-up event", `
SELECT count(*) FROM obligation_escalations WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid`,
		0, testTenantID, catchupObligationID)

	q := domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll,
		DateFrom: time.Now().Add(-24 * time.Hour), DateTo: time.Now().Add(24 * time.Hour),
		Limit: 50, IncludeReminderRail: true, Scope: domain.ScopeFilter{TenantWide: true},
	}
	got, err := repo.ListEvents(ctx, q)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if got.ReminderRail == nil {
		t.Fatalf("reminder rail missing")
	}

	if item := findRailItem(*got.ReminderRail, reminderEvent); item.ReminderLabel != "Reminder queued" {
		t.Fatalf("reminder label = %q, want Reminder queued (derived despite corrupted reminder_state column)", item.ReminderLabel)
	}
	if item := findRailItem(*got.ReminderRail, nudgeEvent); item.ReminderLabel != "Reminder sent" {
		t.Fatalf("nudge label = %q, want Reminder sent", item.ReminderLabel)
	}
	if item := findRailItem(*got.ReminderRail, snoozeEvent); item.ReminderLabel != "Reminder snoozed" {
		t.Fatalf("snooze label = %q, want Reminder snoozed", item.ReminderLabel)
	}
	if item := findRailItem(*got.ReminderRail, catchupEvent); item.EscalationLabel != "Escalated · L1" || item.ReminderLabel != "Reminder escalated" {
		t.Fatalf("catch-up escalation item = %#v, want Escalated L1 / Reminder escalated (derived from notification_requests only)", item)
	}
}

func TestCalendarSweepersDoNotNotifyHeldDeferredOrBlockedWork(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolID := "86000000-0000-4000-8000-000000000931"
	versionID := "86000000-0000-4000-8000-000000000932"
	ruleID := "86000000-0000-4000-8000-000000000933"
	obligationID := "86000000-0000-4000-8000-000000000934"
	dueAt := time.Now().UTC().Add(-2 * time.Hour)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, dueAt)
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET status='deferred', updated_at=now()
WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid`, testTenantID, obligationID); err != nil {
		t.Fatalf("defer obligation: %v", err)
	}
	// "blocked" execution/stock visibility was removed from the obligation model entirely (owner
	// decision 2026-07-14, see calendarCanonicalEventsCTE's doc comment) -- deferred is the only held
	// state left to prove sweepers skip. This deferred obligation surfaces only as a catch-up drive
	// (event_type='vaccination_drive'); an individual obligation's own dose_due row is excluded from
	// the general sweep candidates regardless of status.
	reminders, err := repo.SweepDueReminders(ctx, testTenantID, 10)
	if err != nil {
		t.Fatalf("SweepDueReminders: %v", err)
	}
	if reminders != 0 {
		t.Fatalf("held work reminders = %d, want 0", reminders)
	}
	escalations, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:    testTenantID,
		Limit:       10,
		Now:         time.Now().In(biztime.DefaultLocation()),
		Level1After: 0,
	})
	if err != nil {
		t.Fatalf("SweepEscalations: %v", err)
	}
	if escalations != 0 {
		t.Fatalf("held work escalations = %d, want 0", escalations)
	}
}

func TestCalendarEscalationSweepTargetsObligationBeforeLimit(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	now := time.Now().UTC()
	olderObligationID := "86000000-0000-4000-8000-0000000008a4"
	targetObligationID := "86000000-0000-4000-8000-0000000008b4"
	seedVaccinationObligation(t, ctx, pool,
		"86000000-0000-4000-8000-0000000008a1",
		"86000000-0000-4000-8000-0000000008a2",
		"86000000-0000-4000-8000-0000000008a3",
		olderObligationID,
		now.Add(-3*time.Hour),
	)
	seedVaccinationObligation(t, ctx, pool,
		"86000000-0000-4000-8000-0000000008b1",
		"86000000-0000-4000-8000-0000000008b2",
		"86000000-0000-4000-8000-0000000008b3",
		targetObligationID,
		now.Add(-2*time.Hour),
	)

	queued, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:     testTenantID,
		ObligationID: targetObligationID,
		Limit:        1,
		Now:          now,
		Level1After:  0,
		Level2After:  4 * time.Hour,
		Level3After:  24 * time.Hour,
		Level4After:  48 * time.Hour,
	})
	if err != nil {
		t.Fatalf("SweepEscalations targeted: %v", err)
	}
	if queued != 1 {
		t.Fatalf("targeted queued escalations = %d, want 1", queued)
	}
	targetEventID := "obligation:" + targetObligationID
	olderEventID := "obligation:" + olderObligationID
	assertCount(t, ctx, pool, "target escalation notification", `
SELECT count(*)
FROM notification_requests
WHERE tenant_id=$1::uuid
  AND calendar_event_id=$2
  AND target_id=$3::uuid
  AND notification_type='escalation'
  AND status='queued'`, 1, testTenantID, targetEventID, targetObligationID)
	assertCount(t, ctx, pool, "older obligation skipped despite earlier due date", `
SELECT count(*)
FROM notification_requests
WHERE tenant_id=$1::uuid
  AND calendar_event_id=$2
  AND notification_type='escalation'`, 0, testTenantID, olderEventID)
	assertCount(t, ctx, pool, "target obligation escalation", `
SELECT count(*)
FROM obligation_escalations
WHERE tenant_id=$1::uuid
  AND obligation_id=$2::uuid
  AND level=1
  AND status='open'`, 1, testTenantID, targetObligationID)
	assertCount(t, ctx, pool, "older obligation escalation skipped", `
SELECT count(*)
FROM obligation_escalations
WHERE tenant_id=$1::uuid
  AND obligation_id=$2::uuid`, 0, testTenantID, olderObligationID)
}

func TestCalendarEscalationAcknowledgeAndResolveWorkflow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolID := "86000000-0000-4000-8000-000000000871"
	versionID := "86000000-0000-4000-8000-000000000872"
	ruleID := "86000000-0000-4000-8000-000000000873"
	obligationID := "86000000-0000-4000-8000-000000000874"
	dueAt := time.Now().UTC().Add(-2 * time.Hour)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, dueAt)

	if _, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:     testTenantID,
		ObligationID: obligationID,
		Limit:        10,
		Now:          time.Now().In(biztime.DefaultLocation()),
		Level1After:  0,
		Level2After:  4 * time.Hour,
		Level3After:  24 * time.Hour,
		Level4After:  48 * time.Hour,
	}); err != nil {
		t.Fatalf("SweepEscalations: %v", err)
	}
	eventID := "obligation:" + obligationID
	ack, err := repo.AcknowledgeEscalation(ctx, ports.AcknowledgeEscalation{
		TenantID: testTenantID, EventID: eventID, ActorID: testActorID,
		TraceID: "trace-escalation-ack", IdempotencyKey: "calendar-escalation-ack-key",
		Reason: "shed owner accepted the escalation", Scope: domain.ScopeFilter{TenantWide: true}, ActorGrants: testCalendarActorGrants(),
	})
	if err != nil {
		t.Fatalf("AcknowledgeEscalation: %v", err)
	}
	if ack.ActionType != "acknowledge_escalation" || ack.Status != "acknowledged" {
		t.Fatalf("ack response = %#v, want acknowledge_escalation/acknowledged", ack)
	}
	ackReplay, err := repo.AcknowledgeEscalation(ctx, ports.AcknowledgeEscalation{
		TenantID: testTenantID, EventID: eventID, ActorID: testActorID,
		TraceID: "trace-escalation-ack", IdempotencyKey: "calendar-escalation-ack-key",
		Reason: "shed owner accepted the escalation", Scope: domain.ScopeFilter{TenantWide: true}, ActorGrants: testCalendarActorGrants(),
	})
	if err != nil {
		t.Fatalf("AcknowledgeEscalation replay: %v", err)
	}
	if ackReplay.ActionID != ack.ActionID || !ackReplay.IdempotentReplay {
		t.Fatalf("ack replay = %#v, want same action replay", ackReplay)
	}
	if _, err := repo.AcknowledgeEscalation(ctx, ports.AcknowledgeEscalation{
		TenantID: testTenantID, EventID: eventID, ActorID: testActorID,
		IdempotencyKey: "calendar-escalation-ack-key", Reason: "different", Scope: domain.ScopeFilter{TenantWide: true}, ActorGrants: testCalendarActorGrants(),
	}); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("ack conflict err = %v, want ErrIdempotencyConflict", err)
	}
	assertCount(t, ctx, pool, "acknowledged escalation", `
SELECT count(*)
FROM obligation_escalations
WHERE tenant_id=$1::uuid
  AND escalation_id=$2::uuid
  AND obligation_id=$3::uuid
  AND status='acknowledged'
  AND acknowledged_by=$4::uuid
  AND acknowledgement_note='shed owner accepted the escalation'`, 1, testTenantID, ack.ActionID, obligationID, testActorID)
	assertCount(t, ctx, pool, "ack read notification", `
SELECT count(*)
FROM notification_requests
WHERE tenant_id=$1::uuid AND calendar_event_id=$2 AND notification_type='escalation' AND status='read' AND read_at IS NOT NULL`, 1, testTenantID, eventID)
	assertCount(t, ctx, pool, "ack status event", `
SELECT count(*)
FROM obligation_status_events
WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid AND event_type='escalation_acknowledged'`, 1, testTenantID, obligationID)
	assertOutboxEnvelope(t, ctx, pool, "ack escalation outbox envelope", "obligation_escalation", ack.ActionID, "calendar.escalation.acknowledged", eventID)

	resolved, err := repo.ResolveEscalation(ctx, ports.ResolveEscalation{
		TenantID: testTenantID, EventID: eventID, ActorID: testActorID,
		TraceID: "trace-escalation-resolve", IdempotencyKey: "calendar-escalation-resolve-key",
		Reason: "proof corrected and owner confirmed", Scope: domain.ScopeFilter{TenantWide: true}, ActorGrants: testCalendarActorGrants(),
	})
	if err != nil {
		t.Fatalf("ResolveEscalation: %v", err)
	}
	if resolved.ActionID != ack.ActionID || resolved.ActionType != "resolve_escalation" || resolved.Status != "resolved" {
		t.Fatalf("resolve response = %#v, want same escalation resolved", resolved)
	}
	assertCount(t, ctx, pool, "resolved escalation", `
SELECT count(*)
FROM obligation_escalations
WHERE tenant_id=$1::uuid
  AND escalation_id=$2::uuid
  AND status='resolved'
  AND resolved_by=$3::uuid
  AND resolution_note='proof corrected and owner confirmed'`, 1, testTenantID, ack.ActionID, testActorID)
	assertCount(t, ctx, pool, "resolve status event", `
SELECT count(*)
FROM obligation_status_events
WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid AND event_type='escalation_resolved'`, 1, testTenantID, obligationID)
	assertCount(t, ctx, pool, "resolve outbox", `
SELECT count(*)
FROM outbox_messages
WHERE tenant_id=$1::uuid
  AND aggregate_type='obligation_escalation'
  AND aggregate_id=$2::uuid
  AND event_type='calendar.escalation.resolved'`, 1, testTenantID, ack.ActionID)
	if _, err := repo.ResolveEscalation(ctx, ports.ResolveEscalation{
		TenantID: testTenantID, EventID: eventID, ActorID: testActorID,
		IdempotencyKey: "calendar-escalation-resolve-again", Reason: "again", Scope: domain.ScopeFilter{TenantWide: true}, ActorGrants: testCalendarActorGrants(),
	}); !errors.Is(err, ports.ErrEventNotActionable) {
		t.Fatalf("resolve again err = %v, want ErrEventNotActionable", err)
	}
	history, err := repo.History(ctx, domain.HistoryQuery{TenantID: testTenantID, EventID: eventID, Limit: 20, Scope: domain.ScopeFilter{TenantWide: true}})
	if err != nil {
		t.Fatalf("History after escalation actions: %v", err)
	}
	foundEscalationHistory := false
	for _, item := range history.Items {
		if item.SourceTable == "obligation_escalations" && item.Status == "resolved" {
			foundEscalationHistory = true
			break
		}
	}
	if !foundEscalationHistory {
		t.Fatalf("history missing resolved obligation_escalations row: %#v", history.Items)
	}
}

func TestCalendarEscalationResolveClosesActiveLadder(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolID := "86000000-0000-4000-8000-000000000881"
	versionID := "86000000-0000-4000-8000-000000000882"
	ruleID := "86000000-0000-4000-8000-000000000883"
	obligationID := "86000000-0000-4000-8000-000000000884"
	dueAt := time.Now().UTC().Add(-6 * time.Hour)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, dueAt)

	if _, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:     testTenantID,
		ObligationID: obligationID,
		Limit:        10,
		Now:          time.Now().In(biztime.DefaultLocation()),
		Level1After:  0,
		Level2After:  12 * time.Hour,
		Level3After:  24 * time.Hour,
		Level4After:  48 * time.Hour,
	}); err != nil {
		t.Fatalf("SweepEscalations level 1: %v", err)
	}
	eventID := "obligation:" + obligationID
	level1, err := repo.AcknowledgeEscalation(ctx, ports.AcknowledgeEscalation{
		TenantID: testTenantID, EventID: eventID, ActorID: testActorID,
		TraceID: "trace-escalation-ladder-ack", IdempotencyKey: "calendar-escalation-ladder-ack-key",
		Reason: "level one owner has seen it", Scope: domain.ScopeFilter{TenantWide: true}, ActorGrants: testCalendarActorGrants(),
	})
	if err != nil {
		t.Fatalf("AcknowledgeEscalation level 1: %v", err)
	}
	if _, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:     testTenantID,
		ObligationID: obligationID,
		Limit:        10,
		Now:          time.Now().In(biztime.DefaultLocation()),
		Level1After:  0,
		Level2After:  4 * time.Hour,
		Level3After:  24 * time.Hour,
		Level4After:  48 * time.Hour,
	}); err != nil {
		t.Fatalf("SweepEscalations level 2: %v", err)
	}
	assertCount(t, ctx, pool, "active escalation ladder before resolve", `
SELECT count(*)
FROM obligation_escalations
WHERE tenant_id=$1::uuid
  AND obligation_id=$2::uuid
  AND status IN ('open', 'acknowledged')`, 2, testTenantID, obligationID)
	resolved, err := repo.ResolveEscalation(ctx, ports.ResolveEscalation{
		TenantID: testTenantID, EventID: eventID, ActorID: testActorID,
		TraceID: "trace-escalation-ladder-resolve", IdempotencyKey: "calendar-escalation-ladder-resolve-key",
		Reason: "drive completed after level two escalation", Scope: domain.ScopeFilter{TenantWide: true}, ActorGrants: testCalendarActorGrants(),
	})
	if err != nil {
		t.Fatalf("ResolveEscalation ladder: %v", err)
	}
	if resolved.ActionID == level1.ActionID {
		t.Fatalf("resolve action id = %s, want latest level escalation not level 1 %s", resolved.ActionID, level1.ActionID)
	}
	assertCount(t, ctx, pool, "active escalation ladder after resolve", `
SELECT count(*)
FROM obligation_escalations
WHERE tenant_id=$1::uuid
  AND obligation_id=$2::uuid
  AND status IN ('open', 'acknowledged')`, 0, testTenantID, obligationID)
	assertCount(t, ctx, pool, "resolved escalation ladder after resolve", `
SELECT count(*)
FROM obligation_escalations
WHERE tenant_id=$1::uuid
  AND obligation_id=$2::uuid
  AND status='resolved'
  AND resolved_by=$3::uuid
  AND resolution_note='drive completed after level two escalation'`, 2, testTenantID, obligationID, testActorID)
	assertCount(t, ctx, pool, "ladder escalation notifications read", `
SELECT count(*)
FROM notification_requests
WHERE tenant_id=$1::uuid
  AND calendar_event_id=$2
  AND notification_type='escalation'
  AND status='read'
  AND read_at IS NOT NULL`, 2, testTenantID, eventID)
	reopened, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:     testTenantID,
		ObligationID: obligationID,
		Limit:        10,
		Now:          time.Now().In(biztime.DefaultLocation()),
		Level1After:  0,
		Level2After:  4 * time.Hour,
		Level3After:  24 * time.Hour,
		Level4After:  48 * time.Hour,
	})
	if err != nil {
		t.Fatalf("SweepEscalations after resolve: %v", err)
	}
	if reopened != 0 {
		t.Fatalf("resolved escalation reopened %d notifications, want 0", reopened)
	}
	assertCount(t, ctx, pool, "ladder escalation notifications not reopened", `
SELECT count(*)
FROM notification_requests
WHERE tenant_id=$1::uuid
  AND calendar_event_id=$2
  AND notification_type='escalation'`, 2, testTenantID, eventID)
}

func TestCalendarEscalationResolveDoesNotSilenceNextLevel(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolID := "86000000-0000-4000-8000-000000000886"
	versionID := "86000000-0000-4000-8000-000000000887"
	ruleID := "86000000-0000-4000-8000-000000000888"
	obligationID := "86000000-0000-4000-8000-000000000889"
	dueAt := time.Now().UTC().Add(-6 * time.Hour)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, dueAt)

	if _, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:     testTenantID,
		ObligationID: obligationID,
		Limit:        10,
		Now:          time.Now().In(biztime.DefaultLocation()),
		Level1After:  0,
		Level2After:  12 * time.Hour,
		Level3After:  24 * time.Hour,
		Level4After:  48 * time.Hour,
	}); err != nil {
		t.Fatalf("SweepEscalations level 1: %v", err)
	}
	eventID := "obligation:" + obligationID
	resolved, err := repo.ResolveEscalation(ctx, ports.ResolveEscalation{
		TenantID: testTenantID, EventID: eventID, ActorID: testActorID,
		TraceID: "trace-escalation-rearm-resolve", IdempotencyKey: "calendar-escalation-rearm-resolve-key",
		Reason: "operator handled the first alert but work remains open", Scope: domain.ScopeFilter{TenantWide: true}, ActorGrants: testCalendarActorGrants(),
	})
	if err != nil {
		t.Fatalf("ResolveEscalation level 1: %v", err)
	}
	if resolved.Status != "resolved" {
		t.Fatalf("resolve response = %#v, want resolved", resolved)
	}
	queued, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:     testTenantID,
		ObligationID: obligationID,
		Limit:        10,
		Now:          time.Now().In(biztime.DefaultLocation()),
		Level1After:  0,
		Level2After:  4 * time.Hour,
		Level3After:  24 * time.Hour,
		Level4After:  48 * time.Hour,
	})
	if err != nil {
		t.Fatalf("SweepEscalations next level: %v", err)
	}
	if queued != 1 {
		t.Fatalf("next-level escalation queued = %d, want 1", queued)
	}
	assertCount(t, ctx, pool, "level two escalation after resolve", `
SELECT count(*)
FROM obligation_escalations
WHERE tenant_id=$1::uuid
  AND obligation_id=$2::uuid
  AND level=2
  AND status='open'`, 1, testTenantID, obligationID)
	assertCount(t, ctx, pool, "escalation notifications after rearm", `
SELECT count(*)
FROM notification_requests
WHERE tenant_id=$1::uuid
  AND calendar_event_id=$2
  AND notification_type='escalation'`, 2, testTenantID, eventID)
}

func TestCalendarEscalationSweepRoutesLevel3ToPCDirector(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolID := "86000000-0000-4000-8000-000000000861"
	versionID := "86000000-0000-4000-8000-000000000862"
	ruleID := "86000000-0000-4000-8000-000000000863"
	obligationID := "86000000-0000-4000-8000-000000000864"
	dueAt := time.Now().UTC().Add(-25 * time.Hour)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, dueAt)

	queued, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:     testTenantID,
		ObligationID: obligationID,
		Limit:        10,
		Now:          time.Now().In(biztime.DefaultLocation()),
		Level1After:  0,
		Level2After:  4 * time.Hour,
		Level3After:  24 * time.Hour,
		Level4After:  48 * time.Hour,
	})
	if err != nil {
		t.Fatalf("SweepEscalations: %v", err)
	}
	if queued != 1 {
		t.Fatalf("queued escalations = %d, want 1", queued)
	}
	eventID := "obligation:" + obligationID
	assertCount(t, ctx, pool, "pc director escalation notification", `
SELECT count(*)
FROM notification_requests
WHERE tenant_id=$1::uuid
  AND calendar_event_id=$2
  AND recipient_ref='pc_director'
  AND notification_type='escalation'
  AND status='queued'`, 1, testTenantID, eventID)
	assertCount(t, ctx, pool, "pc director obligation escalation", `
SELECT count(*)
FROM obligation_escalations
WHERE tenant_id=$1::uuid
  AND obligation_id=$2::uuid
  AND level=3
  AND escalated_to_role='pc_director'
  AND status='open'`, 1, testTenantID, obligationID)
	if _, err := repo.ResolveEscalation(ctx, ports.ResolveEscalation{
		TenantID:       testTenantID,
		EventID:        eventID,
		ActorID:        testActorID,
		IdempotencyKey: "calendar-escalation-unauthorized-resolve",
		Reason:         "park head cannot resolve PC director escalation",
		Scope:          domain.ScopeFilter{TenantWide: true},
		ActorGrants:    []ports.ActorGrant{{Role: permissions.RoleParkHead, ScopeType: "tenant", ScopeID: testTenantID}},
	}); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("park head resolve level 3 err = %v, want ErrForbidden", err)
	}
}

func TestCalendarConfigActivationReviewNudgeIsActionable(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolID := "86000000-0000-4000-8000-000000000901"
	versionID := "86000000-0000-4000-8000-000000000902"
	activationDueAt := time.Now().UTC().Add(2 * time.Hour)
	// Canonical config_events derives 'vaccination_config_activation_review' events live from any
	// draft, vaccination-category protocol_version with an activation_due_at in its rule_dsl -- there
	// is no separate projection row to seed.
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
VALUES ($1::uuid, $2::uuid, 'vaccination.calendar.config_activation_test', 'Config Activation Test Vaccine', 'vaccination', 'active')
ON CONFLICT (protocol_id) DO UPDATE SET status='active', updated_at=now()`, protocolID, testTenantID); err != nil {
		t.Fatalf("seed protocol definition: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_versions (
  protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
  version_label, status, effective_from, effective_to, rule_dsl, proof_policy, published_at
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'tenant', NULL, 1,
  'Config activation review test', 'draft', DATE '2026-01-01', DATE '2028-01-01',
  jsonb_build_object('activation_due_at', to_char($4::timestamptz, 'YYYY-MM-DD"T"HH24:MI:SS"Z"')),
  '{}'::jsonb, NULL
)
ON CONFLICT (protocol_version_id) DO UPDATE
SET rule_dsl = EXCLUDED.rule_dsl, status='draft', updated_at=now()`,
		versionID, testTenantID, protocolID, activationDueAt); err != nil {
		t.Fatalf("seed draft protocol version: %v", err)
	}
	eventID := "calendar:" + versionID
	if _, err := repo.SendNudge(ctx, ports.SendNudge{
		TenantID: testTenantID, EventID: eventID, ActorID: testActorID,
		IdempotencyKey: "calendar-config-nudge-key", Channel: "local-stub",
		Message: "Please review the protocol source", Scope: domain.ScopeFilter{TenantWide: true},
	}); err != nil {
		t.Fatalf("SendNudge config approval: %v", err)
	}
}

// TestCalendarParkDriveIdentityAndSnoozeStateSurviveMembershipChange is the CR-003 regression guard:
// a solo drive's snooze + reminder state must stay attached to the SAME event_id after a second
// same-day batch joins the same park+day, because canonical_read.go's park_drive_events now assigns
// the STABLE parkdrive:park:...:date:... identity from the moment the drive exists (never the
// underlying batch's own batch:<id>). Before that fix, a solo drive's event_id WAS batch:<id> and
// mutated to parkdrive:... only once aggregated -- silently detaching any calendar_snoozes/
// notification_requests row keyed on the old id (repository.go's activeSnoozeIDs/
// calendarReminderCandidatesSQL/calendarReminderRailSQL all join by exact calendar_event_id string
// equality, with no FK and no ID-shape awareness since migration 000189) and letting a reminder
// silently rearm despite an active snooze.
func TestCalendarParkDriveIdentityAndSnoozeStateSurviveMembershipChange(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	const (
		protocolA   = "86000000-0000-4000-8000-00000000d101"
		versionA    = "86000000-0000-4000-8000-00000000d102"
		ruleA       = "86000000-0000-4000-8000-00000000d103"
		batchA      = "86000000-0000-4000-8000-00000000d104"
		obligationA = "86000000-0000-4000-8000-00000000d105"
		animalA     = "86000000-0000-4000-8000-00000000d106"
		protocolB   = "86000000-0000-4000-8000-00000000d201"
		versionB    = "86000000-0000-4000-8000-00000000d202"
		ruleB       = "86000000-0000-4000-8000-00000000d203"
		batchB      = "86000000-0000-4000-8000-00000000d204"
		obligationB = "86000000-0000-4000-8000-00000000d205"
		animalB     = "86000000-0000-4000-8000-00000000d206"
	)
	dueAt := time.Now().UTC().Add(30 * time.Minute)
	seedVaccinationObligation(t, ctx, pool, protocolA, versionA, ruleA, obligationA, dueAt)
	attachObligationToGoatScope(t, ctx, pool, obligationA, animalA, "shed", testShedA)
	seedVaccinationBatchForShed(t, ctx, pool, batchA, versionA, testParkA, testShedA, dueAt, obligationA)

	wantID := parkDriveEventID(testParkA, dueAt)
	solo, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll,
		DateFrom: dueAt.Add(-time.Hour), DateTo: dueAt.Add(24 * time.Hour), Limit: 20,
		Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents solo: %v", err)
	}
	if len(solo.Items) != 1 || solo.Items[0].EventID != wantID {
		t.Fatalf("ListEvents solo=%#v, want stable park-drive event %s", solo.Items, wantID)
	}

	// Queue a reminder BEFORE snoozing (the sweep skips events with an active snooze) so there is
	// real reminder history keyed on wantID to prove doesn't duplicate later.
	queued, err := repo.SweepDueReminders(ctx, testTenantID, 10)
	if err != nil {
		t.Fatalf("SweepDueReminders (pre-snooze): %v", err)
	}
	if queued != 1 {
		t.Fatalf("queued reminders = %d, want 1", queued)
	}
	assertCount(t, ctx, pool, "reminder keyed on stable id (pre-aggregation)", `
SELECT count(*) FROM notification_requests
WHERE tenant_id=$1::uuid AND calendar_event_id=$2 AND notification_type='reminder'`, 1, testTenantID, wantID)

	snoozeUntil := time.Now().UTC().Add(6 * time.Hour)
	snooze, err := repo.Snooze(ctx, ports.Snooze{
		TenantID: testTenantID, EventID: wantID, ActorID: testActorID,
		IdempotencyKey: "park-drive-stability-snooze", SnoozeUntil: snoozeUntil,
		Reason: "waiting on stock before this park's drive", Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("Snooze solo drive: %v", err)
	}
	assertCount(t, ctx, pool, "active snooze keyed on stable id (pre-aggregation)", `
SELECT count(*) FROM calendar_snoozes
WHERE tenant_id=$1::uuid AND calendar_event_id=$2 AND status='active' AND snooze_id=$3::uuid`,
		1, testTenantID, wantID, snooze.ActionID)

	// A second same-day batch now joins the SAME park+day.
	seedVaccinationObligation(t, ctx, pool, protocolB, versionB, ruleB, obligationB, dueAt.Add(10*time.Minute))
	attachObligationToGoatScope(t, ctx, pool, obligationB, animalB, "shed", testShedB)
	seedVaccinationBatchForShed(t, ctx, pool, batchB, versionB, testParkA, testShedB, dueAt.Add(10*time.Minute), obligationB)

	// Identity must be UNCHANGED: same park+day, same wantID, now aggregating two batches.
	aggregated, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll,
		DateFrom: dueAt.Add(-time.Hour), DateTo: dueAt.Add(24 * time.Hour), Limit: 20,
		Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents aggregated: %v", err)
	}
	if len(aggregated.Items) != 1 || aggregated.Items[0].EventID != wantID || aggregated.Items[0].DriveCount != 2 {
		t.Fatalf("ListEvents aggregated=%#v, want SAME stable id %s now aggregating 2 drives", aggregated.Items, wantID)
	}

	// The pre-existing snooze row must still be attached (not detached/orphaned) to the identity the
	// aggregated drive now carries -- because it never changed.
	assertCount(t, ctx, pool, "active snooze survives aggregation", `
SELECT count(*) FROM calendar_snoozes
WHERE tenant_id=$1::uuid AND calendar_event_id=$2 AND status='active' AND snooze_id=$3::uuid`,
		1, testTenantID, wantID, snooze.ActionID)

	// The reminder rail must derive "snoozed" for wantID post-aggregation (read-derived state,
	// joined purely by event_id equality against the still-active snooze row).
	railResp, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll,
		DateFrom: dueAt.Add(-time.Hour), DateTo: dueAt.Add(24 * time.Hour), Limit: 20,
		IncludeReminderRail: true, Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents with reminder rail: %v", err)
	}
	if railResp.ReminderRail == nil {
		t.Fatalf("reminder rail missing")
	}
	var railItem *domain.CalendarReminderRailItem
	for i := range railResp.ReminderRail.Items {
		if railResp.ReminderRail.Items[i].EventID == wantID {
			railItem = &railResp.ReminderRail.Items[i]
		}
	}
	if railItem == nil || railItem.ReminderLabel != "Reminder snoozed" {
		t.Fatalf("reminder rail item=%#v, want Reminder snoozed still attached to %s", railItem, wantID)
	}

	// No rearm/duplicate: had the identity mutated, the still-active snooze would no longer match
	// the (new) event_id and the sweep would incorrectly queue a second reminder. It must not.
	replaySweep, err := repo.SweepDueReminders(ctx, testTenantID, 10)
	if err != nil {
		t.Fatalf("SweepDueReminders (post-aggregation): %v", err)
	}
	if replaySweep != 0 {
		t.Fatalf("post-aggregation sweep queued=%d, want 0 (active snooze must still block it, no rearm)", replaySweep)
	}
	assertCount(t, ctx, pool, "reminder history not duplicated", `
SELECT count(*) FROM notification_requests
WHERE tenant_id=$1::uuid AND calendar_event_id=$2 AND notification_type='reminder'`, 1, testTenantID, wantID)
}

// TestCalendarReconcileEventReferencesFlagsOnlyRealOrphans is the CR-004 regression guard: the
// integrity check (goatos_reconcile_calendar_event_references, fixed by migration 000191) must
// parse each of the real, currently-in-use typed calendar_event_id shapes (obligation:/batch:/
// parkdrive:park:.../parkdrive:tenant:.../completion:/calendar:) and resolve each one against its
// real canonical row -- not compare raw UUID text, which flagged every valid row as an orphan
// (migration 000189's original bug). Seeds one VALID notification_requests row per supported shape
// plus exactly one genuine orphan, and asserts ONLY the orphan comes back.
func TestCalendarReconcileEventReferencesFlagsOnlyRealOrphans(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	const (
		protocolID            = "86000000-0000-4000-8000-00000000e101"
		versionID             = "86000000-0000-4000-8000-00000000e102"
		ruleID                = "86000000-0000-4000-8000-00000000e103"
		obligationID          = "86000000-0000-4000-8000-00000000e104"
		batchID               = "86000000-0000-4000-8000-00000000e105"
		batchOnlyObligationID = "86000000-0000-4000-8000-00000000e106"
		draftVersionID        = "86000000-0000-4000-8000-00000000e107"
		completionGoatID      = "86000000-0000-4000-8000-00000000e108"
	)
	dueAt := time.Now().UTC().Add(2 * time.Hour)
	// obligation: -- a real, unbatched obligation.
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, dueAt)
	// batch: -- a real batch (legacy membership id, still independently resolvable).
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, batchOnlyObligationID, dueAt.Add(5*time.Minute))
	seedVaccinationBatchForShed(t, ctx, pool, batchID, versionID, testParkA, testShedA, dueAt.Add(5*time.Minute), batchOnlyObligationID)
	// parkdrive:park:...:date:... -- the stable identity the batch above ALSO resolves under.
	parkDriveID := parkDriveEventID(testParkA, dueAt.Add(5*time.Minute))
	// parkdrive:tenant:...:date:... -- a real tenant-wide catch-up (obligationID above, tenant-scoped
	// by seedVaccinationObligation's default scope_type='tenant').
	parkDriveTenantID := parkDriveTenantEventID(testTenantID, dueAt)
	// completion: -- a real accepted vaccination completion, recorded against obligationID.
	completionID := "86000000-0000-4000-8000-00000000e201"
	seedVaccinationCompletion(t, ctx, pool, obligationID, completionGoatID, completionID)
	// calendar: -- overloaded prefix; a draft protocol_version (config_events' shape) is the simplest
	// real backing row -- sop_events shares the same prefix but needs a heavier sop_definitions/
	// sop_versions/sop_tasks FK chain the reconciler validity check does not otherwise exercise here.
	seedDraftProtocolVersion(t, ctx, pool, protocolID, draftVersionID, dueAt)

	validIDs := []string{
		"obligation:" + obligationID,
		"batch:" + batchID,
		parkDriveID,
		parkDriveTenantID,
		"completion:" + completionID,
		"calendar:" + draftVersionID,
	}
	orphanID := "obligation:" + "86000000-0000-4000-8000-00000000e999"
	for i, id := range append(append([]string{}, validIDs...), orphanID) {
		notificationID := fmt.Sprintf("86000000-0000-4000-8000-00000000e3%02d", i)
		if _, err := pool.Exec(ctx, `
INSERT INTO notification_requests (
  notification_request_id, tenant_id, calendar_event_id, target_type, notification_type, channel,
  title, body, status, idempotency_key, request_fingerprint, context
) VALUES (
  $1::uuid, $2::uuid, $3, 'calendar_event', 'reminder', 'local-stub',
  'Reconciler fixture', 'reconciler fixture body', 'sent', $4, $4 || ':fingerprint', '{}'::jsonb
)`, notificationID, testTenantID, id, "reconciler-fixture-"+notificationID); err != nil {
			t.Fatalf("seed notification %s for %s: %v", notificationID, id, err)
		}
	}

	orphans, err := repo.ReconcileEventReferences(ctx, testTenantID)
	if err != nil {
		t.Fatalf("ReconcileEventReferences: %v", err)
	}
	var flagged []string
	for _, o := range orphans {
		if o.SourceTable == "notification_requests" {
			flagged = append(flagged, o.CalendarEventID)
		}
	}
	if len(flagged) != 1 || flagged[0] != orphanID {
		t.Fatalf("flagged orphans=%#v, want exactly [%s] (every valid typed id must resolve)", flagged, orphanID)
	}
}

// parkDriveEventID builds the stable park-drive identity (CR-002/CR-003) via domain.
// FormatParkDriveEventID -- the SAME Go-side constructor callers/tests should use -- so the test
// fixture and the domain-layer format can never silently drift apart.
func parkDriveEventID(parkID string, dueAt time.Time) string {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5*60*60+30*60)
	}
	return domain.FormatParkDriveEventID(parkID, "", dueAt.In(loc).Format("2006-01-02"))
}

// parkDriveTenantEventID is the stable park-drive identity (CR-002/CR-003) for a tenant-wide drive
// with no resolvable park (canonical_read.go's park_drive_events falls back to
// 'parkdrive:tenant:...' whenever grouped.park_id IS NULL -- e.g. an unbatched catch-up obligation
// scoped directly to the tenant rather than any park/shed).
func parkDriveTenantEventID(tenantID string, dueAt time.Time) string {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5*60*60+30*60)
	}
	return domain.FormatParkDriveEventID("", tenantID, dueAt.In(loc).Format("2006-01-02"))
}

func stableSameLocalDayDueAt(now time.Time) time.Time {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5*60*60+30*60)
	}
	localNow := now.In(loc)
	localDue := localNow.Add(2 * time.Hour).Truncate(time.Hour)
	if localDue.Hour() > 20 || localDue.Day() != localNow.Day() {
		localDue = time.Date(localNow.Year(), localNow.Month(), localNow.Day()+1, 1, 0, 0, 0, loc)
	}
	return localDue.UTC()
}

func assertDriveTargets(t *testing.T, ctx context.Context, repo *Repository, eventID string, wantAnimalIDs []string) {
	t.Helper()
	targets, err := repo.ListDriveTargets(ctx, domain.DriveTargetQuery{
		TenantID: testTenantID,
		EventID:  eventID,
		Scope:    domain.ScopeFilter{TenantWide: true},
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListDriveTargets(%s): %v", eventID, err)
	}
	if len(targets.Items) != len(wantAnimalIDs) {
		t.Fatalf("ListDriveTargets(%s)=%#v, want %d targets", eventID, targets.Items, len(wantAnimalIDs))
	}
	got := make(map[string]bool, len(targets.Items))
	for _, item := range targets.Items {
		got[item.AnimalID] = true
	}
	for _, animalID := range wantAnimalIDs {
		if !got[animalID] {
			t.Fatalf("ListDriveTargets(%s)=%#v, missing animal %s", eventID, targets.Items, animalID)
		}
	}
}

func assertDriveTargetSheds(t *testing.T, ctx context.Context, repo *Repository, eventID string, wantShedByAnimalID map[string]string) {
	t.Helper()
	targets, err := repo.ListDriveTargets(ctx, domain.DriveTargetQuery{
		TenantID: testTenantID,
		EventID:  eventID,
		Scope:    domain.ScopeFilter{TenantWide: true},
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListDriveTargets(%s): %v", eventID, err)
	}
	got := make(map[string]string, len(targets.Items))
	for _, item := range targets.Items {
		if item.ShedName != nil {
			got[item.AnimalID] = *item.ShedName
		}
	}
	for animalID, wantShed := range wantShedByAnimalID {
		if got[animalID] != wantShed {
			t.Fatalf("ListDriveTargets(%s) shed for animal %s = %q, want %q; targets=%#v", eventID, animalID, got[animalID], wantShed, targets.Items)
		}
	}
}

func seedCalendarLocations(t *testing.T, ctx context.Context, pool *pgxpool.Pool, parkID, shedID string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
INSERT INTO locations (
  location_id, tenant_id, location_type, location_code, name, parent_location_id,
  country, timezone, status, updated_at
) VALUES
  ($1::uuid, $3::uuid, 'park', 'TST-' || right(($1::uuid)::text, 4), 'Test Park ' || right(($1::uuid)::text, 4), NULL, 'IN', 'Asia/Kolkata', 'active', now()),
  ($2::uuid, $3::uuid, 'shed', 'TST-' || right(($2::uuid)::text, 4), 'Test Shed ' || right(($2::uuid)::text, 4), $1::uuid, 'IN', 'Asia/Kolkata', 'active', now())
ON CONFLICT (location_id) DO UPDATE
SET status = 'active',
    updated_at = now()`, parkID, shedID, testTenantID)
	if err != nil {
		t.Fatalf("seed scoped locations: %v", err)
	}
}

func seedVaccinationObligation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, protocolID, versionID, ruleID, obligationID string, dueAt time.Time) {
	t.Helper()
	_, err := pool.Exec(ctx, `
INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
VALUES (
  $1::uuid, $2::uuid,
  'vaccination.calendar.projection_test.p' || right(replace(($1::uuid)::text, '-', ''), 12),
  'Projection Test Vaccine', 'vaccination', 'active'
)
ON CONFLICT (protocol_id) DO UPDATE
SET status = 'active',
    updated_at = now()`,
		protocolID, testTenantID)
	if err != nil {
		t.Fatalf("seed protocol definition: %v", err)
	}
	_, err = pool.Exec(ctx, `
INSERT INTO protocol_versions (
  protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
  version_label, status, effective_from, effective_to, rule_dsl, proof_policy, published_at
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'tenant', NULL, 1,
  'Projection active matrix published test', 'draft', DATE '2026-01-01', DATE '2028-01-01',
  '{"source":{"review_status":"approved","source_ref":"docs/preventive-care-vaccination/PRD.md","source_system":"pc","approved_by":"test","approved_at":"2026-06-27T00:00:00Z"}}'::jsonb,
  '{"required_proofs":["administration"]}'::jsonb, NULL
	)
	ON CONFLICT (protocol_version_id) DO NOTHING`,
		versionID, testTenantID, protocolID)
	if err != nil {
		t.Fatalf("seed protocol version: %v", err)
	}
	_, err = pool.Exec(ctx, `
	INSERT INTO protocol_rules (
	  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
	  offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json,
	  proof_policy, sort_order
	)
	SELECT
	  $1::uuid, $2::uuid, $3::uuid, 'PROJ-PRIMARY', 1, 'calendar',
	  0, 1, 0, 'none', 'immediate', '{}'::jsonb, '{"required_proofs":["administration"]}'::jsonb, 10
	WHERE NOT EXISTS (
	  SELECT 1
	  FROM protocol_rules
	  WHERE tenant_id = $2::uuid AND rule_id = $1::uuid
	)`,
		ruleID, testTenantID, versionID)
	if err != nil {
		t.Fatalf("seed protocol rule: %v", err)
	}
	_, err = pool.Exec(ctx, `
UPDATE protocol_versions
SET status = 'published',
    published_at = COALESCE(published_at, now()),
    updated_at = now()
WHERE tenant_id = $1::uuid AND protocol_version_id = $2::uuid`,
		testTenantID, versionID)
	if err != nil {
		t.Fatalf("publish seeded protocol version: %v", err)
	}
	_, err = pool.Exec(ctx, `
INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, window_start, window_end, status, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4::uuid, 'tenant', $2::uuid,
  'tenant', $2::uuid, $5::timestamptz, $5::timestamptz, $5::timestamptz + interval '1 day',
  'scheduled', 'calendar-projection-test-' || ($1::uuid)::text
)
ON CONFLICT (obligation_id) DO UPDATE
SET due_at = EXCLUDED.due_at,
    status = 'scheduled',
    updated_at = now()`,
		obligationID, testTenantID, versionID, ruleID, dueAt)
	if err != nil {
		t.Fatalf("seed vaccination obligation: %v", err)
	}
}

func seedAdditionalVaccinationObligation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID, ruleID, obligationID string, dueAt time.Time) {
	t.Helper()
	seedCalendarGoat(t, ctx, pool, obligationID)
	_, err := pool.Exec(ctx, `
INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, window_start, window_end, status, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', $1::uuid,
  'tenant', $2::uuid, $5::timestamptz, $5::timestamptz, $5::timestamptz + interval '1 day',
  'scheduled', 'calendar-projection-test-' || ($1::uuid)::text
)
ON CONFLICT (obligation_id) DO UPDATE
SET due_at = EXCLUDED.due_at,
    status = 'scheduled',
    batch_id = NULL,
    updated_at = now()`,
		obligationID, testTenantID, versionID, ruleID, dueAt)
	if err != nil {
		t.Fatalf("seed additional vaccination obligation: %v", err)
	}
}

func seedCalendarGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
	INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex)
	VALUES ($1::uuid, $2::uuid, 'alive', 'goat', $3::uuid, 'female')
ON CONFLICT (goat_id) DO UPDATE
SET lifecycle_status = 'alive',
        updated_at = now()`, goatID, testTenantID, testCustodianID)
	if err != nil {
		t.Fatalf("seed calendar goat: %v", err)
	}
}

func setCalendarGoatCurrentShed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, parkID, shedID string) {
	t.Helper()
	seedCalendarLocations(t, ctx, pool, parkID, shedID)
	_, err := pool.Exec(ctx, `
UPDATE goats
SET current_location_id = $3::uuid,
    park_id = $4::uuid,
    shed_id = NULL,
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND goat_id = $2::uuid`, testTenantID, goatID, shedID, parkID)
	if err != nil {
		t.Fatalf("set goat current shed: %v", err)
	}
}

func attachObligationToGoatScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool, obligationID, goatID, scopeType, scopeID string) {
	t.Helper()
	seedCalendarGoat(t, ctx, pool, goatID)
	if scopeType == "park" {
		seedCalendarLocations(t, ctx, pool, scopeID, testShedA)
	}
	if scopeType == "shed" {
		seedCalendarLocations(t, ctx, pool, testParkA, scopeID)
	}
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET target_type = 'goat',
    target_id = $3::uuid,
    scope_type = $4,
    scope_id = $5::uuid,
    batch_id = NULL,
    updated_at = now()
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`,
		testTenantID, obligationID, goatID, scopeType, scopeID); err != nil {
		t.Fatalf("attach obligation %s to goat %s: %v", obligationID, goatID, err)
	}
}

func seedNonVaccinationGoatObligation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, protocolID, versionID, ruleID, obligationID, goatID, parkID string, dueAt time.Time) {
	t.Helper()
	seedCalendarGoat(t, ctx, pool, goatID)
	seedCalendarLocations(t, ctx, pool, parkID, testShedA)
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
VALUES (
  $1::uuid, $2::uuid,
  'feed.calendar.projection_test.p' || right(replace(($1::uuid)::text, '-', ''), 12),
  'Projection Test Feed', 'feed_direction', 'active'
)
ON CONFLICT (protocol_id) DO UPDATE
SET category = 'feed_direction',
    status = 'active',
    updated_at = now()`,
		protocolID, testTenantID); err != nil {
		t.Fatalf("seed non-vaccination protocol definition: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_versions (
  protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
  version_label, status, effective_from, effective_to, rule_dsl, proof_policy, published_at
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'tenant', NULL, 1,
  'Projection active feed test', 'draft', DATE '2026-01-01', DATE '2028-01-01',
  '{}'::jsonb, '{}'::jsonb, NULL
)
ON CONFLICT (protocol_version_id) DO UPDATE
SET status = CASE WHEN protocol_versions.status = 'published' THEN protocol_versions.status ELSE 'draft' END,
    updated_at = now()`,
		versionID, testTenantID, protocolID); err != nil {
		t.Fatalf("seed non-vaccination protocol version: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
  offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json,
  proof_policy, sort_order
)
VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'FEED-QUEUE', 1, 'calendar',
  0, 1, 0, 'none', 'immediate', '{}'::jsonb, '{}'::jsonb, 10
)
ON CONFLICT (rule_id) DO NOTHING`,
		ruleID, testTenantID, versionID); err != nil {
		t.Fatalf("seed non-vaccination protocol rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE protocol_versions
SET status = 'published',
    published_at = COALESCE(published_at, now()),
    updated_at = now()
WHERE tenant_id = $1::uuid AND protocol_version_id = $2::uuid`,
		testTenantID, versionID); err != nil {
		t.Fatalf("publish non-vaccination protocol version: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, window_start, window_end, status, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', $5::uuid,
  'park', $6::uuid, $7::timestamptz, $7::timestamptz, $7::timestamptz + interval '1 day',
  'scheduled', 'calendar-feed-projection-test-' || ($1::uuid)::text
)
ON CONFLICT (obligation_id) DO UPDATE
SET due_at = EXCLUDED.due_at,
    status = 'scheduled',
    updated_at = now()`,
		obligationID, testTenantID, versionID, ruleID, goatID, parkID, dueAt); err != nil {
		t.Fatalf("seed non-vaccination obligation: %v", err)
	}
}

func seedVaccinationBatch(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batchID, versionID string, dueAt time.Time, obligationIDs ...string) {
	t.Helper()
	seedVaccinationBatchForShed(t, ctx, pool, batchID, versionID, testParkA, testShedA, dueAt, obligationIDs...)
}

func seedVaccinationBatchForShed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batchID, versionID, parkID, shedID string, dueAt time.Time, obligationIDs ...string) {
	t.Helper()
	seedCalendarLocations(t, ctx, pool, parkID, shedID)
	_, err := pool.Exec(ctx, `
INSERT INTO obligation_batches (
  batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status,
  planned_date, window_start, window_end, estimated_targets, planned_quantity
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'shed', $4::uuid, 'planned',
  ($5::timestamptz AT TIME ZONE 'Asia/Kolkata')::date, $5::timestamptz, $5::timestamptz + interval '8 hours',
  $6::int, ($6::int)::numeric
)
ON CONFLICT (batch_id) DO UPDATE
SET scope_type = 'shed',
    scope_id = EXCLUDED.scope_id,
    window_start = EXCLUDED.window_start,
    window_end = EXCLUDED.window_end,
    estimated_targets = EXCLUDED.estimated_targets,
    planned_quantity = EXCLUDED.planned_quantity,
    updated_at = now()`,
		batchID, testTenantID, versionID, shedID, dueAt, len(obligationIDs))
	if err != nil {
		t.Fatalf("seed vaccination batch: %v", err)
	}
	for _, obligationID := range obligationIDs {
		if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET batch_id = $3::uuid,
    scope_type = 'shed',
    scope_id = $4::uuid,
    updated_at = now()
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`,
			testTenantID, obligationID, batchID, shedID); err != nil {
			t.Fatalf("attach obligation %s to batch: %v", obligationID, err)
		}
	}
}

func seedVaccinationBatchForPark(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batchID, versionID, parkID string, dueAt time.Time, obligationIDs ...string) {
	t.Helper()
	seedCalendarLocations(t, ctx, pool, parkID, testShedA)
	_, err := pool.Exec(ctx, `
INSERT INTO obligation_batches (
  batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status,
  planned_date, window_start, window_end, estimated_targets, planned_quantity
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'park', $4::uuid, 'planned',
  ($5::timestamptz AT TIME ZONE 'Asia/Kolkata')::date, $5::timestamptz, $5::timestamptz + interval '8 hours',
  $6::int, ($6::int)::numeric
)
ON CONFLICT (batch_id) DO UPDATE
SET scope_type = 'park',
    scope_id = EXCLUDED.scope_id,
    window_start = EXCLUDED.window_start,
    window_end = EXCLUDED.window_end,
    estimated_targets = EXCLUDED.estimated_targets,
    planned_quantity = EXCLUDED.planned_quantity,
    updated_at = now()`,
		batchID, testTenantID, versionID, parkID, dueAt, len(obligationIDs))
	if err != nil {
		t.Fatalf("seed park vaccination batch: %v", err)
	}
	for _, obligationID := range obligationIDs {
		if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET batch_id = $3::uuid,
    updated_at = now()
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`,
			testTenantID, obligationID, batchID); err != nil {
			t.Fatalf("attach obligation %s to park batch: %v", obligationID, err)
		}
	}
}

func seedProtocolRuleVaccineName(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID, ruleID, vaccineName string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rule_dimensions (
  tenant_id, protocol_version_id, rule_id, category, ruleset_family,
  selector_key, dose_code, source_dose_code, vaccine_code, vaccine_json
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'vaccination', 'calendar-projection-test',
  $3::text, 'first', 'first', lower(replace($4, ' ', '_')), jsonb_build_object('name', $4)
)
ON CONFLICT (tenant_id, protocol_version_id, rule_id, selector_key) DO UPDATE
SET vaccine_json = EXCLUDED.vaccine_json`, testTenantID, versionID, ruleID, vaccineName); err != nil {
		t.Fatalf("seed protocol rule vaccine dimension: %v", err)
	}
}

// seedVaccinationCompletion inserts a real, ACCEPTED vaccination_completions row for the CR-004
// reconciler test's 'completion:<uuid>' fixture (canonical_read.go's vaccination_history_events CTE
// / goatos_calendar_event_reference_valid's completion: branch).
func seedVaccinationCompletion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, obligationID, goatID, completionID string) {
	t.Helper()
	seedCalendarGoat(t, ctx, pool, goatID)
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_completions (
  completion_id, tenant_id, obligation_id, goat_id, administered_at, status, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4::uuid, now(), 'accepted', $5
)
ON CONFLICT (completion_id) DO UPDATE
SET status = 'accepted',
    updated_at = now()`,
		completionID, testTenantID, obligationID, goatID, "reconciler-fixture-completion-"+completionID); err != nil {
		t.Fatalf("seed vaccination completion: %v", err)
	}
}

// seedDraftProtocolVersion inserts a real draft, vaccination-category protocol_version under an
// EXISTING protocol_definitions row -- the simplest real backing row for the overloaded 'calendar:'
// prefix (config_events' shape; mirrors TestCalendarConfigActivationReviewNudgeIsActionable's fixture).
func seedDraftProtocolVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, protocolID, versionID string, activationDueAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_versions (
  protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
  version_label, status, effective_from, effective_to, rule_dsl, proof_policy, published_at
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'tenant', NULL, 2,
  'Reconciler draft fixture', 'draft', DATE '2026-01-01', DATE '2028-01-01',
  jsonb_build_object('activation_due_at', to_char($4::timestamptz, 'YYYY-MM-DD"T"HH24:MI:SS"Z"')),
  '{}'::jsonb, NULL
)
ON CONFLICT (protocol_version_id) DO UPDATE
SET rule_dsl = EXCLUDED.rule_dsl, status='draft', updated_at=now()`,
		versionID, testTenantID, protocolID, activationDueAt); err != nil {
		t.Fatalf("seed draft protocol version: %v", err)
	}
}

func assertCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label, sql string, want int, args ...any) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&got); err != nil {
		t.Fatalf("%s count query: %v", label, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", label, got, want)
	}
}

func assertOutboxEnvelope(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label, aggregateType, aggregateID, eventType, calendarEventID string) {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(ctx, `
SELECT payload
FROM outbox_messages
WHERE tenant_id = $1::uuid
  AND aggregate_type = $2
  AND aggregate_id = $3::uuid`, testTenantID, aggregateType, aggregateID).Scan(&raw); err != nil {
		t.Fatalf("%s payload query: %v", label, err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("%s payload decode: %v", label, err)
	}
	if envelope["event_type"] != eventType || envelope["aggregate_type"] != aggregateType || envelope["aggregate_id"] != aggregateID {
		t.Fatalf("%s envelope = %#v", label, envelope)
	}
	if envelope["subject_type"] != "calendar_event" || envelope["subject_id"] != calendarEventID {
		t.Fatalf("%s subject = %v/%v, want calendar_event/%s", label, envelope["subject_type"], envelope["subject_id"], calendarEventID)
	}
	payload, ok := envelope["payload"].(map[string]any)
	if !ok || payload["action_id"] != aggregateID || payload["event_id"] != calendarEventID {
		t.Fatalf("%s nested payload = %#v", label, envelope["payload"])
	}
}

// TestDriveSummaryComputesCountsAndInvariants verifies that a multi-shed, mixed-status
// vaccination_drive calendar event computes a single park-level drive_summary with correct
// obligation status-bucket counts, maintains the invariant
// total = completed + due + overdue + deferred, resolves park_name to the PARK
// (never a shed), and — critically — that the projection refresh does NOT 500 with an
// ON CONFLICT double-row error (the P0 caused by fanning one park-drive event_id into many rows).
func TestDriveSummaryComputesCountsAndInvariants(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	// Fixed business-day window so the projection state (seeded around these dates) covers the drive.
	driveDate := biztime.BusinessDayStart(time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC))

	protocolID := "aa000000-0000-4000-8000-000000000001"
	versionID := "aa000000-0000-4000-8000-000000000002"
	ruleID := "aa000000-0000-4000-8000-000000000003"

	parkID := testParkA
	shedA := testShedA
	shedB := testShedB
	shedC := "86000000-0000-4000-8000-000000000713"

	// 6 obligations across 3 sheds under ONE park, mixed statuses:
	//   Shed A: completed, completed          -> shed fully complete (sheds_completed = 1)
	//   Shed B: completed, scheduled(due)     -> not complete
	//   Shed C: deferred, missed(overdue)     -> not complete
	oA1 := "ab000000-0000-4000-8000-000000000001"
	oA2 := "ab000000-0000-4000-8000-000000000002"
	oB1 := "ab000000-0000-4000-8000-000000000003"
	oB2 := "ab000000-0000-4000-8000-000000000004"
	oC1 := "ab000000-0000-4000-8000-000000000005"
	oC2 := "ab000000-0000-4000-8000-000000000006"

	// First obligation establishes the published vaccination protocol/version/rule.
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, oA1, driveDate)
	for _, id := range []string{oA2, oB1, oB2, oC1, oC2} {
		seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, id, driveDate)
	}
	// Give the rule a vaccine name so drive_summary.vaccine_labels populates.
	seedProtocolRuleVaccineName(t, ctx, pool, versionID, ruleID, "FMD")

	// One batch per shed, same park + date -> a single PARK-level vaccination_drive event.
	seedVaccinationBatchForShed(t, ctx, pool, "ac000000-0000-4000-8000-000000000001", versionID, parkID, shedA, driveDate, oA1, oA2)
	seedVaccinationBatchForShed(t, ctx, pool, "ac000000-0000-4000-8000-000000000002", versionID, parkID, shedB, driveDate, oB1, oB2)
	seedVaccinationBatchForShed(t, ctx, pool, "ac000000-0000-4000-8000-000000000003", versionID, parkID, shedC, driveDate, oC1, oC2)

	// Apply the mixed statuses (batch attach left them 'scheduled').
	setDriveObligationStatus(t, ctx, pool, oA1, "completed")
	setDriveObligationStatus(t, ctx, pool, oA2, "completed")
	setDriveObligationStatus(t, ctx, pool, oB1, "completed")
	setDriveObligationStatus(t, ctx, pool, oB2, "scheduled")
	setDriveObligationStatus(t, ctx, pool, oC1, "deferred")
	setDriveObligationStatus(t, ctx, pool, oC2, "missed")

	// Refresh the projection — must NOT error (the old fan-out caused an ON CONFLICT 500 here).

	q := domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: driveDate.Add(-24 * time.Hour),
		DateTo:   driveDate.Add(24 * time.Hour),
		Limit:    50,
		Scope:    domain.ScopeFilter{TenantWide: true},
	}
	resp, err := repo.ListEvents(ctx, q)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}

	var drive *domain.CalendarEvent
	driveCount := 0
	for i := range resp.Items {
		if resp.Items[i].EventType == domain.EventVaccinationDrive {
			drive = &resp.Items[i]
			driveCount++
		}
	}
	if drive == nil {
		t.Fatalf("no vaccination_drive event found in %d events", len(resp.Items))
	}
	// Exactly one park-level drive event (the fan-out bug would surface as duplicates/500 upstream).
	if driveCount != 1 {
		t.Fatalf("expected exactly 1 vaccination_drive event, got %d", driveCount)
	}
	if drive.DriveSummary == nil {
		t.Fatalf("drive_summary is nil for vaccination_drive event %s", drive.EventID)
	}
	s := drive.DriveSummary

	if s.TotalCount != 6 {
		t.Errorf("total_count = %d, want 6", s.TotalCount)
	}
	if s.CompletedCount != 3 {
		t.Errorf("completed_count = %d, want 3", s.CompletedCount)
	}
	if s.DueCount != 1 {
		t.Errorf("due_count = %d, want 1 (the one scheduled)", s.DueCount)
	}
	if s.OverdueCount != 1 {
		t.Errorf("overdue_count = %d, want 1 (the one missed)", s.OverdueCount)
	}
	if s.DeferredCount != 1 {
		t.Errorf("deferred_count = %d, want 1", s.DeferredCount)
	}
	if s.ShedCount != 3 {
		t.Errorf("shed_count = %d, want 3", s.ShedCount)
	}
	if s.ShedsCompleted != 1 {
		t.Errorf("sheds_completed = %d, want 1 (shed A fully complete)", s.ShedsCompleted)
	}

	// Invariant: total = completed + due + overdue + deferred.
	sum := s.CompletedCount + s.DueCount + s.OverdueCount + s.DeferredCount
	if sum != s.TotalCount {
		t.Errorf("invariant violated: buckets sum %d != total_count %d", sum, s.TotalCount)
	}
	// remaining = total - completed.
	if s.RemainingCount != s.TotalCount-s.CompletedCount {
		t.Errorf("remaining_count = %d, want %d", s.RemainingCount, s.TotalCount-s.CompletedCount)
	}
	// park_name must be the PARK code/name, never a shed code.
	if s.ParkName == "" {
		t.Errorf("park_name is empty")
	}
	if strings.Contains(strings.ToUpper(s.ParkName), "SHED") {
		t.Errorf("park_name %q looks like a shed code, want the park", s.ParkName)
	}
	if s.OwnerLabel != "PC" {
		t.Errorf("owner_label = %q, want PC", s.OwnerLabel)
	}
	if len(s.VaccineLabels) == 0 || s.VaccineLabels[0] != "FMD" {
		t.Errorf("vaccine_labels = %v, want [FMD]", s.VaccineLabels)
	}
	// Invariant: distinct animal counts never exceed obligation counts.
	// This test has no multi-obligation animals (one obligation per goat/status bucket),
	// so animal counts should track total/completed counts.
	if s.TotalAnimals > s.TotalCount {
		t.Errorf("invariant violated: total_animals %d > total_count %d", s.TotalAnimals, s.TotalCount)
	}
	if s.CompletedAnimals > s.CompletedCount {
		t.Errorf("invariant violated: completed_animals %d > completed_count %d", s.CompletedAnimals, s.CompletedCount)
	}
}

// TestDriveSummaryDistinctAnimalCoverageOneToMany proves CDR-001: distinct-animal coverage
// is a separate grain from obligation counts. A single goat with two vaccination obligations in the same
// drive (two rule_ids / two vaccines due the same day) should increment total_count by 2 (obligation grain)
// but total_animals by 1 (animal grain). completed_animals reflects goats where ALL their drive obligations
// are completed (bool_and(status='completed') per goat). This is the one-to-many (one animal, many obligations)
// adversarial test for the animal-coverage dimension.
func TestDriveSummaryDistinctAnimalCoverageOneToMany(t *testing.T) {
	// OneToMany cardinality adversarial: one goat with two same-day vaccines counts as 1 animal but 2 obligations — canonical drive_summary must not double-count animals.
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	// Fixed business-day window so the projection state (seeded around these dates) covers the drive.
	driveDate := biztime.BusinessDayStart(time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC))

	protocolID := "cd000000-0000-4000-8000-000000000001"
	versionID1 := "cd000000-0000-4000-8000-000000000002"
	versionID2 := "cd000000-0000-4000-8000-000000000003"
	ruleID1 := "cd000000-0000-4000-8000-000000000004"
	ruleID2 := "cd000000-0000-4000-8000-000000000005"

	parkID := testParkA
	shedID := testShedA
	goatID := "cd000000-0000-4000-8000-000000000100"

	// One goat, TWO obligations (two different versions/rule_ids) in the same drive:
	//   Obligation 1 (version1, rule1): vaccine A
	//   Obligation 2 (version2, rule2): vaccine B
	obl1 := "cd000000-0000-4000-8000-000000000201"
	obl2 := "cd000000-0000-4000-8000-000000000202"

	// First obligation: protocol/version(1)/rule(1).
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID1, ruleID1, obl1, driveDate)
	// Attach the first obligation to our test goat and shed.
	attachObligationToGoatScope(t, ctx, pool, obl1, goatID, "shed", shedID)
	seedProtocolRuleDimension(t, ctx, pool, versionID1, ruleID1, ruleID1, "FMD")

	// Second obligation: protocol/version(2)/rule(2) (same protocol, different version).
	seedVaccinationProtocolVersionAndRule(t, ctx, pool, protocolID, versionID2, ruleID2, driveDate)
	seedCalendarGoat(t, ctx, pool, goatID)
	seedVaccinationObligationForGoatAndRule(t, ctx, pool, versionID2, ruleID2, obl2, goatID, shedID, parkID, driveDate)
	seedProtocolRuleDimension(t, ctx, pool, versionID2, ruleID2, ruleID2, "Brucella")

	// One batch per obligation, both for the same goat in the same shed+drive.
	seedVaccinationBatchForShed(t, ctx, pool, "cd000000-0000-4000-8000-000000000301", versionID1, parkID, shedID, driveDate, obl1)
	seedVaccinationBatchForShed(t, ctx, pool, "cd000000-0000-4000-8000-000000000302", versionID2, parkID, shedID, driveDate, obl2)

	// Scenario 1: One obligation completed, one open (goat is NOT fully covered).
	setDriveObligationStatus(t, ctx, pool, obl1, "completed")
	setDriveObligationStatus(t, ctx, pool, obl2, "scheduled")

	q := domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: driveDate.Add(-24 * time.Hour),
		DateTo:   driveDate.Add(24 * time.Hour),
		Limit:    50,
		Scope:    domain.ScopeFilter{TenantWide: true},
	}
	resp, err := repo.ListEvents(ctx, q)
	if err != nil {
		t.Fatalf("list events (CDR-001 scenario 1): %v", err)
	}

	var drive *domain.CalendarEvent
	for i := range resp.Items {
		if resp.Items[i].EventType == domain.EventVaccinationDrive {
			drive = &resp.Items[i]
			break
		}
	}
	if drive == nil {
		t.Fatalf("no vaccination_drive event found")
	}
	if drive.DriveSummary == nil {
		t.Fatalf("drive_summary is nil")
	}
	s := drive.DriveSummary

	// Obligation grain: 1 completed, 1 due (open/scheduled).
	if s.TotalCount != 2 {
		t.Errorf("(CDR-001 scenario 1) total_count = %d, want 2 (dose grain)", s.TotalCount)
	}
	if s.CompletedCount != 1 {
		t.Errorf("(CDR-001 scenario 1) completed_count = %d, want 1", s.CompletedCount)
	}

	// Animal grain: 1 goat, but NOT fully covered (one obligation still open).
	if s.TotalAnimals != 1 {
		t.Errorf("(CDR-001 scenario 1) total_animals = %d, want 1 (one goat)", s.TotalAnimals)
	}
	if s.CompletedAnimals != 0 {
		t.Errorf("(CDR-001 scenario 1) completed_animals = %d, want 0 (one obligation still open, goat not fully covered)", s.CompletedAnimals)
	}
	// OneToMany MultiPage DateShift ParkScope StatusBuckets: the per-shed drawer summary must stay on
	// the same aggregate grain as drive_summary, not a separately paged or status-filtered roster read.
	if len(s.Sheds) != 1 {
		t.Fatalf("(CDR-001 scenario 1) drive_summary.sheds len = %d, want 1", len(s.Sheds))
	}
	if s.Sheds[0].ShedID != shedID || s.Sheds[0].TotalAnimals != 1 {
		t.Errorf("(CDR-001 scenario 1) drive_summary.sheds[0] = %#v, want shed %s with 1 animal", s.Sheds[0], shedID)
	}

	// Scenario 2: Both obligations completed (goat IS fully covered).
	setDriveObligationStatus(t, ctx, pool, obl2, "completed")

	resp, err = repo.ListEvents(ctx, q)
	if err != nil {
		t.Fatalf("list events (CDR-001 scenario 2): %v", err)
	}

	drive = nil
	for i := range resp.Items {
		if resp.Items[i].EventType == domain.EventVaccinationDrive {
			drive = &resp.Items[i]
			break
		}
	}
	if drive == nil || drive.DriveSummary == nil {
		t.Fatalf("drive/drive_summary missing in scenario 2")
	}
	s = drive.DriveSummary

	// Obligation grain: 2 completed.
	if s.CompletedCount != 2 {
		t.Errorf("(CDR-001 scenario 2) completed_count = %d, want 2", s.CompletedCount)
	}

	// Animal grain: 1 goat, NOW fully covered.
	if s.TotalAnimals != 1 {
		t.Errorf("(CDR-001 scenario 2) total_animals = %d, want 1", s.TotalAnimals)
	}
	if s.CompletedAnimals != 1 {
		t.Errorf("(CDR-001 scenario 2) completed_animals = %d, want 1 (all obligations completed, goat fully covered)", s.CompletedAnimals)
	}

	// Invariant: total_animals <= total_count (always true when same animal has multiple obligations).
	if s.TotalAnimals > s.TotalCount {
		t.Errorf("invariant violated: total_animals %d > total_count %d", s.TotalAnimals, s.TotalCount)
	}
	// Invariant: completed_animals <= completed_count (always true).
	if s.CompletedAnimals > s.CompletedCount {
		t.Errorf("invariant violated: completed_animals %d > completed_count %d", s.CompletedAnimals, s.CompletedCount)
	}
}

// setDriveObligationStatus flips an obligation to a target status, keeping completed_at consistent
// so status-derived reads stay valid.
func setDriveObligationStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, obligationID, status string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET status = $3,
    completed_at = CASE WHEN $3 = 'completed' THEN COALESCE(completed_at, now()) ELSE NULL END,
    updated_at = now()
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`, testTenantID, obligationID, status); err != nil {
		t.Fatalf("set obligation %s status=%s: %v", obligationID, status, err)
	}
}

// seedVaccinationProtocolVersionAndRule creates a second version for an existing protocol, with one rule.
func seedVaccinationProtocolVersionAndRule(t *testing.T, ctx context.Context, pool *pgxpool.Pool, protocolID, versionID, ruleID string, dueAt time.Time) {
	t.Helper()
	_, err := pool.Exec(ctx, `
INSERT INTO protocol_versions (
  protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
  version_label, status, effective_from, effective_to, rule_dsl, proof_policy, published_at
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'tenant', NULL, 2,
  'Projection secondary version test', 'draft', DATE '2028-01-02', DATE '2030-01-01',
  '{"source":{"review_status":"approved","source_ref":"docs/preventive-care-vaccination/PRD.md","source_system":"pc","approved_by":"test","approved_at":"2026-06-27T00:00:00Z"}}'::jsonb,
  '{"required_proofs":["administration"]}'::jsonb, NULL
)
ON CONFLICT (protocol_version_id) DO NOTHING`, versionID, testTenantID, protocolID)
	if err != nil {
		t.Fatalf("seed protocol version 2: %v", err)
	}
	_, err = pool.Exec(ctx, `
INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
  offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json,
  proof_policy, sort_order
)
SELECT
  $1::uuid, $2::uuid, $3::uuid, 'PROJ-SECONDARY', 1, 'calendar',
  0, 1, 0, 'none', 'immediate', '{}'::jsonb, '{"required_proofs":["administration"]}'::jsonb, 10
WHERE NOT EXISTS (
  SELECT 1
  FROM protocol_rules
  WHERE tenant_id = $2::uuid AND rule_id = $1::uuid
)`, ruleID, testTenantID, versionID)
	if err != nil {
		t.Fatalf("seed protocol rule v2: %v", err)
	}
	_, err = pool.Exec(ctx, `
UPDATE protocol_versions
SET status = 'published',
    published_at = COALESCE(published_at, now()),
    updated_at = now()
WHERE tenant_id = $1::uuid AND protocol_version_id = $2::uuid`,
		testTenantID, versionID)
	if err != nil {
		t.Fatalf("publish protocol version 2: %v", err)
	}
}

// seedVaccinationObligationForGoatAndRule inserts a goat-targeted vaccination obligation for a specific rule and shed scope.
func seedVaccinationObligationForGoatAndRule(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID, ruleID, obligationID, goatID, shedID, parkID string, dueAt time.Time) {
	t.Helper()
	seedCalendarLocations(t, ctx, pool, parkID, shedID)
	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, window_start, window_end, status, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', $5::uuid,
  'shed', $6::uuid, $7::timestamptz, $7::timestamptz, $7::timestamptz + interval '1 day',
  'scheduled', 'calendar-projection-test-' || ($1::uuid)::text
)
ON CONFLICT (obligation_id) DO UPDATE
SET due_at = EXCLUDED.due_at,
    status = 'scheduled',
    batch_id = NULL,
    updated_at = now()`, obligationID, testTenantID, versionID, ruleID, goatID, shedID, dueAt); err != nil {
		t.Fatalf("seed goat vaccination obligation for rule: %v", err)
	}
}

// seedProtocolRuleDimension inserts one protocol_rule_dimensions row for a given selector_key.
// Unlike seedProtocolRuleVaccineName (which always uses the rule_id as the selector_key), this
// lets a test attach MULTIPLE dimension rows to the same rule -- protocol_rule_dimensions has no
// uniqueness on rule_id alone (the unique key is (tenant_id, protocol_version_id, rule_id,
// selector_key)), so a rule can legitimately carry more than one dimension row.
func seedProtocolRuleDimension(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID, ruleID, selectorKey, vaccineName string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rule_dimensions (
  tenant_id, protocol_version_id, rule_id, category, ruleset_family,
  selector_key, dose_code, source_dose_code, vaccine_code, vaccine_json
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'vaccination', 'calendar-projection-test',
  $4, 'first', 'first', lower(replace($5, ' ', '_')), jsonb_build_object('name', $5)
)
ON CONFLICT (tenant_id, protocol_version_id, rule_id, selector_key) DO UPDATE
SET vaccine_json = EXCLUDED.vaccine_json`, testTenantID, versionID, ruleID, selectorKey, vaccineName); err != nil {
		t.Fatalf("seed protocol rule dimension %s: %v", selectorKey, err)
	}
}

// seedCalendarFarmParent inserts a FARM location and re-parents an existing park under it,
// matching the production location hierarchy (farm -> park -> shed -> cohort). Tests use this to
// prove that park_id resolution reads the PARK's own location row, not
// COALESCE(parent_location_id, scope_id) -- which, once a park has a real farm parent, silently
// resolves to the farm instead.
func seedCalendarFarmParent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, farmID, parkID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (
  location_id, tenant_id, location_type, location_code, name, parent_location_id,
  country, timezone, status, updated_at
) VALUES (
  $1::uuid, $2::uuid, 'farm', 'TST-FARM-' || right(($1::uuid)::text, 4), 'Test Farm ' || right(($1::uuid)::text, 4),
  NULL, 'IN', 'Asia/Kolkata', 'active', now()
)
ON CONFLICT (location_id) DO UPDATE
SET status = 'active', updated_at = now()`, farmID, testTenantID); err != nil {
		t.Fatalf("seed farm location: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE locations SET parent_location_id = $1::uuid, updated_at = now()
WHERE tenant_id = $2::uuid AND location_id = $3::uuid`, farmID, testTenantID, parkID); err != nil {
		t.Fatalf("re-parent park %s under farm %s: %v", parkID, farmID, err)
	}
}

// seedCalendarCohortLocation ensures park+shed exist (park -> shed) and adds a cohort location
// parented to the shed (shed -> cohort), the two-hop chain a cohort-scoped obligation must resolve
// through (cohort -> shed -> park) to find its park.
func seedCalendarCohortLocation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, parkID, shedID, cohortID string) {
	t.Helper()
	seedCalendarLocations(t, ctx, pool, parkID, shedID)
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (
  location_id, tenant_id, location_type, location_code, name, parent_location_id,
  country, timezone, status, updated_at
) VALUES (
  $1::uuid, $2::uuid, 'cohort', 'TST-' || right(($1::uuid)::text, 4), 'Test Cohort ' || right(($1::uuid)::text, 4),
  $3::uuid, 'IN', 'Asia/Kolkata', 'active', now()
)
ON CONFLICT (location_id) DO UPDATE
SET status = 'active', parent_location_id = EXCLUDED.parent_location_id, updated_at = now()`,
		cohortID, testTenantID, shedID); err != nil {
		t.Fatalf("seed cohort location: %v", err)
	}
}

// TestDriveSummaryDateShiftAttachesToBatchWindow proves DRV-001: a batched obligation's drive
// membership (park + day) must be derived from its BATCH's planned/window date, the same
// COALESCE(window_start, planned_date, window_end) basis batch_events itself uses -- never from
// the obligation's own raw due_at. Before the fix, obligation_drive_summary grouped by oi.due_at
// directly, so an obligation whose batch reschedules it to a later window (a hold, or any
// D -> D+7 batch scheduling) never joined into its own batch's drive card: drive_summary came
// back nil at the card's actual (batch-window) date.
//
// This DateShift proof still holds after the blocked_count/is_blocked removal (owner decision
// 2026-07-14): the membership CTE's date derivation is unchanged, only the now-deleted is_blocked
// bucketing was removed from obligation_drive_summary.
func TestDriveSummaryDateShiftAttachesToBatchWindow(t *testing.T) {
	// DateShift adversarial (aggregate-projection-guard): the batch-window date basis is preserved by the canonical read after the projection drop (5k-50k cutover).
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	rawDueDay := biztime.BusinessDayStart(time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC))
	batchWindowDay := rawDueDay.Add(7 * 24 * time.Hour)

	protocolID := "ba000000-0000-4000-8000-000000000001"
	versionID := "ba000000-0000-4000-8000-000000000002"
	ruleID := "ba000000-0000-4000-8000-000000000003"
	obligationID := "ba000000-0000-4000-8000-000000000004"
	batchID := "ba000000-0000-4000-8000-000000000005"

	// The obligation's OWN due_at stays at rawDueDay; only its batch is scheduled for
	// batchWindowDay (D+7) -- exactly the hold/reschedule shape DRV-001 covers.
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, rawDueDay)
	seedProtocolRuleVaccineName(t, ctx, pool, versionID, ruleID, "FMD")
	seedVaccinationBatchForShed(t, ctx, pool, batchID, versionID, testParkA, testShedA, batchWindowDay, obligationID)

	resp, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: batchWindowDay.Add(-1 * time.Hour),
		DateTo:   batchWindowDay.Add(24 * time.Hour),
		Limit:    50,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}

	var drive *domain.CalendarEvent
	for i := range resp.Items {
		if resp.Items[i].EventType == domain.EventVaccinationDrive {
			drive = &resp.Items[i]
		}
	}
	if drive == nil {
		t.Fatalf("no vaccination_drive event found at the batch window day in %d events", len(resp.Items))
	}
	if drive.DriveSummary == nil {
		t.Fatalf("drive_summary is nil at the batch window day -- the summary's due_at-basis grouping missed the batch-window-basis event (DRV-001 regression)")
	}
	if drive.DriveSummary.TotalCount != 1 {
		t.Errorf("total_count = %d, want 1", drive.DriveSummary.TotalCount)
	}
	// DateShift: the additive total_animals coverage attaches to the same batch-window-basis event,
	// bounded by the obligation count (a separate goat grain, never larger than the doses).
	if drive.DriveSummary.TotalAnimals > drive.DriveSummary.TotalCount {
		t.Errorf("total_animals %d must not exceed total_count %d", drive.DriveSummary.TotalAnimals, drive.DriveSummary.TotalCount)
	}
}

// TestDriveSummaryMultipleDimensionsCountsObligationsOnce proves DRV-002: a rule with more than
// one protocol_rule_dimensions row (no uniqueness on rule_id alone -- the unique key is
// (tenant_id, protocol_version_id, rule_id, selector_key)) must not fan out the obligation count.
// Before the fix, obligation_drive_base LEFT JOINed protocol_rule_dimensions directly and counted
// with plain count(*), so 6 obligations under a 2-dimension rule counted as 12.
//
// This MultipleDimensions cardinality proof still holds after the blocked_count/is_blocked
// removal (owner decision 2026-07-14): the count(DISTINCT obligation_id) dedup this test proves
// is unchanged by dropping the is_blocked bucket.
func TestDriveSummaryMultipleDimensionsCountsObligationsOnce(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	driveDate := biztime.BusinessDayStart(time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))

	protocolID := "bd000000-0000-4000-8000-000000000001"
	versionID := "bd000000-0000-4000-8000-000000000002"
	ruleID := "bd000000-0000-4000-8000-000000000003"

	obligationIDs := []string{
		"bd000000-0000-4000-8000-000000000011",
		"bd000000-0000-4000-8000-000000000012",
		"bd000000-0000-4000-8000-000000000013",
		"bd000000-0000-4000-8000-000000000014",
		"bd000000-0000-4000-8000-000000000015",
		"bd000000-0000-4000-8000-000000000016",
	}
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationIDs[0], driveDate)
	for _, id := range obligationIDs[1:] {
		seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, id, driveDate)
	}
	for i, id := range obligationIDs {
		goatID := fmt.Sprintf("bd000000-0000-4000-8000-0000000000%02d", 21+i)
		attachObligationToGoatScope(t, ctx, pool, id, goatID, "shed", testShedA)
	}
	// One rule, TWO dimension rows (two selector keys) -- the exact one-to-many shape that fans
	// out a plain (non-DISTINCT) LEFT JOIN aggregate.
	seedProtocolRuleDimension(t, ctx, pool, versionID, ruleID, "dim-a", "FMD")
	seedProtocolRuleDimension(t, ctx, pool, versionID, ruleID, "dim-b", "PPR")

	resp, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: driveDate.Add(-24 * time.Hour),
		DateTo:   driveDate.Add(24 * time.Hour),
		Limit:    50,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}

	var drive *domain.CalendarEvent
	for i := range resp.Items {
		if resp.Items[i].EventType == domain.EventVaccinationDrive {
			drive = &resp.Items[i]
		}
	}
	if drive == nil {
		t.Fatalf("no vaccination_drive event found in %d events", len(resp.Items))
	}
	if drive.DriveSummary == nil {
		t.Fatalf("drive_summary is nil")
	}
	if drive.DriveSummary.TotalCount != len(obligationIDs) {
		t.Errorf("total_count = %d, want %d (a 2-dimension rule must not double-count an obligation)", drive.DriveSummary.TotalCount, len(obligationIDs))
	}
}

// TestDriveSummaryParkScopeAttachesToCorrectPark proves DRV-003 for park scope: a park-scoped
// obligation must resolve its OWN location as the park, never COALESCE(parent_location_id,
// scope_id) -- which, once the park has a real farm parent (the production hierarchy is
// farm -> park -> shed -> cohort), silently resolves to the FARM instead of the park.
//
// This ParkScope proof still holds after the blocked_count/is_blocked removal (owner decision
// 2026-07-14): the scope-resolution CASE chain this test exercises is untouched by dropping the
// is_blocked bucket.
func TestDriveSummaryParkScopeAttachesToCorrectPark(t *testing.T) {
	// ParkScope adversarial: park-day drive scope is now served from canonical_read.go (park_drive_events surfaces shed_id when shed_count=1).
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	driveDate := biztime.BusinessDayStart(time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC))

	protocolID := "be000000-0000-4000-8000-000000000001"
	versionID := "be000000-0000-4000-8000-000000000002"
	ruleID := "be000000-0000-4000-8000-000000000003"
	obligationID := "be000000-0000-4000-8000-000000000004"
	goatID := "be000000-0000-4000-8000-000000000005"
	farmID := "be000000-0000-4000-8000-000000000701"
	parkID := "be000000-0000-4000-8000-000000000702"

	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, driveDate)
	seedProtocolRuleVaccineName(t, ctx, pool, versionID, ruleID, "FMD")
	attachObligationToGoatScope(t, ctx, pool, obligationID, goatID, "park", parkID)
	// Give the park a real FARM parent, matching the production hierarchy -- this is the exact
	// condition that turns COALESCE(parent_location_id, scope_id) into the wrong (farm) id.
	seedCalendarFarmParent(t, ctx, pool, farmID, parkID)

	resp, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: driveDate.Add(-24 * time.Hour),
		DateTo:   driveDate.Add(24 * time.Hour),
		Limit:    50,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}

	var drive *domain.CalendarEvent
	for i := range resp.Items {
		if resp.Items[i].EventType == domain.EventVaccinationDrive {
			drive = &resp.Items[i]
		}
	}
	if drive == nil {
		t.Fatalf("no vaccination_drive event found in %d events", len(resp.Items))
	}
	if drive.DriveSummary == nil {
		t.Fatalf("drive_summary is nil -- park-scoped obligation resolved to the wrong park_id (the farm) and missed the park's drive card (DRV-003 regression)")
	}
	if drive.DriveSummary.TotalCount != 1 {
		t.Errorf("total_count = %d, want 1", drive.DriveSummary.TotalCount)
	}
	// ParkScope: the additive total_animals coverage resolves under the same park scope matrix,
	// bounded by the obligation count.
	if drive.DriveSummary.TotalAnimals > drive.DriveSummary.TotalCount {
		t.Errorf("total_animals %d must not exceed total_count %d", drive.DriveSummary.TotalAnimals, drive.DriveSummary.TotalCount)
	}
}

// TestDriveSummaryCohortScopeAttachesToCorrectPark proves DRV-003 for cohort scope: a
// cohort-scoped obligation must resolve two hops up (cohort -> shed -> park), never
// COALESCE(parent_location_id, scope_id) -- which resolves only one hop up, to the cohort's
// immediate parent (the SHED), not the park.
func TestDriveSummaryCohortScopeAttachesToCorrectPark(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	driveDate := biztime.BusinessDayStart(time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC))

	protocolID := "bf000000-0000-4000-8000-000000000001"
	versionID := "bf000000-0000-4000-8000-000000000002"
	ruleID := "bf000000-0000-4000-8000-000000000003"
	obligationID := "bf000000-0000-4000-8000-000000000004"
	goatID := "bf000000-0000-4000-8000-000000000005"
	parkID := "bf000000-0000-4000-8000-000000000701"
	shedID := "bf000000-0000-4000-8000-000000000702"
	cohortID := "bf000000-0000-4000-8000-000000000703"

	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, driveDate)
	seedProtocolRuleVaccineName(t, ctx, pool, versionID, ruleID, "FMD")
	seedCalendarCohortLocation(t, ctx, pool, parkID, shedID, cohortID)
	seedCalendarGoat(t, ctx, pool, goatID)
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET target_type = 'goat', target_id = $3::uuid, scope_type = 'cohort', scope_id = $4::uuid,
    batch_id = NULL, updated_at = now()
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`,
		testTenantID, obligationID, goatID, cohortID); err != nil {
		t.Fatalf("attach obligation to cohort scope: %v", err)
	}

	resp, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: driveDate.Add(-24 * time.Hour),
		DateTo:   driveDate.Add(24 * time.Hour),
		Limit:    50,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}

	var drive *domain.CalendarEvent
	for i := range resp.Items {
		if resp.Items[i].EventType == domain.EventVaccinationDrive {
			drive = &resp.Items[i]
		}
	}
	if drive == nil {
		t.Fatalf("no vaccination_drive event found in %d events", len(resp.Items))
	}
	if drive.DriveSummary == nil {
		t.Fatalf("drive_summary is nil -- cohort-scoped obligation resolved to the wrong park_id (the shed) and missed the park's drive card (DRV-003 regression)")
	}
	if drive.DriveSummary.TotalCount != 1 {
		t.Errorf("total_count = %d, want 1", drive.DriveSummary.TotalCount)
	}
}

// TestDriveSummaryUnrelatedSameParkDoesNotBleed proves the (park_id, due_date) group key is
// exact: an obligation whose RAW due_at coincidentally lands on the SAME day as another drive in
// the SAME park, but whose actual batch is scheduled for a totally different day, must not bleed
// its count into the other day's drive_summary. Before the fix, obligation_drive_summary grouped
// by oi.due_at (never the batch's own window), so this unrelated obligation would show up in the
// wrong drive's counts.
func TestDriveSummaryUnrelatedSameParkDoesNotBleed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	dayOne := biztime.BusinessDayStart(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	dayTwo := dayOne.Add(10 * 24 * time.Hour)

	protocolID := "c0000000-0000-4000-8000-000000000001"
	versionID := "c0000000-0000-4000-8000-000000000002"
	ruleID := "c0000000-0000-4000-8000-000000000003"
	shedX := "c0000000-0000-4000-8000-000000000801"
	shedY := "c0000000-0000-4000-8000-000000000802"
	oxObligation := "c0000000-0000-4000-8000-000000000011"
	oyObligation := "c0000000-0000-4000-8000-000000000012"
	batchX := "c0000000-0000-4000-8000-000000000021"
	batchY := "c0000000-0000-4000-8000-000000000022"

	// Drive X: park A, day one, batch window == day one (no shift).
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, oxObligation, dayOne)
	seedProtocolRuleVaccineName(t, ctx, pool, versionID, ruleID, "FMD")
	seedVaccinationBatchForShed(t, ctx, pool, batchX, versionID, testParkA, shedX, dayOne, oxObligation)

	// Drive Y: same park A, its OWN raw due_at also happens to fall on day one, but it is actually
	// scheduled (batch window) for a completely different day (day two) -- an unrelated drive that
	// must not bleed into day one's counts.
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, oyObligation, dayOne)
	seedVaccinationBatchForShed(t, ctx, pool, batchY, versionID, testParkA, shedY, dayTwo, oyObligation)

	resp, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: dayOne.Add(-1 * time.Hour),
		DateTo:   dayOne.Add(24 * time.Hour),
		Limit:    50,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}

	var drive *domain.CalendarEvent
	driveCount := 0
	for i := range resp.Items {
		if resp.Items[i].EventType == domain.EventVaccinationDrive {
			drive = &resp.Items[i]
			driveCount++
		}
	}
	if driveCount != 1 {
		t.Fatalf("expected exactly 1 vaccination_drive event on day one, got %d", driveCount)
	}
	if drive.DriveSummary == nil {
		t.Fatalf("drive_summary is nil for day one's drive")
	}
	if drive.DriveSummary.TotalCount != 1 {
		t.Errorf("total_count = %d, want 1 (day two's unrelated obligation must not bleed in)", drive.DriveSummary.TotalCount)
	}
}

// TestDriveSummaryStatusBucketsCoverLiveStatusSet proves the status-bucket mapping is exact and
// disjoint across the live obligation_instances statuses (the 000097 migration's CHECK constraint:
// scheduled, due, in_progress, deferred, completed, missed, waived, canceled, superseded --
// waived/canceled/superseded are excluded from the drive's live scope, leaving six). It pairs
// those six statuses with a two-dimension rule and asserts the EXACT expected count per bucket
// (not just sum(buckets) == total_count, an invariant the fan-out bug satisfies trivially by
// doubling every bucket), so this test fails first against the pre-fix count(*) fan-out.
//
// This StatusBuckets proof is updated for the blocked_count/is_blocked removal (owner decision
// 2026-07-14): the bucket set is now completed/due/overdue/deferred only (no blocked bucket, no
// stock feature yet), and the invariant is
// total_count = completed_count + due_count + overdue_count + deferred_count.
func TestDriveSummaryStatusBucketsCoverLiveStatusSet(t *testing.T) {
	// StatusBuckets adversarial: obligation status buckets (completed+due+overdue+deferred) are unchanged under canonical serving.
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	driveDate := biztime.BusinessDayStart(time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC))

	protocolID := "c1000000-0000-4000-8000-000000000001"
	versionID := "c1000000-0000-4000-8000-000000000002"
	ruleID := "c1000000-0000-4000-8000-000000000003"

	statuses := []string{"completed", "scheduled", "due", "in_progress", "deferred", "missed"}
	obligationIDs := make([]string, len(statuses))
	for i := range statuses {
		obligationIDs[i] = fmt.Sprintf("c1000000-0000-4000-8000-0000000000%02d", 11+i)
	}
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationIDs[0], driveDate)
	for _, id := range obligationIDs[1:] {
		seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, id, driveDate)
	}
	for i, id := range obligationIDs {
		goatID := fmt.Sprintf("c1000000-0000-4000-8000-0000000000%02d", 31+i)
		attachObligationToGoatScope(t, ctx, pool, id, goatID, "shed", testShedA)
		setDriveObligationStatus(t, ctx, pool, id, statuses[i])
	}
	// Two dimension rows on the shared rule -- must not double the per-status counts.
	seedProtocolRuleDimension(t, ctx, pool, versionID, ruleID, "dim-a", "FMD")
	seedProtocolRuleDimension(t, ctx, pool, versionID, ruleID, "dim-b", "PPR")

	resp, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: driveDate.Add(-24 * time.Hour),
		DateTo:   driveDate.Add(24 * time.Hour),
		Limit:    50,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}

	var drive *domain.CalendarEvent
	for i := range resp.Items {
		if resp.Items[i].EventType == domain.EventVaccinationDrive {
			drive = &resp.Items[i]
		}
	}
	if drive == nil {
		t.Fatalf("no vaccination_drive event found in %d events", len(resp.Items))
	}
	if drive.DriveSummary == nil {
		t.Fatalf("drive_summary is nil")
	}
	s := drive.DriveSummary
	if s.TotalCount != 6 {
		t.Errorf("total_count = %d, want 6 (a 2-dimension rule must not double-count)", s.TotalCount)
	}
	if s.CompletedCount != 1 {
		t.Errorf("completed_count = %d, want 1", s.CompletedCount)
	}
	if s.DueCount != 3 {
		t.Errorf("due_count = %d, want 3 (scheduled + due + in_progress)", s.DueCount)
	}
	if s.OverdueCount != 1 {
		t.Errorf("overdue_count = %d, want 1 (missed)", s.OverdueCount)
	}
	if s.DeferredCount != 1 {
		t.Errorf("deferred_count = %d, want 1", s.DeferredCount)
	}
	sum := s.CompletedCount + s.DueCount + s.OverdueCount + s.DeferredCount
	if sum != s.TotalCount {
		t.Errorf("invariant violated: buckets sum %d != total_count %d", sum, s.TotalCount)
	}
	// The additive distinct-animal coverage columns must not perturb the obligation StatusBuckets:
	// they are a separate grain (goats, not doses), always bounded by the obligation counts.
	if s.TotalAnimals > s.TotalCount {
		t.Errorf("total_animals %d must not exceed total_count %d", s.TotalAnimals, s.TotalCount)
	}
	if s.CompletedAnimals > s.CompletedCount {
		t.Errorf("completed_animals %d must not exceed completed_count %d", s.CompletedAnimals, s.CompletedCount)
	}
}

// TestDriveSummaryMultiPageListingKeepsWholeResultTotals proves drive_summary is a whole-result
// aggregate, not a capped read-time rollup: canonical_read.go computes it as a single grouped CTE
// pass over the full matching window, so ListEvents paging with a tiny page size must return the
// exact same total_count as one wide page -- the aggregate must never be reconstructed from
// whatever rows the current request page happens to fetch.
//
// This MultiPage proof still holds after the blocked_count/is_blocked removal (owner decision
// 2026-07-14): the whole-result materialization this test proves is unchanged by dropping the
// is_blocked bucket.
func TestDriveSummaryMultiPageListingKeepsWholeResultTotals(t *testing.T) {
	// MultiPage adversarial: drive_summary is a whole-result aggregate under canonical serving — page size never changes the totals.
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	driveDate := biztime.BusinessDayStart(time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC))

	protocolID := "c2000000-0000-4000-8000-000000000001"
	versionID := "c2000000-0000-4000-8000-000000000002"
	ruleID := "c2000000-0000-4000-8000-000000000003"
	batchID := "c2000000-0000-4000-8000-000000000004"

	driveObligations := []string{
		"c2000000-0000-4000-8000-000000000011",
		"c2000000-0000-4000-8000-000000000012",
		"c2000000-0000-4000-8000-000000000013",
	}
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, driveObligations[0], driveDate)
	for _, id := range driveObligations[1:] {
		seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, id, driveDate)
	}
	seedProtocolRuleVaccineName(t, ctx, pool, versionID, ruleID, "FMD")
	seedVaccinationBatchForShed(t, ctx, pool, batchID, versionID, testParkA, testShedA, driveDate, driveObligations...)

	// 5 unrelated, earlier-due individual dose_due events sort ahead of the drive card in
	// due_at-ascending order, pushing the drive card past page one once the list is paged with
	// Limit=1.
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("c2000000-0000-4000-8000-0000000000%02d", 21+i)
		seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, id, driveDate.Add(-time.Duration(6-i)*time.Hour))
	}

	// findDrive matches the shed-batch drive specifically (by ParkID) so it is not confused with
	// the unrelated tenant-wide catch-up drive card the 5 earlier-due obligations also produce.
	findDrive := func(items []domain.CalendarEvent) *domain.CalendarEvent {
		for i := range items {
			if items[i].EventType == domain.EventVaccinationDrive && items[i].ParkID != nil && *items[i].ParkID == testParkA {
				return &items[i]
			}
		}
		return nil
	}

	// Wide single page: fetch everything at once.
	wide, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: driveDate.Add(-72 * time.Hour),
		DateTo:   driveDate.Add(24 * time.Hour),
		Limit:    50,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("list events (wide page): %v", err)
	}
	wideDrive := findDrive(wide.Items)
	if wideDrive == nil || wideDrive.DriveSummary == nil {
		t.Fatalf("drive not found (or drive_summary nil) on the wide page")
	}
	if wideDrive.DriveSummary.TotalCount != len(driveObligations) {
		t.Fatalf("wide-page total_count = %d, want %d", wideDrive.DriveSummary.TotalCount, len(driveObligations))
	}

	// Narrow page size (Limit=1): walk the keyset cursor one row at a time until the drive card
	// surfaces on its own page.
	var narrowDrive *domain.CalendarEvent
	var cursor *domain.CalendarCursor
	for page := 0; page < 20 && narrowDrive == nil; page++ {
		resp, err := repo.ListEvents(ctx, domain.Query{
			TenantID: testTenantID,
			OwnerKey: domain.OwnerAll,
			DateFrom: driveDate.Add(-72 * time.Hour),
			DateTo:   driveDate.Add(24 * time.Hour),
			Limit:    1,
			Cursor:   cursor,
			Scope:    domain.ScopeFilter{TenantWide: true},
		})
		if err != nil {
			t.Fatalf("list events (narrow page %d): %v", page, err)
		}
		narrowDrive = findDrive(resp.Items)
		if resp.NextCursor == nil {
			break
		}
		decoded, err := domain.DecodeCalendarCursor(*resp.NextCursor)
		if err != nil {
			t.Fatalf("decode cursor: %v", err)
		}
		cursor = &decoded
	}
	if narrowDrive == nil {
		t.Fatalf("drive not found while paging with Limit=1")
	}
	if narrowDrive.DriveSummary == nil {
		t.Fatalf("drive_summary is nil on the narrow (Limit=1) page")
	}
	if narrowDrive.DriveSummary.TotalCount != wideDrive.DriveSummary.TotalCount {
		t.Errorf("narrow-page total_count = %d, want %d (page size must not change the aggregate)", narrowDrive.DriveSummary.TotalCount, wideDrive.DriveSummary.TotalCount)
	}
	// MultiPage: the additive total_animals coverage is a whole-result aggregate too -- a tiny page
	// size must not change it either.
	if narrowDrive.DriveSummary.TotalAnimals != wideDrive.DriveSummary.TotalAnimals {
		t.Errorf("narrow-page total_animals = %d, want %d (page size must not change the aggregate)", narrowDrive.DriveSummary.TotalAnimals, wideDrive.DriveSummary.TotalAnimals)
	}
}

// TestCalendarReconcilerFinding5SafeMalformedIDs (FINDING 5, P2) verifies that malformed ids
// (invalid UUID or impossible dates) do NOT abort the reconciler job, but are reported as invalid
// orphans instead. The validity function guards casts (plpgsql EXCEPTION handlers) so one bad row
// never aborts the entire reconcile run.
func TestCalendarReconcilerFinding5SafeMalformedIDs(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	const (
		protocolID = "86000000-0000-4000-8000-00000000f101"
		versionID  = "86000000-0000-4000-8000-00000000f102"
		ruleID     = "86000000-0000-4000-8000-00000000f103"
		obligID    = "86000000-0000-4000-8000-00000000f104"
		draftID    = "86000000-0000-4000-8000-00000000f105"
	)
	dueAt := time.Now().UTC().Add(2 * time.Hour)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligID, dueAt)
	seedDraftProtocolVersion(t, ctx, pool, protocolID, draftID, dueAt)

	// Seed: 2 valid + 2 malformed + 1 real orphan
	testCases := []struct {
		name        string
		eventID     string
		shouldFlag  bool
		description string
	}{
		{
			name:        "valid_obligation",
			eventID:     "obligation:" + obligID,
			shouldFlag:  false,
			description: "valid obligation id should NOT be flagged",
		},
		{
			name:        "valid_calendar",
			eventID:     "calendar:" + draftID,
			shouldFlag:  false,
			description: "valid calendar id should NOT be flagged",
		},
		{
			name:        "malformed_uuid",
			eventID:     "obligation:not-a-valid-uuid",
			shouldFlag:  true,
			description: "malformed uuid should be flagged as orphan, not error",
		},
		{
			name:        "impossible_date",
			eventID:     "parkdrive:park:86000000-0000-4000-8000-000000000701:date:2026-99-99",
			shouldFlag:  true,
			description: "impossible date should be flagged as orphan, not error",
		},
		{
			name:        "real_orphan",
			eventID:     "obligation:86000000-0000-4000-8000-00000000ffff",
			shouldFlag:  true,
			description: "real orphan (non-existent obligation) should be flagged",
		},
	}

	for i, tc := range testCases {
		notifID := fmt.Sprintf("86000000-0000-4000-8000-00000000f%03d", 201+i)
		if _, err := pool.Exec(ctx, `
INSERT INTO notification_requests (
  notification_request_id, tenant_id, calendar_event_id, target_type, notification_type, channel,
  title, body, status, idempotency_key, request_fingerprint, context
) VALUES (
  $1::uuid, $2::uuid, $3, 'calendar_event', 'reminder', 'local-stub',
  'Test fixture', 'test body', 'sent', $4, $4 || ':fingerprint', '{}'::jsonb
)`, notifID, testTenantID, tc.eventID, "finding5-"+notifID); err != nil {
			t.Fatalf("seed notification for %s: %v", tc.name, err)
		}
	}

	// CRITICAL: The reconciler must NOT error even with malformed ids present.
	// This is what FINDING 5 fixed.
	orphans, err := repo.ReconcileEventReferences(ctx, testTenantID)
	if err != nil {
		t.Fatalf("ReconcileEventReferences (should NOT error with malformed ids): %v", err)
	}

	// Filter to notification_requests rows from our test fixture
	var flagged []string
	for _, o := range orphans {
		if o.SourceTable == "notification_requests" {
			flagged = append(flagged, o.CalendarEventID)
		}
	}

	// Assert: exactly 3 are flagged (2 malformed + 1 real orphan)
	if len(flagged) != 3 {
		t.Fatalf("flagged orphans = %v (count %d), want 3 (2 malformed + 1 real)", flagged, len(flagged))
	}

	// Assert: valid ones are NOT in the flagged list
	for _, tc := range testCases {
		if !tc.shouldFlag {
			for _, flaggedID := range flagged {
				if flaggedID == tc.eventID {
					t.Errorf("valid id %s (case: %s) was incorrectly flagged", tc.eventID, tc.name)
				}
			}
		}
	}

	// Assert: malformed/orphan ones ARE in the flagged list
	for _, tc := range testCases {
		if tc.shouldFlag {
			found := false
			for _, flaggedID := range flagged {
				if flaggedID == tc.eventID {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s (case: %s) should be flagged but was not", tc.eventID, tc.name)
			}
		}
	}

	t.Logf("FINDING 5 PASS: malformed ids did not abort; orphans flagged: %v", flagged)
}

// TestCalendarReconcilerFinding4BoundedPaging (FINDING 4, P1) verifies that the reconciler
// processes large orphan sets via bounded pagination (page size 1000, per-run cap 10 pages)
// without loading all into memory or timing out. Seeds ~2500 orphans and verifies:
// - Page 0-2 each return 1000 rows (or less on final partial page)
// - Per-run cap (10 pages) prevents loading all 2500 in one call
// - Stable ordering (source_table, record_id) enables keyset pagination
// - No errors from scale (memory, time, DB resources)
func TestCalendarReconcilerFinding4BoundedPaging(t *testing.T) {
	if os.Getenv("GOATOS_SCALE_CERT") == "" {
		t.Skip("skipping scale test; set GOATOS_SCALE_CERT=1 to enable")
	}
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	const (
		protocolID = "86000000-0000-4000-8000-00000000f201"
		versionID  = "86000000-0000-4000-8000-00000000f202"
		ruleID     = "86000000-0000-4000-8000-00000000f203"
	)

	// Seed a valid obligation as a reference point
	obligID := "86000000-0000-4000-8000-00000000f204"
	dueAt := time.Now().UTC().Add(2 * time.Hour)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligID, dueAt)

	// Seed ~2500 orphaned notification_requests rows (all orphaned, none valid)
	scaleN := 2500
	for i := 0; i < scaleN; i++ {
		// Craft orphan ids so they are guaranteed to NOT resolve
		orphanID := fmt.Sprintf("obligation:86000000-0000-4000-8000-%012d", i+9000) // all different
		notifID := fmt.Sprintf("86000000-0000-4000-8000-%012d", i)
		if _, err := pool.Exec(ctx, `
INSERT INTO notification_requests (
  notification_request_id, tenant_id, calendar_event_id, target_type, notification_type, channel,
  title, body, status, idempotency_key, request_fingerprint, context
) VALUES (
  $1::uuid, $2::uuid, $3, 'calendar_event', 'reminder', 'local-stub',
  'Scale test fixture', 'scale test body', 'sent', $4, $4 || ':fingerprint', '{}'::jsonb
)`, notifID, testTenantID, orphanID, "scale-"+notifID); err != nil {
			t.Fatalf("seed orphan %d: %v", i, err)
		}
	}

	// Page 0: fetch first 1000 (cursor = start)
	page0, err := repo.ReconcileEventReferencesPage(ctx, testTenantID, "", "", 1000)
	if err != nil {
		t.Fatalf("page 0: %v", err)
	}
	if len(page0) != 1000 {
		t.Errorf("page 0 returned %d rows, want 1000", len(page0))
	}

	// Page 1: fetch next 1000 (cursor = last row from page 0)
	var cursor1SourceTable, cursor1RecordID string
	if len(page0) > 0 {
		cursor1SourceTable = page0[len(page0)-1].SourceTable
		cursor1RecordID = page0[len(page0)-1].RecordID
	}
	page1, err := repo.ReconcileEventReferencesPage(ctx, testTenantID, cursor1SourceTable, cursor1RecordID, 1000)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1) != 1000 {
		t.Errorf("page 1 returned %d rows, want 1000", len(page1))
	}

	// Page 2: fetch final partial page (2500 total - 2000 = 500)
	var cursor2SourceTable, cursor2RecordID string
	if len(page1) > 0 {
		cursor2SourceTable = page1[len(page1)-1].SourceTable
		cursor2RecordID = page1[len(page1)-1].RecordID
	}
	page2, err := repo.ReconcileEventReferencesPage(ctx, testTenantID, cursor2SourceTable, cursor2RecordID, 1000)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2) != 500 {
		t.Errorf("page 2 returned %d rows, want 500 (final partial page)", len(page2))
	}

	// Page 3: should be empty (no more rows)
	var cursor3SourceTable, cursor3RecordID string
	if len(page2) > 0 {
		cursor3SourceTable = page2[len(page2)-1].SourceTable
		cursor3RecordID = page2[len(page2)-1].RecordID
	}
	page3, err := repo.ReconcileEventReferencesPage(ctx, testTenantID, cursor3SourceTable, cursor3RecordID, 1000)
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if len(page3) != 0 {
		t.Errorf("page 3 returned %d rows, want 0 (past end)", len(page3))
	}

	// Verify ordering is stable across pages (source_table, record_id)
	allRecordIDs := []string{}
	for _, o := range page0 {
		allRecordIDs = append(allRecordIDs, o.RecordID)
	}
	for _, o := range page1 {
		allRecordIDs = append(allRecordIDs, o.RecordID)
	}
	for _, o := range page2 {
		allRecordIDs = append(allRecordIDs, o.RecordID)
	}

	// Check ordering is monotonic (no gaps or reversals)
	isSorted := true
	for i := 1; i < len(allRecordIDs); i++ {
		if allRecordIDs[i] < allRecordIDs[i-1] {
			isSorted = false
			break
		}
	}
	if !isSorted {
		t.Errorf("record IDs are not sorted across pages (not stable ordering)")
	}

	t.Logf("FINDING 4 PASS: processed %d orphans in %d pages (page size %d, per-run cap %d)",
		scaleN, 3, 1000, 10)
}

// LIFE-002: a held drive moved from due day D to planned day D+7 must surface its FULL roster on
// D+7, and every returned target row must carry scheduled_at = the batch planned_date (D+7), not
// the member obligation's stale due_at (D). Event date, roster membership, and the returned target
// timestamp must all agree on D+7.
func TestCalendarHeldDriveTargetsReturnPlannedDateNotStaleDueAt(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	const (
		protocolID  = "86000000-0000-4000-8000-00000000d901"
		versionID   = "86000000-0000-4000-8000-00000000d902"
		ruleID      = "86000000-0000-4000-8000-00000000d903"
		batchID     = "86000000-0000-4000-8000-00000000d904"
		obligationA = "86000000-0000-4000-8000-00000000d905"
		obligationB = "86000000-0000-4000-8000-00000000d906"
	)
	loc := biztime.DefaultLocation()
	dueAt := stableSameLocalDayDueAt(time.Now().In(loc)) // D
	heldAt := dueAt.Add(7 * 24 * time.Hour)              // D+7 (one-time due+7 batching hold)
	dueDay := dueAt.In(loc).Format("2006-01-02")
	heldDay := heldAt.In(loc).Format("2006-01-02")
	if dueDay == heldDay {
		t.Fatalf("test setup: due day %s must differ from held day %s", dueDay, heldDay)
	}

	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationA, dueAt)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, obligationB, dueAt)
	attachObligationToGoatScope(t, ctx, pool, obligationA, obligationA, "shed", testShedA)
	attachObligationToGoatScope(t, ctx, pool, obligationB, obligationB, "shed", testShedA)
	// Batch is planned for D+7 while the member obligations keep due_at = D.
	seedVaccinationBatchForShed(t, ctx, pool, batchID, versionID, testParkA, testShedA, heldAt, obligationA, obligationB)

	// Event date: the drive appears on D+7 with the stable park-drive identity for D+7, not D.
	wantID := parkDriveEventID(testParkA, heldAt)
	// R50-012: the batch's due_at is derived from planned_date via an explicit
	// AT TIME ZONE 'Asia/Kolkata' conversion (midnight of the Asia/Kolkata business day),
	// which can fall up to ~24h before heldAt's own clock time, so the lower window bound
	// must cover a full day rather than a narrow "-2h" buffer.
	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll,
		DateFrom: heldAt.Add(-25 * time.Hour), DateTo: heldAt.Add(24 * time.Hour), Limit: 20,
		Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents held window: %v", err)
	}
	var found bool
	for _, item := range list.Items {
		if item.EventID == wantID {
			found = true
			if got := item.DueAt.In(loc).Format("2006-01-02"); got != heldDay {
				t.Fatalf("held drive event due day=%s, want %s", got, heldDay)
			}
		}
	}
	if !found {
		t.Fatalf("held drive %s missing from D+7 window list=%#v", wantID, list.Items)
	}

	// Roster membership + returned target timestamp: both animals present, every row's
	// scheduled_at is the batch planned_date business day (D+7), never the stale due day D.
	targets, err := repo.ListDriveTargets(ctx, domain.DriveTargetQuery{
		TenantID: testTenantID, EventID: wantID, Scope: domain.ScopeFilter{TenantWide: true}, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListDriveTargets(%s): %v", wantID, err)
	}
	if len(targets.Items) != 2 {
		t.Fatalf("held drive targets=%#v, want both member animals", targets.Items)
	}
	seen := map[string]bool{}
	for _, item := range targets.Items {
		seen[item.AnimalID] = true
		gotDay := item.ScheduledAt.In(loc).Format("2006-01-02")
		if gotDay != heldDay {
			t.Fatalf("target %s scheduled_at day=%s, want held day %s (stale due day %s must not leak)",
				item.AnimalID, gotDay, heldDay, dueDay)
		}
	}
	if !seen[obligationA] || !seen[obligationB] {
		t.Fatalf("held drive roster=%#v, want animals %s and %s", targets.Items, obligationA, obligationB)
	}
}

// LIFE-005: park-drive summary_primary copy must come from the scheduled status bucket, never from
// the total roster target_count. Completed-only, completed+deferred, and canceled+deferred drives
// must not claim their whole roster as "scheduled doses".
func TestCalendarParkDriveSummaryPrimaryUsesStatusBucketsNotRosterTotal(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	type driveSummaryBlock struct {
		TargetCount    int    `json:"target_count"`
		ScheduledCount int    `json:"scheduled_count"`
		DeferredCount  int    `json:"deferred_count"`
		SummaryPrimary string `json:"summary_primary"`
	}
	readSummary := func(t *testing.T, eventID string) driveSummaryBlock {
		t.Helper()
		detail, err := repo.GetEventDetail(ctx, domain.EventQuery{
			TenantID: testTenantID, EventID: eventID, Scope: domain.ScopeFilter{TenantWide: true},
		})
		if err != nil {
			t.Fatalf("GetEventDetail(%s): %v", eventID, err)
		}
		var block driveSummaryBlock
		if err := json.Unmarshal(detail.Summary, &block); err != nil {
			t.Fatalf("unmarshal summary for %s: %v (raw=%s)", eventID, err, string(detail.Summary))
		}
		return block
	}
	scheduledCopy := func(n int) string {
		if n == 1 {
			return "1 scheduled dose"
		}
		return fmt.Sprintf("%d scheduled doses", n)
	}

	const (
		protocolID = "86000000-0000-4000-8000-00000000da01"
		versionID  = "86000000-0000-4000-8000-00000000da02"
		ruleID     = "86000000-0000-4000-8000-00000000da03"
	)
	dueAt := stableSameLocalDayDueAt(time.Now().In(biztime.DefaultLocation()))

	// Scenario 1 (completed-only): one completed batch of two obligations. target_count stays the
	// roster size (2); summary_primary must say 0 scheduled doses, not 2.
	const (
		completedPark  = "86000000-0000-4000-8000-00000000da11"
		completedShed  = "86000000-0000-4000-8000-00000000da12"
		completedBatch = "86000000-0000-4000-8000-00000000da13"
		completedOblA  = "86000000-0000-4000-8000-00000000da14"
		completedOblB  = "86000000-0000-4000-8000-00000000da15"
	)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, completedOblA, dueAt)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, completedOblB, dueAt)
	seedVaccinationBatchForShed(t, ctx, pool, completedBatch, versionID, completedPark, completedShed, dueAt, completedOblA, completedOblB)
	if _, err := pool.Exec(ctx, `
UPDATE obligation_batches SET status='completed', updated_at=now()
WHERE tenant_id=$1::uuid AND batch_id=$2::uuid`, testTenantID, completedBatch); err != nil {
		t.Fatalf("mark batch completed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances SET status='completed', completed_at=now(), updated_at=now()
WHERE tenant_id=$1::uuid AND obligation_id IN ($2::uuid, $3::uuid)`, testTenantID, completedOblA, completedOblB); err != nil {
		t.Fatalf("mark obligations completed: %v", err)
	}
	completedOnly := readSummary(t, parkDriveEventID(completedPark, dueAt))
	if completedOnly.TargetCount != 2 || completedOnly.ScheduledCount != 0 {
		t.Fatalf("completed-only summary=%#v, want target_count=2 scheduled_count=0", completedOnly)
	}
	if completedOnly.SummaryPrimary != scheduledCopy(0) {
		t.Fatalf("completed-only summary_primary=%q, want %q (roster total %d must not drive the copy)",
			completedOnly.SummaryPrimary, scheduledCopy(0), completedOnly.TargetCount)
	}

	// Scenario 2 (completed+deferred): a completed batch plus an unbatched deferred obligation on
	// the same park/day. Neither bucket is scheduled; the copy must stay at 0.
	const (
		mixedPark     = "86000000-0000-4000-8000-00000000da21"
		mixedShed     = "86000000-0000-4000-8000-00000000da22"
		mixedBatch    = "86000000-0000-4000-8000-00000000da23"
		mixedOblDone  = "86000000-0000-4000-8000-00000000da24"
		mixedOblDefer = "86000000-0000-4000-8000-00000000da25"
	)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, mixedOblDone, dueAt)
	seedVaccinationBatchForShed(t, ctx, pool, mixedBatch, versionID, mixedPark, mixedShed, dueAt, mixedOblDone)
	if _, err := pool.Exec(ctx, `
UPDATE obligation_batches SET status='completed', updated_at=now()
WHERE tenant_id=$1::uuid AND batch_id=$2::uuid`, testTenantID, mixedBatch); err != nil {
		t.Fatalf("mark mixed batch completed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances SET status='completed', completed_at=now(), updated_at=now()
WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid`, testTenantID, mixedOblDone); err != nil {
		t.Fatalf("mark mixed obligation completed: %v", err)
	}
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, mixedOblDefer, dueAt)
	attachObligationToGoatScope(t, ctx, pool, mixedOblDefer, mixedOblDefer, "shed", mixedShed)
	seedCalendarLocations(t, ctx, pool, mixedPark, mixedShed)
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances SET status='deferred', updated_at=now()
WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid`, testTenantID, mixedOblDefer); err != nil {
		t.Fatalf("defer mixed obligation: %v", err)
	}
	mixed := readSummary(t, parkDriveEventID(mixedPark, dueAt))
	if mixed.ScheduledCount != 0 {
		t.Fatalf("completed+deferred summary=%#v, want scheduled_count=0", mixed)
	}
	if mixed.SummaryPrimary != scheduledCopy(0) {
		t.Fatalf("completed+deferred summary_primary=%q, want %q", mixed.SummaryPrimary, scheduledCopy(0))
	}
	if mixed.SummaryPrimary == scheduledCopy(mixed.TargetCount) && mixed.TargetCount != 0 {
		t.Fatalf("completed+deferred summary_primary=%q still mirrors roster total %d", mixed.SummaryPrimary, mixed.TargetCount)
	}

	// Scenario 3 (canceled+deferred): a canceled (superseded) batch plus an unbatched deferred
	// obligation. The canceled batch contributes nothing schedulable; the copy must stay at 0.
	const (
		canPark     = "86000000-0000-4000-8000-00000000da31"
		canShed     = "86000000-0000-4000-8000-00000000da32"
		canBatch    = "86000000-0000-4000-8000-00000000da33"
		canOblGone  = "86000000-0000-4000-8000-00000000da34"
		canOblDefer = "86000000-0000-4000-8000-00000000da35"
	)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, canOblGone, dueAt)
	seedVaccinationBatchForShed(t, ctx, pool, canBatch, versionID, canPark, canShed, dueAt, canOblGone)
	if _, err := pool.Exec(ctx, `
UPDATE obligation_batches SET status='superseded', updated_at=now()
WHERE tenant_id=$1::uuid AND batch_id=$2::uuid`, testTenantID, canBatch); err != nil {
		t.Fatalf("cancel batch: %v", err)
	}
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, canOblDefer, dueAt)
	attachObligationToGoatScope(t, ctx, pool, canOblDefer, canOblDefer, "shed", canShed)
	seedCalendarLocations(t, ctx, pool, canPark, canShed)
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances SET status='deferred', updated_at=now()
WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid`, testTenantID, canOblDefer); err != nil {
		t.Fatalf("defer obligation next to canceled batch: %v", err)
	}
	canceled := readSummary(t, parkDriveEventID(canPark, dueAt))
	if canceled.ScheduledCount != 0 {
		t.Fatalf("canceled+deferred summary=%#v, want scheduled_count=0", canceled)
	}
	if canceled.SummaryPrimary != scheduledCopy(0) {
		t.Fatalf("canceled+deferred summary_primary=%q, want %q", canceled.SummaryPrimary, scheduledCopy(0))
	}
}
