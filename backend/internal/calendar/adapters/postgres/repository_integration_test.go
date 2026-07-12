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
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	testTenantID      = "00000000-0000-4000-8000-000000000001"
	testCustodianID   = "00000000-0000-4000-8000-000000001001"
	testActorID       = "86000000-0000-4000-8000-000000009999"
	testDriveRuleID   = "86000000-0000-4000-8000-000000001011"
	testDriveShedID   = "86000000-0000-4000-8000-000000001012"
	testCalendarEvent = "batch:86000000-0000-4000-8000-000000001001:rule:86000000-0000-4000-8000-000000001011:shed:86000000-0000-4000-8000-000000001012"
	testReminderEvent = "batch:86000000-0000-4000-8000-000000001002:rule:86000000-0000-4000-8000-000000001011:shed:86000000-0000-4000-8000-000000001012"
	testParkA         = "86000000-0000-4000-8000-000000000701"
	testParkB         = "86000000-0000-4000-8000-000000000702"
	testShedA         = "86000000-0000-4000-8000-000000000711"
	testShedB         = "86000000-0000-4000-8000-000000000712"
)

func testCalendarActorGrants() []ports.ActorGrant {
	return []ports.ActorGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: testTenantID}}
}

func TestCalendarListRequiresFreshProjectionAndExposesVersion(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	q := domain.Query{TenantID: testTenantID, OwnerKey: domain.OwnerAll, DateFrom: time.Now().Add(-time.Hour), DateTo: time.Now().Add(time.Hour), Limit: 10, Scope: domain.ScopeFilter{TenantWide: true}}
	if _, err := repo.ListEvents(ctx, q); !errors.Is(err, ports.ErrProjectionUnavailable) {
		t.Fatalf("missing projection error=%v, want ErrProjectionUnavailable", err)
	}
	seedCalendarProjectionState(t, ctx, pool)
	got, err := repo.ListEvents(ctx, q)
	if err != nil {
		t.Fatalf("fresh projection list: %v", err)
	}
	if got.Projection.ProjectionVersion <= 0 || got.Projection.Stale || got.Projection.ServingState != "fresh" {
		t.Fatalf("projection metadata=%+v", got.Projection)
	}
	if _, err := pool.Exec(ctx, `
UPDATE calendar_projection_state
SET projected_at = now() - interval '6 minutes', freshness_status = 'yellow', serving_state = 'stale'
WHERE tenant_id = $1::uuid AND slice_key = 'vaccination'`, testTenantID); err != nil {
		t.Fatalf("mark calendar projection stale: %v", err)
	}
	if _, err := repo.ListEvents(ctx, q); !errors.Is(err, ports.ErrProjectionStale) {
		t.Fatalf("stale projection error=%v, want ErrProjectionStale", err)
	}
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
	seedCalendarProjection(t, ctx, pool, testCalendarEvent, time.Now().UTC().Add(2*time.Hour), "not_scheduled")
	seedCalendarProjection(t, ctx, pool, testReminderEvent, time.Now().UTC().Add(30*time.Minute), "scheduled")

	repo := NewRepository(pool, 5*time.Second)
	from := time.Now().UTC().Add(-24 * time.Hour)
	to := time.Now().UTC().Add(45 * 24 * time.Hour)
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
	if detail.Event.EventID != testCalendarEvent || len(detail.NotificationChannels) != 2 {
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
	eventID := "calendar:86000000-0000-4000-8000-000000001222"
	seedCalendarProjection(t, ctx, pool, eventID, time.Now().UTC().Add(30*time.Minute), "queued")
	yesterdayKey := testTenantID + ":calendar.reminder:" + eventID + ":" + calendarBusinessDate(time.Now().UTC().Add(-24*time.Hour))
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
	todayKey := testTenantID + ":calendar.reminder:" + eventID + ":" + calendarBusinessDate(time.Now().UTC())
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

func TestCalendarWidestRequestPlanUsesHotListIndex(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedCalendarProjection(t, ctx, pool, testCalendarEvent, time.Now().UTC().Add(2*time.Hour), "not_scheduled")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `EXPLAIN (COSTS OFF) `+calendarListSQL,
		testTenantID, "", "", "", "", time.Now().UTC().Add(-24*time.Hour), time.Now().UTC().Add(45*24*time.Hour),
		nil, "", 200, true, []string{}, []string{})
	if err != nil {
		t.Fatalf("explain calendar list: %v", err)
	}
	defer rows.Close()
	var planLines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		planLines = append(planLines, line)
	}
	plan := strings.Join(planLines, "\n")
	if strings.Contains(plan, "Seq Scan on calendar_event_projections") {
		t.Fatalf("calendar widest request plan used seq scan:\n%s", plan)
	}
	if !strings.Contains(plan, "calendar_event_projections_hot_list_idx") {
		t.Fatalf("calendar widest request plan did not use hot list index:\n%s", plan)
	}
}

func TestCalendarPostgresAppliesParkShedScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	eventA := "batch:86000000-0000-4000-8000-000000001101:rule:86000000-0000-4000-8000-000000001111:shed:" + testShedA
	eventB := "batch:86000000-0000-4000-8000-000000001102:rule:86000000-0000-4000-8000-000000001112:shed:" + testShedB
	seedScopedCalendarProjection(t, ctx, pool, eventA, "86000000-0000-4000-8000-00000000a101", testParkA, testShedA, false)
	seedScopedCalendarProjection(t, ctx, pool, eventB, "86000000-0000-4000-8000-00000000a102", testParkB, testShedB, false)

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

	list, err = repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: time.Now().UTC().Add(-24 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    20,
		Scope:    domain.ScopeFilter{ShedIDs: []string{testShedB}},
	})
	if err != nil {
		t.Fatalf("ListEvents shed scope: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].EventID != eventB {
		t.Fatalf("shed scoped list = %#v, want only %s", list.Items, eventB)
	}
}

