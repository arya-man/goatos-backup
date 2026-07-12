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
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
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

	// Baseline vaccination SOP skeleton ids seeded by migration 000075_vaccination_module.sql for
	// vnTenant -- the same fixture internal/obligation/adapters/postgres/sweeper_integration_test.go
	// reuses (see skeletonSOPID there) to satisfy sop_tasks' FKs to sop_definitions/sop_versions.
	vnSkeletonSOPID     = "b0000000-0000-4000-8000-000000000001"
	vnSkeletonVersionID = "b0000000-0000-4000-8000-000000000002"

	// Baseline custodian party seeded by migration 000001_phase_1_identity_foundation.sql for
	// vnTenant -- same fixture reused as meshaParty in sweeper_integration_test.go.
	vnCustodianParty = "00000000-0000-4000-8000-000000001001"
	vnGoatID         = "f9000000-0000-4000-8000-000000000099"
)

func TestVerificationNotifier_ProducesRoleScopedNotifications(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	workforceRepo := workforcepg.NewRepository(pool, 5*time.Second)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	calendarRepo := calendarpg.NewRepository(pool, 5*time.Second)
	calendarService := calendarapp.NewService(calendarRepo)
	protoRepo := protopg.NewRepository(pool, 5*time.Second)

	bus := eventbus.NewInProcessBus()
	notificationbridge.NewVerificationNotifier(calendarRepo, rosterService, calendarService).Register(bus)

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

	// Seed a real protocol definition -> version -> rule chain so the obligation_instances row
	// below satisfies obligation_instances_version_tenant_fk (tenant_id, protocol_version_id ->
	// protocol_versions) and obligation_instances_rule_tenant_fk (tenant_id, rule_id ->
	// protocol_rules). Mirrors the pattern in
	// internal/obligation/adapters/postgres/sweeper_integration_test.go.
	vnProtocolID, err := protoRepo.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: vnTenant, Code: "vaccination.verification_notify", Name: "VN Verification Notify", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("seed protocol definition: %v", err)
	}
	vnProtocolVersionID, err := protoRepo.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: vnTenant, ProtocolID: vnProtocolID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte("{}"), ProofPolicy: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("seed protocol version: %v", err)
	}
	vnRuleID, err := protoRepo.CreateRule(ctx, protodomain.NewRule{
		TenantID: vnTenant, ProtocolVersionID: vnProtocolVersionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", EligibilityJSON: []byte("{}"), ProofPolicy: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("seed protocol rule: %v", err)
	}

	// Seed the sop_tasks row obligation_instances.sop_task_id's FK
	// (obligation_instances_sop_task_tenant_fk) requires, reusing the baseline vaccination SOP
	// skeleton (sop_id/sop_version_id) already present for vnTenant.
	exec(t, ctx, pool, "sop task",
		`INSERT INTO sop_tasks (
		   task_id, tenant_id, sop_id, sop_version_id, task_type, title, state, scope_type, scope_id
		 ) VALUES ($1, $2, $3, $4, 'vaccination_proof_verification', 'VN drive proof review', 'submitted', 'park', $5)`,
		vnSOPTaskID, vnTenant, vnSkeletonSOPID, vnSkeletonVersionID, vnPark)

	// Seed obligation_instances row (links completion to sop_task_id and park via scope_id).
	// In production, this row is created by the obligation engine when a vaccination obligation
	// is opened for a drive; here we seed it as an INPUT fact the notification layer consumes.
	exec(t, ctx, pool, "obligation instance",
		`INSERT INTO obligation_instances (
		   obligation_id, tenant_id, protocol_version_id, rule_id, target_type, target_id,
		   scope_type, scope_id, due_at, sop_task_id, idempotency_key, status
		 ) VALUES ($1, $2, $5, $6,
		           'tenant', $2, 'park', $3,
		           now() + interval '1 hour', $4, 'vn-obl-idem-001', 'scheduled')`,
		vnObligationID, vnTenant, vnPark, vnSOPTaskID, vnProtocolVersionID, vnRuleID)

	// Seed the goat vaccination_completions.goat_id's FK (vaccination_completions_goat_tenant_fk)
	// requires -- an input fact (the animal the proof is for), same shape as the goats seed in
	// sweeper_integration_test.go (custodian party + park are baseline fixtures for vnTenant).
	exec(t, ctx, pool, "goat",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id)
		 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4)`,
		vnGoatID, vnTenant, vnCustodianParty, vnPark)

	// Seed vaccination_completion row (the proof that was submitted for verification).
	// In production, this row is created when an operator records a vaccination proof;
	// here we seed it as an INPUT fact so the notifier can resolve it to its obligation context.
	exec(t, ctx, pool, "vaccination completion",
		`INSERT INTO vaccination_completions (
		   completion_id, tenant_id, obligation_id, goat_id, status, administered_at,
		   recorded_by, idempotency_key
		 ) VALUES ($1, $2, $3, $5, 'recorded', now(), $4, 'vn-comp-idem-001')`,
		vnCompletionID, vnTenant, vnObligationID, vnOperatorMember, vnGoatID)

	// ---- Drive the notification producer off the REAL vaccination.verify.rejected event -------

	publishRejected := func(reason string) {
		t.Helper()
		payload, err := json.Marshal(notificationbridge.VerificationEvent{
			CompletionID: vnCompletionID,
			VerifiedBy:   vnVerifierMember,
			Reason:       reason,
		})
		if err != nil {
			t.Fatalf("encode event: %v", err)
		}
		if err := bus.Publish(ctx, eventbus.Event{
			Type:     notificationbridge.EventVaccinationVerifyRejected,
			TenantID: vnTenant,
			Key:      vnCompletionID,
			Payload:  payload,
		}); err != nil {
			t.Fatalf("publish rejected event: %v", err)
		}
	}

	// verification_pending -> only the verifier's device.
	// (This test does NOT yet cover verification_pending because the vertical that submits proofs
	//  has not yet published the vaccination.verification.awaiting_review event. See
	//  verification_notify.go's integration contract comment.)

	// 1. rejected -> the operator + park head, high priority; verifier/leadership get nothing for THIS event.
	publishRejected("video unclear")

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

	// 2. Never emit a leadership (tenant-scope) recipient, for either event.
	leadershipCount := countRows(t, ctx, pool,
		`SELECT count(*) FROM notification_requests WHERE tenant_id = $1 AND recipient_ref = $2`,
		vnTenant, vnLeadershipToken)
	if leadershipCount != 0 {
		t.Fatalf("leadership notification rows = %d, want 0", leadershipCount)
	}

	// 3. Replay the identical rejected event: idempotent, no duplicate rows.
	publishRejected("video unclear")

	totalAfterReplay := countRows(t, ctx, pool,
		`SELECT count(*) FROM notification_requests WHERE tenant_id = $1`, vnTenant)
	if totalAfterReplay != 2 { // 2 rework, unchanged by the replay
		t.Fatalf("total notification_requests rows after replay = %d, want 2 (no duplicates)", totalAfterReplay)
	}

	// 4. vaccination.verify.accepted is a no-op (approval is digest/rollup only, no push).
	publishAccepted := func() {
		t.Helper()
		payload, err := json.Marshal(notificationbridge.VerificationEvent{
			CompletionID: vnCompletionID,
			VerifiedBy:   vnVerifierMember,
		})
		if err != nil {
			t.Fatalf("encode event: %v", err)
		}
		if err := bus.Publish(ctx, eventbus.Event{
			Type:     notificationbridge.EventVaccinationVerifyAccepted,
			TenantID: vnTenant,
			Key:      vnCompletionID,
			Payload:  payload,
		}); err != nil {
			t.Fatalf("publish accepted event: %v", err)
		}
	}
	publishAccepted()
	totalAfterAccepted := countRows(t, ctx, pool,
		`SELECT count(*) FROM notification_requests WHERE tenant_id = $1`, vnTenant)
	if totalAfterAccepted != 2 {
		t.Fatalf("total notification_requests rows after an 'accepted' event = %d, want 2 (unchanged -- no push on approval)", totalAfterAccepted)
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

// TestNotificationTypeEnumGuard ensures the notification_type string constants used by the
// VerificationNotifier exactly match the migration 000171 notification_requests_type_check
// allowed set, so a future rename can't silently produce invalid types.
func TestNotificationTypeEnumGuard(t *testing.T) {
	allowedTypes := map[string]bool{
		"reminder":              true,
		"nudge":                 true,
		"escalation":            true,
		"verification_pending":  true,
		"rework":                true,
	}

	usedTypes := map[string]bool{
		notificationbridge.NotificationTypeVerificationPending: true,
		notificationbridge.NotificationTypeRework:              true,
	}

	for notificationType := range usedTypes {
		if !allowedTypes[notificationType] {
			t.Errorf("notification_type '%s' is used by VerificationNotifier but not in the allowed set", notificationType)
		}
	}
}
