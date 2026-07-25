package kernelstages

import (
	"context"
	"log/slog"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

// Baseline fixture ids seeded by migration 000001_phase_1_identity_foundation.sql for every migrated
// DB (same ones the calendar/notificationbridge integration tests reuse): the tenant, a real
// location_type='park' park, and the custodian party goats hang off.
const (
	rcTenant    = "00000000-0000-4000-8000-000000000001"
	rcPark      = "00000000-0000-4000-8000-000000003001"
	rcCustodian = "00000000-0000-4000-8000-000000001001"

	// Workforce roster seats for the park (the operational reminder audience,
	// vaccination-notification-rules.md §4a).
	rcOperatorMember = "e1000000-0000-4000-8000-000000000001"
	rcParkHeadMember = "e1000000-0000-4000-8000-000000000002"
	rcManagerMember  = "e1000000-0000-4000-8000-000000000003"
	rcDirectorMember = "e1000000-0000-4000-8000-000000000004"
	rcCEOMember      = "e1000000-0000-4000-8000-000000000005"

	rcOperatorDevice = "e1000000-0000-4000-8000-000000000011"
	rcParkHeadDevice = "e1000000-0000-4000-8000-000000000012"
	rcManagerDevice  = "e1000000-0000-4000-8000-000000000013"
	rcDirectorDevice = "e1000000-0000-4000-8000-000000000014"
	rcCEODevice      = "e1000000-0000-4000-8000-000000000015"

	rcOperatorToken = "fcm-reminder-operator-e1-0001"
	rcParkHeadToken = "fcm-reminder-parkhead-e1-0002"
	rcManagerToken  = "fcm-reminder-manager-e1-0003"
	rcDirectorToken = "fcm-reminder-director-e1-0004"
	rcCEOToken      = "fcm-reminder-ceo-e1-0005"

	rcProtocolID = "e1000000-0000-4000-8000-000000000101"
	rcVersionID  = "e1000000-0000-4000-8000-000000000102"
	rcRuleID     = "e1000000-0000-4000-8000-000000000103"
)

// TestReminderCadenceStageRunsAgainstRealPostgres exercises the reminder cadence stage against a
// migration-complete Postgres. On empty tables it must run cleanly (0 notification rows, no error),
// proving the stage reuses the real schema/queries and satisfies the StageRunner contract end to end.
func TestReminderCadenceStageRunsAgainstRealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := Deps{Pool: pool, PgCfg: platformpg.Config{QueryTimeout: 5 * time.Second}, Logger: logger}

	stage := NewReminderCadenceStage(deps, rcTenant)
	if got := stage.Name(); got != "reminder-cadence" {
		t.Fatalf("unexpected stage name: %q", got)
	}
	if err := stage.Run(ctx); err != nil {
		t.Fatalf("reminder cadence stage failed against real Postgres: %v", err)
	}
}

