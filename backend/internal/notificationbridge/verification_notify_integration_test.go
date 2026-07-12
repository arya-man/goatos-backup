package notificationbridge_test

// TestVerificationNotifier_ProducesRoleScopedNotifications proves the notification PUSH LAYER
// end-to-end against a real Postgres container: given an already-decided obligation verification
// status change (published as the notificationbridge.EventObligationVerificationStatus this package
// consumes -- the same contract the verification state machine, owned by a separate vertical, is
// expected to publish), the real workforce recipient-resolution queries and the real calendar
// notification-queue write together produce exactly the right notification_requests rows:
//   - verification_pending  -> only the verifier(s) holding duty_type='verify' for pc.vaccination at
//     the park
//   - rejected/rework_due   -> only the executing operator + that park's park head, at high priority
//   - any other status      -> no row (approval is rollup/digest only)
//   - leadership (tenant-scope position) NEVER receives a row, for either event
//   - replaying the identical event is a no-op (idempotent, no duplicate rows)
//
// This test deliberately does NOT build or drive the verification state machine (that vertical is
// owned elsewhere): it seeds only INPUT facts (tenant/park, workforce members, positions + the verify
// duty, devices + FCM tokens, and the calendar_event_projections row the notification links to -- the
// FK this table enforces) and then publishes the status-changed event directly, exactly as the
// coordinator's notification-layer scope requires. It lives under internal/notificationbridge (an
// integration test, not tests/e2e) because it seeds calendar_event_projections directly, which
// tools/agent-hooks/check-e2e-kernel-integrity.sh rightly blocks inside tests/e2e for any test that
// claims to prove the DERIVED business state of that table -- this test claims only the notification
// layer built on top of it.
import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// Baseline fixture ids already present in every migrated database (migration
// 000001_phase_1_identity_foundation.sql), the same ones the rest of the backend's integration/E2E
// tests reuse (see tests/e2e/harness_test.go's fxTenant/fxPark doc comment).
const (
	vnTenant = "00000000-0000-4000-8000-000000000001"
	vnPark   = "00000000-0000-4000-8000-000000003001"
)

const (
	vnOperatorMember   = "f9000000-0000-4000-8000-000000000001"
	vnParkHeadMember   = "f9000000-0000-4000-8000-000000000002"
	vnVerifierMember   = "f9000000-0000-4000-8000-000000000003"
	vnLeadershipMember = "f9000000-0000-4000-8000-000000000004"

	vnOperatorDevice   = "f9000000-0000-4000-8000-000000000011"
	vnParkHeadDevice   = "f9000000-0000-4000-8000-000000000012"
	vnVerifierDevice   = "f9000000-0000-4000-8000-000000000013"
	vnLeadershipDevice = "f9000000-0000-4000-8000-000000000014"

	vnOperatorToken   = "fcm-token-operator-f9-0001"
	vnParkHeadToken   = "fcm-token-parkhead-f9-0002"
	vnVerifierToken   = "fcm-token-verifier-f9-0003"
	vnLeadershipToken = "fcm-token-leadership-f9-0004"

	vnObligationID = "f9000000-0000-4000-8000-000000000021"
	vnSOPTaskID    = "f9000000-0000-4000-8000-000000000022"
	vnCompletionID = "f9000000-0000-4000-8000-000000000031"

	vnVerifierPositionCode = "preventive_care_verifier"
	vnLeadershipPosition   = "pc_director"
)

