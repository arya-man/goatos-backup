package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, time.Now().UTC().Add(2*time.Hour))
	seedVaccinationBatchForShed(t, ctx, pool, batchID, versionID, testParkA, testShedA, time.Now().UTC().Add(2*time.Hour), obligationID)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, reminderObligationID, time.Now().UTC().Add(30*time.Minute))
	seedVaccinationBatchForShed(t, ctx, pool, reminderBatchID, versionID, testParkB, testShedB, time.Now().UTC().Add(30*time.Minute), reminderObligationID)
	testCalendarEvent := batchEventID(batchID)
	testReminderEvent := batchEventID(reminderBatchID)

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
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, time.Now().UTC().Add(30*time.Minute))
	seedVaccinationBatchForShed(t, ctx, pool, batchID, versionID, testParkA, testShedA, time.Now().UTC().Add(30*time.Minute), obligationID)
	eventID := batchEventID(batchID)
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
	eventA := batchEventID(batchA)
	eventB := batchEventID(batchB)

	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: time.Now().UTC().Add(-24 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    20,
		Scope:    domain.ScopeFilter{ParkIDs: []string{testParkA}},
	})
	if err != nil {
		t.Fatalf("ListEvents park scope: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].EventID != eventA {
		t.Fatalf("park scoped list = %#v, want only %s", list.Items, eventA)
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
	activeID := batchEventID(activeBatch)

	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, completedObligation, dueAt.Add(time.Minute))
	seedVaccinationBatchForShed(t, ctx, pool, completedBatch, versionID, completedParkID, completedShedID, dueAt.Add(time.Minute), completedObligation)
	completedID := batchEventID(completedBatch)
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
		DateFrom: time.Now().UTC().Add(-time.Hour),
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
		DateFrom: time.Now().UTC().Add(-time.Hour),
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
	if len(list.Items) != 1 || list.Items[0].EventID != catchupEventID(testTenantID, dueAt) || list.Items[0].EventType != domain.EventVaccinationDrive {
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
	if len(list.Items) != 1 || list.Items[0].EventID != batchEventID(batchID) ||
		list.Items[0].EventType != domain.EventVaccinationDrive || list.Items[0].TargetCount != len(obligationIDs) {
		t.Fatalf("list after batch=%#v, want one batch drive with %d goats", list.Items, len(obligationIDs))
	}
	assertDriveTargets(t, ctx, repo, batchEventID(batchID), obligationIDs)
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
		protocolB   = "86000000-0000-4000-8000-00000000c201"
		versionB    = "86000000-0000-4000-8000-00000000c202"
		ruleB       = "86000000-0000-4000-8000-00000000c203"
		batchB      = "86000000-0000-4000-8000-00000000c204"
		obligationB = "86000000-0000-4000-8000-00000000c205"
	)
	seedVaccinationObligation(t, ctx, pool, protocolA, versionA, ruleA, obligationA, dueAt)
	seedProtocolRuleVaccineName(t, ctx, pool, versionA, ruleA, "ET+TT")
	seedVaccinationBatchForShed(t, ctx, pool, batchA, versionA, testParkA, testShedA, dueAt, obligationA)

	// A single source for the park/day keeps the plain batch event id (canonical is live, no refresh).
	soloList, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll, DateFrom: dueAt.Add(-time.Hour), DateTo: dueAt.Add(24 * time.Hour), Limit: 20,
		Scope: domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents single batch: %v", err)
	}
	if len(soloList.Items) != 1 || soloList.Items[0].EventID != batchEventID(batchA) || soloList.Items[0].Status == domain.StatusCanceled {
		t.Fatalf("ListEvents single batch=%#v, want stable batch event %s", soloList.Items, batchEventID(batchA))
	}

	seedVaccinationObligation(t, ctx, pool, protocolB, versionB, ruleB, obligationB, dueAt.Add(10*time.Minute))
	seedProtocolRuleVaccineName(t, ctx, pool, versionB, ruleB, "PPR")
	seedVaccinationBatchForShed(t, ctx, pool, batchB, versionB, testParkA, testShedB, dueAt.Add(10*time.Minute), obligationB)

	wantID := parkDriveEventID(testParkA, dueAt)
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
	if len(list.Items) != 1 || list.Items[0].EventID != catchupEventID(testTenantID, dueAt) ||
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

	catchupID := catchupParkEventID(testParkA, dueAt)
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
	catchupID := catchupEventID(testTenantID, dueAt)
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
	if len(list.Items) != 1 || list.Items[0].EventID != batchEventID(batchID) ||
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
	eventID := catchupEventID(testTenantID, oldDueAt)
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
	eventID := catchupEventID(testTenantID, oldDueAt)
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
	eventID := catchupEventID(testTenantID, dueAt)
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
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, reminderObligation, time.Now().UTC().Add(30*time.Minute))
	seedVaccinationBatchForShed(t, ctx, pool, reminderBatch, versionID, reminderParkID, reminderShedID, time.Now().UTC().Add(30*time.Minute), reminderObligation)
	reminderEvent := batchEventID(reminderBatch)
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
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, nudgeObligation, time.Now().UTC().Add(45*time.Minute))
	seedVaccinationBatchForShed(t, ctx, pool, nudgeBatch, versionID, nudgeParkID, nudgeShedID, time.Now().UTC().Add(45*time.Minute), nudgeObligation)
	nudgeEvent := batchEventID(nudgeBatch)
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
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, snoozeObligation, time.Now().UTC().Add(50*time.Minute))
	seedVaccinationBatchForShed(t, ctx, pool, snoozeBatch, versionID, snoozeParkID, snoozeShedID, time.Now().UTC().Add(50*time.Minute), snoozeObligation)
	snoozeEvent := batchEventID(snoozeBatch)
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
	catchupEvent := catchupEventID(testTenantID, catchupDueAt)
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

func batchEventID(batchID string) string {
	return fmt.Sprintf("batch:%s", batchID)
}

func catchupEventID(tenantID string, dueAt time.Time) string {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5*60*60+30*60)
	}
	day := dueAt.In(loc).Format("2006-01-02")
	return fmt.Sprintf("catchup:tenant:%s:due:%s", tenantID, day)
}

func catchupParkEventID(parkID string, dueAt time.Time) string {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5*60*60+30*60)
	}
	day := dueAt.In(loc).Format("2006-01-02")
	return fmt.Sprintf("catchup:park:%s:due:%s", parkID, day)
}

func parkDriveEventID(parkID string, dueAt time.Time) string {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5*60*60+30*60)
	}
	return fmt.Sprintf("parkdrive:park:%s:date:%s", parkID, dueAt.In(loc).Format("2006-01-02"))
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