// TestReminderCadenceStageQueuesNotificationsAtEachLadderSlot is the CR-001 GUARD. It boots the REAL
// production ReminderCadenceStage (the stage wired into cmd/kernel-worker), NOT a direct repository
// call, against a pgtest DB seeded with only INPUT facts:
//
//   - a park + workforce roster: operator, park head, PHC manager, PC director, and CEO, each with
//     an active device carrying an FCM token — so ResolvePositionRecipientsBatch actually returns
//     recipients;
//   - three unbatched, scheduled vaccination obligations in that park, due at D0 (today), D+3, and
//     D+7 relative to biztime.BusinessDayStart(time.Now()) — anchored to the business day, NO fixed
//     calendar dates (india-date-guard / time-bomb safe).
//
// The stage is evaluated at a pinned evening instant (19:45 IST today, after every ladder slot and
// before quiet hours) so each obligation lands exactly one ladder rung ON today's fire day:
//   - the D+7 obligation -> advance_notice (D-7 rung)
//   - the D+3 obligation -> reminder      (D-6..D-1 rung)
//   - the D0 obligation  -> due_today     (D-0 rung)
//
// It then asserts real notification_requests rows EXIST for EVERY ladder slot (advance_notice,
// reminder, due_today), addressed to the seeded recipients' FCM tokens, with the fire type carried in
// context. A second run must NOT duplicate them (idempotency via the fire-marker + per-device
// idempotency key). If any slot produces no row, the test fails — proving each rung genuinely fires.
func TestReminderCadenceStageQueuesNotificationsAtEachLadderSlot(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := Deps{Pool: pool, PgCfg: platformpg.Config{QueryTimeout: 10 * time.Second}, Logger: logger}

	// ---- Anchor everything to the business day, never a fixed date. -----------------------------
	dayStart := biztime.BusinessDayStart(time.Now())          // midnight IST today
	evalNow := dayStart.Add(19*time.Hour + 45*time.Minute)    // 19:45 IST today (after all slots, before quiet hours)
	dueToday := dayStart.Add(10 * time.Hour)                  // today 10:00 IST  -> due_today rung fires today
	duePlus3 := dayStart.AddDate(0, 0, 3).Add(10 * time.Hour) // today+3          -> reminder rung fires today
	duePlus7 := dayStart.AddDate(0, 0, 7).Add(10 * time.Hour) // today+7          -> advance_notice rung fires today

	// ---- Seed INPUT facts only. -----------------------------------------------------------------
	seedReminderRoster(t, ctx, pool, evalNow)
	seedReminderProtocol(t, ctx, pool)
	seedReminderObligation(t, ctx, pool, "e1000000-0000-4000-9000-000000000001", "rc-obl-today", dueToday)
	seedReminderObligation(t, ctx, pool, "e1000000-0000-4000-9000-000000000002", "rc-obl-plus3", duePlus3)
	seedReminderObligation(t, ctx, pool, "e1000000-0000-4000-9000-000000000003", "rc-obl-plus7", duePlus7)

	// ---- Boot the REAL stage with the pinned clock. ---------------------------------------------
	stage := NewReminderCadenceStage(deps, rcTenant).withClock(func() time.Time { return evalNow })
	if err := stage.Run(ctx); err != nil {
		t.Fatalf("first stage run failed: %v", err)
	}

	// ---- Assert: a notification_requests row per ladder slot, to the intended hierarchy. --------
	fieldTokens := []string{rcOperatorToken, rcParkHeadToken, rcManagerToken}
	leadershipExceptionTokens := []string{rcOperatorToken, rcParkHeadToken, rcManagerToken, rcDirectorToken, rcCEOToken}
	for _, slot := range []string{"advance_notice", "reminder"} {
		tokens := reminderRecipientTokensForType(t, ctx, pool, slot)
		if len(tokens) == 0 {
			t.Fatalf("ladder slot %q produced NO notification_requests rows — the rung does not fire", slot)
		}
		if !sameStringSet(tokens, fieldTokens) {
			t.Fatalf("ladder slot %q recipients = %v, want field roster %v", slot, tokens, fieldTokens)
		}
		t.Logf("LADDER SLOT %-14s -> %d notification_requests rows, recipients=%v", slot, len(tokens), tokens)
	}
	dueTodayTokens := reminderRecipientTokensForType(t, ctx, pool, "due_today")
	if len(dueTodayTokens) == 0 {
		t.Fatalf("7:30 due_today slot produced NO notification_requests rows")
	}
	if !sameStringSet(dueTodayTokens, leadershipExceptionTokens) {
		t.Fatalf("7:30 due_today recipients = %v, want field + leadership %v", dueTodayTokens, leadershipExceptionTokens)
	}
	if title, body := notificationTitleBodyForType(t, ctx, pool, "due_today"); title != "Vaccination EOD exception" ||
		body != "1 scheduled vaccination shed(s) still not submitted by 7:30 PM" {
		t.Fatalf("7:30 due_today title/body = %q/%q, want EOD exception copy", title, body)
	}

	firstTotal := reminderNotificationCount(t, ctx, pool)
	if firstTotal != 11 { // 2 routine slots x 3 field devices + 1 EOD slot x 5 field/leadership devices
		t.Fatalf("first run total notification_requests = %d, want 11", firstTotal)
	}
	t.Logf("FIRST RUN total notification_requests = %d", firstTotal)

	// ---- Assert idempotency: a second run at the same instant must NOT duplicate. ---------------
	if err := stage.Run(ctx); err != nil {
		t.Fatalf("second stage run failed: %v", err)
	}
	secondTotal := reminderNotificationCount(t, ctx, pool)
	if secondTotal != firstTotal {
		t.Fatalf("second run DUPLICATED notifications: %d -> %d (idempotency broken)", firstTotal, secondTotal)
	}
	t.Logf("SECOND RUN total notification_requests = %d (no duplicates — idempotent)", secondTotal)
}

