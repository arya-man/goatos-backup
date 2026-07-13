package main

// TestReminderCadence_ProducesRoleScopedFiresOverTime proves the vaccination reminder cadence ladder
// (docs/decisions/vaccination-notification-rules.md §3/§4a) end to end against a real Postgres
// container, driving the SAME production sweep/queue path a deployed calendar-reminder-sweeper binary
// runs (sweepReminderCadence -> calendarapp.Service.SweepReminderCadence ->
// workforceapp.RosterService.ResolvePositionRecipientsBatch -> calendarapp.Service.
// QueueReminderCadenceBatch), at a sequence of FIXED as-of times (never time.Now()):
//   - T-7        -> exactly one advance_notice row per park operator/park_head/phc_manager device;
//     leadership (tenant-scope) gets none.
//   - T-3 08:00  -> a reminder row; the SAME slot replayed is a no-op (per-offset-slot idempotency).
//   - quiet hours (22:00) defer a still-pending fire (no row); the next allowed slot (07:00) then
//     queues it.
//   - T-0 08:00  -> a due_today (high priority) row.
//   - after the obligation is marked completed, a later sweep queues NO further reminder
//     (de-scheduling).
//   - leadership NEVER receives a notification_requests row, across every sweep in the test.
//
// This test seeds only INPUT facts (tenant/park, workforce members/positions/devices, a protocol
// definition/version/rule chain, sop_tasks, obligation_instances, and the calendar_event_projections
// row the real vaccination projector would materialize for that obligation -- out of scope for this
// notification-layer test to re-drive, same precedent as
// internal/notificationbridge/verification_notify_integration_test.go) and then drives the REAL sweep
// through sweepReminderCadence -- never a direct INSERT into notification_requests.
import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	calendardomain "github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// Baseline fixture ids already present in every migrated database (migration
// 000001_phase_1_identity_foundation.sql), the same ones
// internal/notificationbridge/verification_notify_integration_test.go and tests/e2e/harness_test.go
// reuse.
const (
	rcTenant = "00000000-0000-4000-8000-000000000001"
	rcPark   = "00000000-0000-4000-8000-000000003001"
)

const (
	rcOperatorMember   = "fa000000-0000-4000-8000-000000000001"
	rcParkHeadMember   = "fa000000-0000-4000-8000-000000000002"
	rcManagerMember    = "fa000000-0000-4000-8000-000000000003"
	rcLeadershipMember = "fa000000-0000-4000-8000-000000000004"

	rcOperatorDevice   = "fa000000-0000-4000-8000-000000000011"
	rcParkHeadDevice   = "fa000000-0000-4000-8000-000000000012"
	rcManagerDevice    = "fa000000-0000-4000-8000-000000000013"
	rcLeadershipDevice = "fa000000-0000-4000-8000-000000000014"

	rcOperatorToken   = "fcm-token-rc-operator-0001"
	rcParkHeadToken   = "fcm-token-rc-parkhead-0002"
	rcManagerToken    = "fcm-token-rc-manager-0003"
	rcLeadershipToken = "fcm-token-rc-leadership-0004"

	rcLeadershipPosition = "pc_director"

	// Baseline vaccination SOP skeleton ids seeded by migration 000075_vaccination_module.sql for
	// rcTenant (same fixture internal/notificationbridge/verification_notify_integration_test.go and
	// internal/obligation/adapters/postgres/sweeper_integration_test.go reuse).
	rcSkeletonSOPID     = "b0000000-0000-4000-8000-000000000001"
	rcSkeletonVersionID = "b0000000-0000-4000-8000-000000000002"
)

