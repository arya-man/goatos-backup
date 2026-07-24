package notificationbridge_test

// Real-Postgres tests for the durable VerificationEventConsumer (verification_notify_consumer.go).
// They seed only INPUT facts (workforce members/positions/verify-duty/devices) and then drive
// consumer.HandleEvent with the exact verification.* outbox payload shape the verification
// repository emits, asserting the produced notification_requests rows. They reuse the package-level
// fixture ids (vnTenant/vnPark/vn*Member/vn*Device/vn*Token/vnVerifierPositionCode) and helpers
// (exec/countRows) defined in verification_notify_integration_test.go.
//
// The four required behaviors, each of which was confirmed to FAIL when its production behavior is
// broken (verified by temporarily inverting the consumer, then restoring):
//   - TestVerificationEventConsumer_Idempotent      : same event twice -> exactly ONE row.
//   - TestVerificationEventConsumer_ReplayAndDLQ    : retried transient failure -> no duplicate;
//                                                     poison payload -> eventbus.PermanentError (DLQ).
//   - TestVerificationEventConsumer_RecipientResolution : pending->verifier + park head + PC
//                                                     director + CEO; rework->operator + park head;
//                                                     approved->park head; closed->operator.
//   - TestVerificationEventConsumer_LegacyDedup     : legacy vaccination SOP item rework -> NO row.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// Distinct verification_item ids (the consumer targets these; the sibling test uses completion ids).
const (
	vecItemPending  = "fa000000-0000-4000-8000-0000000000a1"
	vecItemRework   = "fa000000-0000-4000-8000-0000000000a2"
	vecItemApproved = "fa000000-0000-4000-8000-0000000000a3"
	vecItemLegacy   = "fa000000-0000-4000-8000-0000000000a4"
	vecItemIdem     = "fa000000-0000-4000-8000-0000000000a5"
	vecItemReplay   = "fa000000-0000-4000-8000-0000000000a6"
	vecItemClosed   = "fa000000-0000-4000-8000-0000000000a7"
	vecOperatorUser = "fa000000-0000-4000-8000-0000000000b1"
)

// vecSetup stands up a fresh Postgres, the roster + calendar services, the consumer, and seeds the
// recipient input facts (verifier verify-duty, park-head position, operator/park-head/verifier
// devices). Returns the pool and the wired consumer.
func vecSetup(t *testing.T) (*pgxpool.Pool, *notificationbridge.VerificationEventConsumer) {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	t.Cleanup(pool.Close)

	workforceRepo := workforcepg.NewRepository(pool, 5*time.Second)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	calendarRepo := calendarpg.NewRepository(pool, 5*time.Second)
	calendarService := calendarapp.NewService(calendarRepo)
	consumer := notificationbridge.NewVerificationEventConsumer(rosterService, calendarService, slog.Default())

	// Workforce members.
	seedMember := func(id, code, name, hint string) {
		exec(t, ctx, pool, "member "+code,
			`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint)
			 VALUES ($1, $2, $3, $4, 'active', $5)`, id, vnTenant, code, name, hint)
	}
	seedMember(vnOperatorMember, "VEC-OP", "VEC Operator", "operator")
	seedMember(vnParkHeadMember, "VEC-PH", "VEC Park Head", "park_head")
	seedMember(vnVerifierMember, "VEC-VER", "VEC Verifier", "verifier")
	seedMember(vnLeadershipMember, "VEC-PCD", "VEC PC Director", "other")
	seedMember(vnCEOMember, "VEC-CEO", "VEC CEO", "other")
	exec(t, ctx, pool, "operator auth identity",
		`UPDATE workforce_members SET user_id = $1 WHERE tenant_id = $2 AND workforce_member_id = $3`,
		vecOperatorUser, vnTenant, vnOperatorMember)

	// Positions: verifier (center/park, manager) + park head (center/park, head).
	seedPosition := func(memberID, positionCode, tier string) {
		exec(t, ctx, pool, "position "+positionCode,
			`INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, status, valid_from)
			 VALUES ($1, $2, 'center', $3, $4, $5, 'active', now() - interval '1 hour')`,
			vnTenant, memberID, vnPark, positionCode, tier)
	}
	seedPosition(vnVerifierMember, vnVerifierPositionCode, "manager")
	seedPosition(vnParkHeadMember, "park_head", "head")
	seedTenantPosition := func(memberID, positionCode, tier string) {
		exec(t, ctx, pool, "tenant position "+positionCode,
			`INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, status, valid_from)
			 VALUES ($1, $2, 'tenant', $1, $3, $4, 'active', now() - interval '1 hour')`,
			vnTenant, memberID, positionCode, tier)
	}
	seedTenantPosition(vnLeadershipMember, "pc_director", "director")
	seedTenantPosition(vnCEOMember, "ceo_internal", "cxo")

	// Verify duty for the verifier position under pc.vaccination.
	exec(t, ctx, pool, "verify duty",
		`INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, status, effective_from)
		 VALUES ($1, $2, 'pc.vaccination', 'verify', 'active', now() - interval '1 hour')`,
		vnTenant, vnVerifierPositionCode)

	// Devices + FCM tokens (the delivery address recipient resolution returns).
	seedDevice := func(id, memberID, install, token string) {
		exec(t, ctx, pool, "device "+install,
			`INSERT INTO workforce_member_devices (device_id, tenant_id, workforce_member_id, platform, app_install_id, fcm_token, app_version, os_version, status, last_seen_at, registered_by)
			 VALUES ($1, $2, $3, 'android', $4, $5, '1.0.0', '14', 'active', now(), $3)`,
			id, vnTenant, memberID, install, token)
	}
	seedDevice(vnOperatorDevice, vnOperatorMember, "vec-op", vnOperatorToken)
	seedDevice(vnParkHeadDevice, vnParkHeadMember, "vec-ph", vnParkHeadToken)
	seedDevice(vnVerifierDevice, vnVerifierMember, "vec-ver", vnVerifierToken)
	seedDevice(vnLeadershipDevice, vnLeadershipMember, "vec-pcd", vnLeadershipToken)
	seedDevice(vnCEODevice, vnCEOMember, "vec-ceo", vnCEOToken)

	// notification_requests.calendar_event_id no longer has an FK to satisfy (calendar_event_projections
	// is retired; the FK was already dropped in migration 000186), so there is no projection fixture to
	// seed here anymore.
	return pool, consumer
}