// TestReminderCadenceStageSkipsInProgressVaccinationSheds is the operator-scanning guard. Daily
// reminders are for scheduled/open vaccination sheds; once an operator starts scanning and the drive
// is in_progress, reminder cadence must stop for that shed. Submission/review notifications are
// handled by separate event-triggered paths.
func TestReminderCadenceStageSkipsInProgressVaccinationSheds(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := Deps{Pool: pool, PgCfg: platformpg.Config{QueryTimeout: 10 * time.Second}, Logger: logger}

	dayStart := biztime.BusinessDayStart(time.Now())
	evalNow := dayStart.Add(13*time.Hour + 30*time.Minute) // due_today 13:00 would be eligible
	dueToday := dayStart.Add(10 * time.Hour)

	seedReminderRoster(t, ctx, pool, evalNow)
	seedReminderProtocol(t, ctx, pool)
	seedReminderObligation(t, ctx, pool, "e1000000-0000-4000-9000-000000000011", "rc-obl-in-progress", dueToday)
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET status = 'in_progress', updated_at = $3::timestamptz
WHERE tenant_id = $1::uuid AND idempotency_key = $2`,
		rcTenant, "rc-obl-in-progress", evalNow); err != nil {
		t.Fatalf("mark obligation in_progress: %v", err)
	}

	stage := NewReminderCadenceStage(deps, rcTenant).withClock(func() time.Time { return evalNow })
	if err := stage.Run(ctx); err != nil {
		t.Fatalf("stage run failed: %v", err)
	}
	if got := reminderNotificationCount(t, ctx, pool); got != 0 {
		t.Fatalf("in_progress vaccination shed queued %d reminder notification_requests, want 0", got)
	}
}

// seedReminderRoster seeds the reminder audience: park-scoped operator, park head, PHC manager,
// plus tenant-scoped PC director and CEO, each with an active FCM device. Position validity is
// anchored to activeAt so pinned-clock tests never depend on database wall time.
func seedReminderRoster(t *testing.T, ctx context.Context, pool *pgxpool.Pool, activeAt time.Time) {
	t.Helper()
	member := func(id, code, name, hint string) {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
VALUES ($1::uuid, $2::uuid, $3, $4, 'active', $5)`,
			id, rcTenant, code, name, hint); err != nil {
			t.Fatalf("seed member %s: %v", code, err)
		}
	}
	member(rcOperatorMember, "RC-OP", "RC Operator", "operator")
	member(rcParkHeadMember, "RC-PH", "RC Park Head", "park_head")
	member(rcManagerMember, "RC-MGR", "RC PHC Manager", "supervisor")
	member(rcDirectorMember, "RC-DIR", "RC PC Director", "other")
	member(rcCEOMember, "RC-CEO", "RC CEO", "other")

	position := func(memberID, positionCode, tier string) {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'center', $3::uuid, $4, $5, 'active', $6::timestamptz)`,
			rcTenant, memberID, rcPark, positionCode, tier, activeAt.Add(-time.Hour)); err != nil {
			t.Fatalf("seed position %s: %v", positionCode, err)
		}
	}
	position(rcOperatorMember, "operator", "assistant")
	position(rcParkHeadMember, "park_head", "head")
	position(rcManagerMember, "phc_manager", "manager")
	tenantPosition := func(memberID, positionCode, tier string) {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'tenant', $1::uuid, $3, $4, 'active', $5::timestamptz)`,
			rcTenant, memberID, positionCode, tier, activeAt.Add(-time.Hour)); err != nil {
			t.Fatalf("seed tenant position %s: %v", positionCode, err)
		}
	}
	tenantPosition(rcDirectorMember, "pc_director", "director")
	tenantPosition(rcCEOMember, "ceo_internal", "cxo")

	device := func(id, memberID, appInstall, token string) {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_member_devices (device_id, tenant_id, workforce_member_id, platform, app_install_id, fcm_token, app_version, os_version, status, last_seen_at, registered_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'android', $4, $5, '1.0.0', '14', 'active', now(), $3::uuid)`,
			id, rcTenant, memberID, appInstall, token); err != nil {
			t.Fatalf("seed device %s: %v", appInstall, err)
		}
	}
	device(rcOperatorDevice, rcOperatorMember, "rc-install-operator", rcOperatorToken)
	device(rcParkHeadDevice, rcParkHeadMember, "rc-install-parkhead", rcParkHeadToken)
	device(rcManagerDevice, rcManagerMember, "rc-install-manager", rcManagerToken)
	device(rcDirectorDevice, rcDirectorMember, "rc-install-director", rcDirectorToken)
	device(rcCEODevice, rcCEOMember, "rc-install-ceo", rcCEOToken)
}

// seedReminderProtocol seeds a published vaccination protocol definition -> version -> rule so the
// canonical calendar read (which the reminder-cadence candidate query wraps) resolves the obligations
// as drive events (pd.category='vaccination' AND pv.status='published').
func seedReminderProtocol(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
VALUES ($1::uuid, $2::uuid, 'vaccination.reminder_cadence_guard', 'RC Guard Vaccine', 'vaccination', 'active')`,
		rcProtocolID, rcTenant); err != nil {
		t.Fatalf("seed protocol definition: %v", err)
	}
	// Insert the version as draft, add the rule (published config is immutable — the rule INSERT is
	// rejected by a trigger once the version is published), then publish.
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_versions (
  protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
  version_label, status, effective_from, effective_to, rule_dsl, proof_policy, published_at
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'tenant', NULL, 1,
  'RC guard draft', 'draft', DATE '2026-01-01', DATE '2028-01-01',
  '{}'::jsonb, '{"required_proofs":["administration"]}'::jsonb, NULL
)`,
		rcVersionID, rcTenant, rcProtocolID); err != nil {
		t.Fatalf("seed protocol version: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
  offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json, proof_policy, sort_order
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'RC-GUARD', 1, 'calendar',
  0, 1, 0, 'none', 'immediate', '{}'::jsonb, '{"required_proofs":["administration"]}'::jsonb, 10
)`,
		rcRuleID, rcTenant, rcVersionID); err != nil {
		t.Fatalf("seed protocol rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE protocol_versions SET status = 'published', published_at = COALESCE(published_at, now())
WHERE tenant_id = $1::uuid AND protocol_version_id = $2::uuid`,
		rcTenant, rcVersionID); err != nil {
		t.Fatalf("publish protocol version: %v", err)
	}
}

// seedReminderObligation seeds one unbatched, scheduled, park-scoped vaccination obligation (plus the
// goat its target_id references) due at dueAt.
func seedReminderObligation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, idemKey string, dueAt time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id)
VALUES ($1::uuid, $2::uuid, 'alive', 'goat', $3::uuid, 'female', $4::uuid, $4::uuid)`,
		goatID, rcTenant, rcCustodian, rcPark); err != nil {
		t.Fatalf("seed goat %s: %v", goatID, err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO obligation_instances (
  obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id,
  scope_type, scope_id, due_at, status, idempotency_key
) VALUES (
  gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid, 'goat', $4::uuid,
  'park', $5::uuid, $6::timestamptz, 'scheduled', $7
)`,
		rcTenant, rcVersionID, rcRuleID, goatID, rcPark, dueAt, idemKey); err != nil {
		t.Fatalf("seed obligation %s: %v", idemKey, err)
	}
}

// reminderRecipientTokensForType returns the recipient_ref (FCM token) of every notification_requests
// row of the given notification_type, asserting each row's context carries the matching fire_type.
func reminderRecipientTokensForType(t *testing.T, ctx context.Context, pool *pgxpool.Pool, notifType string) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT recipient_ref, COALESCE(context->>'fire_type', '')
FROM notification_requests
WHERE tenant_id = $1::uuid AND notification_type = $2
ORDER BY recipient_ref`,
		rcTenant, notifType)
	if err != nil {
		t.Fatalf("query notifications for %s: %v", notifType, err)
	}
	defer rows.Close()
	var tokens []string
	for rows.Next() {
		var token, fireType string
		if err := rows.Scan(&token, &fireType); err != nil {
			t.Fatalf("scan notification row: %v", err)
		}
		if fireType != notifType {
			t.Fatalf("notification_type %q row carries context.fire_type=%q (mismatch)", notifType, fireType)
		}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate notifications for %s: %v", notifType, err)
	}
	return tokens
}

// reminderNotificationCount counts all notification_requests rows for the tenant.
func reminderNotificationCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM notification_requests WHERE tenant_id = $1::uuid`, rcTenant).Scan(&count); err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	return count
}

func notificationTitleBodyForType(t *testing.T, ctx context.Context, pool *pgxpool.Pool, notifType string) (string, string) {
	t.Helper()
	var title, body string
	if err := pool.QueryRow(ctx, `
SELECT title, body
FROM notification_requests
WHERE tenant_id = $1::uuid AND notification_type = $2
ORDER BY notification_request_id
LIMIT 1`,
		rcTenant, notifType).Scan(&title, &body); err != nil {
		t.Fatalf("query title/body for %s: %v", notifType, err)
	}
	return title, body
}

func sameStringSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}
