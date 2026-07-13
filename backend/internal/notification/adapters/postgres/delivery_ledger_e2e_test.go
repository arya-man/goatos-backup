package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	notificationapp "github.com/vgoats/goatos/backend/internal/notification/app"
	"github.com/vgoats/goatos/backend/internal/notification/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestNotificationDeliveryLedgerSuccess drives one notification_requests row through the REAL
// dispatcher (notification/app.Service.RunOnce) against the REAL repository, with a stub gateway
// that succeeds on the first send. It certifies the per-attempt audit ledger (migration 000182,
// notification_delivery_attempts) and the notification.sent durable event
// (insertNotificationSentEvidence) that MarkSent now writes atomically with the status flip.
func TestNotificationDeliveryLedgerSuccess(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	eventID := "calendar:87000000-0000-4000-8000-000000020001"
	requestKey := "notification-ledger-success"
	seedCalendarEvent(t, ctx, pool, eventID)
	requestID := seedNotification(t, ctx, pool, eventID, requestKey, "queued", 0, nil)

	now := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	gateway := &ledgerTestGateway{}
	service := notificationapp.NewService(repo, gateway, notificationapp.Config{
		Limit:       10,
		MaxAttempts: 5,
		BackoffBase: time.Second,
		BackoffMax:  time.Second,
		Now:         func() time.Time { return now },
	}, nil)

	result, err := service.RunOnce(ctx, testTenantID)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if result.ClaimedCount != 1 || result.SentCount != 1 || result.FailedCount != 0 || result.ExhaustedCount != 0 {
		t.Fatalf("result=%#v want one claimed+sent", result)
	}
	if gateway.callCount() != 1 {
		t.Fatalf("gateway calls=%d want 1", gateway.callCount())
	}

	assertNotificationState(t, ctx, pool, requestID, "sent", 1, false)
	assertDeliveryAttempts(t, ctx, pool, requestID, []deliveryAttemptExpectation{
		{attemptNo: 1, result: "sent", hasError: false},
	})
	assertCount(t, ctx, pool, "notification.sent outbox after success", `
SELECT count(*)
FROM outbox_messages
WHERE tenant_id = $1::uuid
  AND aggregate_type = 'calendar_notification'
  AND aggregate_id = $2::uuid
  AND event_type = 'notification.sent'
  AND status = 'pending'
  AND idempotency_key = $3`, 1, testTenantID, requestID, "notification.sent:"+requestID)
	assertCount(t, ctx, pool, "notification.exhausted outbox after success", `
SELECT count(*)
FROM outbox_messages
WHERE tenant_id = $1::uuid
  AND aggregate_id = $2::uuid
  AND event_type = 'notification.exhausted'`, 0, testTenantID, requestID)

	// Replay: a second MarkSent call for the same (already-sent) request must be a no-op -- the
	// lease + status guard rejects it before it ever reaches the ledger/event inserts, so no
	// duplicate attempt row and no duplicate notification.sent event.
	replayErr := repo.MarkSent(ctx, testTenantID, requestID, "00000000-0000-4000-8000-000000000000", gateway.Name(), now.Add(time.Minute))
	if replayErr == nil {
		t.Fatalf("MarkSent replay with stale lease should fail, got nil")
	}
	assertDeliveryAttempts(t, ctx, pool, requestID, []deliveryAttemptExpectation{
		{attemptNo: 1, result: "sent", hasError: false},
	})
	assertCount(t, ctx, pool, "notification.sent outbox after replay attempt", `
SELECT count(*)
FROM outbox_messages
WHERE tenant_id = $1::uuid AND aggregate_id = $2::uuid AND event_type = 'notification.sent'`,
		1, testTenantID, requestID)
}

