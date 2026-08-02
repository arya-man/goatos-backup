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
	// BUG-B fix: ClaimDue incremented delivery_attempts to 1 at claim time, but this row
	// crashed before a real delivery attempt (MarkSent/MarkFailed) ever ran. Lease-expiry
	// reclaim must restore that consumed attempt so the count reflects only claims that
	// reached a genuine delivery outcome — see TestNotificationRepositoryReclaimRestoresAttemptAcrossRepeatedCrashes
	// for the full max_attempts regression.
	assertNotificationState(t, ctx, pool, failedID, "queued", 0, true)
}

// TestNotificationRepositoryReclaimRestoresAttemptAcrossRepeatedCrashes is the BUG-B
// regression: a worker that crashes AFTER ClaimDue (which increments delivery_attempts)
// but BEFORE a real delivery outcome (MarkSent/MarkFailed) must not permanently consume
// that attempt. Without ReclaimStaleSending restoring the count, repeated claim/crash
// cycles would exhaust max_attempts and the row would flip to 'exhausted' without a single
// real delivery attempt ever happening. This drives claim -> simulate-crash (force the
// lease to look expired) -> reclaim, MaxAttempts times, and asserts the message is still
// 'queued' and claimable — never 'exhausted' — because none of those claims were real
// delivery attempts. It then proves a REAL failure still counts by claiming once more and
// calling MarkFailed, which must actually be able to exhaust the row afterward.
func TestNotificationRepositoryReclaimRestoresAttemptAcrossRepeatedCrashes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	now := time.Date(2026, 6, 27, 9, 30, 0, 0, time.UTC)
	seedCalendarEvent(t, ctx, pool, testEventID)
	requestID := seedNotification(t, ctx, pool, testEventID, "notification-repo-crash-loop", "queued", 0, nil)

	const maxAttempts = 3

	// Claim and "crash" (never call MarkSent/MarkFailed) maxAttempts times in a row. If the
	// crash-before-delivery attempt were permanently consumed (the bug), the row would flip
	// to 'exhausted' by the ClaimDue call that pushes delivery_attempts >= maxAttempts, well
	// before any real delivery was ever attempted.
	for i := 0; i < maxAttempts; i++ {
		iterNow := now.Add(time.Duration(i) * time.Hour)
		claimed, err := repo.ClaimDue(ctx, ports.ClaimParams{
			TenantID:    testTenantID,
			Limit:       10,
			MaxAttempts: maxAttempts,
			Now:         iterNow,
		})
		if err != nil {
			t.Fatalf("iteration %d: ClaimDue: %v", i, err)
		}
		if len(claimed) != 1 || claimed[0].NotificationRequestID != requestID {
			t.Fatalf("iteration %d: claimed=%#v want request re-claimable every crash cycle", i, claimed)
		}

		// Simulate a crash: back-date the lease so the row looks stale, then reclaim it —
		// exactly the recovery path a real lease-expiry sweep takes after a worker dies
		// mid-flight, never having reached MarkSent/MarkFailed.
		if _, err := pool.Exec(ctx, `
UPDATE notification_requests
SET leased_at = $3::timestamptz
WHERE tenant_id = $1::uuid AND notification_request_id = $2::uuid`,
			testTenantID, requestID, iterNow.Add(-10*time.Minute)); err != nil {
			t.Fatalf("iteration %d: back-date lease: %v", i, err)
		}
		reclaimed, err := repo.ReclaimStaleSending(ctx, testTenantID, iterNow, 2*time.Minute)
		if err != nil {
			t.Fatalf("iteration %d: ReclaimStaleSending: %v", i, err)
		}
		if reclaimed != 1 {
			t.Fatalf("iteration %d: reclaimed=%d want 1", i, reclaimed)
		}
		// The consumed attempt must be restored every time: after each crash cycle the
		// row is back to delivery_attempts=0, status='queued', never 'exhausted'.
		assertNotificationState(t, ctx, pool, requestID, "queued", 0, true)
	}

	// Now prove a REAL delivery attempt still counts and can genuinely exhaust the row:
	// claim maxAttempts times, calling MarkFailed (a real, completed delivery outcome) each
	// time instead of crashing. The final MarkFailed call must exhaust the message.
	var leaseToken string
	for i := 0; i < maxAttempts; i++ {
		iterNow := now.Add(time.Duration(maxAttempts+i) * time.Hour)
		claimed, err := repo.ClaimDue(ctx, ports.ClaimParams{
			TenantID:    testTenantID,
			Limit:       10,
			MaxAttempts: maxAttempts,
			Now:         iterNow,
		})
		if err != nil {
			t.Fatalf("real-attempt iteration %d: ClaimDue: %v", i, err)
		}
		if len(claimed) != 1 || claimed[0].NotificationRequestID != requestID {
			t.Fatalf("real-attempt iteration %d: claimed=%#v want the same request claimable", i, claimed)
		}
		leaseToken = claimed[0].LeaseToken
		// Mirror the production service's retry decision (app/service.go): retryable
		// (nextAttemptAt != nil) while DeliveryAttempts < MaxAttempts, permanent
		// (nextAttemptAt == nil) once the real attempt count reaches MaxAttempts.
		var nextAttempt *time.Time
		if claimed[0].DeliveryAttempts < maxAttempts {
			next := iterNow.Add(time.Hour)
			nextAttempt = &next
		}
		if err := repo.MarkFailed(ctx, testTenantID, requestID, leaseToken, "test-dispatcher", "simulated_real_failure", nextAttempt, iterNow.Add(time.Minute)); err != nil {
			t.Fatalf("real-attempt iteration %d: MarkFailed: %v", i, err)
		}
	}
	// After maxAttempts REAL (non-crash) failures, the message must be permanently exhausted.
	assertNotificationState(t, ctx, pool, requestID, "exhausted", maxAttempts, true)
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

	// Heavy future-retry skew: 40k failed rows scheduled to retry in the future
	// (NOT due) + 15 due queued. At this size a sequential scan is clearly more
	// expensive than the bounded index range, so the planner's NATURAL choice
	// (no enable_seqscan=off) is the index — proving production behaviour, not a
	// forced answer.
	const futureSkew = 40000
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
	// Real planner statistics after the bulk load (mandatory post-seed ANALYZE).
	if _, err := pool.Exec(ctx, `ANALYZE notification_requests`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	// EXPLAIN (ANALYZE, BUFFERS) the REAL production writable CTE (candidate
	// selection + FOR UPDATE SKIP LOCKED + UPDATE ... RETURNING) — ClaimDueSQL, the
	// exact string ClaimDue runs — with sequential scans ENABLED. ANALYZE executes
	// it (claims the due rows), so run inside a tx and roll back.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	var raw []byte
	// ClaimDueSQL params: $1 tenant, $2 now, $3 limit, $4 max attempts.
	if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)\n"+postgresClaimDueSQL(), testTenantID, now, 100, 5).Scan(&raw); err != nil {
		t.Fatalf("explain: %v", err)
	}

	var plans []struct {
		Plan          planNode `json:"Plan"`
		ExecutionTime float64  `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) == 0 {
		t.Fatalf("parse explain json: %v (%s)", err, raw)
	}
	root := plans[0].Plan
	scans := collectRelationScans(root, "notification_requests")
	if len(scans) == 0 {
		t.Fatalf("no notification_requests scan node in plan: %s", raw)
	}
	// (a) The planner NATURALLY reached notification_requests via an index path,
	// never a Seq Scan, and the driving scan touched only the due rows (<< 40k
	// skew) — the future-scheduled retries were not scanned to fill the LIMIT.
	sawIndex := false
	for _, sc := range scans {
		if strings.Contains(sc.NodeType, "Seq Scan") {
			t.Fatalf("notification_requests reached via Seq Scan under skew (predicate/index regressed): %s", raw)
		}
		if strings.Contains(sc.NodeType, "Index") {
			sawIndex = true
		}
		if sc.ActualRows > 500 {
			t.Fatalf("a notification_requests scan read %d rows of %d skew — the scan was not bounded to due rows", int(sc.ActualRows), futureSkew)
		}
	}
	if !sawIndex {
		t.Fatalf("no index access path on notification_requests in the plan: %s", raw)
	}
	// (b) Buffers stay bounded: a 40k Seq Scan would touch hundreds+ of shared
	// blocks; the bounded index range touches far fewer.
	if blocks := root.SharedHit + root.SharedRead; blocks > 500 {
		t.Fatalf("plan touched %.0f shared blocks — consistent with a full scan, not a bounded index range: %s", blocks, raw)
	}
	// (c) Execution time ceiling (generous, still catches a full scan).
	if plans[0].ExecutionTime > 1000 {
		t.Fatalf("claim executed in %.1fms — too slow for a bounded index plan", plans[0].ExecutionTime)
	}
}

// postgresClaimDueSQL returns the exact production claim SQL (the inner query from ClaimDue)
// so this plan gate cannot drift into a simplified imitation. The query is the CTE that
// claims retryable rows after exhausted rows are transitioned to 'exhausted' status.
func postgresClaimDueSQL() string {
	return `
WITH candidates AS (
  SELECT notification_request_id
  FROM notification_requests
  WHERE tenant_id = $1::uuid
    AND status IN ('queued', 'failed')
    AND COALESCE(next_attempt_at, requested_at) <= $2::timestamptz
    AND delivery_attempts < $4
  ORDER BY COALESCE(next_attempt_at, requested_at), notification_request_id
  LIMIT $3
  FOR UPDATE SKIP LOCKED
),
claimed AS (
  UPDATE notification_requests nr
  SET status = 'sending',
      lease_token = gen_random_uuid(),
      leased_at = $2::timestamptz,
      delivery_attempts = delivery_attempts + 1,
      updated_at = $2::timestamptz
  FROM candidates c
  WHERE nr.notification_request_id = c.notification_request_id
  RETURNING
    nr.notification_request_id::text,
    nr.tenant_id::text,
    nr.calendar_event_id,
    nr.target_type,
    COALESCE(nr.target_id::text, ''),
    nr.notification_type,
    nr.channel,
    COALESCE(nr.recipient_ref, ''),
    nr.title,
    nr.body,
    nr.status,
    COALESCE(nr.trace_id, ''),
    nr.context,
    nr.delivery_attempts,
    nr.lease_token::text,
    nr.requested_at
)
SELECT *
FROM claimed
ORDER BY requested_at, notification_request_id`
}

type planNode struct {
	NodeType        string     `json:"Node Type"`
	RelationName    string     `json:"Relation Name"`
	ActualRows      float64    `json:"Actual Rows"`
	ActualTotalTime float64    `json:"Actual Total Time"`
	SharedHit       float64    `json:"Shared Hit Blocks"`
	SharedRead      float64    `json:"Shared Read Blocks"`
	Plans           []planNode `json:"Plans"`
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

// anyNodeType reports whether any node in the plan tree (rooted at n) has a
// "Node Type" containing substr. Needed for bitmap plans: a "Bitmap Index
// Scan" node drives the index access but carries no "Relation Name" of its
// own (only its parent "Bitmap Heap Scan" does), so collectRelationScans
// alone cannot see it.
func anyNodeType(n planNode, substr string) bool {
	if strings.Contains(n.NodeType, substr) {
		return true
	}
	for _, c := range n.Plans {
		if anyNodeType(c, substr) {
			return true
		}
	}
	return false
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

// TestNotificationRepositoryOldestDueRequestedAtUnderSaturation is the
// KERN-REV-06A regression: under saturation the backlog-age metric must report
// the GLOBALLY oldest currently-due request, which the claimed batch could hide.
// A hundred newer queued rows sort earlier by the ClaimDue scheduling key
// (COALESCE(next_attempt_at, requested_at)) than an hour-old failed request whose
// retry only just became due, so a batch-derived metric would report ~1 minute
// and suppress the alert. The global query must return the hour-old requested_at,
// and a future-scheduled retry (oldest requested_at, but not due) must be excluded.
func TestNotificationRepositoryOldestDueRequestedAtUnderSaturation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	now := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	seedCalendarEvent(t, ctx, pool, testEventID)

	insertNotif := func(key string, requestedAt time.Time, nextAttempt *time.Time, status string) {
		if _, err := pool.Exec(ctx, `
INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, notification_type, channel, title, body,
  status, idempotency_key, request_fingerprint, context, delivery_attempts, next_attempt_at, requested_at
) VALUES (
  $1::uuid, $2, 'cohort', 'reminder', 'local-stub', 't', 'b',
  $3, $4, $4 || ':fp', '{}'::jsonb, 1, $5::timestamptz, $6::timestamptz
)`, testTenantID, testEventID, status, key, nextAttempt, requestedAt); err != nil {
			t.Fatalf("insert %s: %v", key, err)
		}
	}

	// 120 newer queued (scheduling key = requested_at = now-1m) sort ahead of the
	// old failed row (scheduling key = next_attempt_at = now-30s).
	for i := 0; i < 120; i++ {
		insertNotif(fmt.Sprintf("newer-%03d", i), now.Add(-1*time.Minute), nil, "queued")
	}
	oldFailedNext := now.Add(-30 * time.Second)
	insertNotif("old-failed", now.Add(-1*time.Hour), &oldFailedNext, "failed")
	futureNext := now.Add(1 * time.Hour)
	insertNotif("future-retry", now.Add(-2*time.Hour), &futureNext, "failed")

	oldest, found, err := repo.OldestDueRequestedAt(ctx, testTenantID, now)
	if err != nil || !found {
		t.Fatalf("OldestDueRequestedAt: found=%v err=%v", found, err)
	}
	if want := now.Add(-1 * time.Hour); !oldest.Equal(want) {
		t.Fatalf("oldest due requested_at = %v; want %v (the hour-old failed request — not a newer batch row, not the future-excluded 2h row)", oldest, want)
	}
}

// TestNotificationRepositoryOldestDueRequestedAtPlanStaysIndexBoundedUnderSaturation
// is the KERN-04 query-plan gate. OldestDueRequestedAtSQL (ORDER BY requested_at
// ASC LIMIT 1, semantically equivalent to MIN(requested_at) but with early-stop
// optimization via notification_requests_oldest_due_requested_at_idx) must use
// that index to achieve O(1) early-stop behavior: scan the index in ascending
// order of requested_at, return the first row matching the COALESCE filter, and
// stop immediately (LIMIT 1) instead of visiting all backlog rows. This test runs
// at the maximum 5k-50k envelope size (40k backlog) to prove the early-stop works
// under realistic saturation. The seed mirrors production: a large HISTORICAL
// population (sent, no longer due -- most of a live table over time) plus a
// smaller due backlog, so the due predicate is genuinely selective against the
// full table, the condition under which the planner's natural (unforced) choice
// is the partial index. Proven with sequential scans left ENABLED (no enable_seqscan=off).
func TestNotificationRepositoryOldestDueRequestedAtPlanStaysIndexBoundedUnderSaturation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	now := time.Date(2026, 6, 27, 9, 30, 0, 0, time.UTC)
	seedCalendarEvent(t, ctx, pool, testEventID)

	const historical = 300000
	const dueBacklog = 40000
	if _, err := pool.Exec(ctx, `
INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, notification_type, channel, title, body,
  status, idempotency_key, request_fingerprint, context, delivery_attempts, next_attempt_at, requested_at
)
SELECT $1::uuid, $2, 'cohort', 'reminder', 'local-stub', 't', 'b',
  'sent', 'oldest-due-hist-' || g, 'oldest-due-hist-' || g || ':fp', '{}'::jsonb, 1,
  NULL, $3::timestamptz - (g || ' seconds')::interval
FROM generate_series(1, $4::int) g`,
		testTenantID, testEventID, now, historical); err != nil {
		t.Fatalf("seed historical population: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, notification_type, channel, title, body,
  status, idempotency_key, request_fingerprint, context, delivery_attempts, next_attempt_at, requested_at
)
SELECT $1::uuid, $2, 'cohort', 'reminder', 'local-stub', 't', 'b',
  'queued', 'oldest-due-backlog-' || g, 'oldest-due-backlog-' || g || ':fp', '{}'::jsonb, 0,
  NULL, $3::timestamptz - (g || ' seconds')::interval
FROM generate_series(1, $4::int) g`,
		testTenantID, testEventID, now, dueBacklog); err != nil {
		t.Fatalf("seed due backlog: %v", err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE notification_requests`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	// EXPLAIN (ANALYZE) the EXACT production probe query (OldestDueRequestedAtSQL,
	// the same string OldestDueRequestedAt runs).
	var raw []byte
	if err := pool.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)\n"+OldestDueRequestedAtSQL, testTenantID, now).Scan(&raw); err != nil {
		t.Fatalf("explain: %v", err)
	}

	var plans []struct {
		Plan          planNode `json:"Plan"`
		ExecutionTime float64  `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) == 0 {
		t.Fatalf("parse explain json: %v (%s)", err, raw)
	}
	root := plans[0].Plan
	scans := collectRelationScans(root, "notification_requests")
	if len(scans) == 0 {
		t.Fatalf("no notification_requests scan node in plan: %s", raw)
	}
	// (a) Planner must use index path, never Seq Scan of full table. Early-stop on
	// LIMIT 1 ORDER BY requested_at means we only scan ~1 row, not the full backlog.
	sawIndex := anyNodeType(root, "Index")
	for _, sc := range scans {
		if strings.Contains(sc.NodeType, "Seq Scan") {
			t.Fatalf("notification_requests reached via Seq Scan (probe regressed off index): %s", raw)
		}
		if sc.ActualRows > float64(dueBacklog) {
			t.Fatalf("scan read %.0f rows, more than backlog size %d (touched historical): %s", sc.ActualRows, dueBacklog, raw)
		}
	}
	if !sawIndex {
		t.Fatalf("no index access path (fell back to full table scan): %s", raw)
	}

	// (b) CRITICAL: Early-stop behavior. Actual rows read should be ~1 (LIMIT 1 returns
	// after finding first row), not proportional to backlog. This proves the index enables
	// O(1) behavior instead of O(backlog).
	for _, sc := range scans {
		if sc.ActualRows > 10 {
			t.Fatalf("Index Scan read %.0f rows (want ~1 due to LIMIT 1 early-stop); plan: %s",
				sc.ActualRows, raw)
		}
	}

	// (c) Execution time must stay constant, not scale with backlog size. With early-stop
	// at 1 row, latency should be ~constant regardless of backlog size.
	if plans[0].ExecutionTime > 100 {
		t.Fatalf("probe executed in %.1fms (want <100ms); early-stop may not be working", plans[0].ExecutionTime)
	}

	// (d) Buffers stay minimal due to early-stop (just index leaf page + maybe one data page).
	if blocks := root.SharedHit + root.SharedRead; blocks > 50 {
		t.Fatalf("probe touched %.0f shared blocks (want ~1-2 due to early-stop): %s", blocks, raw)
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

func TestNotificationRepositorySuppressInvalidRecipientOverwritesStaleFailureReason(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	now := time.Date(2026, 7, 25, 15, 30, 0, 0, time.UTC)
	token := "dead-fcm-token-review"
	reason := "invalid FCM recipient: NotRegistered"
	var requestID string
	if err := pool.QueryRow(ctx, `
INSERT INTO notification_requests (
  tenant_id, calendar_event_id, target_type, target_id, notification_type, channel,
  recipient_ref, title, body, status, idempotency_key, request_fingerprint, context,
  delivery_attempts, failure_reason, next_attempt_at, trace_id, requested_at
) VALUES (
  $1::uuid, 'verification:item-1', 'verification_item', NULL, 'verification_pending', 'push_fcm',
  $2, 'Video verification waiting', '12 goats vaccinated; video is waiting for verification.',
  'failed', 'invalid-recipient-overwrite', 'invalid-recipient-overwrite:fingerprint', '{}'::jsonb,
  1, 'previous transient timeout', $3::timestamptz, 'trace-invalid-recipient-overwrite',
  TIMESTAMPTZ '2026-07-25 15:00:00+00'
)
RETURNING notification_request_id::text`, testTenantID, token, now.Add(time.Minute)).Scan(&requestID); err != nil {
		t.Fatalf("seed push notification: %v", err)
	}

	suppressed, err := repo.SuppressInvalidRecipient(ctx, testTenantID, token, reason, now)
	if err != nil {
		t.Fatalf("SuppressInvalidRecipient: %v", err)
	}
	if suppressed != 1 {
		t.Fatalf("suppressed rows=%d want 1", suppressed)
	}
	var status, failureReason string
	var nextAttemptCleared bool
	if err := pool.QueryRow(ctx, `
SELECT status, COALESCE(failure_reason, ''), next_attempt_at IS NULL
FROM notification_requests
WHERE tenant_id = $1::uuid AND notification_request_id = $2::uuid`,
		testTenantID, requestID).Scan(&status, &failureReason, &nextAttemptCleared); err != nil {
		t.Fatalf("query suppressed notification: %v", err)
	}
	if status != "suppressed" || failureReason != reason || !nextAttemptCleared {
		t.Fatalf("suppressed notification status=%q failure=%q nextCleared=%t, want suppressed/%q/true",
			status, failureReason, nextAttemptCleared, reason)
	}
}

func TestNotificationRepositorySuppressInvalidRecipientDeactivatesDevice(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	now := time.Date(2026, 7, 25, 15, 30, 0, 0, time.UTC)
	token := "fQJhrzTJTyCkz4T5dZAyLZ:APA91bHmCYIQTBYjnnoEWX2EXd88YMQlSKtoU"
	reason := "FCM: UNREGISTERED"

	// Seed a device with an active token
	var deviceID, tenantID, memberID string
	tenantID = testTenantID
	memberID = "11111111-1111-4000-8000-111111111111"
	if err := pool.QueryRow(ctx, `
INSERT INTO workforce_members (tenant_id, workforce_member_id, display_code, display_name, status)
VALUES ($1::uuid, $2::uuid, 'FCM-PRUNE-1', 'FCM Prune Fixture', 'active')
RETURNING workforce_member_id::text`, tenantID, memberID).Scan(&memberID); err != nil {
		t.Fatalf("seed workforce member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO workforce_member_devices (
  tenant_id, workforce_member_id, platform, app_install_id, fcm_token, app_version, os_version, status
) VALUES (
  $1::uuid, $2::uuid, 'android', 'app-install-1', $3, '1.0.0', '14', 'active'
)
RETURNING device_id::text`, tenantID, memberID, token).Scan(&deviceID); err != nil {
		t.Fatalf("seed device: %v", err)
	}

	// Suppress the invalid FCM token
	suppressed, err := repo.SuppressInvalidRecipient(ctx, tenantID, token, reason, now)
	if err != nil {
		t.Fatalf("SuppressInvalidRecipient: %v", err)
	}
	if suppressed != 0 {
		t.Fatalf("suppressed notification requests=%d want 0", suppressed)
	}

	// Verify the device is now revoked
	var deviceStatus, revokedReason string
	var deviceToken *string
	var revokedAt *time.Time
	var revokedBy *string
	if err := pool.QueryRow(ctx, `
SELECT status, fcm_token, revoked_at, revoked_by, COALESCE(metadata->>'fcm_invalidated_reason', '')
FROM workforce_member_devices
WHERE device_id = $1::uuid`, deviceID).Scan(&deviceStatus, &deviceToken, &revokedAt, &revokedBy, &revokedReason); err != nil {
		t.Fatalf("query device: %v", err)
	}

	if deviceStatus != "revoked" {
		t.Errorf("device status=%q want revoked", deviceStatus)
	}
	if deviceToken != nil {
		t.Errorf("fcm_token=%v want NULL", *deviceToken)
	}
	if revokedAt == nil || !revokedAt.Equal(now) {
		t.Errorf("revoked_at=%v want %v", revokedAt, now)
	}
	if revokedBy != nil {
		t.Errorf("revoked_by=%v want NULL (system action)", revokedBy)
	}
	if revokedReason != reason {
		t.Errorf("metadata.fcm_invalidated_reason=%q want %q", revokedReason, reason)
	}

	// Verify idempotency: second call should be a no-op
	suppressed2, err := repo.SuppressInvalidRecipient(ctx, tenantID, token, reason, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("SuppressInvalidRecipient (second call): %v", err)
	}
	if suppressed2 != 0 {
		t.Errorf("second suppress affected=%d want 0 (idempotent)", suppressed2)
	}

	// Verify the revoked_at is unchanged (not re-set on idempotent call)
	var revokedAtAfterSecond *time.Time
	if err := pool.QueryRow(ctx, `
SELECT revoked_at
FROM workforce_member_devices
WHERE device_id = $1::uuid`, deviceID).Scan(&revokedAtAfterSecond); err != nil {
		t.Fatalf("query device after second suppress: %v", err)
	}
	if revokedAtAfterSecond == nil || !revokedAtAfterSecond.Equal(now) {
		t.Errorf("revoked_at after second suppress=%v want %v (unchanged)", revokedAtAfterSecond, now)
	}
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