func TestVerificationNotifier_ProducesRoleScopedNotifications(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	workforceRepo := workforcepg.NewRepository(pool, 5*time.Second)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	calendarService := calendarapp.NewService(calendarpg.NewRepository(pool, 5*time.Second))

	bus := eventbus.NewInProcessBus()
	notificationbridge.NewVerificationNotifier(rosterService, calendarService).Register(bus)

	// ---- Seed INPUT facts only ------------------------------------------------------------------

	seedMember := func(id, code, name, roleHint string) {
		exec(t, ctx, pool, "workforce member "+code,
			`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
			 VALUES ($1, $2, $3, $4, 'active', $5)`,
			id, vnTenant, code, name, roleHint)
	}
	seedMember(vnOperatorMember, "VN-OP", "VN Operator", "operator")
	seedMember(vnParkHeadMember, "VN-PH", "VN Park Head", "park_head")
	seedMember(vnVerifierMember, "VN-VER", "VN Verifier", "verifier")
	seedMember(vnLeadershipMember, "VN-LEAD", "VN PC Director", "other")

	seedPosition := func(memberID, positionCode, scopeType, scopeID, tier string) {
		exec(t, ctx, pool, "workforce position "+positionCode,
			`INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, status, valid_from)
			 VALUES ($1, $2, $3, $4, $5, $6, 'active', now() - interval '1 hour')`,
			vnTenant, memberID, scopeType, scopeID, positionCode, tier)
	}
	// The verifier duty is NOT derived by cmd/seed-position-duties today (it only derives
	// execute/manage from position tier) -- a duty_type='verify' seat is genuine input config this
	// test authors directly, exactly the "position + verify duty" input fact the notification-layer
	// scope calls for.
	seedPosition(vnVerifierMember, vnVerifierPositionCode, "center", vnPark, "manager")
	seedPosition(vnParkHeadMember, "park_head", "center", vnPark, "head")
	// Leadership sits at tenant scope -- structurally outside every recipient-resolution query this
	// bridge runs (all hardcoded to scope_type='center'), proving the "never bombard leadership" rule
	// at the SQL level, not just by omission.
	seedPosition(vnLeadershipMember, vnLeadershipPosition, "tenant", vnTenant, "director")

	exec(t, ctx, pool, "verify duty",
		`INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, status, effective_from)
		 VALUES ($1, $2, 'pc.vaccination', 'verify', 'active', now() - interval '1 hour')`,
		vnTenant, vnVerifierPositionCode)

	seedDevice := func(id, memberID, appInstallID, fcmToken string) {
		exec(t, ctx, pool, "device "+appInstallID,
			`INSERT INTO workforce_member_devices (device_id, tenant_id, workforce_member_id, platform, app_install_id, fcm_token, app_version, os_version, status, last_seen_at, registered_by)
			 VALUES ($1, $2, $3, 'android', $4, $5, '1.0.0', '14', 'active', now(), $3)`,
			id, vnTenant, memberID, appInstallID, fcmToken)
	}
	seedDevice(vnOperatorDevice, vnOperatorMember, "vn-install-operator", vnOperatorToken)
	seedDevice(vnParkHeadDevice, vnParkHeadMember, "vn-install-parkhead", vnParkHeadToken)
	seedDevice(vnVerifierDevice, vnVerifierMember, "vn-install-verifier", vnVerifierToken)
	seedDevice(vnLeadershipDevice, vnLeadershipMember, "vn-install-leadership", vnLeadershipToken)

	// The calendar_event_projections row this notification links to (notification_requests.
	// calendar_event_id has a hard FK to it). In production this row is materialized by the real
	// calendar vaccination projector (cmd/calendar-vaccination-projector / the obligation sweeper)
	// right after the SOP task is created -- seeding it directly here stands in for that already-real
	// upstream projector, which is out of this notification-layer test's scope to re-drive.
	exec(t, ctx, pool, "calendar event projection",
		`INSERT INTO calendar_event_projections (
		   tenant_id, event_id, slice_key, event_type, owner_key, title, status, due_at,
		   target_type, target_count, park_id, executor_role
		 ) VALUES ($1, $2, 'vaccination', 'vaccination_proof_verification', 'pc', 'VN drive proof review',
		           'verification_pending', now(), 'obligation', 1, $3, 'operator')`,
		vnTenant, "calendar:"+vnSOPTaskID, vnPark)

	// ---- Drive the notification producer off the published status-changed event ------------------

	publish := func(status, reason string) {
		t.Helper()
		payload, err := json.Marshal(notificationbridge.ObligationVerificationStatusEvent{
			ObligationID: vnObligationID,
			CompletionID: vnCompletionID,
			Status:       status,
			ParkID:       vnPark,
			SOPTaskID:    vnSOPTaskID,
			ExecutedBy:   vnOperatorMember,
			Reason:       reason,
		})
		if err != nil {
			t.Fatalf("encode event: %v", err)
		}
		if err := bus.Publish(ctx, eventbus.Event{
			Type:     notificationbridge.EventObligationVerificationStatus,
			TenantID: vnTenant,
			Key:      vnObligationID,
			Payload:  payload,
		}); err != nil {
			t.Fatalf("publish %s event: %v", status, err)
		}
	}

	// 1. verification_pending -> only the verifier's device.
	publish(notificationbridge.StatusVerificationPending, "")

	pendingRows := queryRecipients(t, ctx, pool, vnTenant, "verification_pending")
	if len(pendingRows) != 1 {
		t.Fatalf("verification_pending rows = %d, want 1: %#v", len(pendingRows), pendingRows)
	}
	if pendingRows[0].recipientRef != vnVerifierToken {
		t.Fatalf("verification_pending recipient = %q, want verifier token %q", pendingRows[0].recipientRef, vnVerifierToken)
	}
	if pendingRows[0].channel != "push_fcm" {
		t.Fatalf("verification_pending channel = %q, want push_fcm", pendingRows[0].channel)
	}

	// 2. rejected -> the operator + park head, high priority; verifier/leadership get nothing for
	// THIS event.
	publish(notificationbridge.StatusRejected, "video unclear")

	reworkRows := queryRecipients(t, ctx, pool, vnTenant, "rework")
	if len(reworkRows) != 2 {
		t.Fatalf("rework rows = %d, want 2: %#v", len(reworkRows), reworkRows)
	}
	gotRework := map[string]bool{}
	for _, r := range reworkRows {
		gotRework[r.recipientRef] = true
		if r.channel != "push_fcm" {
			t.Fatalf("rework channel = %q, want push_fcm", r.channel)
		}
		if r.priority != "high" {
			t.Fatalf("rework priority = %q, want high (row=%#v)", r.priority, r)
		}
	}
	if !gotRework[vnOperatorToken] || !gotRework[vnParkHeadToken] {
		t.Fatalf("rework recipients = %#v, want operator %q + park head %q", gotRework, vnOperatorToken, vnParkHeadToken)
	}

	// 3. Never emit a leadership (tenant-scope) recipient, for either event.
	leadershipCount := countRows(t, ctx, pool,
		`SELECT count(*) FROM notification_requests WHERE tenant_id = $1 AND recipient_ref = $2`,
		vnTenant, vnLeadershipToken)
	if leadershipCount != 0 {
		t.Fatalf("leadership notification rows = %d, want 0", leadershipCount)
	}

	// 4. Replay the identical rejected event: idempotent, no duplicate rows.
	publish(notificationbridge.StatusRejected, "video unclear")

	totalAfterReplay := countRows(t, ctx, pool,
		`SELECT count(*) FROM notification_requests WHERE tenant_id = $1`, vnTenant)
	if totalAfterReplay != 3 { // 1 verification_pending + 2 rework, unchanged by the replay
		t.Fatalf("total notification_requests rows after replay = %d, want 3 (no duplicates)", totalAfterReplay)
	}

	// 5. A status this bridge doesn't act on (e.g. approval) produces no row.
	publish("completed", "")
	totalAfterCompleted := countRows(t, ctx, pool,
		`SELECT count(*) FROM notification_requests WHERE tenant_id = $1`, vnTenant)
	if totalAfterCompleted != 3 {
		t.Fatalf("total notification_requests rows after a 'completed' event = %d, want 3 (unchanged -- no push on approval)", totalAfterCompleted)
	}
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
		 ORDER BY recipient_ref`,
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