func TestCalendarPostgresHidesSystemEventsFromDirectReads(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	eventID := "calendar:86000000-0000-4000-8000-000000001201"
	seedScopedCalendarProjection(t, ctx, pool, eventID, "86000000-0000-4000-8000-00000000a201", testParkA, testShedA, true)

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
	if len(list.Items) != 0 {
		t.Fatalf("system list items = %d, want 0", len(list.Items))
	}
	if _, err := repo.GetEventDetail(ctx, domain.EventQuery{TenantID: testTenantID, EventID: eventID, Scope: domain.ScopeFilter{TenantWide: true}}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("system detail err = %v, want ErrNotFound", err)
	}
	if _, err := repo.History(ctx, domain.HistoryQuery{TenantID: testTenantID, EventID: eventID, Limit: 10, Scope: domain.ScopeFilter{TenantWide: true}}); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("system history err = %v, want ErrNotFound", err)
	}
}

func TestCalendarListExcludesClosedEventsByDefault(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	dueAt := time.Now().UTC().Add(2 * time.Hour)
	activeID := "batch:86000000-0000-4000-8000-000000000791:rule:86000000-0000-4000-8000-000000000794:shed:" + testShedA
	completedID := "batch:86000000-0000-4000-8000-000000000792:rule:86000000-0000-4000-8000-000000000795:shed:" + testShedA
	canceledID := "batch:86000000-0000-4000-8000-000000000793:rule:86000000-0000-4000-8000-000000000796:shed:" + testShedA
	seedCalendarProjection(t, ctx, pool, activeID, dueAt, "not_scheduled")
	seedCalendarProjection(t, ctx, pool, completedID, dueAt.Add(time.Minute), "not_scheduled")
	seedCalendarProjection(t, ctx, pool, canceledID, dueAt.Add(2*time.Minute), "not_scheduled")
	if _, err := pool.Exec(ctx, `
UPDATE calendar_event_projections
SET status = CASE event_id
  WHEN $2 THEN 'completed'
  WHEN $3 THEN 'canceled'
  ELSE status
END
WHERE tenant_id = $1::uuid AND event_id IN ($2, $3)`,
		testTenantID, completedID, canceledID); err != nil {
		t.Fatalf("close seeded projections: %v", err)
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

	count, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: time.Now().UTC().Add(-time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    100,
	})
	if err != nil {
		t.Fatalf("RefreshVaccinationProjection: %v", err)
	}
	if count != 2 {
		t.Fatalf("projection count = %d, want obligation row plus one catch-up drive summary", count)
	}
	var gotEventID string
	var sourceBacked bool
	if err := pool.QueryRow(ctx, `
SELECT event_id, source_backed
FROM calendar_event_projections
WHERE tenant_id = $1::uuid AND event_id = $2`,
		testTenantID, "obligation:"+obligationID).Scan(&gotEventID, &sourceBacked); err != nil {
		t.Fatalf("query projection: %v", err)
	}
	if gotEventID != "obligation:"+obligationID || !sourceBacked {
		t.Fatalf("projection event_id=%s source_backed=%t", gotEventID, sourceBacked)
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

	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: time.Now().UTC().Add(-time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    100,
	}); err != nil {
		t.Fatalf("RefreshVaccinationProjection before batch: %v", err)
	}
	assertCount(t, ctx, pool, "pre-batch visible dose events", `
SELECT count(*)
FROM calendar_event_projections
WHERE tenant_id=$1::uuid
  AND event_type='vaccination_dose_due'
  AND status <> 'canceled'`, len(obligationIDs), testTenantID)

	seedVaccinationBatch(t, ctx, pool, batchID, versionID, dueAt, obligationIDs...)
	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: time.Now().UTC().Add(-time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    100,
	}); err != nil {
		t.Fatalf("RefreshVaccinationProjection after batch: %v", err)
	}

	assertCount(t, ctx, pool, "active dose events after batching", `
SELECT count(*)
FROM calendar_event_projections
WHERE tenant_id=$1::uuid
  AND event_type='vaccination_dose_due'
  AND status <> 'canceled'`, 0, testTenantID)
	var eventID, eventType string
	var targetCount int
	if err := pool.QueryRow(ctx, `
SELECT event_id, event_type, target_count
FROM calendar_event_projections
WHERE tenant_id=$1::uuid
  AND event_type='vaccination_drive'
  AND status <> 'canceled'`,
		testTenantID).Scan(&eventID, &eventType, &targetCount); err != nil {
		t.Fatalf("query drive projection: %v", err)
	}
	if eventID != batchEventID(batchID) || eventType != domain.EventVaccinationDrive || targetCount != len(obligationIDs) {
		t.Fatalf("drive projection id=%s type=%s targets=%d, want one batch drive with %d goats", eventID, eventType, targetCount, len(obligationIDs))
	}
	assertDriveTargets(t, ctx, repo, batchEventID(batchID), obligationIDs)

	legacyBatchID := fmt.Sprintf("batch:%s:rule:%s:shed:%s", batchID, ruleID, testShedA)
	seedScopedCalendarProjection(t, ctx, pool, legacyBatchID, batchID, testParkA, testShedA, false)
	assertDriveTargets(t, ctx, repo, legacyBatchID, obligationIDs)
}

