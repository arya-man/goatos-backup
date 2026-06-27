package postgres

import (
	"context"
	"errors"
	"fmt"
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
	testParkA         = "86000000-0000-4000-8000-000000000701"
	testParkB         = "86000000-0000-4000-8000-000000000702"
	testShedA         = "86000000-0000-4000-8000-000000000711"
	testShedB         = "86000000-0000-4000-8000-000000000712"
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
		Scope:    domain.ScopeFilter{TenantWide: true},
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("list items = %d, want 2", len(list.Items))
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
	eventA := "obligation:86000000-0000-4000-8000-000000001101"
	eventB := "obligation:86000000-0000-4000-8000-000000001102"
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
	activeID := "obligation:86000000-0000-4000-8000-000000000791"
	completedID := "obligation:86000000-0000-4000-8000-000000000792"
	canceledID := "obligation:86000000-0000-4000-8000-000000000793"
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
	if count != 1 {
		t.Fatalf("projection count = %d, want 1", count)
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
}

func TestCalendarVaccinationProjectionRefreshPaginatesAndTombstonesStaleSource(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	dueAt := time.Now().UTC().Add(4 * time.Hour)
	obligationIDs := []string{
		"86000000-0000-4000-8000-000000000814",
		"86000000-0000-4000-8000-000000000824",
		"86000000-0000-4000-8000-000000000834",
	}
	for i, obligationID := range obligationIDs {
		seedVaccinationObligation(t, ctx, pool,
			fmt.Sprintf("86000000-0000-4000-8000-00000000081%d", i),
			fmt.Sprintf("86000000-0000-4000-8000-00000000082%d", i),
			fmt.Sprintf("86000000-0000-4000-8000-00000000083%d", i),
			obligationID,
			dueAt.Add(time.Duration(i)*time.Hour),
		)
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
	if count != len(obligationIDs) {
		t.Fatalf("projection count = %d, want %d", count, len(obligationIDs))
	}
	assertCount(t, ctx, pool, "projected obligations", `
SELECT count(*)
FROM calendar_event_projections
WHERE tenant_id=$1::uuid
  AND event_id = ANY($2::text[])
  AND status <> 'canceled'`, len(obligationIDs), testTenantID, []string{
		"obligation:" + obligationIDs[0],
		"obligation:" + obligationIDs[1],
		"obligation:" + obligationIDs[2],
	})
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
	var status string
	var tombstoneReason string
	if err := pool.QueryRow(ctx, `
SELECT status, COALESCE(detail -> 'tombstone' ->> 'reason', '')
FROM calendar_event_projections
WHERE tenant_id = $1::uuid AND event_id = $2`,
		testTenantID, "obligation:"+obligationIDs[1]).Scan(&status, &tombstoneReason); err != nil {
		t.Fatalf("query tombstoned projection: %v", err)
	}
	if status != domain.StatusCanceled || tombstoneReason != "source_no_longer_qualifies" {
		t.Fatalf("tombstone status=%s reason=%s", status, tombstoneReason)
	}
}

func TestCalendarConfigSourceApprovalNudgeIsActionable(t *testing.T) {
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
  $1::uuid, $2, 'vaccination', 'vaccination_config_source_approval', 'admin_data_ops',
  'Approve source review', 'Protocol source review due', 'due', 'warning',
  now() + interval '2 hours', now() + interval '2 hours', now() + interval '1 day',
  'Asia/Kolkata', 'fallback', 'protocol_version', 1, false, 'draft protocol',
  'protocol_version', '86000000-0000-4000-8000-000000000902',
  'Admin Data Ops reviewer', 'admin_data_ops_reviewer', 'not_scheduled', 'local-stub',
  'none', false, false, '{}'::jsonb,
  '{"summary":{"owner":"Admin / Data Ops"},"source_and_rule":{"review_state":"approval_due"},"execution":{},"stock":{},"proof":{},"verification":{},"notification_channels":["local-stub"],"notification_policy":{"nudge_allowed":true},"links":{}}'::jsonb
)`, testTenantID, eventID); err != nil {
		t.Fatalf("seed config approval event: %v", err)
	}
	if _, err := repo.SendNudge(ctx, ports.SendNudge{
		TenantID: testTenantID, EventID: eventID, ActorID: testActorID,
		IdempotencyKey: "calendar-config-nudge-key", Channel: "local-stub",
		Message: "Please review the protocol source", Scope: domain.ScopeFilter{TenantWide: true},
	}); err != nil {
		t.Fatalf("SendNudge config approval: %v", err)
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

func seedScopedCalendarProjection(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID, sourceID, parkID, shedID string, system bool) {
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
	_, err = pool.Exec(ctx, `
INSERT INTO calendar_event_projections (
  tenant_id, event_id, slice_key, event_type, owner_key, title, subtitle, status, severity,
  due_at, window_start, window_end, timezone, timezone_source, park_id, park_code, shed_id, shed_name,
  target_type, target_count, source_backed, source_label, source_target_type, source_target_id,
  assignee_label, executor_role, reminder_state, primary_notification_channel, escalation_state,
  system, cross_cutting, links, detail
) VALUES (
  $5::uuid, $3, 'vaccination', 'vaccination_dose_due', 'phc', 'Scoped dose due',
  'Scoped integration test', 'due', 'warning', now() + interval '2 hours', now(), now() + interval '1 day',
  'Asia/Kolkata', 'location', $1::uuid, 'TST', $2::uuid, 'Scoped Shed',
  'shed', 1, true, 'source-backed test', 'shed', $4::uuid,
  'PHC test owner', 'phc_vaccinator', 'not_scheduled', 'local-stub', 'none',
  $6, false, '{}'::jsonb,
  '{"summary":{"owner":"PHC"},"source_and_rule":{"source_backed":true},"execution":{"work_state":"due"},"stock":{},"proof":{},"verification":{},"notification_channels":["local-stub"],"notification_policy":{"nudge_allowed":true},"links":{}}'::jsonb
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
  'Projection source-backed published test', 'published', DATE '2026-01-01', DATE '2028-01-01',
  '{"source":{"review_status":"approved","source_ref":"docs/phc-vaccination/PRD.md","source_system":"phc","approved_by":"test","approved_at":"2026-06-27T00:00:00Z"}}'::jsonb,
  '{"required_proofs":["administration"]}'::jsonb, now()
)
ON CONFLICT (protocol_version_id) DO UPDATE
SET status = 'published',
    rule_dsl = EXCLUDED.rule_dsl,
    updated_at = now()`,
		versionID, testTenantID, protocolID)
	if err != nil {
		t.Fatalf("seed protocol version: %v", err)
	}
	_, err = pool.Exec(ctx, `
INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
  offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json,
  proof_policy, sort_order
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'PROJ-PRIMARY', 1, 'calendar',
  0, 1, 0, 'none', 'immediate', '{}'::jsonb, '{"required_proofs":["administration"]}'::jsonb, 10
)
ON CONFLICT (rule_id) DO UPDATE
SET dose_code = EXCLUDED.dose_code`,
		ruleID, testTenantID, versionID)
	if err != nil {
		t.Fatalf("seed protocol rule: %v", err)
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
