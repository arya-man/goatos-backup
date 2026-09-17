package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/notificationcentre/domain"
	"github.com/vgoats/goatos/backend/internal/notificationcentre/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
)

const (
	ncTenant = "00000000-0000-4000-8000-00000000b001"
	// Two DIFFERENT people. The whole module is the promise that these two never see each
	// other's notifications.
	ncUserA = "00000000-0000-4000-8000-00000000b0a1"
	ncUserB = "00000000-0000-4000-8000-00000000b0b1"
	// A third person who has LEFT: an inactive roster row must resolve to nothing.
	ncUserGone = "00000000-0000-4000-8000-00000000b0c1"
)

// seedExternalFacts inserts ONLY external facts: the tenant, three roster rows (two active,
// one left) and their registered phones. A has TWO phones, which is what makes the
// one-row-per-device fan-out real.
//
// Not one notification_requests row is written here. Every row this test reads is produced
// by the PRODUCTION writer -- workforce's ResolveMemberRecipients resolving the reachable
// devices, then calendar's QueueRoleNotifications writing the queue rows -- so the test
// proves this module reads what the real notification path actually writes, including where
// it puts the recipient's identity. A hand-written fixture row could put member_id anywhere
// and would prove nothing.
func seedExternalFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Notification Centre Test', 'active') ON CONFLICT DO NOTHING`, ncTenant); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	people := []struct{ userID, code, name, status string }{
		{ncUserA, "AAA", "Anita", "active"},
		{ncUserB, "BBB", "Bhaskar", "active"},
		{ncUserGone, "CCC", "Chandra", "left"},
	}
	for _, p := range people {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status)
VALUES ($1::uuid, $2::uuid, $3, $4, $5)`, ncTenant, p.userID, p.code, p.name, p.status); err != nil {
			t.Fatalf("seed member %s: %v", p.name, err)
		}
	}
	devices := []struct{ userID, install, token string }{
		{ncUserA, "anita-phone-1", "fcm-anita-1"},
		{ncUserA, "anita-phone-2", "fcm-anita-2"},
		{ncUserB, "bhaskar-phone-1", "fcm-bhaskar-1"},
		{ncUserGone, "chandra-phone-1", "fcm-chandra-1"},
	}
	for _, d := range devices {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_member_devices (tenant_id, workforce_member_id, app_install_id, app_version, status, fcm_token)
SELECT $1::uuid, wm.workforce_member_id, $3, '1.0.0', 'active', $4
FROM workforce_members wm
WHERE wm.tenant_id = $1::uuid AND wm.user_id = $2::uuid`, ncTenant, d.userID, d.install, d.token); err != nil {
			t.Fatalf("seed device %s: %v", d.install, err)
		}
	}
}

// queueFor drives the PRODUCTION notification write for one person: resolve their reachable
// devices the way every consumer does, then queue one notification per device.
func queueFor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID, eventKey, title, body string, extra map[string]string) {
	t.Helper()
	roster := workforcepg.NewRepository(pool, 10*time.Second)
	resolved, err := roster.ResolveMemberRecipients(ctx, ncTenant, userID)
	if err != nil {
		t.Fatalf("resolve recipients for %s: %v", userID, err)
	}
	if len(resolved) == 0 {
		t.Fatalf("no reachable device resolved for %s -- the fixture is wrong, not the code", userID)
	}
	recipients := make([]calendarports.NotificationRecipient, 0, len(resolved))
	for _, r := range resolved {
		recipients = append(recipients, calendarports.NotificationRecipient{
			MemberID: r.WorkforceMemberID, DeviceID: r.DeviceID, FCMToken: r.FCMToken, RoleLabel: "operator",
		})
	}
	calendar := calendarpg.NewRepository(pool, 10*time.Second)
	if _, err := calendar.QueueRoleNotifications(ctx, calendarports.QueueRoleNotifications{
		TenantID:         ncTenant,
		CalendarEventID:  "notificationcentre-test:" + eventKey,
		TargetType:       "tenant",
		NotificationType: "reminder",
		Channel:          "push_fcm",
		Priority:         "normal",
		Title:            title,
		Body:             body,
		TraceID:          "notificationcentre-test",
		EventKey:         eventKey,
		Recipients:       recipients,
		Context:          extra,
	}); err != nil {
		t.Fatalf("queue notifications for %s: %v", userID, err)
	}
}

