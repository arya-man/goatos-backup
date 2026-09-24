package postgres

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/notificationcentre/domain"
	"github.com/vgoats/goatos/backend/internal/notificationcentre/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// The stored unread counter (migration 000403) must equal the legacy whole-history
// COUNT(DISTINCT dedupe key) for every member, after any sequence of writes by any writer:
// the production queue insert, raw inserts (read, unread, member-less, duplicate delivery rows),
// MarkRead, the calendar escalation-style partial UPDATE, a DELETE, and concurrent writers.

const counterTenant2 = "00000000-0000-4000-8000-00000000b002"

// legacyUnread is sqlUnreadCountLegacy for one resolved member, the parity oracle.
func legacyUnread(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, memberID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sqlUnreadCountLegacy, tenantID, memberID).Scan(&n); err != nil {
		t.Fatalf("legacy unread: %v", err)
	}
	return n
}

// storedUnread reads the counter row directly (absent row = 0).
func storedUnread(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, memberID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `
SELECT COALESCE((SELECT unread_count FROM notification_member_unread_counts
                 WHERE tenant_id = $1::uuid AND member_id = $2), 0)`, tenantID, memberID).Scan(&n); err != nil {
		t.Fatalf("stored unread: %v", err)
	}
	return n
}

// counterMembers seeds n active members in a tenant and returns their workforce_member_ids.
func counterMembers(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, n int) []string {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Counter '||$1, 'active') ON CONFLICT DO NOTHING`, tenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status)
VALUES ($1::uuid, gen_random_uuid(), $2, $3, 'active')
RETURNING workforce_member_id::text`, tenantID, fmt.Sprintf("C%03d", i), fmt.Sprintf("Counter %d", i)).Scan(&id); err != nil {
			t.Fatalf("seed member: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

// rawInsert writes delivery rows the way any producer could: `devices` rows sharing one event key
// (or no event key), optionally already read.
func rawInsert(ctx context.Context, pool *pgxpool.Pool, tenantID, memberID, eventKey string, devices int, read bool) error {
	status, readAt := "queued", "NULL"
	if read {
		status, readAt = "read", "now()"
	}
	_, err := pool.Exec(ctx, `
INSERT INTO notification_requests (tenant_id, calendar_event_id, target_type, notification_type, channel,
  title, body, status, read_at, idempotency_key, request_fingerprint, context)
SELECT $1::uuid, 'counter-test', 'tenant', 'reminder', 'push_fcm', 'Counter', '', $4, `+readAt+`,
  'counter:'||gen_random_uuid(), 'fp',
  CASE WHEN $2 = '' THEN '{}'::jsonb
       WHEN $3 = '' THEN jsonb_build_object('member_id', $2::text)
       ELSE jsonb_build_object('member_id', $2::text, 'event_key', $3::text) END
FROM generate_series(1, $5::int)`, tenantID, memberID, eventKey, status, devices)
	return err
}

func assertParity(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, members []string, when string) {
	t.Helper()
	for _, m := range members {
		legacy, stored := legacyUnread(t, ctx, pool, tenantID, m), storedUnread(t, ctx, pool, tenantID, m)
		if legacy != stored {
			t.Fatalf("%s: member %s stored unread=%d, legacy COUNT=%d", when, m, stored, legacy)
		}
	}
}

// pageIDs lists one member's feed ids (newest first) via the production read.
func pageIDs(t *testing.T, ctx context.Context, repo *Repository, tenantID, memberID string) []string {
	t.Helper()
	page, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: tenantID, MemberOrUserID: memberID, Limit: domain.MaxLimit})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ids := make([]string, 0, len(page.Items))
	for _, it := range page.Items {
		ids = append(ids, it.NotificationRequestID)
	}
	return ids
}

func TestUnreadCounterParityAcrossWritersAndMembers(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedExternalFacts(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	// Production writer path first: A has two phones, so one transition = two rows = one unread.
	queueFor(t, ctx, pool, ncUserA, "counter.prod.1", "Prod one", "body", nil)
	queueFor(t, ctx, pool, ncUserA, "counter.prod.1", "Prod one replay", "body", nil) // idempotent replay
	queueFor(t, ctx, pool, ncUserB, "counter.prod.2", "Prod two", "body", nil)
	var memberA, memberB string
	if err := pool.QueryRow(ctx, `SELECT workforce_member_id::text FROM workforce_members WHERE tenant_id=$1::uuid AND user_id=$2::uuid`, ncTenant, ncUserA).Scan(&memberA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT workforce_member_id::text FROM workforce_members WHERE tenant_id=$1::uuid AND user_id=$2::uuid`, ncTenant, ncUserB).Scan(&memberB); err != nil {
		t.Fatal(err)
	}
	if got := storedUnread(t, ctx, pool, ncTenant, memberA); got != 1 {
		t.Fatalf("stored unread for A after production write = %d, want 1 (two phones, one notification)", got)
	}
	assertParity(t, ctx, pool, ncTenant, []string{memberA, memberB}, "after production writes")

	// The read path is the stored number: corrupting it must show up in the page. This is what
	// proves ListNotifications no longer runs the history aggregate.
	if _, err := pool.Exec(ctx, `UPDATE notification_member_unread_counts SET unread_count = 42 WHERE tenant_id=$1::uuid AND member_id=$2`, ncTenant, memberA); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: ncTenant, MemberOrUserID: ncUserA, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.UnreadCount != 42 {
		t.Fatalf("ListNotifications unread=%d, want the stored 42 (it must read the counter row, not recount history)", page.UnreadCount)
	}
	if _, err := pool.Exec(ctx, `UPDATE notification_member_unread_counts SET unread_count = 1 WHERE tenant_id=$1::uuid AND member_id=$2`, ncTenant, memberA); err != nil {
		t.Fatal(err)
	}

	// Randomised single-writer sequence across two tenants and many members.
	members1 := counterMembers(t, ctx, pool, ncTenant, 8)
	members2 := counterMembers(t, ctx, pool, counterTenant2, 6)
	rng := rand.New(rand.NewSource(20260924))
	type tm struct{ tenant, member string }
	all := make([]tm, 0, 14)
	for _, m := range members1 {
		all = append(all, tm{ncTenant, m})
	}
	for _, m := range members2 {
		all = append(all, tm{counterTenant2, m})
	}
	for step := 0; step < 400; step++ {
		pick := all[rng.Intn(len(all))]
		key := fmt.Sprintf("k%d", rng.Intn(12)) // small key space so keys recur after being read
		switch op := rng.Intn(10); {
		case op < 4: // unread delivery rows, 1-3 devices
			if err := rawInsert(ctx, pool, pick.tenant, pick.member, key, 1+rng.Intn(3), false); err != nil {
				t.Fatal(err)
			}
		case op == 4: // producer with no event key: row groups with itself
			if err := rawInsert(ctx, pool, pick.tenant, pick.member, "", 1, false); err != nil {
				t.Fatal(err)
			}
		case op == 5: // row inserted already read
			if err := rawInsert(ctx, pool, pick.tenant, pick.member, key, 1, true); err != nil {
				t.Fatal(err)
			}
		case op == 6: // member-less system row: never counted for anyone
			if err := rawInsert(ctx, pool, pick.tenant, "", key, 1, false); err != nil {
				t.Fatal(err)
			}
		case op == 7: // MarkRead through the production write, on up to 3 ids of the feed
			ids := pageIDs(t, ctx, repo, pick.tenant, pick.member)
			rng.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
			if len(ids) > 3 {
				ids = ids[:3]
			}
			if len(ids) == 0 {
				continue
			}
			if _, err := repo.MarkRead(ctx, ports.MarkReadParams{TenantID: pick.tenant, MemberOrUserID: pick.member, IDs: ids, IdempotencyKey: fmt.Sprintf("counter-%d", step)}); err != nil {
				t.Fatal(err)
			}
		case op == 8: // escalation-style partial read: ONE delivery row of a key, not its siblings
			if _, err := pool.Exec(ctx, `
UPDATE notification_requests SET read_at = now(), status = 'read'
WHERE notification_request_id = (
  SELECT notification_request_id FROM notification_requests
  WHERE tenant_id=$1::uuid AND context->>'member_id'=$2 AND read_at IS NULL
  ORDER BY requested_at LIMIT 1)`, pick.tenant, pick.member); err != nil {
				t.Fatal(err)
			}
		default: // retention-style delete of some rows
			if _, err := pool.Exec(ctx, `
DELETE FROM notification_requests
WHERE notification_request_id IN (
  SELECT notification_request_id FROM notification_requests
  WHERE tenant_id=$1::uuid AND context->>'member_id'=$2 ORDER BY random() LIMIT 2)`, pick.tenant, pick.member); err != nil {
				t.Fatal(err)
			}
		}
		if step%50 == 0 {
			assertParity(t, ctx, pool, ncTenant, members1, fmt.Sprintf("step %d", step))
			assertParity(t, ctx, pool, counterTenant2, members2, fmt.Sprintf("step %d", step))
		}
	}
	assertParity(t, ctx, pool, ncTenant, append(members1, memberA, memberB), "after random sequence")
	assertParity(t, ctx, pool, counterTenant2, members2, "after random sequence")

	// MarkRead replay with the same key is a no-op and never double-decrements.
	ids := pageIDs(t, ctx, repo, ncTenant, members1[0])
	if len(ids) > 0 {
		p := ports.MarkReadParams{TenantID: ncTenant, MemberOrUserID: members1[0], IDs: ids[:1], IdempotencyKey: "replay-key"}
		if _, err := repo.MarkRead(ctx, p); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.MarkRead(ctx, p); err != nil {
			t.Fatal(err)
		}
		assertParity(t, ctx, pool, ncTenant, members1[:1], "after replayed MarkRead")
	}
}