// TestNotificationDeliveryLedgerRetryThenSuccess fails once (transient) then succeeds, driven by
// two real RunOnce dispatch cycles. It certifies two ledger rows (attempt 1 failed, attempt 2
// sent), a final 'sent' status, exactly one notification.sent event (only on the eventual
// success), and no notification.exhausted event.
func TestNotificationDeliveryLedgerRetryThenSuccess(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	eventID := "calendar:87000000-0000-4000-8000-000000020002"
	requestKey := "notification-ledger-retry-success"
	seedCalendarEvent(t, ctx, pool, eventID)
	requestID := seedNotification(t, ctx, pool, eventID, requestKey, "queued", 0, nil)

	current := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	gateway := &ledgerTestGateway{failCount: 1, failMessage: "synthetic transient failure"}
	service := notificationapp.NewService(repo, gateway, notificationapp.Config{
		Limit:       10,
		MaxAttempts: 5,
		BackoffBase: time.Second,
		BackoffMax:  time.Second,
		Now:         func() time.Time { return current },
	}, nil)

	firstResult, err := service.RunOnce(ctx, testTenantID)
	if err != nil {
		t.Fatalf("RunOnce (attempt 1): %v", err)
	}
	if firstResult.ClaimedCount != 1 || firstResult.FailedCount != 1 || firstResult.SentCount != 0 {
		t.Fatalf("first result=%#v want one claimed+failed(retryable)", firstResult)
	}
	assertNotificationState(t, ctx, pool, requestID, "failed", 1, true)

	// Advance past the backoff-computed next_attempt_at so the retry is claimable.
	current = current.Add(10 * time.Second)

	secondResult, err := service.RunOnce(ctx, testTenantID)
	if err != nil {
		t.Fatalf("RunOnce (attempt 2): %v", err)
	}
	if secondResult.ClaimedCount != 1 || secondResult.SentCount != 1 || secondResult.FailedCount != 0 {
		t.Fatalf("second result=%#v want one claimed+sent", secondResult)
	}
	if gateway.callCount() != 2 {
		t.Fatalf("gateway calls=%d want 2", gateway.callCount())
	}

	assertNotificationState(t, ctx, pool, requestID, "sent", 2, false)
	assertDeliveryAttempts(t, ctx, pool, requestID, []deliveryAttemptExpectation{
		{attemptNo: 1, result: "failed", hasError: true},
		{attemptNo: 2, result: "sent", hasError: false},
	})
	assertCount(t, ctx, pool, "notification.sent outbox after retry-then-success", `
SELECT count(*)
FROM outbox_messages
WHERE tenant_id = $1::uuid AND aggregate_id = $2::uuid AND event_type = 'notification.sent'`,
		1, testTenantID, requestID)
	assertCount(t, ctx, pool, "notification.exhausted outbox after retry-then-success", `
SELECT count(*)
FROM outbox_messages
WHERE tenant_id = $1::uuid AND aggregate_id = $2::uuid AND event_type = 'notification.exhausted'`,
		0, testTenantID, requestID)
}