func unreadRowsFor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM notification_requests nr
JOIN workforce_members wm ON wm.tenant_id = nr.tenant_id AND wm.user_id = $2::uuid
WHERE nr.tenant_id = $1::uuid
  AND nr.context->>'member_id' = wm.workforce_member_id::text
  AND nr.read_at IS NULL`, ncTenant, userID).Scan(&n); err != nil {
		t.Fatalf("count unread rows: %v", err)
	}
	return n
}

// TestNotificationCentreScopesStrictlyToTheCallingUser is the correctness property the whole
// module exists for: A never sees B's notifications, and cannot mark them read either.
func TestNotificationCentreScopesStrictlyToTheCallingUser(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedExternalFacts(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	queueFor(t, ctx, pool, ncUserA, "anita.event.1", "Anita task one", "Only Anita may read this.", map[string]string{
		"message_key": "leadership_task.raised", "screen": "leadership_tasks", "target": "/leadership-tasks",
		"task_id": "77777777-7777-4777-8777-777777777771", "task_no": "11", "group_key": "leadership_task", "status": "open",
	})
	queueFor(t, ctx, pool, ncUserB, "bhaskar.event.1", "Bhaskar task one", "Only Bhaskar may read this.", map[string]string{
		"message_key": "leadership_task.raised", "screen": "leadership_tasks",
	})
	queueFor(t, ctx, pool, ncUserGone, "chandra.event.1", "Chandra task one", "Chandra has left.", nil)

	// --- 1. A's feed is A's rows and nothing else. ---
	pageA, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: ncTenant, MemberOrUserID: ncUserA, Limit: 20})
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if len(pageA.Items) != 1 {
		t.Fatalf("A's feed = %d items, want 1 (A's two phones are ONE notification): %+v", len(pageA.Items), pageA.Items)
	}
	if pageA.Items[0].Title != "Anita task one" {
		t.Errorf("A's item = %q", pageA.Items[0].Title)
	}
	if pageA.Items[0].Context.TaskNo != "11" || pageA.Items[0].Context.Screen != "leadership_tasks" ||
		pageA.Items[0].Context.Target != "/leadership-tasks" || pageA.Items[0].Context.MessageKey != "leadership_task.raised" ||
		pageA.Items[0].Context.GroupKey != "leadership_task" || pageA.Items[0].Context.Status != "open" ||
		pageA.Items[0].Context.Priority != "normal" || pageA.Items[0].Context.TaskID == "" {
		t.Errorf("A's routing context = %+v", pageA.Items[0].Context)
	}
	if pageA.UnreadCount != 1 {
		t.Errorf("A's unread = %d, want 1", pageA.UnreadCount)
	}

	// --- 2. B's feed is B's rows and nothing else. ---
	pageB, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: ncTenant, MemberOrUserID: ncUserB, Limit: 20})
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(pageB.Items) != 1 || pageB.Items[0].Title != "Bhaskar task one" {
		t.Fatalf("B's feed = %+v", pageB.Items)
	}

	// --- 3. The two feeds are DISJOINT: no id crosses over. ---
	aIDs := map[string]bool{}
	for _, item := range pageA.Items {
		aIDs[item.NotificationRequestID] = true
	}
	for _, item := range pageB.Items {
		if aIDs[item.NotificationRequestID] {
			t.Fatalf("id %s appears in BOTH feeds", item.NotificationRequestID)
		}
		if item.Title == "Anita task one" {
			t.Fatalf("B can read A's notification: %+v", item)
		}
	}

	// --- 4. A person who has LEFT resolves to nothing: an empty feed, never an unfiltered one. ---
	pageGone, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: ncTenant, MemberOrUserID: ncUserGone, Limit: 20})
	if err != nil {
		t.Fatalf("list inactive: %v", err)
	}
	if len(pageGone.Items) != 0 || pageGone.UnreadCount != 0 {
		t.Fatalf("an inactive member must see NOTHING, got %d items / %d unread", len(pageGone.Items), pageGone.UnreadCount)
	}

	// --- 5. An unknown caller in a real tenant also sees nothing (fail closed). ---
	pageStranger, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: ncTenant, MemberOrUserID: "00000000-0000-4000-8000-0000000000ff", Limit: 20})
	if err != nil {
		t.Fatalf("list stranger: %v", err)
	}
	if len(pageStranger.Items) != 0 {
		t.Fatalf("an unresolvable caller must see NOTHING, got %+v", pageStranger.Items)
	}

	// --- 6. A CANNOT mark B's notification read. ---
	bID := pageB.Items[0].NotificationRequestID
	bUnreadBefore := unreadRowsFor(t, ctx, pool, ncUserB)
	readCount, err := repo.MarkRead(ctx, ports.MarkReadParams{
		TenantID: ncTenant, MemberOrUserID: ncUserA, IDs: []string{bID}, IdempotencyKey: "a-tries-bs-row",
	})
	if err != nil {
		t.Fatalf("A marking B's id must be a no-op, not an error: %v", err)
	}
	if readCount != 0 {
		t.Errorf("A marked %d of B's notifications read; must be 0", readCount)
	}
	if after := unreadRowsFor(t, ctx, pool, ncUserB); after != bUnreadBefore {
		t.Fatalf("A's mark-as-read TOUCHED B's rows: B had %d unread, now %d", bUnreadBefore, after)
	}
	stillB, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: ncTenant, MemberOrUserID: ncUserB, Limit: 20})
	if err != nil {
		t.Fatalf("re-list B: %v", err)
	}
	if stillB.UnreadCount != 1 || stillB.Items[0].ReadAt != "" {
		t.Fatalf("B's notification was marked read by A: unread=%d read_at=%q", stillB.UnreadCount, stillB.Items[0].ReadAt)
	}

	// --- 7. A CAN mark their own, and both of A's device rows are stamped together, so the
	// badge cannot disagree with the list. ---
	aID := pageA.Items[0].NotificationRequestID
	if aUnread := unreadRowsFor(t, ctx, pool, ncUserA); aUnread != 2 {
		t.Fatalf("A should have 2 unread DELIVERY rows (two phones), got %d", aUnread)
	}
	readCount, err = repo.MarkRead(ctx, ports.MarkReadParams{
		TenantID: ncTenant, MemberOrUserID: ncUserA, IDs: []string{aID}, IdempotencyKey: "a-marks-own-row",
	})
	if err != nil {
		t.Fatalf("A marking own id: %v", err)
	}
	if readCount != 1 {
		t.Errorf("read_count = %d, want 1 notification (not 2 delivery rows)", readCount)
	}
	if aUnread := unreadRowsFor(t, ctx, pool, ncUserA); aUnread != 0 {
		t.Errorf("both of A's device rows must be stamped, %d left unread", aUnread)
	}
	afterA, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: ncTenant, MemberOrUserID: ncUserA, Limit: 20})
	if err != nil {
		t.Fatalf("re-list A: %v", err)
	}
	if afterA.UnreadCount != 0 {
		t.Errorf("A's unread = %d, want 0", afterA.UnreadCount)
	}
	if afterA.Items[0].ReadAt == "" {
		t.Error("A's item must carry read_at once marked")
	}

	// --- 8. A mixed batch marks only the caller's own half. ---
	queueFor(t, ctx, pool, ncUserA, "anita.event.2", "Anita task two", "Second one.", nil)
	pageA2, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: ncTenant, MemberOrUserID: ncUserA, Limit: 20})
	if err != nil {
		t.Fatalf("list A again: %v", err)
	}
	var newAID string
	for _, item := range pageA2.Items {
		if item.Title == "Anita task two" {
			newAID = item.NotificationRequestID
		}
	}
	if newAID == "" {
		t.Fatal("A's second notification is missing from A's own feed")
	}
	readCount, err = repo.MarkRead(ctx, ports.MarkReadParams{
		TenantID: ncTenant, MemberOrUserID: ncUserA, IDs: []string{newAID, bID}, IdempotencyKey: "a-mixed-batch",
	})
	if err != nil {
		t.Fatalf("mixed batch: %v", err)
	}
	if readCount != 1 {
		t.Errorf("a mixed batch marked %d; only A's own notification may count", readCount)
	}
	if after := unreadRowsFor(t, ctx, pool, ncUserB); after != bUnreadBefore {
		t.Fatalf("a mixed batch reached B's rows: %d unread, want %d", after, bUnreadBefore)
	}
}

// TestNotificationCentreKeysetPagesWithoutDuplicatesOrGaps walks the cursor over a feed
// whose rows share an instant, which is exactly where a timestamp-only cursor loses rows.
func TestNotificationCentreKeysetPagesWithoutDuplicatesOrGaps(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedExternalFacts(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	const total = 7
	for i := 0; i < total; i++ {
		queueFor(t, ctx, pool, ncUserA, "anita.page."+string(rune('a'+i)), "Page item", "Body", nil)
	}

	seen := make(map[string]int, total)
	order := make([]string, 0, total)
	cursor := ""
	pages := 0
	for {
		page, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: ncTenant, MemberOrUserID: ncUserA, Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		pages++
		if pages > total+2 {
			t.Fatal("paging did not terminate")
		}
		for _, item := range page.Items {
			seen[item.NotificationRequestID]++
			order = append(order, item.RequestedAt)
		}
		// unread_count is the WHOLE feed on every page, never the page's own rows.
		if page.UnreadCount != total {
			t.Errorf("page %d unread_count = %d, want the whole-feed %d", pages, page.UnreadCount, total)
		}
		if page.NextCursor == "" {
			break
		}
		if len(page.Items) != 2 {
			t.Errorf("page %d returned %d items with a next cursor", pages, len(page.Items))
		}
		cursor = page.NextCursor
	}
	if len(seen) != total {
		t.Fatalf("paged %d distinct notifications, want %d (gap or duplicate)", len(seen), total)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("notification %s appeared %d times across pages", id, n)
		}
	}
	for i := 1; i < len(order); i++ {
		if order[i] > order[i-1] {
			t.Errorf("feed is not newest-first at %d: %s then %s", i, order[i-1], order[i])
		}
	}

	// A cursor that is not a cursor this feed minted is refused, never served as page one.
	for _, bad := range []string{"not-base64!!", "eyJ9", "eyJyZXF1ZXN0ZWRfYXQiOiIifQ"} {
		if _, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: ncTenant, MemberOrUserID: ncUserA, Limit: 2, Cursor: bad}); !errors.Is(err, domain.ErrInvalidCursor) {
			t.Errorf("cursor %q gave %v, want ErrInvalidCursor", bad, err)
		}
	}
}

// TestNotificationCentreMarkReadIsIdempotent pins the Idempotency-Key contract: an exact
// replay returns the ORIGINAL count with no second write, and the same key re-presented with
// a DIFFERENT set is refused rather than applied.
func TestNotificationCentreMarkReadIsIdempotent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedExternalFacts(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	queueFor(t, ctx, pool, ncUserA, "anita.idem.1", "One", "Body", nil)
	queueFor(t, ctx, pool, ncUserA, "anita.idem.2", "Two", "Body", nil)
	page, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: ncTenant, MemberOrUserID: ncUserA, Limit: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("feed = %d items", len(page.Items))
	}
	first, second := page.Items[0].NotificationRequestID, page.Items[1].NotificationRequestID

	got, err := repo.MarkRead(ctx, ports.MarkReadParams{TenantID: ncTenant, MemberOrUserID: ncUserA, IDs: []string{first}, IdempotencyKey: "idem-key-1"})
	if err != nil || got != 1 {
		t.Fatalf("first mark = %d, %v", got, err)
	}
	// Exact replay: the original answer, and nothing new marked.
	replay, err := repo.MarkRead(ctx, ports.MarkReadParams{TenantID: ncTenant, MemberOrUserID: ncUserA, IDs: []string{first}, IdempotencyKey: "idem-key-1"})
	if err != nil || replay != 1 {
		t.Fatalf("replay = %d, %v; want the original 1", replay, err)
	}
	// Same key, DIFFERENT set: refused, and the other notification stays unread.
	if _, err := repo.MarkRead(ctx, ports.MarkReadParams{TenantID: ncTenant, MemberOrUserID: ncUserA, IDs: []string{second}, IdempotencyKey: "idem-key-1"}); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same key with a different set gave %v, want ErrIdempotencyConflict", err)
	}
	after, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: ncTenant, MemberOrUserID: ncUserA, Limit: 20})
	if err != nil {
		t.Fatalf("re-list: %v", err)
	}
	if after.UnreadCount != 1 {
		t.Errorf("unread = %d, want 1: the refused call must not have marked anything", after.UnreadCount)
	}

	// A fresh key for the second notification clears the feed.
	got, err = repo.MarkRead(ctx, ports.MarkReadParams{TenantID: ncTenant, MemberOrUserID: ncUserA, IDs: []string{second}, IdempotencyKey: "idem-key-2"})
	if err != nil || got != 1 {
		t.Fatalf("second mark = %d, %v", got, err)
	}
	// Re-marking an ALREADY read notification under a fresh key moves nothing.
	got, err = repo.MarkRead(ctx, ports.MarkReadParams{TenantID: ncTenant, MemberOrUserID: ncUserA, IDs: []string{first, second}, IdempotencyKey: "idem-key-3"})
	if err != nil {
		t.Fatalf("re-mark: %v", err)
	}
	if got != 0 {
		t.Errorf("re-marking already-read notifications = %d, want 0", got)
	}
	final, err := repo.ListNotifications(ctx, ports.ListParams{TenantID: ncTenant, MemberOrUserID: ncUserA, Limit: 20})
	if err != nil {
		t.Fatalf("final list: %v", err)
	}
	if final.UnreadCount != 0 {
		t.Errorf("final unread = %d, want 0", final.UnreadCount)
	}
	for _, item := range final.Items {
		if item.ReadAt == "" {
			t.Errorf("item %s has no read_at", item.NotificationRequestID)
		}
	}
}