// TestUnreadCounterParityUnderConcurrentWriters races producers against MarkRead on the SAME
// member and the SAME keys: a mark-read that commits while a new delivery row for that key is in
// flight must never delete a key that is still unread.
func TestUnreadCounterParityUnderConcurrentWriters(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 30*time.Second)
	members := counterMembers(t, ctx, pool, counterTenant2, 3)

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(w)))
			for i := 0; i < 60; i++ {
				m := members[rng.Intn(len(members))]
				key := fmt.Sprintf("hot%d", rng.Intn(4))
				if w%2 == 0 {
					if err := rawInsert(ctx, pool, counterTenant2, m, key, 1+rng.Intn(2), false); err != nil {
						errs <- err
						return
					}
					continue
				}
				page, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: counterTenant2, MemberOrUserID: m, Limit: 10})
				if err != nil {
					errs <- err
					return
				}
				if len(page.Items) == 0 {
					continue
				}
				ids := []string{page.Items[rng.Intn(len(page.Items))].NotificationRequestID}
				if _, err := repo.MarkRead(ctx, ports.MarkReadParams{TenantID: counterTenant2, MemberOrUserID: m, IDs: ids, IdempotencyKey: fmt.Sprintf("c-%d-%d", w, i)}); err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent writer: %v", err)
	}
	assertParity(t, ctx, pool, counterTenant2, members, "after concurrent writers")
}