func TestCalendarVaccinationProjectionGroupsMultipleShedsAndVaccinesIntoOneAllDayParkDrive(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	dueAt := stableSameLocalDayDueAt(time.Now().UTC())
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
	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID, DateFrom: dueAt.Add(-time.Hour), DateTo: dueAt.Add(24 * time.Hour), Limit: 100,
	}); err != nil {
		t.Fatalf("first RefreshVaccinationProjection: %v", err)
	}
	assertCount(t, ctx, pool, "single source keeps stable batch event", `
SELECT count(*) FROM calendar_event_projections
WHERE tenant_id=$1::uuid AND event_id=$2 AND status <> 'canceled'`, 1, testTenantID, batchEventID(batchA))

	seedVaccinationObligation(t, ctx, pool, protocolB, versionB, ruleB, obligationB, dueAt.Add(10*time.Minute))
	seedProtocolRuleVaccineName(t, ctx, pool, versionB, ruleB, "PPR")
	seedVaccinationBatchForShed(t, ctx, pool, batchB, versionB, testParkA, testShedB, dueAt.Add(10*time.Minute), obligationB)
	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID, DateFrom: dueAt.Add(-time.Hour), DateTo: dueAt.Add(24 * time.Hour), Limit: 100,
	}); err != nil {
		t.Fatalf("second RefreshVaccinationProjection: %v", err)
	}

	wantID := parkDriveEventID(testParkA, dueAt)
	var gotID string
	var targetCount, shedCount, vaccineCount, driveCount int
	var allDay, aggregated bool
	var shedLabels, vaccineLabels []string
	if err := pool.QueryRow(ctx, `
SELECT event_id, target_count,
       (detail #>> '{summary,shed_count}')::int,
       (detail #>> '{summary,vaccine_count}')::int,
       (detail #>> '{summary,drive_count}')::int,
       ARRAY(SELECT jsonb_array_elements_text(detail #> '{summary,shed_labels}')),
       ARRAY(SELECT jsonb_array_elements_text(detail #> '{summary,vaccine_labels}'))
FROM calendar_event_projections
WHERE tenant_id=$1::uuid AND source_target_type='park_drive' AND status <> 'canceled'`, testTenantID).Scan(
		&gotID, &targetCount, &shedCount, &vaccineCount, &driveCount, &shedLabels, &vaccineLabels,
	); err != nil {
		t.Fatalf("query grouped park drive: %v", err)
	}
	if gotID != wantID || targetCount != 2 || shedCount != 2 || vaccineCount != 2 || driveCount != 2 {
		t.Fatalf("grouped drive id=%s targets=%d sheds=%d vaccines=%d packets=%d", gotID, targetCount, shedCount, vaccineCount, driveCount)
	}
	if !slices.Equal(shedLabels, []string{"Test Shed 0711", "Test Shed 0712"}) || !slices.Equal(vaccineLabels, []string{"ET+TT", "PPR"}) {
		t.Fatalf("grouped labels sheds=%v vaccines=%v", shedLabels, vaccineLabels)
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
	allDay, aggregated = list.Items[0].AllDay, list.Items[0].Aggregated
	if !allDay || !aggregated || list.Items[0].ShedCount != 2 || list.Items[0].VaccineCount != 2 {
		t.Fatalf("ListEvents summary=%#v", list.Items[0])
	}
	assertCount(t, ctx, pool, "superseded raw batch card tombstoned", `
SELECT count(*) FROM calendar_event_projections
WHERE tenant_id=$1::uuid AND event_id=$2 AND status='canceled'`, 1, testTenantID, batchEventID(batchA))
}

func TestCalendarVaccinationProjectionDoesNotReclassifyDeferredCatchupAsOverdue(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	dueAt := stableSameLocalDayDueAt(time.Now().UTC()).Add(-2 * time.Hour)
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
	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID, DateFrom: dueAt.Add(-time.Hour), DateTo: dueAt.Add(24 * time.Hour), Limit: 100,
	}); err != nil {
		t.Fatalf("RefreshVaccinationProjection: %v", err)
	}
	assertCount(t, ctx, pool, "deferred park drive stays deferred", `
SELECT count(*) FROM calendar_event_projections
WHERE tenant_id=$1::uuid
  AND event_type='vaccination_drive'
  AND status='deferred'`, 1, testTenantID)
	assertCount(t, ctx, pool, "deferred park drive never becomes overdue", `
SELECT count(*) FROM calendar_event_projections
WHERE tenant_id=$1::uuid
  AND event_type='vaccination_drive'
  AND status='overdue'`, 0, testTenantID)
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
	dueAt := stableSameLocalDayDueAt(time.Now().UTC())
	seedVaccinationObligation(t, ctx, pool, protocolIDs[0], versionIDs[0], ruleIDs[0], obligationIDs[0], dueAt)
	seedVaccinationObligation(t, ctx, pool, protocolIDs[1], versionIDs[1], ruleIDs[1], obligationIDs[1], dueAt.Add(15*time.Minute))
	for _, obligationID := range obligationIDs {
		attachObligationToGoatScope(t, ctx, pool, obligationID, obligationID, "tenant", testTenantID)
	}

	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: dueAt.Add(-time.Hour),
		DateTo:   dueAt.Add(24 * time.Hour),
		Limit:    100,
	}); err != nil {
		t.Fatalf("RefreshVaccinationProjection: %v", err)
	}

	assertCount(t, ctx, pool, "multi-rule catchup visible drive", `
SELECT count(*)
FROM calendar_event_projections
WHERE tenant_id=$1::uuid
  AND event_id = $2
  AND event_type='vaccination_drive'
  AND status <> 'canceled'
  AND target_count = 2`, 1, testTenantID, catchupEventID(testTenantID, dueAt))
}