func TestReminderCadence_ProducesRoleScopedFiresOverTime(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	workforceRepo := workforcepg.NewRepository(pool, 5*time.Second)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	calendarRepo := calendarpg.NewRepository(pool, 5*time.Second)
	calendarService := calendarapp.NewService(calendarRepo)
	protoRepo := protopg.NewRepository(pool, 5*time.Second)

	// ---- Seed INPUT facts only ------------------------------------------------------------------

	seedMember := func(id, code, name, roleHint string) {
		exec(t, ctx, pool, "workforce member "+code,
			`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
			 VALUES ($1, $2, $3, $4, 'active', $5)`,
			id, rcTenant, code, name, roleHint)
	}
	seedMember(rcOperatorMember, "RC-OP", "RC Operator", "operator")
	seedMember(rcParkHeadMember, "RC-PH", "RC Park Head", "park_head")
	seedMember(rcManagerMember, "RC-MGR", "RC PHC Manager", "other")
	seedMember(rcLeadershipMember, "RC-LEAD", "RC PC Director", "other")

	// valid_from is a FIXED instant well before the earliest sweep as-of (T-7 = dueD-7d = 2026-07-13
	// 09:00 IST), NOT now()-based: ResolvePositionRecipientsBatch filters `valid_from <= $at`, and the
	// sweep drives fixed historical as-of times. A wall-clock now() seed makes the seat invalid at the
	// T-7 as-of whenever the test runs after ~10:30 IST on 2026-07-13, producing 0 recipients (a
	// time-of-day-dependent failure). Seed the window around the test's fixed dates instead.
	const rcPositionValidFrom = "2026-06-20 00:00:00+05:30"
	seedPosition := func(memberID, positionCode, scopeType, scopeID, tier string) {
		exec(t, ctx, pool, "workforce position "+positionCode,
			`INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, status, valid_from)
			 VALUES ($1, $2, $3, $4, $5, $6, 'active', $7::timestamptz)`,
			rcTenant, memberID, scopeType, scopeID, positionCode, tier, rcPositionValidFrom)
	}
	seedPosition(rcOperatorMember, "operator", "center", rcPark, "assistant")
	seedPosition(rcParkHeadMember, "park_head", "center", rcPark, "head")
	seedPosition(rcManagerMember, "phc_manager", "center", rcPark, "manager")
	// Leadership sits at TENANT scope -- structurally outside ResolvePositionRecipientsBatch, which
	// this sweeper only ever calls with scopeType="center". Proves the "never leadership" rule at
	// the SQL level, not just by omission.
	seedPosition(rcLeadershipMember, rcLeadershipPosition, "tenant", rcTenant, "director")

	seedDevice := func(id, memberID, appInstallID, fcmToken string) {
		exec(t, ctx, pool, "device "+appInstallID,
			`INSERT INTO workforce_member_devices (device_id, tenant_id, workforce_member_id, platform, app_install_id, fcm_token, app_version, os_version, status, last_seen_at, registered_by)
			 VALUES ($1, $2, $3, 'android', $4, $5, '1.0.0', '14', 'active', now(), $3)`,
			id, rcTenant, memberID, appInstallID, fcmToken)
	}
	seedDevice(rcOperatorDevice, rcOperatorMember, "rc-install-operator", rcOperatorToken)
	seedDevice(rcParkHeadDevice, rcParkHeadMember, "rc-install-parkhead", rcParkHeadToken)
	seedDevice(rcManagerDevice, rcManagerMember, "rc-install-manager", rcManagerToken)
	seedDevice(rcLeadershipDevice, rcLeadershipMember, "rc-install-leadership", rcLeadershipToken)

	// Protocol definition -> version -> rule chain so obligation_instances' FKs are satisfied
	// (mirrors internal/notificationbridge/verification_notify_integration_test.go).
	protocolID, err := protoRepo.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: rcTenant, Code: "vaccination.reminder_cadence", Name: "RC Reminder Cadence", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("seed protocol definition: %v", err)
	}
	protocolVersionID, err := protoRepo.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: rcTenant, ProtocolID: protocolID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte("{}"), ProofPolicy: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("seed protocol version: %v", err)
	}
	ruleID, err := protoRepo.CreateRule(ctx, protodomain.NewRule{
		TenantID: rcTenant, ProtocolVersionID: protocolVersionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", EligibilityJSON: []byte("{}"), ProofPolicy: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("seed protocol rule: %v", err)
	}

	// seedObligation seeds one sop_task + obligation_instance + calendar_event_projections row for a
	// FIXED due date D, standing in for the real vaccination projector's output (out of scope for
	// this notification-layer test to re-drive -- same precedent as verification_notify_integration_test.go).
	seedObligation := func(suffix, obligationID, sopTaskID string, dueAt time.Time) string {
		eventID := "calendar:" + sopTaskID
		exec(t, ctx, pool, "sop task "+suffix,
			`INSERT INTO sop_tasks (task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id)
			 VALUES ($1, $2, $3, $4, 'vaccination_drive', $5, 'queued', 'park', $6)`,
			sopTaskID, rcTenant, rcSkeletonSOPID, rcSkeletonVersionID, "RC drive "+suffix, rcPark)
		exec(t, ctx, pool, "obligation instance "+suffix,
			`INSERT INTO obligation_instances (
			   obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id,
			   scope_type, scope_id, due_at, sop_task_id, idempotency_key, status
			 ) VALUES ($1, $2, $3, $4, 'tenant', $2, 'park', $5, $6, $7, $8, 'scheduled')`,
			obligationID, rcTenant, protocolVersionID, ruleID, rcPark, dueAt, sopTaskID, "rc-obl-idem-"+suffix)
		exec(t, ctx, pool, "calendar event projection "+suffix,
			`INSERT INTO calendar_event_projections (
			   tenant_id, event_id, slice_key, event_type, owner_key, title, status, due_at,
			   target_type, target_count, park_id, executor_role, source_target_type, source_target_id
			 ) VALUES ($1, $2, 'vaccination', 'vaccination_drive', 'pc', $3, 'scheduled', $4,
			           'obligation', 1, $5, 'operator', 'calendar_event', $6)`,
			rcTenant, eventID, "RC drive "+suffix, dueAt, rcPark, obligationID)
		return eventID
	}

	// ---- Scenario obligation: due date D = 2026-07-20 10:00 IST ---------------------------------
	dueD := time.Date(2026, 7, 20, 10, 0, 0, 0, biztime.DefaultLocation())
	primaryObligationID := "fa100000-0000-4000-8000-000000000001"
	primarySOPTaskID := "fa100000-0000-4000-8000-000000000002"
	primaryEventID := seedObligation("primary", primaryObligationID, primarySOPTaskID, dueD)

	ladder := calendardomain.DefaultReminderLadder()

	sweepAt := func(label string, now time.Time) int {
		t.Helper()
		n, err := sweepReminderCadence(ctx, calendarService, rosterService, reminderCadenceTick{
			TenantID: rcTenant, Now: now, Ladder: ladder, Limit: 50,
		})
		if err != nil {
			t.Fatalf("sweepReminderCadence(%s): %v", label, err)
		}
		return n
	}

	assertNoLeadershipRows := func(label string) {
		t.Helper()
		n := countRows(t, ctx, pool, `SELECT count(*) FROM notification_requests WHERE tenant_id = $1 AND recipient_ref = $2`, rcTenant, rcLeadershipToken)
		if n != 0 {
			t.Fatalf("[%s] leadership notification rows = %d, want 0", label, n)
		}
	}

	// ---- 1. T-7 09:30 IST: exactly one advance_notice row per operator/park_head/phc_manager device.
	tMinus7 := dueD.AddDate(0, 0, -7).Add(30 * time.Minute) // 09:30, past the 09:00 slot
	queued := sweepAt("T-7", tMinus7)
	if queued != 3 {
		t.Fatalf("T-7 queued = %d, want 3 (operator + park_head + phc_manager)", queued)
	}
	advanceRows := queryRecipients(t, ctx, pool, rcTenant, "advance_notice")
	if len(advanceRows) != 3 {
		t.Fatalf("advance_notice rows = %d, want 3: %#v", len(advanceRows), advanceRows)
	}
	gotAdvance := map[string]bool{}
	for _, r := range advanceRows {
		gotAdvance[r.recipientRef] = true
		if r.priority != "normal" {
			t.Fatalf("advance_notice priority = %q, want normal (row=%#v)", r.priority, r)
		}
	}
	if !gotAdvance[rcOperatorToken] || !gotAdvance[rcParkHeadToken] || !gotAdvance[rcManagerToken] {
		t.Fatalf("advance_notice recipients = %#v, want operator+park_head+phc_manager", gotAdvance)
	}
	assertNoLeadershipRows("T-7")

	// ---- 2. T-3 08:00 IST: a reminder row; replaying the SAME slot is a no-op (per-offset dedup).
	tMinus3_0800 := dueD.AddDate(0, 0, -3)
	tMinus3_0800 = time.Date(tMinus3_0800.Year(), tMinus3_0800.Month(), tMinus3_0800.Day(), 8, 0, 0, 0, biztime.DefaultLocation())
	queued = sweepAt("T-3 08:00 (first)", tMinus3_0800)
	if queued != 3 {
		t.Fatalf("T-3 08:00 first sweep queued = %d, want 3", queued)
	}
	totalAfterTMinus3First := countRows(t, ctx, pool, `SELECT count(*) FROM notification_requests WHERE tenant_id = $1`, rcTenant)
	if totalAfterTMinus3First != 6 { // 3 advance_notice + 3 reminder
		t.Fatalf("total rows after T-3 08:00 first sweep = %d, want 6", totalAfterTMinus3First)
	}
	queued = sweepAt("T-3 08:00 (replay)", tMinus3_0800)
	if queued != 0 {
		t.Fatalf("T-3 08:00 replay queued = %d, want 0 (same-slot idempotency)", queued)
	}
	totalAfterReplay := countRows(t, ctx, pool, `SELECT count(*) FROM notification_requests WHERE tenant_id = $1`, rcTenant)
	if totalAfterReplay != 6 {
		t.Fatalf("total rows after T-3 08:00 replay = %d, want 6 (no duplicates)", totalAfterReplay)
	}
	assertNoLeadershipRows("T-3 08:00")

	// ---- 3. Quiet hours: a fire timed into quiet hours (22:00 on T-1) defers -- no row at 22:00.
	// The next allowed slot (07:00 on the due day, BEFORE the 08:00 due_today slot becomes due) then
	// queues that SAME still-pending reminder.
	tMinus1_2200 := dueD.AddDate(0, 0, -1)
	tMinus1_2200 = time.Date(tMinus1_2200.Year(), tMinus1_2200.Month(), tMinus1_2200.Day(), 22, 0, 0, 0, biztime.DefaultLocation())
	beforeQuiet := countRows(t, ctx, pool, `SELECT count(*) FROM notification_requests WHERE tenant_id = $1`, rcTenant)
	queued = sweepAt("T-1 22:00 (quiet hours)", tMinus1_2200)
	if queued != 0 {
		t.Fatalf("quiet-hours sweep queued = %d, want 0 (deferred)", queued)
	}
	afterQuiet := countRows(t, ctx, pool, `SELECT count(*) FROM notification_requests WHERE tenant_id = $1`, rcTenant)
	if afterQuiet != beforeQuiet {
		t.Fatalf("quiet-hours sweep changed row count %d -> %d, want unchanged (no row at 22:00)", beforeQuiet, afterQuiet)
	}

	dueDay0700 := time.Date(dueD.Year(), dueD.Month(), dueD.Day(), 7, 0, 0, 0, biztime.DefaultLocation())
	queued = sweepAt("due day 07:00 (next allowed slot)", dueDay0700)
	if queued != 3 {
		t.Fatalf("next-allowed-slot sweep queued = %d, want 3 (the deferred reminder now fires)", queued)
	}
	deferredRows := queryRecipients(t, ctx, pool, rcTenant, "reminder")
	// 3 from T-3 08:00 + 3 from the deferred T-1 17:00 slot = 6 reminder rows total.
	if len(deferredRows) != 6 {
		t.Fatalf("reminder rows after deferred fire = %d, want 6 (3 T-3 + 3 deferred T-1)", len(deferredRows))
	}
	assertNoLeadershipRows("quiet hours deferred fire")

	// ---- 4. T-0 08:00 IST: a due_today (high priority) row.
	dueDay0800 := time.Date(dueD.Year(), dueD.Month(), dueD.Day(), 8, 0, 0, 0, biztime.DefaultLocation())
	queued = sweepAt("T-0 08:00", dueDay0800)
	if queued != 3 {
		t.Fatalf("T-0 08:00 queued = %d, want 3", queued)
	}
	dueTodayRows := queryRecipients(t, ctx, pool, rcTenant, "due_today")
	if len(dueTodayRows) != 3 {
		t.Fatalf("due_today rows = %d, want 3: %#v", len(dueTodayRows), dueTodayRows)
	}
	for _, r := range dueTodayRows {
		if r.priority != "high" {
			t.Fatalf("due_today priority = %q, want high (row=%#v)", r.priority, r)
		}
	}
	assertNoLeadershipRows("T-0 08:00")

	// ---- 5. De-scheduling: mark the obligation completed, then sweep the NEXT due_today slot
	// (T-0 12:00) -- no further reminder is queued.
	exec(t, ctx, pool, "mark obligation completed",
		`UPDATE obligation_instances SET status = 'completed', completed_at = now() WHERE tenant_id = $1 AND obligation_id = $2`,
		rcTenant, primaryObligationID)
	exec(t, ctx, pool, "mark calendar projection completed",
		`UPDATE calendar_event_projections SET status = 'completed', updated_at = now() WHERE tenant_id = $1 AND event_id = $2`,
		rcTenant, primaryEventID)

	dueDay1200 := time.Date(dueD.Year(), dueD.Month(), dueD.Day(), 12, 0, 0, 0, biztime.DefaultLocation())
	queued = sweepAt("T-0 12:00 (after completion)", dueDay1200)
	if queued != 0 {
		t.Fatalf("post-completion sweep queued = %d, want 0 (de-scheduled)", queued)
	}
	totalAfterCompletion := countRows(t, ctx, pool, `SELECT count(*) FROM notification_requests WHERE tenant_id = $1`, rcTenant)
	totalBeforeCompletion := 3 /* advance_notice */ + 6 /* reminder */ + 3 /* due_today */
	if totalAfterCompletion != totalBeforeCompletion {
		t.Fatalf("total rows after completion sweep = %d, want unchanged %d (de-scheduled, no new reminder)", totalAfterCompletion, totalBeforeCompletion)
	}
	assertNoLeadershipRows("final")
}

type notificationRow struct {
	recipientRef string
	channel      string
	priority     string
}

func queryRecipients(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, notificationType string) []notificationRow {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT recipient_ref, channel, COALESCE(context->>'priority', '')
		 FROM notification_requests
		 WHERE tenant_id = $1 AND notification_type = $2
		 ORDER BY recipient_ref, notification_request_id`,
		tenantID, notificationType)
	if err != nil {
		t.Fatalf("query %s recipients: %v", notificationType, err)
	}
	defer rows.Close()
	var out []notificationRow
	for rows.Next() {
		var r notificationRow
		if err := rows.Scan(&r.recipientRef, &r.channel, &r.priority); err != nil {
			t.Fatalf("scan %s recipient row: %v", notificationType, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s recipient rows: %v", notificationType, err)
	}
	return out
}

func exec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed %s: %v", label, err)
	}
}

func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count rows (%s): %v", sql, err)
	}
	return n
}