// TestUnreadCounterBackfillAndGate: rows that existed BEFORE the trigger (simulated by
// clearing the counter tables and reopening the gate) are counted by the legacy query until the
// batched backfill completes, and by the stored counter after it, with equal numbers.
func TestUnreadCounterBackfillAndGate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 10*time.Second)
	members := counterMembers(t, ctx, pool, counterTenant2, 4)
	for i, m := range members {
		for k := 0; k < 7+i*5; k++ {
			if err := rawInsert(ctx, pool, counterTenant2, m, fmt.Sprintf("bk%d", k%(5+i)), 2, k%4 == 0); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Pretend these rows predate 000403.
	if _, err := pool.Exec(ctx, `
DELETE FROM notification_member_unread_keys;
DELETE FROM notification_member_unread_counts;
UPDATE notification_unread_counter_state SET backfill_complete_at = NULL`); err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		page, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: counterTenant2, MemberOrUserID: m, Limit: 5})
		if err != nil {
			t.Fatal(err)
		}
		if want := legacyUnread(t, ctx, pool, counterTenant2, m); page.UnreadCount != want {
			t.Fatalf("before backfill, gate closed: unread=%d, want legacy %d", page.UnreadCount, want)
		}
	}
	// A write lands mid-migration; the trigger tracks it and the backfill must not double it.
	if err := rawInsert(ctx, pool, counterTenant2, members[0], "bk0", 1, false); err != nil {
		t.Fatal(err)
	}
	stats, err := repo.BackfillUnreadCounters(ctx, 3)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if stats.Members < len(members) {
		t.Fatalf("backfill visited %d members, want >= %d", stats.Members, len(members))
	}
	assertParity(t, ctx, pool, counterTenant2, members, "after backfill")
	// Re-running is a no-op.
	if _, err := repo.BackfillUnreadCounters(ctx, 3); err != nil {
		t.Fatal(err)
	}
	assertParity(t, ctx, pool, counterTenant2, members, "after second backfill")
	mismatches, err := repo.ReconcileUnreadCounters(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(mismatches) != 0 {
		t.Fatalf("reconciler found mismatches after backfill: %+v", mismatches)
	}
	// Gate now open: the page reads the stored row.
	if _, err := pool.Exec(ctx, `UPDATE notification_member_unread_counts SET unread_count = unread_count + 100 WHERE tenant_id=$1::uuid AND member_id=$2`, counterTenant2, members[1]); err != nil {
		t.Fatal(err)
	}
	page, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: counterTenant2, MemberOrUserID: members[1], Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if page.UnreadCount < 100 {
		t.Fatalf("after backfill the page must read the stored counter, got %d", page.UnreadCount)
	}
	mismatches, err = repo.ReconcileUnreadCounters(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(mismatches) != 1 || mismatches[0].MemberID != members[1] {
		t.Fatalf("reconciler must flag the corrupted member, got %+v", mismatches)
	}
}

// TestListNotificationsCapsPageSizeServerSide: the adapter enforces MaxLimit even if a caller
// bypasses the service's ClampLimit (P9).
func TestListNotificationsCapsPageSizeServerSide(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 10*time.Second)
	members := counterMembers(t, ctx, pool, counterTenant2, 1)
	for k := 0; k < domain.MaxLimit+10; k++ {
		if err := rawInsert(ctx, pool, counterTenant2, members[0], fmt.Sprintf("cap%d", k), 1, false); err != nil {
			t.Fatal(err)
		}
	}
	page, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: counterTenant2, MemberOrUserID: members[0], Limit: 10000})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != domain.MaxLimit || page.NextCursor == "" {
		t.Fatalf("page size=%d cursor=%q, want %d items and a next cursor", len(page.Items), page.NextCursor, domain.MaxLimit)
	}
}