func TestCalendarCatchupDriveTargetsUseVaccinationAnimalAndQueueSemantics(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	dueAt := stableSameLocalDayDueAt(time.Now().UTC())
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

	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: dueAt.Add(-time.Hour),
		DateTo:   dueAt.Add(24 * time.Hour),
		Limit:    100,
	}); err != nil {
		t.Fatalf("RefreshVaccinationProjection: %v", err)
	}

	catchupID := catchupParkEventID(testParkA, dueAt)
	var targetCount, queueCount int
	var nudgeAllowed string
	if err := pool.QueryRow(ctx, `
SELECT target_count,
       (detail #>> '{summary,queue_count}')::int,
       detail #>> '{notification_policy,nudge_allowed}'
FROM calendar_event_projections
WHERE tenant_id=$1::uuid AND event_id=$2`,
		testTenantID, catchupID).Scan(&targetCount, &queueCount, &nudgeAllowed); err != nil {
		t.Fatalf("query catch-up projection metadata: %v", err)
	}
	if targetCount != 1 || queueCount != 2 || nudgeAllowed != "false" {
		t.Fatalf("catch-up target_count=%d queue_count=%d nudge_allowed=%s, want 1 animal, 2 queues, read-only", targetCount, queueCount, nudgeAllowed)
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
	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: time.Now().UTC().Add(-time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    100,
	}); err != nil {
		t.Fatalf("RefreshVaccinationProjection: %v", err)
	}
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
	dueAt := stableSameLocalDayDueAt(time.Now().UTC())
	seedVaccinationObligation(t, ctx, pool, protocolIDs[0], versionIDs[0], ruleIDs[0], obligationIDs[0], dueAt)
	seedVaccinationObligation(t, ctx, pool, protocolIDs[1], versionIDs[1], ruleIDs[1], obligationIDs[1], dueAt.Add(15*time.Minute))
	seedVaccinationBatch(t, ctx, pool, batchID, versionIDs[0], dueAt, obligationIDs...)

	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: dueAt.Add(-time.Hour),
		DateTo:   dueAt.Add(24 * time.Hour),
		Limit:    100,
	}); err != nil {
		t.Fatalf("RefreshVaccinationProjection: %v", err)
	}

	assertCount(t, ctx, pool, "multi-rule batch visible drive", `
SELECT count(*)
FROM calendar_event_projections
WHERE tenant_id=$1::uuid
  AND event_id = $2
  AND event_type='vaccination_drive'
  AND status <> 'canceled'
  AND target_count = 2`, 1, testTenantID, batchEventID(batchID))
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

	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: time.Now().UTC().Add(-72 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    100,
	}); err != nil {
		t.Fatalf("RefreshVaccinationProjection: %v", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `
SELECT status
FROM calendar_event_projections
WHERE tenant_id = $1::uuid AND event_id = $2`,
		testTenantID, "obligation:"+obligationID).Scan(&status); err != nil {
		t.Fatalf("query projection: %v", err)
	}
	if status != domain.StatusMissed {
		t.Fatalf("projection status=%s, want missed", status)
	}
}

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
	now := time.Now().UTC()
	dueAt := now.Add(-10 * 24 * time.Hour)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, dueAt)
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET status = 'missed', updated_at = now()
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`, testTenantID, obligationID); err != nil {
		t.Fatalf("mark obligation missed: %v", err)
	}

	count, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: now.Add(-24 * time.Hour),
		DateTo:   now.Add(24 * time.Hour),
		Limit:    100,
	})
	if err != nil {
		t.Fatalf("RefreshVaccinationProjection: %v", err)
	}
	if count != 2 {
		t.Fatalf("old missed projection count = %d, want obligation row plus one visible drive", count)
	}
	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: now,
		DateTo:   now.Add(24 * time.Hour),
		Limit:    20,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].EventID != catchupEventID(testTenantID, dueAt) || list.Items[0].Status != domain.StatusMissed {
		t.Fatalf("list items=%#v, want old missed drive summary visible outside date window", list.Items)
	}
}

func TestCalendarProjectionAndListIncludePastDueOpenExceptions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	protocolID := "86000000-0000-4000-8000-000000000891"
	versionID := "86000000-0000-4000-8000-000000000892"
	ruleID := "86000000-0000-4000-8000-000000000893"
	obligationID := "86000000-0000-4000-8000-000000000894"
	now := time.Now().UTC()
	dueAt := now.Add(-2 * time.Hour)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, dueAt)

	count, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: now,
		DateTo:   now.Add(24 * time.Hour),
		Limit:    100,
	})
	if err != nil {
		t.Fatalf("RefreshVaccinationProjection: %v", err)
	}
	if count != 2 {
		t.Fatalf("past-due open projection count = %d, want obligation row plus one visible drive", count)
	}
	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID,
		OwnerKey: domain.OwnerAll,
		DateFrom: now,
		DateTo:   now.Add(24 * time.Hour),
		Limit:    20,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].EventID != catchupEventID(testTenantID, dueAt) || list.Items[0].Status != domain.StatusOverdue {
		t.Fatalf("list items=%#v, want old overdue drive summary visible outside date window", list.Items)
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
	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: time.Now().UTC().Add(-24 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    100,
	}); err != nil {
		t.Fatalf("RefreshVaccinationProjection: %v", err)
	}
	queued, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:    testTenantID,
		Limit:       10,
		Now:         time.Now().UTC(),
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
  AND status='queued'`, 1, testTenantID, eventID, testTenantID)
	assertCount(t, ctx, pool, "hidden obligation escalation skipped for catch-up summary", `
SELECT count(*)
FROM obligation_escalations
WHERE tenant_id=$1::uuid
  AND obligation_id=$2::uuid
  AND level=1`, 0, testTenantID, obligationID)
	var status, escalationState string
	if err := pool.QueryRow(ctx, `
SELECT status, escalation_state
FROM calendar_event_projections
WHERE tenant_id=$1::uuid AND event_id=$2`, testTenantID, eventID).Scan(&status, &escalationState); err != nil {
		t.Fatalf("query escalation projection: %v", err)
	}
	if status != domain.StatusOverdue || escalationState != "level_1_open" {
		t.Fatalf("projection status=%s escalation=%s, want overdue/level_1_open", status, escalationState)
	}
	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: time.Now().UTC().Add(-24 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    100,
	}); err != nil {
		t.Fatalf("RefreshVaccinationProjection after escalation: %v", err)
	}
	var reminderState, channel string
	if err := pool.QueryRow(ctx, `
SELECT status, reminder_state, primary_notification_channel, escalation_state
FROM calendar_event_projections
WHERE tenant_id=$1::uuid AND event_id=$2`, testTenantID, eventID).Scan(&status, &reminderState, &channel, &escalationState); err != nil {
		t.Fatalf("query refreshed escalation projection: %v", err)
	}
	if status != domain.StatusOverdue || reminderState != "escalated" || channel != "local-stub" || escalationState != "level_1_open" {
		t.Fatalf("refreshed projection status=%s reminder=%s channel=%s escalation=%s", status, reminderState, channel, escalationState)
	}
	again, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:    testTenantID,
		Limit:       10,
		Now:         time.Now().UTC(),
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

