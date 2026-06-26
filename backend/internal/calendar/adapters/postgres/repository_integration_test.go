package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	testTenantID      = "00000000-0000-4000-8000-000000000001"
	testActorID       = "86000000-0000-4000-8000-000000009999"
	testCalendarEvent = "obligation:86000000-0000-4000-8000-000000001001"
	testReminderEvent = "calendar:86000000-0000-4000-8000-000000001002"
)

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
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("list items = %d, want 2", len(list.Items))
	}
	detail, err := repo.GetEventDetail(ctx, testTenantID, testCalendarEvent)
	if err != nil {
		t.Fatalf("GetEventDetail: %v", err)
	}
	if detail.Event.EventID != testCalendarEvent || len(detail.NotificationChannels) != 2 {
		t.Fatalf("detail = %#v channels=%#v", detail.Event, detail.NotificationChannels)
	}

	first, err := repo.SendNudge(ctx, ports.SendNudge{
		TenantID: testTenantID, EventID: testCalendarEvent, ActorID: testActorID,
		TraceID: "trace-nudge", IdempotencyKey: "calendar-nudge-key", Channel: "slack",
		Message: "Please handle this dose", Reason: "CEO follow-up",
	})
	if err != nil {
		t.Fatalf("SendNudge first: %v", err)
	}
	replay, err := repo.SendNudge(ctx, ports.SendNudge{
		TenantID: testTenantID, EventID: testCalendarEvent, ActorID: testActorID,
		TraceID: "trace-nudge", IdempotencyKey: "calendar-nudge-key", Channel: "slack",
		Message: "Please handle this dose", Reason: "CEO follow-up",
	})
	if err != nil {
		t.Fatalf("SendNudge replay: %v", err)
	}
	if replay.ActionID != first.ActionID || !replay.IdempotentReplay {
		t.Fatalf("nudge replay = %#v, want same action replay", replay)
	}
	if _, err := repo.SendNudge(ctx, ports.SendNudge{
		TenantID: testTenantID, EventID: testCalendarEvent, ActorID: testActorID,
		IdempotencyKey: "calendar-nudge-key", Channel: "email", Message: "different",
	}); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("SendNudge conflict err = %v, want ErrIdempotencyConflict", err)
	}
	assertCount(t, ctx, pool, "nudge notifications", `SELECT count(*) FROM notification_requests WHERE tenant_id=$1 AND calendar_event_id=$2 AND notification_type='nudge'`, 1, testTenantID, testCalendarEvent)
	assertCount(t, ctx, pool, "nudge outbox", `SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_type='calendar_notification' AND aggregate_id=$2::uuid`, 1, testTenantID, first.ActionID)
	assertCount(t, ctx, pool, "nudge audit", `SELECT count(*) FROM audit_log WHERE tenant_id=$1 AND metadata->>'calendar_event_id'=$2 AND action='calendar.nudge.sent'`, 1, testTenantID, testCalendarEvent)

	snoozeUntil := time.Now().UTC().Add(6 * time.Hour)
	snooze, err := repo.Snooze(ctx, ports.Snooze{
		TenantID: testTenantID, EventID: testCalendarEvent, ActorID: testActorID,
		TraceID: "trace-snooze", IdempotencyKey: "calendar-snooze-key",
		SnoozeUntil: snoozeUntil, Reason: "wait for morning round",
	})
	if err != nil {
		t.Fatalf("Snooze first: %v", err)
	}
	snoozeReplay, err := repo.Snooze(ctx, ports.Snooze{
		TenantID: testTenantID, EventID: testCalendarEvent, ActorID: testActorID,
		TraceID: "trace-snooze", IdempotencyKey: "calendar-snooze-key",
		SnoozeUntil: snoozeUntil, Reason: "wait for morning round",
	})
	if err != nil {
		t.Fatalf("Snooze replay: %v", err)
	}
	if snoozeReplay.ActionID != snooze.ActionID || !snoozeReplay.IdempotentReplay {
		t.Fatalf("snooze replay = %#v, want same action replay", snoozeReplay)
	}
	if _, err := repo.Snooze(ctx, ports.Snooze{
		TenantID: testTenantID, EventID: testCalendarEvent, ActorID: testActorID,
		IdempotencyKey: "calendar-snooze-key", SnoozeUntil: snoozeUntil.Add(time.Hour), Reason: "different",
	}); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("Snooze conflict err = %v, want ErrIdempotencyConflict", err)
	}
	if _, err := repo.Snooze(ctx, ports.Snooze{
		TenantID: testTenantID, EventID: testCalendarEvent, ActorID: testActorID,
		IdempotencyKey: "calendar-snooze-second", SnoozeUntil: snoozeUntil.Add(time.Hour), Reason: "second active",
	}); !errors.Is(err, ports.ErrActiveSnoozeExists) {
		t.Fatalf("second active snooze err = %v, want ErrActiveSnoozeExists", err)
	}
	assertCount(t, ctx, pool, "active snoozes", `SELECT count(*) FROM calendar_snoozes WHERE tenant_id=$1 AND calendar_event_id=$2 AND status='active'`, 1, testTenantID, testCalendarEvent)

	history, err := repo.History(ctx, domain.HistoryQuery{TenantID: testTenantID, EventID: testCalendarEvent, Limit: 20})
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
		nil, "", 200)
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

func seedCalendarProjection(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID string, dueAt time.Time, reminderState string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
INSERT INTO calendar_event_projections (
  tenant_id, event_id, slice_key, event_type, owner_key, title, subtitle, status, severity,
  due_at, window_start, window_end, timezone, timezone_source, target_type, target_count,
  source_backed, source_label, source_target_type, source_target_id, assignee_label,
  executor_role, reminder_state, primary_notification_channel, escalation_state,
  system, cross_cutting, links, detail
) VALUES (
  $1::uuid, $2, 'vaccination', 'vaccination_dose_due', 'phc', 'ET primary dose due',
  'Calendar integration test', 'due', 'warning', $3::timestamptz, $3::timestamptz,
  $3::timestamptz + interval '1 day', 'Asia/Kolkata', 'fallback', 'cohort', 20,
  true, 'integration source-backed rule', 'cohort', '86000000-0000-4000-8000-00000000f001',
  'PHC test owner', 'phc_vaccinator', $4, 'local-stub', 'none', false, false,
  '{"workflow":"/vaccination/workflows/test"}'::jsonb,
  '{"summary":{"owner":"PHC"},"source_and_rule":{"source_backed":true},"execution":{"work_state":"due"},"stock":{},"proof":{},"verification":{},"notification_channels":["local-stub","slack"],"notification_policy":{"nudge_allowed":true},"links":{}}'::jsonb
)
ON CONFLICT (tenant_id, event_id) DO UPDATE
SET due_at = EXCLUDED.due_at,
    reminder_state = EXCLUDED.reminder_state,
    updated_at = now()`, testTenantID, eventID, dueAt, reminderState)
	if err != nil {
		t.Fatalf("seed calendar projection: %v", err)
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