// TestNotificationDeliveryLedgerPermanentFailureExhausts drives a request that fails on every
// attempt across two dispatch cycles (MaxAttempts=2) until it exhausts. It certifies one ledger
// row per attempt (both 'failed'), the existing notification.exhausted event, and no
// notification.sent event.
func TestNotificationDeliveryLedgerPermanentFailureExhausts(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	eventID := "calendar:87000000-0000-4000-8000-000000020003"
	requestKey := "notification-ledger-exhausted"
	seedCalendarEvent(t, ctx, pool, eventID)
	requestID := seedNotification(t, ctx, pool, eventID, requestKey, "queued", 0, nil)

	current := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	gateway := &ledgerTestGateway{failCount: -1, failMessage: "synthetic permanent failure"}
	service := notificationapp.NewService(repo, gateway, notificationapp.Config{
		Limit:       10,
		MaxAttempts: 2,
		BackoffBase: time.Second,
		BackoffMax:  time.Second,
		Now:         func() time.Time { return current },
	}, nil)

	firstResult, err := service.RunOnce(ctx, testTenantID)
	if err != nil {
		t.Fatalf("RunOnce (attempt 1): %v", err)
	}
	if firstResult.ClaimedCount != 1 || firstResult.FailedCount != 1 || firstResult.ExhaustedCount != 0 {
		t.Fatalf("first result=%#v want one claimed+failed(retryable)", firstResult)
	}
	assertNotificationState(t, ctx, pool, requestID, "failed", 1, true)

	current = current.Add(10 * time.Second)

	secondResult, err := service.RunOnce(ctx, testTenantID)
	if err != nil {
		t.Fatalf("RunOnce (attempt 2): %v", err)
	}
	if secondResult.ClaimedCount != 1 || secondResult.ExhaustedCount != 1 || secondResult.SentCount != 0 {
		t.Fatalf("second result=%#v want one claimed+exhausted", secondResult)
	}
	if gateway.callCount() != 2 {
		t.Fatalf("gateway calls=%d want 2", gateway.callCount())
	}

	assertNotificationState(t, ctx, pool, requestID, "exhausted", 2, true)
	assertDeliveryAttempts(t, ctx, pool, requestID, []deliveryAttemptExpectation{
		{attemptNo: 1, result: "failed", hasError: true},
		{attemptNo: 2, result: "failed", hasError: true},
	})
	assertCount(t, ctx, pool, "notification.exhausted outbox after permanent failure", `
SELECT count(*)
FROM outbox_messages
WHERE tenant_id = $1::uuid AND aggregate_id = $2::uuid AND event_type = 'notification.exhausted'`,
		1, testTenantID, requestID)
	assertCount(t, ctx, pool, "notification.sent outbox after permanent failure", `
SELECT count(*)
FROM outbox_messages
WHERE tenant_id = $1::uuid AND aggregate_id = $2::uuid AND event_type = 'notification.sent'`,
		0, testTenantID, requestID)
}

type deliveryAttemptExpectation struct {
	attemptNo int
	result    string
	hasError  bool
}

func assertDeliveryAttempts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, requestID string, want []deliveryAttemptExpectation) {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT attempt_no, result, error IS NOT NULL, provider_message_id IS NULL
FROM notification_delivery_attempts
WHERE tenant_id = $1::uuid AND notification_request_id = $2::uuid
ORDER BY attempt_no`, testTenantID, requestID)
	if err != nil {
		t.Fatalf("query delivery attempts: %v", err)
	}
	defer rows.Close()

	var got []deliveryAttemptExpectation
	for rows.Next() {
		var attemptNo int
		var result string
		var hasError bool
		var providerMessageIDNull bool
		if err := rows.Scan(&attemptNo, &result, &hasError, &providerMessageIDNull); err != nil {
			t.Fatalf("scan delivery attempt: %v", err)
		}
		if !providerMessageIDNull {
			t.Fatalf("attempt_no=%d provider_message_id expected NULL (gateway does not surface a provider id)", attemptNo)
		}
		got = append(got, deliveryAttemptExpectation{attemptNo: attemptNo, result: result, hasError: hasError})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate delivery attempts: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("delivery attempts=%#v want %#v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("delivery attempt[%d]=%#v want %#v", i, got[i], w)
		}
	}
}

// ledgerTestGateway is a stub ports.Gateway: failCount < 0 fails every call; failCount == N fails
// the first N calls then succeeds; failCount == 0 always succeeds. It never talks to a real
// channel adapter -- the channel/vendor send is out of scope for the audit-ledger logic under
// test here.
type ledgerTestGateway struct {
	mu          sync.Mutex
	calls       int
	failCount   int
	failMessage string
}

func (g *ledgerTestGateway) Name() string { return "ledger-test-gateway" }

func (g *ledgerTestGateway) Send(context.Context, domain.Request) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	if g.failCount < 0 || g.calls <= g.failCount {
		msg := g.failMessage
		if msg == "" {
			msg = "synthetic failure"
		}
		return errors.New(msg)
	}
	return nil
}

func (g *ledgerTestGateway) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}