func TestCalendarSweepersDoNotNotifyHeldDeferredOrBlockedWork(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	deferredEventID := "calendar:86000000-0000-4000-8000-000000000931"
	blockedEventID := "calendar:86000000-0000-4000-8000-000000000932"
	seedCalendarProjection(t, ctx, pool, deferredEventID, time.Now().UTC().Add(-2*time.Hour), "not_scheduled")
	seedCalendarProjection(t, ctx, pool, blockedEventID, time.Now().UTC().Add(-2*time.Hour), "not_scheduled")
	if _, err := pool.Exec(ctx, `
UPDATE calendar_event_projections
SET status = CASE event_id
  WHEN $2 THEN 'deferred'
  WHEN $3 THEN 'blocked'
  ELSE status
END
WHERE tenant_id=$1::uuid AND event_id IN ($2, $3)`, testTenantID, deferredEventID, blockedEventID); err != nil {
		t.Fatalf("mark held projections: %v", err)
	}
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
		Now:         time.Now().UTC(),
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
	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: now.Add(-4 * time.Hour),
		DateTo:   now.Add(24 * time.Hour),
		Limit:    100,
	}); err != nil {
		t.Fatalf("RefreshVaccinationProjection: %v", err)
	}
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
	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: time.Now().UTC().Add(-24 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    100,
	}); err != nil {
		t.Fatalf("RefreshVaccinationProjection: %v", err)
	}
	if _, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:     testTenantID,
		ObligationID: obligationID,
		Limit:        10,
		Now:          time.Now().UTC(),
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
	assertCount(t, ctx, pool, "acknowledged escalation projection", `
SELECT count(*)
FROM calendar_event_projections
WHERE tenant_id=$1::uuid AND event_id=$2 AND escalation_state='level_1_acknowledged'`, 1, testTenantID, eventID)
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
	assertCount(t, ctx, pool, "resolved escalation projection", `
SELECT count(*)
FROM calendar_event_projections
WHERE tenant_id=$1::uuid AND event_id=$2 AND escalation_state='resolved'`, 1, testTenantID, eventID)
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
	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: time.Now().UTC().Add(-24 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    100,
	}); err != nil {
		t.Fatalf("RefreshVaccinationProjection: %v", err)
	}
	if _, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:     testTenantID,
		ObligationID: obligationID,
		Limit:        10,
		Now:          time.Now().UTC(),
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
		Now:          time.Now().UTC(),
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
	assertCount(t, ctx, pool, "resolved ladder projection", `
SELECT count(*)
FROM calendar_event_projections
WHERE tenant_id=$1::uuid AND event_id=$2 AND escalation_state='resolved'`, 1, testTenantID, eventID)
	reopened, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:     testTenantID,
		ObligationID: obligationID,
		Limit:        10,
		Now:          time.Now().UTC(),
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
	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: time.Now().UTC().Add(-24 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    100,
	}); err != nil {
		t.Fatalf("RefreshVaccinationProjection: %v", err)
	}
	if _, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:     testTenantID,
		ObligationID: obligationID,
		Limit:        10,
		Now:          time.Now().UTC(),
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
		Now:          time.Now().UTC(),
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
	assertCount(t, ctx, pool, "projection rearmed escalation", `
SELECT count(*)
FROM calendar_event_projections
WHERE tenant_id=$1::uuid AND event_id=$2 AND escalation_state='level_2_open'`, 1, testTenantID, eventID)
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
	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: time.Now().UTC().Add(-48 * time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    100,
	}); err != nil {
		t.Fatalf("RefreshVaccinationProjection: %v", err)
	}
	queued, err := repo.SweepEscalations(ctx, ports.SweepEscalations{
		TenantID:     testTenantID,
		ObligationID: obligationID,
		Limit:        10,
		Now:          time.Now().UTC(),
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

func TestCalendarVaccinationProjectionRefreshPaginatesAndTombstonesStaleSource(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	dueAt := stableSameLocalDayDueAt(time.Now().UTC())
	protocolID := "86000000-0000-4000-8000-000000000810"
	versionID := "86000000-0000-4000-8000-000000000820"
	sharedRuleID := "86000000-0000-4000-8000-000000000830"
	obligationIDs := []string{
		"86000000-0000-4000-8000-000000000814",
		"86000000-0000-4000-8000-000000000824",
		"86000000-0000-4000-8000-000000000834",
	}
	for i, obligationID := range obligationIDs {
		seedVaccinationObligation(t, ctx, pool,
			protocolID,
			versionID,
			sharedRuleID,
			obligationID,
			dueAt.Add(time.Duration(i)*time.Hour),
		)
		attachObligationToGoatScope(t, ctx, pool, obligationID, obligationID, "tenant", testTenantID)
	}
	count, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: time.Now().UTC().Add(-time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    1,
	})
	if err != nil {
		t.Fatalf("RefreshVaccinationProjection page size 1: %v", err)
	}
	if count != 4 {
		t.Fatalf("projection count = %d, want 3 obligations plus 1 aggregated catch-up drive", count)
	}
	catchupID := catchupEventID(testTenantID, dueAt)
	legacyCatchupID := fmt.Sprintf("catchup:shed:%s:rule:%s:due:%s", testShedA, sharedRuleID, calendarBusinessDate(dueAt))
	assertCount(t, ctx, pool, "projected catch-up drive", `
SELECT count(*)
FROM calendar_event_projections
WHERE tenant_id=$1::uuid
	  AND event_id = $2
	  AND status <> 'canceled'
	  AND target_count = 3`, 1, testTenantID, catchupID)
	seedLegacyCatchupProjection(t, ctx, pool, legacyCatchupID, testShedA, dueAt)
	if _, err := pool.Exec(ctx, `
	UPDATE obligation_instances
	SET status = 'completed', updated_at = now()
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`, testTenantID, obligationIDs[1]); err != nil {
		t.Fatalf("complete obligation: %v", err)
	}
	if _, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: time.Now().UTC().Add(-time.Hour),
		DateTo:   time.Now().UTC().Add(24 * time.Hour),
		Limit:    1,
	}); err != nil {
		t.Fatalf("RefreshVaccinationProjection tombstone: %v", err)
	}
	var targetCount int
	if err := pool.QueryRow(ctx, `
SELECT target_count
FROM calendar_event_projections
WHERE tenant_id = $1::uuid AND event_id = $2`,
		testTenantID, catchupID).Scan(&targetCount); err != nil {
		t.Fatalf("query catch-up drive after completion: %v", err)
	}
	if targetCount != 2 {
		t.Fatalf("catch-up target_count=%d, want 2 after one completed obligation", targetCount)
	}
	assertCount(t, ctx, pool, "old-format catch-up projection tombstoned", `
SELECT count(*)
FROM calendar_event_projections
WHERE tenant_id=$1::uuid
  AND event_id=$2
  AND status='canceled'
  AND detail ? 'tombstone'`, 1, testTenantID, legacyCatchupID)
	targets, err := repo.ListDriveTargets(ctx, domain.DriveTargetQuery{
		TenantID: testTenantID,
		EventID:  catchupID,
		Scope:    domain.ScopeFilter{TenantWide: true},
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("ListDriveTargets after completion: %v", err)
	}
	if len(targets.Items) != 2 {
		t.Fatalf("targets after completion=%#v, want two open catch-up targets", targets.Items)
	}
	for _, item := range targets.Items {
		if item.AnimalID == obligationIDs[1] || item.ObligationID == obligationIDs[1] || item.Status == domain.StatusCompleted {
			t.Fatalf("targets after completion=%#v, included completed obligation %s", targets.Items, obligationIDs[1])
		}
	}
}

