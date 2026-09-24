package postgres

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// A "please verify this" notification is obsolete the moment the item is decided. Before this,
// nothing ever marked them read: 85% of stg's notification history was *.proof.pending.verifier,
// all unread, and the bell badge counted every one of them forever.

func seedPendingNotice(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, itemID, memberID, eventKey, status string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO notification_requests (tenant_id, calendar_event_id, target_type, target_id, notification_type, channel,
  title, body, status, idempotency_key, request_fingerprint, context)
VALUES ($1::uuid, 'verification:'||$2, 'verification_item', $2::uuid, 'verification_pending', 'push_fcm',
  'Video to verify', '', $5, 'autoresolve:'||gen_random_uuid(), 'fp',
  jsonb_build_object('member_id', $3::text, 'event_key', $4::text, 'item_id', $2::text,
                     'message_key', 'weighing.proof.pending.verifier'))`,
		tenantID, itemID, memberID, eventKey, status); err != nil {
		t.Fatalf("seed pending notice: %v", err)
	}
}

func unreadNotices(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, itemID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM notification_requests
WHERE tenant_id=$1::uuid AND calendar_event_id='verification:'||$2 AND read_at IS NULL AND status <> 'read'`,
		tenantID, itemID).Scan(&n); err != nil {
		t.Fatalf("count unread notices: %v", err)
	}
	return n
}