// vecPayload builds the verification.* outbox payload shape the verification repository emits
// (verificationVerdictPayload / verificationItemPendingPayload), with an optional legacy source.
func vecPayload(itemID, operatorID, decision, reason string, legacy bool) []byte {
	source := map[string]any{"module": "generic", "ref_type": "capture", "ref_id": itemID}
	if legacy {
		source = map[string]any{
			"module": "vaccination", "ref_type": "sop_submission",
			"ref_id": itemID, "task_id": "fa000000-0000-4000-8000-0000000000f1",
			"submission_id": itemID,
		}
	}
	m := map[string]any{
		"tenant_id": vnTenant, "item_id": itemID, "vertical": "preventive_care",
		"module": "vaccination", "category": "vaccination_proof",
		"operator_id": operatorID, "park_id": vnPark, "source": source,
	}
	if decision != "" {
		m["decision"] = decision
		m["status"] = decision
	}
	if reason != "" {
		m["reason"] = reason
	}
	b, _ := json.Marshal(m)
	return b
}

func vecEvent(eventType, itemID string, payload []byte) eventbus.Event {
	return eventbus.Event{Type: eventType, TenantID: vnTenant, Key: itemID, Payload: payload}
}

// TestVerificationEventConsumer_Idempotent: the SAME pending event processed twice produces exactly
// one notification_request per required pending recipient (ON CONFLICT DO NOTHING on the device
// idempotency key).
func TestVerificationEventConsumer_Idempotent(t *testing.T) {
	ctx := context.Background()
	pool, consumer := vecSetup(t)

	ev := vecEvent(notificationbridge.EventVerificationItemPending, vecItemIdem,
		vecPayload(vecItemIdem, vnOperatorMember, "", "", false))

	if err := consumer.HandleEvent(ctx, ev); err != nil {
		t.Fatalf("first handle: %v", err)
	}
	if err := consumer.HandleEvent(ctx, ev); err != nil { // exact replay
		t.Fatalf("replay handle: %v", err)
	}

	got := countRows(t, ctx, pool,
		`SELECT count(*) FROM notification_requests WHERE tenant_id = $1 AND target_id = $2`,
		vnTenant, vecItemIdem)
	if got != 4 {
		t.Fatalf("notification_requests after duplicate processing = %d, want 4 (idempotent)", got)
	}
}