func TestCalendarVaccinationClosedProjectionRetentionAndPrune(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	oldDueAt := time.Now().UTC().Add(-120 * 24 * time.Hour)
	protocolID := "86000000-0000-4000-8000-000000000921"
	versionID := "86000000-0000-4000-8000-000000000922"
	ruleID := "86000000-0000-4000-8000-000000000923"
	obligationID := "86000000-0000-4000-8000-000000000924"
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationID, oldDueAt)
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET status = 'completed', updated_at = now()
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`, testTenantID, obligationID); err != nil {
		t.Fatalf("mark old obligation completed: %v", err)
	}

	count, err := repo.RefreshVaccinationProjection(ctx, ports.RefreshVaccinationProjection{
		TenantID: testTenantID,
		DateFrom: oldDueAt.Add(-time.Hour),
		DateTo:   time.Now().UTC().Add(time.Hour),
		Limit:    100,
	})
	if err != nil {
		t.Fatalf("RefreshVaccinationProjection old completed: %v", err)
	}
	if count != 0 {
		t.Fatalf("old completed refresh count = %d, want 0", count)
	}
	assertCount(t, ctx, pool, "old completed projection", `
SELECT count(*)
FROM calendar_event_projections
WHERE tenant_id=$1::uuid AND event_id=$2`, 0, testTenantID, "obligation:"+obligationID)

	oldEventID := testCalendarEvent
	seedCalendarProjection(t, ctx, pool, oldEventID, oldDueAt, "not_scheduled")
	if _, err := pool.Exec(ctx, `
UPDATE calendar_event_projections
SET status='completed', severity='info', updated_at=now()
WHERE tenant_id=$1::uuid AND event_id=$2`, testTenantID, oldEventID); err != nil {
		t.Fatalf("close old projection: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, notification_type, channel,
  title, body, status, idempotency_key, request_fingerprint, context
) VALUES (
  $1::uuid, $2, 'calendar_event', 'nudge', 'local-stub',
  'Old proof nudge', 'kept as history', 'sent',
  'old-closed-prune-nudge', 'old-closed-prune-nudge', '{}'::jsonb
)`, testTenantID, oldEventID); err != nil {
		t.Fatalf("seed old notification: %v", err)
	}
	pruned, err := repo.PruneClosedVaccinationProjection(ctx, testTenantID, time.Now().UTC().Add(-90*24*time.Hour), 10)
	if err != nil {
		t.Fatalf("PruneClosedVaccinationProjection: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("pruned = %d, want 1", pruned)
	}
	assertCount(t, ctx, pool, "pruned projection", `
SELECT count(*)
FROM calendar_event_projections
WHERE tenant_id=$1::uuid AND event_id=$2`, 0, testTenantID, oldEventID)
	assertCount(t, ctx, pool, "notification history after prune", `
SELECT count(*)
FROM notification_requests
WHERE tenant_id=$1::uuid AND calendar_event_id=$2`, 1, testTenantID, oldEventID)
}