func storedMemberUnread(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, memberID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT COALESCE((SELECT unread_count FROM notification_member_unread_counts WHERE tenant_id=$1::uuid AND member_id=$2), 0)`, tenantID, memberID).Scan(&n); err != nil {
		t.Fatalf("stored unread: %v", err)
	}
	return n
}

func createWeighingItem(t *testing.T, ctx context.Context, repo *Repository, tenantID, refID, key string, submissionID *string) domain.Item {
	t.Helper()
	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID: tenantID, Vertical: "growth", Module: "weighing", Category: "weighing_animal",
		Source:         domain.SourceRef{Module: "weighing", RefType: "weighing_animal_observation", RefID: refID, SubmissionID: submissionID},
		MediaRefs:      []string{"proof-" + key},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "autoresolve:" + key,
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	return created.Item
}

func TestVerdictAutoResolvesPendingNotifications(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	const verifier = "11111111-1111-4111-8111-111111111111"
	const director = "22222222-2222-4222-8222-222222222222"

	// --- single item: approve resolves verifier + leadership notices, queued ones are suppressed ---
	item := createWeighingItem(t, ctx, repo, tenantID, "33333333-3333-4333-8333-333333333301", "single", nil)
	other := createWeighingItem(t, ctx, repo, tenantID, "33333333-3333-4333-8333-333333333302", "other", nil)
	seedPendingNotice(t, ctx, pool, tenantID, item.ItemID, verifier, "verification.item.pending:"+item.ItemID, "sent")
	seedPendingNotice(t, ctx, pool, tenantID, item.ItemID, verifier, "verification.item.pending:"+item.ItemID, "queued") // 2nd phone
	seedPendingNotice(t, ctx, pool, tenantID, item.ItemID, director, "verification.item.pending:"+item.ItemID, "sent")
	seedPendingNotice(t, ctx, pool, tenantID, other.ItemID, verifier, "verification.item.pending:"+other.ItemID, "sent")
	if got := storedMemberUnread(t, ctx, pool, tenantID, verifier); got != 2 {
		t.Fatalf("verifier unread before verdict = %d, want 2", got)
	}

	if _, err := repo.RecordVerdict(ctx, domain.Verdict{
		TenantID: tenantID, ItemID: item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: tenantID, RowVersion: item.RowVersion,
	}); err != nil {
		t.Fatalf("RecordVerdict: %v", err)
	}
	if got := unreadNotices(t, ctx, pool, tenantID, item.ItemID); got != 0 {
		t.Fatalf("decided item still has %d unread pending notices", got)
	}
	if got := unreadNotices(t, ctx, pool, tenantID, other.ItemID); got != 1 {
		t.Fatalf("an UNDECIDED item's notice was resolved: unread=%d, want 1", got)
	}
	var queuedLeft int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM notification_requests WHERE tenant_id=$1::uuid AND calendar_event_id='verification:'||$2 AND status IN ('queued','failed')`, tenantID, item.ItemID).Scan(&queuedLeft); err != nil {
		t.Fatal(err)
	}
	if queuedLeft != 0 {
		t.Fatalf("%d undelivered push(es) for a decided item are still queued to send", queuedLeft)
	}
	if got := storedMemberUnread(t, ctx, pool, tenantID, verifier); got != 1 {
		t.Fatalf("verifier unread after verdict = %d, want 1 (only the undecided item)", got)
	}
	if got := storedMemberUnread(t, ctx, pool, tenantID, director); got != 0 {
		t.Fatalf("director unread after verdict = %d, want 0", got)
	}

	// --- rework also resolves ---
	if _, err := repo.RecordVerdict(ctx, domain.Verdict{
		TenantID: tenantID, ItemID: other.ItemID, Decision: domain.DecisionRejected, Reason: "blurry",
		VerifierID: tenantID, RowVersion: other.RowVersion,
	}); err != nil {
		t.Fatalf("RecordVerdict rework: %v", err)
	}
	if got := unreadNotices(t, ctx, pool, tenantID, other.ItemID); got != 0 {
		t.Fatalf("rework left %d unread pending notices", got)
	}

	// --- submission grain: ONE notice (under the first item's calendar id) covers the whole
	// submission; it stays unread until the LAST item of the submission is decided ---
	sub := "44444444-4444-4444-8444-444444444401"
	first := createWeighingItem(t, ctx, repo, tenantID, "33333333-3333-4333-8333-333333333303", "sub-1", &sub)
	second := createWeighingItem(t, ctx, repo, tenantID, "33333333-3333-4333-8333-333333333304", "sub-2", &sub)
	seedPendingNotice(t, ctx, pool, tenantID, first.ItemID, verifier, "verification.item.pending:submission:"+sub, "sent")
	if _, err := repo.RecordVerdict(ctx, domain.Verdict{TenantID: tenantID, ItemID: first.ItemID, Decision: domain.DecisionApproved, VerifierID: tenantID, RowVersion: first.RowVersion}); err != nil {
		t.Fatal(err)
	}
	if got := unreadNotices(t, ctx, pool, tenantID, first.ItemID); got != 1 {
		t.Fatalf("submission notice resolved while a sibling is still pending: unread=%d, want 1", got)
	}
	if _, err := repo.RecordVerdict(ctx, domain.Verdict{TenantID: tenantID, ItemID: second.ItemID, Decision: domain.DecisionApproved, VerifierID: tenantID, RowVersion: second.RowVersion}); err != nil {
		t.Fatal(err)
	}
	if got := unreadNotices(t, ctx, pool, tenantID, first.ItemID); got != 0 {
		t.Fatalf("submission notice still unread after its last item was decided: %d", got)
	}

	// --- a notice keyed to its OWN item resolves when that item is decided, even while a sibling
	// of its submission is still pending ---
	sub2 := "44444444-4444-4444-8444-444444444402"
	own := createWeighingItem(t, ctx, repo, tenantID, "33333333-3333-4333-8333-333333333306", "sub2-1", &sub2)
	_ = createWeighingItem(t, ctx, repo, tenantID, "33333333-3333-4333-8333-333333333307", "sub2-2", &sub2)
	seedPendingNotice(t, ctx, pool, tenantID, own.ItemID, verifier, "verification.item.pending:"+own.ItemID, "sent")
	if _, err := repo.RecordVerdict(ctx, domain.Verdict{TenantID: tenantID, ItemID: own.ItemID, Decision: domain.DecisionApproved, VerifierID: tenantID, RowVersion: own.RowVersion}); err != nil {
		t.Fatal(err)
	}
	if got := unreadNotices(t, ctx, pool, tenantID, own.ItemID); got != 0 {
		t.Fatalf("per-item notice held back by an open sibling: unread=%d", got)
	}

	// --- withdrawal resolves too ---
	w := createWeighingItem(t, ctx, repo, tenantID, "33333333-3333-4333-8333-333333333305", "withdraw", nil)
	seedPendingNotice(t, ctx, pool, tenantID, w.ItemID, verifier, "verification.item.pending:"+w.ItemID, "sent")
	if _, err := repo.WithdrawItemsBySource(ctx, tenantID, "weighing", "weighing_animal_observation", []string{"33333333-3333-4333-8333-333333333305"}); err != nil {
		t.Fatal(err)
	}
	if got := unreadNotices(t, ctx, pool, tenantID, w.ItemID); got != 0 {
		t.Fatalf("withdrawn item still has %d unread pending notices", got)
	}
	if got := storedMemberUnread(t, ctx, pool, tenantID, verifier); got != 0 {
		t.Fatalf("verifier unread at the end = %d, want 0", got)
	}
}