// TestVerificationEventConsumer_ReplayAndDLQ covers both durable-delivery failure modes:
//   - replay: a handler whose queue write COMMITTED but then returned a transient error is retried;
//     the retry must NOT create a second row (idempotency makes replay side-effect-free).
//   - DLQ: a poison (unparseable) payload is returned as eventbus.PermanentError so the domain-event
//     consumer nacks it straight to the DLQ (IsPermanentError -> nacked_for_dlq) instead of looping.
func TestVerificationEventConsumer_ReplayAndDLQ(t *testing.T) {
	ctx := context.Background()
	pool, consumer := vecSetup(t)

	// --- replay leg: first attempt commits the row, then fails transiently; retry must not dup. ---
	realCalendar := calendarapp.NewService(calendarpg.NewRepository(pool, 5*time.Second))
	failing := &failOnceQueue{inner: realCalendar}
	roster := workforceapp.NewRosterService(
		workforcepg.NewRepository(pool, 5*time.Second),
		workforcepg.NewRepository(pool, 5*time.Second),
	)
	replayConsumer := notificationbridge.NewVerificationEventConsumer(roster, failing, slog.Default())

	ev := vecEvent(notificationbridge.EventVerificationItemPending, vecItemReplay,
		vecPayload(vecItemReplay, vnOperatorMember, "", "", false))

	err := replayConsumer.HandleEvent(ctx, ev)
	if err == nil {
		t.Fatalf("expected transient error on first (fail-once) attempt")
	}
	if eventbus.IsPermanentError(err) {
		t.Fatalf("transient failure must be retryable, not permanent: %v", err)
	}
	// The retry (queue now healthy) reprocesses the SAME event; idempotency must prevent a duplicate.
	if err := replayConsumer.HandleEvent(ctx, ev); err != nil {
		t.Fatalf("retry handle: %v", err)
	}
	rows := countRows(t, ctx, pool,
		`SELECT count(*) FROM notification_requests WHERE tenant_id = $1 AND target_id = $2`,
		vnTenant, vecItemReplay)
	if rows != 4 {
		t.Fatalf("notification_requests after failed-then-retried run = %d, want 4 (no duplicate side effect)", rows)
	}

	// --- DLQ leg: a poison payload can never parse -> PermanentError -> DLQ, and writes NOTHING. ---
	poison := eventbus.Event{Type: notificationbridge.EventVerificationVerdictRework, TenantID: vnTenant, Key: "poison", Payload: []byte("{not valid json")}
	perr := consumer.HandleEvent(ctx, poison)
	if perr == nil {
		t.Fatalf("expected an error for a poison payload")
	}
	if !eventbus.IsPermanentError(perr) {
		t.Fatalf("poison payload must be a permanent (DLQ) error, got retryable: %v", perr)
	}
	poisonRows := countRows(t, ctx, pool,
		`SELECT count(*) FROM notification_requests WHERE tenant_id = $1`, vnTenant)
	if poisonRows != 4 { // only the replay-leg rows exist; the poison event wrote nothing
		t.Fatalf("notification_requests after poison event = %d, want 4 (poison writes nothing)", poisonRows)
	}
}

// TestVerificationEventConsumer_RecipientResolution proves per-event routing:
// pending -> verifier + park head + PC director + CEO; rework -> operator + park head;
// approved -> park head; leadership closure -> operator.
func TestVerificationEventConsumer_RecipientResolution(t *testing.T) {
	ctx := context.Background()
	pool, consumer := vecSetup(t)

	// pending -> verifier, park head, PC director, and CEO.
	if err := consumer.HandleEvent(ctx, vecEvent(notificationbridge.EventVerificationItemPending,
		vecItemPending, vecPayload(vecItemPending, vnOperatorMember, "", "", false))); err != nil {
		t.Fatalf("pending handle: %v", err)
	}
	pendingRefs := vecRecipientRefs(t, ctx, pool, vecItemPending)
	if len(pendingRefs) != 4 ||
		!pendingRefs[vnVerifierToken] ||
		!pendingRefs[vnParkHeadToken] ||
		!pendingRefs[vnLeadershipToken] ||
		!pendingRefs[vnCEOToken] {
		t.Fatalf("pending recipients = %v, want verifier %q, park head %q, PC director %q, CEO %q",
			pendingRefs, vnVerifierToken, vnParkHeadToken, vnLeadershipToken, vnCEOToken)
	}
	if pendingRefs[vnOperatorToken] {
		t.Fatalf("pending must NOT notify operator: %v", pendingRefs)
	}

	// rework -> operator + park head (both), never the verifier.
	if err := consumer.HandleEvent(ctx, vecEvent(notificationbridge.EventVerificationVerdictRework,
		vecItemRework, vecPayload(vecItemRework, vecOperatorUser, "rejected", "blurry", false))); err != nil {
		t.Fatalf("rework handle: %v", err)
	}
	reworkRefs := vecRecipientRefs(t, ctx, pool, vecItemRework)
	if len(reworkRefs) != 2 || !reworkRefs[vnOperatorToken] || !reworkRefs[vnParkHeadToken] {
		t.Fatalf("rework recipients = %v, want {operator %q, park head %q}", reworkRefs, vnOperatorToken, vnParkHeadToken)
	}
	if reworkRefs[vnVerifierToken] {
		t.Fatalf("rework must NOT notify the verifier: %v", reworkRefs)
	}

	// approved -> park head only, so the independently verified item reaches operational closure.
	if err := consumer.HandleEvent(ctx, vecEvent(notificationbridge.EventVerificationVerdictApproved,
		vecItemApproved, vecPayload(vecItemApproved, vnOperatorMember, "approved", "", false))); err != nil {
		t.Fatalf("approved handle: %v", err)
	}
	approvedRefs := vecRecipientRefs(t, ctx, pool, vecItemApproved)
	if len(approvedRefs) != 1 || !approvedRefs[vnParkHeadToken] {
		t.Fatalf("approved recipients = %v, want exactly {park head %q}", approvedRefs, vnParkHeadToken)
	}
	if approvedRefs[vnOperatorToken] || approvedRefs[vnVerifierToken] {
		t.Fatalf("approved must NOT notify operator/verifier: %v", approvedRefs)
	}

	// closed -> the originating operator only; park head/verifier already completed their actions.
	if err := consumer.HandleEvent(ctx, vecEvent(notificationbridge.EventVerificationItemClosed,
		vecItemClosed, vecPayload(vecItemClosed, vecOperatorUser, "approved", "", false))); err != nil {
		t.Fatalf("closed handle: %v", err)
	}
	closedRefs := vecRecipientRefs(t, ctx, pool, vecItemClosed)
	if len(closedRefs) != 1 || !closedRefs[vnOperatorToken] {
		t.Fatalf("closed recipients = %v, want exactly {operator %q}", closedRefs, vnOperatorToken)
	}
	if closedRefs[vnParkHeadToken] || closedRefs[vnVerifierToken] {
		t.Fatalf("closed must NOT notify park head/verifier: %v", closedRefs)
	}
}