func TestCalendarListsOldAcceptedVaccinationHistoryWithoutHotProjection(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	const (
		protocolID  = "86000000-0000-4000-8000-000000000931"
		versionID   = "86000000-0000-4000-8000-000000000932"
		ruleID      = "86000000-0000-4000-8000-000000000933"
		obligationA = "86000000-0000-4000-8000-000000000934"
		obligationB = "86000000-0000-4000-8000-000000000935"
		completionA = "86000000-0000-4000-8000-000000000936"
		completionB = "86000000-0000-4000-8000-000000000937"
	)
	administeredAt := time.Date(2025, time.December, 9, 3, 30, 0, 0, time.UTC)
	completedStatus := domain.StatusCompleted
	seedCalendarLocations(t, ctx, pool, testParkA, testShedA)
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, obligationA, administeredAt)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, obligationB, administeredAt.Add(20*time.Minute))
	seedCalendarGoat(t, ctx, pool, obligationA)
	if _, err := pool.Exec(ctx, `
UPDATE goats
SET park_id=$2::uuid, shed_id=$3::uuid, current_location_id=$3::uuid, updated_at=now()
	WHERE tenant_id=$1::uuid AND goat_id IN ($4::uuid, $5::uuid)`,
		testTenantID, testParkA, testShedA, obligationA, obligationB); err != nil {
		t.Fatalf("locate history goats: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET target_type='goat', target_id=obligation_id, scope_type='shed', scope_id=$2::uuid,
    status='completed', completed_at=due_at, updated_at=now()
	WHERE tenant_id=$1::uuid AND obligation_id IN ($3::uuid, $4::uuid)`,
		testTenantID, testShedA, obligationA, obligationB); err != nil {
		t.Fatalf("complete history obligations: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_completions (
  completion_id, tenant_id, obligation_id, goat_id, administered_at, verified_at,
  verified_by, status, idempotency_key
) VALUES
	  ($2::uuid, $1::uuid, $3::uuid, $3::uuid, $6::timestamptz, $6::timestamptz, $5::uuid, 'accepted', 'calendar-history-a'),
	  ($7::uuid, $1::uuid, $4::uuid, $4::uuid, $8::timestamptz, $8::timestamptz, $5::uuid, 'accepted', 'calendar-history-b')`,
		testTenantID, completionA, obligationA, obligationB, testActorID,
		administeredAt, completionB, administeredAt.Add(20*time.Minute)); err != nil {
		t.Fatalf("seed accepted vaccination history: %v", err)
	}

	list, err := repo.ListEvents(ctx, domain.Query{
		TenantID:           testTenantID,
		OwnerKey:           domain.OwnerAll,
		Status:             &completedStatus,
		DateFrom:           administeredAt.Add(-time.Hour),
		DateTo:             administeredAt.Add(24 * time.Hour),
		Limit:              20,
		IncludeDateMarkers: true,
		Scope:              domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents history: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("history items=%d, want one shed/rule/day group: %#v", len(list.Items), list.Items)
	}
	history := list.Items[0]
	if history.EventType != domain.EventVaccinationHistory || history.Status != domain.StatusCompleted || history.TargetCount != 2 {
		t.Fatalf("history event=%#v, want completed history with two targets", history)
	}
	expectedHistoryEventID := "history:2025-12-09:" + testParkA + ":" + testShedA + ":" + ruleID
	if history.EventID != expectedHistoryEventID {
		t.Fatalf("history event id=%q, want stable grouped id %q", history.EventID, expectedHistoryEventID)
	}
	if len(list.DateMarkers) != 1 || list.DateMarkers[0].Date != "2025-12-09" || list.DateMarkers[0].CompletedCount != 2 || list.DateMarkers[0].OpenCount != 0 {
		t.Fatalf("history date markers=%#v, want one completed marker with two administrations", list.DateMarkers)
	}
	assertCount(t, ctx, pool, "old history not copied to hot projection", `
SELECT count(*) FROM calendar_event_projections
WHERE tenant_id=$1::uuid AND event_id=$2`, 0, testTenantID, history.EventID)

	detail, err := repo.GetEventDetail(ctx, domain.EventQuery{
		TenantID: testTenantID,
		EventID:  history.EventID,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("GetEventDetail history: %v", err)
	}
	if detail.Event.TargetCount != 2 || detail.Event.Status != domain.StatusCompleted || !strings.Contains(string(detail.Execution), `"completed_count": 2`) {
		t.Fatalf("history detail=%#v execution=%s", detail.Event, detail.Execution)
	}
	seedCalendarLocations(t, ctx, pool, testParkB, testShedB)
	if _, err := pool.Exec(ctx, `
UPDATE goats
SET park_id=$2::uuid, shed_id=$3::uuid, current_location_id=$3::uuid, updated_at=now()
	WHERE tenant_id=$1::uuid AND goat_id IN ($4::uuid, $5::uuid)`,
		testTenantID, testParkB, testShedB, obligationA, obligationB); err != nil {
		t.Fatalf("relocate completed-history goats: %v", err)
	}
	stableList, err := repo.ListEvents(ctx, domain.Query{
		TenantID:           testTenantID,
		OwnerKey:           domain.OwnerAll,
		Status:             &completedStatus,
		DateFrom:           administeredAt.Add(-time.Hour),
		DateTo:             administeredAt.Add(24 * time.Hour),
		Limit:              20,
		IncludeDateMarkers: true,
		Scope:              domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents history after relocation: %v", err)
	}
	if len(stableList.Items) != 1 || stableList.Items[0].EventID != expectedHistoryEventID {
		t.Fatalf("history after relocation=%#v, want stable event id %q", stableList.Items, expectedHistoryEventID)
	}
	stableDetail, err := repo.GetEventDetail(ctx, domain.EventQuery{
		TenantID: testTenantID,
		EventID:  expectedHistoryEventID,
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("GetEventDetail history after relocation: %v", err)
	}
	if stableDetail.Event.EventID != expectedHistoryEventID || stableDetail.Event.TargetCount != 2 {
		t.Fatalf("stable history detail=%#v, want stable id %q with two targets", stableDetail.Event, expectedHistoryEventID)
	}

	openEventID := "calendar:86000000-0000-4000-8000-000000000938"
	openDueAt := administeredAt.Add(8 * 24 * time.Hour)
	seedCalendarProjection(t, ctx, pool, openEventID, openDueAt, "not_scheduled")
	markerList, err := repo.ListEvents(ctx, domain.Query{
		TenantID:           testTenantID,
		OwnerKey:           domain.OwnerAll,
		DateFrom:           administeredAt.Add(-time.Hour),
		DateTo:             openDueAt.Add(time.Hour),
		Limit:              1,
		IncludeDateMarkers: true,
		Scope:              domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents open work with date markers: %v", err)
	}
	if len(markerList.Items) != 1 || markerList.Items[0].EventID != openEventID {
		t.Fatalf("default items=%#v, want only open work (history is fetched explicitly)", markerList.Items)
	}
	if len(markerList.DateMarkers) != 2 || markerList.DateMarkers[0].CompletedCount != 2 || markerList.DateMarkers[1].OpenCount != 1 {
		t.Fatalf("date markers=%#v, want history and future dates independent of item pagination", markerList.DateMarkers)
	}
}

func TestCalendarConfigActivationReviewNudgeIsActionable(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	eventID := "calendar:86000000-0000-4000-8000-000000000901"
	if _, err := pool.Exec(ctx, `
INSERT INTO calendar_event_projections (
  tenant_id, event_id, slice_key, event_type, owner_key, title, subtitle, status, severity,
  due_at, window_start, window_end, timezone, timezone_source, target_type, target_count,
  source_backed, source_label, source_target_type, source_target_id, assignee_label,
  executor_role, reminder_state, primary_notification_channel, escalation_state,
  system, cross_cutting, links, detail
) VALUES (
  $1::uuid, $2, 'vaccination', 'vaccination_config_activation_review', 'admin_data_ops',
  'Review activation', 'Protocol activation review due', 'due', 'warning',
  now() + interval '2 hours', now() + interval '2 hours', now() + interval '1 day',
  'Asia/Kolkata', 'india_only', 'protocol_version', 1, false, 'draft protocol',
  'protocol_version', '86000000-0000-4000-8000-000000000902',
  'Admin Data Ops reviewer', 'admin_data_ops_reviewer', 'not_scheduled', 'local-stub',
  'none', false, false, '{}'::jsonb,
  '{"summary":{"owner":"Admin / Data Ops"},"source_and_rule":{"review_state":"approval_due"},"execution":{},"stock":{},"proof":{},"verification":{},"notification_channels":["local-stub"],"notification_policy":{"nudge_allowed":true},"links":{}}'::jsonb
)`, testTenantID, eventID); err != nil {
		t.Fatalf("seed config activation event: %v", err)
	}
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

func seedCalendarProjectionState(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO calendar_projection_state (
  tenant_id, slice_key, projection_version, projected_at, date_from, date_to,
  freshness_status, serving_state
) VALUES ($1::uuid, 'vaccination', (extract(epoch FROM now()) * 1000)::bigint, now(),
  now() - interval '90 days', now() + interval '45 days', 'green', 'fresh')
ON CONFLICT (tenant_id, slice_key) DO UPDATE SET
  projection_version = EXCLUDED.projection_version,
  projected_at = now(), freshness_status = 'green', serving_state = 'fresh', last_error = NULL`, testTenantID); err != nil {
		t.Fatalf("seed calendar projection state: %v", err)
	}
}

func seedCalendarProjection(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID string, dueAt time.Time, reminderState string) {
	t.Helper()
	seedCalendarProjectionState(t, ctx, pool)
	_, err := pool.Exec(ctx, `
INSERT INTO calendar_event_projections (
  tenant_id, event_id, slice_key, event_type, owner_key, title, subtitle, status, severity,
  due_at, window_start, window_end, timezone, timezone_source, target_type, target_count,
  source_backed, source_label, source_target_type, source_target_id, assignee_label,
  executor_role, reminder_state, primary_notification_channel, escalation_state,
  system, cross_cutting, links, detail
) VALUES (
	  $1::uuid, $2, 'vaccination', 'vaccination_drive', 'pc', 'ET primary drive',
	  'Calendar integration test', 'due', 'warning', $3::timestamptz, $3::timestamptz,
	  $3::timestamptz + interval '1 day', 'Asia/Kolkata', 'india_only', 'shed', 20,
	  true, 'integration source-backed rule', 'batch', '86000000-0000-4000-8000-000000001001',
	  'PC test owner', 'pc_vaccinator', $4, 'local-stub', 'none', false, false,
  '{"workflow":"/vaccination/workflows/test"}'::jsonb,
  '{"summary":{"owner":"PC"},"source_and_rule":{"source_backed":true},"execution":{"work_state":"due"},"stock":{},"proof":{},"verification":{},"notification_channels":["local-stub","slack"],"notification_policy":{"nudge_allowed":true},"links":{}}'::jsonb
)
ON CONFLICT (tenant_id, event_id) DO UPDATE
SET due_at = EXCLUDED.due_at,
    reminder_state = EXCLUDED.reminder_state,
    updated_at = now()`, testTenantID, eventID, dueAt, reminderState)
	if err != nil {
		t.Fatalf("seed calendar projection: %v", err)
	}
}

func seedScopedCalendarProjection(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID, sourceID, parkID, shedID string, system bool) {
	t.Helper()
	seedCalendarProjectionState(t, ctx, pool)
	seedCalendarLocations(t, ctx, pool, parkID, shedID)
	_, err := pool.Exec(ctx, `
INSERT INTO calendar_event_projections (
  tenant_id, event_id, slice_key, event_type, owner_key, title, subtitle, status, severity,
  due_at, window_start, window_end, timezone, timezone_source, park_id, park_code, shed_id, shed_name,
  target_type, target_count, source_backed, source_label, source_target_type, source_target_id,
  assignee_label, executor_role, reminder_state, primary_notification_channel, escalation_state,
  system, cross_cutting, links, detail
) VALUES (
  $5::uuid, $3, 'vaccination', 'vaccination_drive', 'pc', 'Scoped drive',
  'Scoped integration test', 'due', 'warning', now() + interval '2 hours', now(), now() + interval '1 day',
  'Asia/Kolkata', 'india_only', $1::uuid, 'TST', $2::uuid, 'Scoped Shed',
  'shed', 1, true, 'active matrix test', 'shed', $4::uuid,
  'PC test owner', 'pc_vaccinator', 'not_scheduled', 'local-stub', 'none',
  $6, false, '{}'::jsonb,
  '{"summary":{"owner":"PC"},"source_and_rule":{"source_backed":true},"execution":{"work_state":"due"},"stock":{},"proof":{},"verification":{},"notification_channels":["local-stub"],"notification_policy":{"nudge_allowed":true},"links":{}}'::jsonb
)
ON CONFLICT (tenant_id, event_id) DO UPDATE
SET park_id = EXCLUDED.park_id,
    shed_id = EXCLUDED.shed_id,
    system = EXCLUDED.system,
    updated_at = now()`, parkID, shedID, eventID, sourceID, testTenantID, system)
	if err != nil {
		t.Fatalf("seed scoped calendar projection: %v", err)
	}
}

func seedLegacyCatchupProjection(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID, sourceTargetID string, dueAt time.Time) {
	t.Helper()
	seedCalendarProjectionState(t, ctx, pool)
	_, err := pool.Exec(ctx, `
INSERT INTO calendar_event_projections (
  tenant_id, event_id, slice_key, event_type, owner_key, title, subtitle, status, severity,
  due_at, window_start, window_end, timezone, timezone_source, target_type, target_count,
  source_backed, source_label, source_target_type, source_target_id, assignee_label,
  executor_role, reminder_state, primary_notification_channel, escalation_state,
  system, cross_cutting, links, detail
) VALUES (
  $1::uuid, $2, 'vaccination', 'vaccination_drive', 'pc', 'Legacy catch-up drive',
  'Legacy catch-up projection', 'due', 'warning', $3::timestamptz, $3::timestamptz,
  $3::timestamptz + interval '1 day', 'Asia/Kolkata', 'india_only', 'shed', 1,
  true, 'legacy catch-up source', 'catchup', $4::uuid,
  'PC test owner', 'pc_vaccinator', 'not_scheduled', 'local-stub', 'none',
  false, false, '{}'::jsonb,
  '{"summary":{"owner":"PC"},"source_and_rule":{"legacy_format":true},"execution":{"work_state":"due"},"stock":{},"proof":{},"verification":{},"notification_channels":["local-stub"],"notification_policy":{"nudge_allowed":false},"links":{}}'::jsonb
)`, testTenantID, eventID, dueAt, sourceTargetID)
	if err != nil {
		t.Fatalf("seed legacy catch-up projection: %v", err)
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
	seedCalendarProjectionState(t, ctx, pool)
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
