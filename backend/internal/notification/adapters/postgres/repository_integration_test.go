package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/notification/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	testTenantID = "00000000-0000-4000-8000-000000000001"
	testEventID  = "calendar:86000000-0000-4000-8000-000000010001"
)

func TestNotificationRepositoryClaimMarkAndReclaim(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	now := time.Date(2026, 6, 27, 9, 30, 0, 0, time.UTC)
	seedCalendarEvent(t, ctx, pool, testEventID)
	requestID := seedNotification(t, ctx, pool, testEventID, "notification-repo-claim", "queued", 0, nil)

	claimed, err := repo.ClaimDue(ctx, ports.ClaimParams{
		TenantID:    testTenantID,
		Limit:       10,
		MaxAttempts: 5,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 1 || claimed[0].NotificationRequestID != requestID || claimed[0].LeaseToken == "" || claimed[0].DeliveryAttempts != 1 {
		t.Fatalf("claimed=%#v want one leased request", claimed)
	}
	if err := repo.MarkSent(ctx, testTenantID, requestID, claimed[0].LeaseToken, "test-dispatcher", now.Add(time.Minute)); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	assertNotificationState(t, ctx, pool, requestID, "sent", 1, false)

	failedID := seedNotification(t, ctx, pool, testEventID, "notification-repo-reclaim", "sending", 1, nil)
	if _, err := pool.Exec(ctx, `
UPDATE notification_requests
SET lease_token = gen_random_uuid(),
    leased_at = $3::timestamptz
WHERE tenant_id = $1::uuid AND notification_request_id = $2::uuid`,
		testTenantID, failedID, now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("make stale sending: %v", err)
	}
	reclaimed, err := repo.ReclaimStaleSending(ctx, testTenantID, now, 2*time.Minute)
	if err != nil {
		t.Fatalf("ReclaimStaleSending: %v", err)
	}
	if reclaimed != 1 {
		t.Fatalf("reclaimed=%d want 1", reclaimed)
	}
	assertNotificationState(t, ctx, pool, failedID, "queued", 1, true)
}

// TestNotificationRepositoryClaimDuePlanSkipsFutureRetriesUnderSkew is the
// KERN-REV-06 query-plan gate. Under heavy future-retry skew (thousands of
// failed rows scheduled to retry later, a handful actually due), the claim
// predicate must let notification_requests_queue_idx RANGE-scan only the due
// rows and stop at now — never scan every future-scheduled row to fill the
// LIMIT. Because the predicate is the index expression
// (COALESCE(next_attempt_at, requested_at) <= now), the index scan is bounded:
// the executed plan reaches notification_requests via an index path and its
// actual rows read stay far below the skew population.
func TestNotificationRepositoryClaimDuePlanSkipsFutureRetriesUnderSkew(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	now := time.Date(2026, 6, 27, 9, 30, 0, 0, time.UTC)
	seedCalendarEvent(t, ctx, pool, testEventID)

	// 8000 failed rows scheduled to retry in the future (NOT due) + 15 due queued.
	const futureSkew = 8000
	if _, err := pool.Exec(ctx, `
INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, notification_type, channel, title, body,
  status, idempotency_key, request_fingerprint, context, delivery_attempts, next_attempt_at, requested_at
)
SELECT $1::uuid, $2, 'cohort', 'reminder', 'local-stub', 't', 'b',
  'failed', 'skew-' || g, 'skew-' || g || ':fp', '{}'::jsonb, 1,
  $3::timestamptz, $4::timestamptz
FROM generate_series(1, $5::int) g`,
		testTenantID, testEventID, now.Add(time.Hour), now.Add(-2*time.Hour), futureSkew); err != nil {
		t.Fatalf("seed future-retry skew: %v", err)
	}
	for i := 0; i < 15; i++ {
		seedNotification(t, ctx, pool, testEventID, fmt.Sprintf("due-%02d", i), "queued", 0, nil)
	}
	if _, err := pool.Exec(ctx, `ANALYZE notification_requests`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	// Force the index path so the assertion isolates whether the predicate lets
	// the index BOUND the scan (skip future rows), independent of small-fixture
	// planner cost preferences.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("disable seqscan: %v", err)
	}

	var raw []byte
	if err := tx.QueryRow(ctx, `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
SELECT notification_request_id
FROM notification_requests
WHERE tenant_id = $1::uuid
  AND status IN ('queued', 'failed')
  AND COALESCE(next_attempt_at, requested_at) <= $2::timestamptz
  AND delivery_attempts < $3
ORDER BY COALESCE(next_attempt_at, requested_at), notification_request_id
LIMIT $4`, testTenantID, now, 5, 100).Scan(&raw); err != nil {
		t.Fatalf("explain: %v", err)
	}

	var plans []struct {
		Plan planNode `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) == 0 {
		t.Fatalf("parse explain json: %v (%s)", err, raw)
	}
	scans := collectRelationScans(plans[0].Plan, "notification_requests")
	if len(scans) == 0 {
		t.Fatalf("no notification_requests scan node in plan: %s", raw)
	}
	for _, s := range scans {
		if strings.Contains(s.NodeType, "Seq Scan") {
			t.Fatalf("notification_requests reached via Seq Scan under skew: %s", raw)
		}
		if s.ActualRows > 500 {
			t.Fatalf("index scan read %d rows (of %d skew) — predicate did not bound the scan to due rows", int(s.ActualRows), futureSkew)
		}
	}
}

type planNode struct {
	NodeType     string     `json:"Node Type"`
	RelationName string     `json:"Relation Name"`
	ActualRows   float64    `json:"Actual Rows"`
	Plans        []planNode `json:"Plans"`
}

func collectRelationScans(n planNode, rel string) []planNode {
	var out []planNode
	if n.RelationName == rel {
		out = append(out, n)
	}
	for _, c := range n.Plans {
		out = append(out, collectRelationScans(c, rel)...)
	}
	return out
}

// TestNotificationRepositoryClaimHonorsBurstLimit proves the KERN-REV-05 batch
// budget: with the retired job's limit of 100 (not the one-shot default of 50),
// a burst of 120 due notifications is drained 100 at a time, so the backlog
// clears in bounded cycles instead of stalling at 50/tick.
func TestNotificationRepositoryClaimHonorsBurstLimit(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	now := time.Date(2026, 6, 27, 9, 30, 0, 0, time.UTC)
	seedCalendarEvent(t, ctx, pool, testEventID)

	const burst = 120
	for i := 0; i < burst; i++ {
		seedNotification(t, ctx, pool, testEventID, fmt.Sprintf("notif-burst-%03d", i), "queued", 0, nil)
	}

	claimed, err := repo.ClaimDue(ctx, ports.ClaimParams{TenantID: testTenantID, Limit: 100, MaxAttempts: 5, Now: now})
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 100 {
		t.Fatalf("claimed %d of a 120 burst with limit 100; want 100 (default 50 would stall the backlog)", len(claimed))
	}
}

func TestNotificationRepositoryRetryFailureDoesNotWriteExhaustedEvidence(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	now := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)
	eventID := "calendar:86000000-0000-4000-8000-000000010003"
	requestKey := "notification-repo-retry-failed"
	seedCalendarEvent(t, ctx, pool, eventID)
	requestID := seedNotification(t, ctx, pool, eventID, requestKey, "queued", 1, nil)

	claimed, err := repo.ClaimDue(ctx, ports.ClaimParams{
		TenantID:    testTenantID,
		Limit:       10,
		MaxAttempts: 5,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 1 || claimed[0].NotificationRequestID != requestID || claimed[0].DeliveryAttempts != 2 {
		t.Fatalf("claimed=%#v want retryable request %s", claimed, requestID)
	}
	nextAttempt := now.Add(5 * time.Minute)
	if err := repo.MarkFailed(ctx, testTenantID, requestID, claimed[0].LeaseToken, "test-dispatcher", "temporary retry", &nextAttempt, now.Add(time.Minute)); err != nil {
		t.Fatalf("MarkFailed retry: %v", err)
	}
	assertNotificationState(t, ctx, pool, requestID, "failed", 2, true)
	assertCount(t, ctx, pool, "notification exhausted audit after retry", `
SELECT count(*)
FROM audit_log
WHERE tenant_id = $1::uuid
  AND resource_type = 'calendar_notification'
  AND resource_id = $2::uuid
  AND action = 'notification.exhausted'`, 0, testTenantID, requestID)
	assertCount(t, ctx, pool, "notification exhausted outbox after retry", `
SELECT count(*)
FROM outbox_messages
WHERE tenant_id = $1::uuid
  AND aggregate_type = 'calendar_notification'
  AND aggregate_id = $2::uuid
  AND event_type = 'notification.exhausted'`, 0, testTenantID, requestID)
	assertNotificationRetryDetails(t, ctx, pool, requestID, "temporary retry", "test-dispatcher", nextAttempt)
}

func TestNotificationRepositoryFinalFailureWritesAuditAndOutbox(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	now := time.Date(2026, 6, 27, 10, 30, 0, 0, time.UTC)
	eventID := "calendar:86000000-0000-4000-8000-000000010002"
	requestKey := "notification-repo-exhausted"
	requestTraceID := "trace-" + requestKey
	seedCalendarEvent(t, ctx, pool, eventID)
	requestID := seedNotification(t, ctx, pool, eventID, requestKey, "queued", 4, nil)

	claimed, err := repo.ClaimDue(ctx, ports.ClaimParams{
		TenantID:    testTenantID,
		Limit:       10,
		MaxAttempts: 5,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("ClaimDue: %v", err)
	}
	if len(claimed) != 1 || claimed[0].NotificationRequestID != requestID || claimed[0].DeliveryAttempts != 5 {
		t.Fatalf("claimed=%#v want final-attempt request %s", claimed, requestID)
	}
	if err := repo.MarkFailed(ctx, testTenantID, requestID, claimed[0].LeaseToken, "test-dispatcher", "synthetic exhausted", nil, now.Add(time.Minute)); err != nil {
		t.Fatalf("MarkFailed final: %v", err)
	}
	replayErr := repo.MarkFailed(ctx, testTenantID, requestID, claimed[0].LeaseToken, "test-dispatcher", "synthetic exhausted replay", nil, now.Add(2*time.Minute))
	if replayErr == nil || !strings.Contains(replayErr.Error(), "mark failed claim missing") {
		t.Fatalf("MarkFailed final replay error=%v, want claim missing", replayErr)
	}
	assertNotificationState(t, ctx, pool, requestID, "exhausted", 5, true)
	assertCount(t, ctx, pool, "notification exhausted audit", `
SELECT count(*)
FROM audit_log
WHERE tenant_id = $1::uuid
  AND resource_type = 'calendar_notification'
  AND resource_id = $2::uuid
  AND action = 'notification.exhausted'
  AND trace_id = $3`, 1, testTenantID, requestID, requestTraceID)
	assertCount(t, ctx, pool, "notification exhausted outbox", `
SELECT count(*)
FROM outbox_messages
WHERE tenant_id = $1::uuid
  AND aggregate_type = 'calendar_notification'
  AND aggregate_id = $2::uuid
  AND event_type = 'notification.exhausted'
  AND status = 'pending'
  AND trace_id = $3
  AND payload ->> 'trace_id' = $3`, 1, testTenantID, requestID, requestTraceID)
}

// seedCalendarEvent is a deliberate no-op now: calendar_event_projections and the
// calendar_event_identities identity table its trigger fed (and the notification_requests/
// calendar_snoozes FKs that validated against calendar_event_identities) are all retired by the
// 5k-50k envelope cutover (migration 000189, docs/decisions/operational-kernel-5k-50k-scale-envelope.md).
// notification_requests.calendar_event_id is a plain, unconstrained text column now -- there is
// nothing left to seed a referential fixture row for. Kept as a function (rather than deleting every
// call site) so this test's intent at each call site stays self-documenting.
func seedCalendarEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID string) {
	t.Helper()
}

func assertNotificationRetryDetails(t *testing.T, ctx context.Context, pool *pgxpool.Pool, requestID, wantFailure, wantDeliveredBy string, wantNextAttempt time.Time) {
	t.Helper()
	var failureReason string
	var deliveredBy string
	var nextAttempt time.Time
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(failure_reason, ''), COALESCE(delivered_by, ''), next_attempt_at
FROM notification_requests
WHERE tenant_id = $1::uuid AND notification_request_id = $2::uuid`,
		testTenantID, requestID).Scan(&failureReason, &deliveredBy, &nextAttempt); err != nil {
		t.Fatalf("query notification retry details: %v", err)
	}
	if failureReason != wantFailure || deliveredBy != wantDeliveredBy || !nextAttempt.Equal(wantNextAttempt) {
		t.Fatalf("notification retry details failure=%q deliveredBy=%q next=%s, want %q/%q/%s",
			failureReason, deliveredBy, nextAttempt.Format(time.RFC3339Nano),
			wantFailure, wantDeliveredBy, wantNextAttempt.Format(time.RFC3339Nano))
	}
}

func seedNotification(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID, key, status string, attempts int, nextAttemptAt *time.Time) string {
	t.Helper()
	var requestID string
	if err := pool.QueryRow(ctx, `
INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, target_id, notification_type, channel,
  title, body, status, idempotency_key, request_fingerprint, context,
  delivery_attempts, next_attempt_at, trace_id, requested_at
) VALUES (
  $1::uuid, $2, 'cohort', NULL, 'reminder', 'local-stub',
  'Notification repo test', 'Notification repo body', $3, $4, $5, '{}'::jsonb,
  $6, $7::timestamptz, $8,
  -- A fixed past request time (before the tests' fixed clock) so a queued row is
  -- due under the index-bounded predicate COALESCE(next_attempt_at, requested_at)
  -- <= now, mirroring production where a request always precedes its claim.
  TIMESTAMPTZ '2026-06-01 00:00:00+00'
)
RETURNING notification_request_id::text`,
		testTenantID, eventID, status, key, key+":fingerprint", attempts, nextAttemptAt, "trace-"+key).Scan(&requestID); err != nil {
		t.Fatalf("seed notification: %v", err)
	}
	return requestID
}

func assertCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label, query string, want int, args ...any) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, query, args...).Scan(&got); err != nil {
		t.Fatalf("%s count query: %v", label, err)
	}
	if got != want {
		t.Fatalf("%s count=%d want %d", label, got, want)
	}
}

func assertNotificationState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, requestID, wantStatus string, wantAttempts int, wantFailure bool) {
	t.Helper()
	var status string
	var attempts int
	var leaseCleared bool
	var hasFailure bool
	if err := pool.QueryRow(ctx, `
SELECT status, delivery_attempts, lease_token IS NULL, COALESCE(failure_reason, '') <> ''
FROM notification_requests
WHERE tenant_id = $1::uuid AND notification_request_id = $2::uuid`,
		testTenantID, requestID).Scan(&status, &attempts, &leaseCleared, &hasFailure); err != nil {
		t.Fatalf("query notification state: %v", err)
	}
	if status != wantStatus || attempts != wantAttempts || !leaseCleared || hasFailure != wantFailure {
		t.Fatalf("notification state status=%s attempts=%d leaseCleared=%t hasFailure=%t, want %s/%d/true/%t",
			status, attempts, leaseCleared, hasFailure, wantStatus, wantAttempts, wantFailure)
	}
}