// TestMultiItemDecisionDoesNotDeadlockWithAMultiMemberProducer reproduces the judge's deadlock:
// a decision transaction that resolved notices item-by-item touched the unread counter in
// SEVERAL statements over different member sets, while the bridge's QueueRoleNotifications -- ONE
// statement fanning out to many members -- locked the same members' counter rows in sorted
// order. Decision (per-item statements: B, then A) vs producer (A then B, one statement) inverts
// the order. The fix is one counter-touching statement per decision, so both lock sorted.
//
// The race is timing-dependent, so it is run many times concurrently; the assertion is on the
// server's own deadlock counter, because the RetryOnDeadlock backstop would otherwise hide a
// regression by re-running the victim.
func TestMultiItemDecisionDoesNotDeadlockWithAMultiMemberProducer(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 10*time.Second)
	tenantID := newTenant(t, ctx, pool)
	members := []string{
		"aaaaaaaa-0000-4000-8000-000000000001", "bbbbbbbb-0000-4000-8000-000000000002",
		"cccccccc-0000-4000-8000-000000000003", "dddddddd-0000-4000-8000-000000000004",
	}
	deadlocks := func() int64 {
		time.Sleep(1200 * time.Millisecond) // let backends flush their stats
		var n int64
		if err := pool.QueryRow(ctx, `SELECT pg_stat_clear_snapshot() IS NULL, deadlocks FROM pg_stat_database WHERE datname = current_database()`).Scan(new(bool), &n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := deadlocks()

	const rounds = 25
	for round := 0; round < rounds; round++ {
		refs := make([]string, len(members))
		for i := range members {
			refs[i] = fmt.Sprintf("55555555-5555-4555-8555-%012d", round*10+i)
			// Visit order is reversed relative to the members' sort order, so a per-item
			// statement sequence locks the counters high -> low.
			item := createWeighingItem(t, ctx, repo, tenantID, refs[i], fmt.Sprintf("dl-%d-%d", round, i), nil)
			seedPendingNotice(t, ctx, pool, tenantID, item.ItemID, members[len(members)-1-i], "verification.item.pending:"+item.ItemID, "sent")
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make(chan error, 8)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := repo.WithdrawItemsBySource(ctx, tenantID, "weighing", "weighing_animal_observation", refs); err != nil {
				errs <- fmt.Errorf("withdraw: %w", err)
			}
		}()
		for p := 0; p < 3; p++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				// The bridge's shape: ONE multi-member INSERT per transaction.
				if _, err := pool.Exec(ctx, `
INSERT INTO notification_requests (tenant_id, calendar_event_id, target_type, notification_type, channel,
  title, body, status, idempotency_key, request_fingerprint, context)
SELECT $1::uuid, 'producer', 'tenant', 'reminder', 'push_fcm', 'P', '', 'queued',
  'producer:'||gen_random_uuid(), 'fp', jsonb_build_object('member_id', m)
FROM unnest($2::text[]) m`, tenantID, members); err != nil {
					errs <- fmt.Errorf("producer: %w", err)
				}
			}()
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("round %d: %v", round, err)
		}
	}
	if after := deadlocks(); after != before {
		t.Fatalf("PostgreSQL detected %d deadlock(s) (a retry may have hidden them); a decision must touch the unread counter in ONE statement", after-before)
	}
}