// TestUnreadCounterMarkReadRacingANewDeliveryOfTheSameKey pins the one interleaving a
// delta-only design gets wrong: a producer has inserted (not yet committed) a new unread delivery
// row for key K while the reader marks K read. MarkRead cannot see the in-flight row, so it does
// not stamp it; after both commit K is still unread and must still be counted.
func TestUnreadCounterMarkReadRacingANewDeliveryOfTheSameKey(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 30*time.Second)
	m := counterMembers(t, ctx, pool, counterTenant2, 1)[0]
	if err := rawInsert(ctx, pool, counterTenant2, m, "raceK", 1, false); err != nil {
		t.Fatal(err)
	}
	ids := pageIDs(t, ctx, repo, counterTenant2, m)

	producer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = producer.Rollback(ctx) }()
	if _, err := producer.Exec(ctx, `
INSERT INTO notification_requests (tenant_id, calendar_event_id, target_type, notification_type, channel,
  title, body, status, idempotency_key, request_fingerprint, context)
VALUES ($1::uuid, 'counter-test', 'tenant', 'reminder', 'push_fcm', 'Race', '', 'queued',
  'race:'||gen_random_uuid(), 'fp', jsonb_build_object('member_id', $2::text, 'event_key', 'raceK'))`, counterTenant2, m); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := repo.MarkRead(ctx, ports.MarkReadParams{TenantID: counterTenant2, MemberOrUserID: m, IDs: ids, IdempotencyKey: "race"})
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	if err := producer.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if legacy := legacyUnread(t, ctx, pool, counterTenant2, m); legacy != 1 {
		t.Fatalf("fixture: legacy unread=%d, want 1 (the in-flight row was not visible to MarkRead)", legacy)
	}
	assertParity(t, ctx, pool, counterTenant2, []string{m}, "after mark-read raced a new delivery")
}