// TestVerificationEventConsumer_LegacyDedup: a generic verification_item that mirrors a legacy
// vaccination SOP verification (source.module=vaccination, source.ref_type=sop_submission) is
// ALREADY notified by the legacy vaccination.verify.rejected path, so the generic rework push is
// suppressed -> NO second notification_request. A non-legacy item on the same park still notifies.
func TestVerificationEventConsumer_LegacyDedup(t *testing.T) {
	ctx := context.Background()
	pool, consumer := vecSetup(t)

	// Legacy-sourced rework -> suppressed (zero rows).
	if err := consumer.HandleEvent(ctx, vecEvent(notificationbridge.EventVerificationVerdictRework,
		vecItemLegacy, vecPayload(vecItemLegacy, vnOperatorMember, "rejected", "legacy dup", true))); err != nil {
		t.Fatalf("legacy rework handle: %v", err)
	}
	legacyRows := countRows(t, ctx, pool,
		`SELECT count(*) FROM notification_requests WHERE tenant_id = $1 AND target_id = $2`,
		vnTenant, vecItemLegacy)
	if legacyRows != 0 {
		t.Fatalf("legacy vaccination rework produced %d rows, want 0 (suppressed to avoid double push)", legacyRows)
	}

	// Control: a NON-legacy (generic) rework on the same park is NOT suppressed.
	if err := consumer.HandleEvent(ctx, vecEvent(notificationbridge.EventVerificationVerdictRework,
		vecItemRework, vecPayload(vecItemRework, vnOperatorMember, "rejected", "generic", false))); err != nil {
		t.Fatalf("generic rework handle: %v", err)
	}
	genericRows := countRows(t, ctx, pool,
		`SELECT count(*) FROM notification_requests WHERE tenant_id = $1 AND target_id = $2`,
		vnTenant, vecItemRework)
	if genericRows == 0 {
		t.Fatalf("non-legacy generic rework must still notify (got 0 rows); suppression is too broad")
	}
}

// vecRecipientRefs returns the set of recipient_ref (FCM tokens) on notification_requests for an item.
func vecRecipientRefs(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string) map[string]bool {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT recipient_ref FROM notification_requests WHERE tenant_id = $1 AND target_id = $2`,
		vnTenant, itemID)
	if err != nil {
		t.Fatalf("query recipients for %s: %v", itemID, err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			t.Fatalf("scan recipient: %v", err)
		}
		out[ref] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate recipients: %v", err)
	}
	return out
}

// failOnceQueue delegates the FIRST QueueRoleNotifications to the real queue (committing the row) and
// then returns a transient error, simulating a crash/ack-loss AFTER the side effect committed. Every
// later call delegates and returns success. Models at-least-once redelivery for the replay test.
type failOnceQueue struct {
	inner  notificationbridge.NotificationQueue
	failed bool
}

func (q *failOnceQueue) QueueRoleNotifications(ctx context.Context, in calendarports.QueueRoleNotifications) (int, error) {
	n, err := q.inner.QueueRoleNotifications(ctx, in)
	if err != nil {
		return n, err
	}
	if !q.failed {
		q.failed = true
		return n, errors.New("transient: simulated ack loss after commit")
	}
	return n, nil
}
