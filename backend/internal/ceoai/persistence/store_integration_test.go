package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	actorA1 = "00000000-0000-4000-8000-00000000a101"
	actorA2 = "00000000-0000-4000-8000-00000000a102"
	actorB1 = "00000000-0000-4000-8000-00000000b101"
)

// newTestStores boots an isolated migrated Postgres (real ceo_ai_* tables from
// migrations 000021+000022) and seeds two tenant rows for the FK. Returns the
// conversation store, pool, and the two tenant ids. Skipped unless pgtest opt-in + Docker.
func newTestStores(t *testing.T, ctx context.Context) (*PostgresConversationStore, *pgxpool.Pool, string, string) {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	pool := pgtest.StartPostgres(t, ctx)
	tenantA := insertTenant(t, ctx, pool, "Tenant A")
	tenantB := insertTenant(t, ctx, pool, "Tenant B")
	return NewPostgresConversationStore(pool, 5*time.Second), pool, tenantA, tenantB
}

func insertTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO tenants (tenant_id, name, status) VALUES (gen_random_uuid(), $1, 'active') RETURNING tenant_id::text`,
		name).Scan(&id); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	return id
}

func TestConversationCreateGetAndActorScope(t *testing.T) {
	ctx := context.Background()
	conv, _, tenantA, tenantB := newTestStores(t, ctx)

	c, created, err := conv.Create(ctx, NewConversation{TenantID: tenantA, ActorID: actorA1, Title: "Morning review"})
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	if c.Title != "Morning review" {
		t.Fatalf("title=%q", c.Title)
	}

	got, err := conv.Get(ctx, tenantA, actorA1, c.ID)
	if err != nil || got.ID != c.ID {
		t.Fatalf("get own: %+v err=%v", got, err)
	}

	// Cross-actor within same tenant: A2 cannot read A1's thread.
	if _, err := conv.Get(ctx, tenantA, actorA2, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-actor read: err=%v, want ErrNotFound", err)
	}
	// Cross-tenant: B cannot read A's thread.
	if _, err := conv.Get(ctx, tenantB, actorB1, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant read: err=%v, want ErrNotFound", err)
	}
}

func TestConversationCreateIdempotent(t *testing.T) {
	ctx := context.Background()
	conv, _, tenantA, _ := newTestStores(t, ctx)

	first, created1, err := conv.Create(ctx, NewConversation{TenantID: tenantA, ActorID: actorA1, Title: "T", IdempotencyKey: "k-1"})
	if err != nil || !created1 {
		t.Fatalf("first create: created=%v err=%v", created1, err)
	}
	second, created2, err := conv.Create(ctx, NewConversation{TenantID: tenantA, ActorID: actorA1, Title: "T", IdempotencyKey: "k-1"})
	if err != nil {
		t.Fatalf("replay create: %v", err)
	}
	if created2 {
		t.Fatal("replay must not create a second thread")
	}
	if second.ID != first.ID {
		t.Fatalf("replay returned different thread: %s vs %s", second.ID, first.ID)
	}
}

func TestConversationListKeysetPagination(t *testing.T) {
	ctx := context.Background()
	conv, _, tenantA, _ := newTestStores(t, ctx)

	ids := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		c, _, err := conv.Create(ctx, NewConversation{TenantID: tenantA, ActorID: actorA1})
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if _, err := conv.AppendMessage(ctx, NewMessage{ConversationID: c.ID, TenantID: tenantA, ActorID: actorA1, Role: RoleUser, Content: "hi"}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		ids = append(ids, c.ID)
		time.Sleep(2 * time.Millisecond) // ensure distinct updated_at ordering
	}

	page1, err := conv.List(ctx, ListConversationsQuery{TenantID: tenantA, ActorID: actorA1, PageSize: 2})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Items) != 2 || !page1.HasMore || page1.NextCursor == nil {
		t.Fatalf("page1=%+v", page1)
	}
	page2, err := conv.List(ctx, ListConversationsQuery{TenantID: tenantA, ActorID: actorA1, PageSize: 2, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	seen := map[string]bool{}
	for _, it := range append(append([]Conversation{}, page1.Items...), page2.Items...) {
		if seen[it.ID] {
			t.Fatalf("duplicate across pages: %s", it.ID)
		}
		seen[it.ID] = true
	}
	page3, err := conv.List(ctx, ListConversationsQuery{TenantID: tenantA, ActorID: actorA1, PageSize: 2, Cursor: page2.NextCursor})
	if err != nil {
		t.Fatalf("page3: %v", err)
	}
	for _, it := range page3.Items {
		seen[it.ID] = true
	}
	if len(seen) != 5 {
		t.Fatalf("saw %d distinct threads across pages, want 5", len(seen))
	}
	if page1.Items[0].ID != ids[4] {
		t.Fatalf("page1[0]=%s, want newest %s", page1.Items[0].ID, ids[4])
	}
}

func TestConversationListExcludesOtherActorsAndDeleted(t *testing.T) {
	ctx := context.Background()
	conv, _, tenantA, _ := newTestStores(t, ctx)

	mine, _, _ := conv.Create(ctx, NewConversation{TenantID: tenantA, ActorID: actorA1})
	_, _, _ = conv.Create(ctx, NewConversation{TenantID: tenantA, ActorID: actorA2}) // other actor
	del, _, _ := conv.Create(ctx, NewConversation{TenantID: tenantA, ActorID: actorA1})
	if err := conv.SoftDelete(ctx, tenantA, actorA1, del.ID); err != nil {
		t.Fatalf("soft-delete: %v", err)
	}

	page, err := conv.List(ctx, ListConversationsQuery{TenantID: tenantA, ActorID: actorA1, PageSize: 20})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != mine.ID {
		t.Fatalf("list=%+v, want only my live thread %s", page.Items, mine.ID)
	}
	// A soft-deleted thread is not gettable.
	if _, err := conv.Get(ctx, tenantA, actorA1, del.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted: err=%v, want ErrNotFound", err)
	}
}

func TestConversationRenameArchiveDeleteScoped(t *testing.T) {
	ctx := context.Background()
	conv, _, tenantA, tenantB := newTestStores(t, ctx)
	c, _, _ := conv.Create(ctx, NewConversation{TenantID: tenantA, ActorID: actorA1, Title: "orig"})

	// Wrong actor / tenant cannot rename/delete.
	if _, err := conv.Rename(ctx, tenantA, actorA2, c.ID, "hijack"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-actor rename err=%v", err)
	}
	if err := conv.SoftDelete(ctx, tenantB, actorB1, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant delete err=%v", err)
	}

	renamed, err := conv.Rename(ctx, tenantA, actorA1, c.ID, "updated")
	if err != nil || renamed.Title != "updated" {
		t.Fatalf("rename: %+v err=%v", renamed, err)
	}
	arch, err := conv.Archive(ctx, tenantA, actorA1, c.ID)
	if err != nil || arch.ArchivedAt == nil {
		t.Fatalf("archive: %+v err=%v", arch, err)
	}
	def, _ := conv.List(ctx, ListConversationsQuery{TenantID: tenantA, ActorID: actorA1, PageSize: 20})
	if len(def.Items) != 0 {
		t.Fatalf("archived thread should be hidden by default: %+v", def.Items)
	}
	withArch, _ := conv.List(ctx, ListConversationsQuery{TenantID: tenantA, ActorID: actorA1, PageSize: 20, IncludeArchived: true})
	if len(withArch.Items) != 1 {
		t.Fatalf("archived thread should appear with IncludeArchived: %+v", withArch.Items)
	}
	if _, err := conv.Unarchive(ctx, tenantA, actorA1, c.ID); err != nil {
		t.Fatalf("unarchive: %v", err)
	}
}

func TestMessageAppendHistoryAndScope(t *testing.T) {
	ctx := context.Background()
	conv, _, tenantA, tenantB := newTestStores(t, ctx)
	c, _, _ := conv.Create(ctx, NewConversation{TenantID: tenantA, ActorID: actorA1})

	tc := json.RawMessage(`[{"tool":"cube.active_animals"}]`)
	cit := json.RawMessage(`[{"surface":"cube","as_of":"2026-07-22"}]`)
	if _, err := conv.AppendMessage(ctx, NewMessage{ConversationID: c.ID, TenantID: tenantA, ActorID: actorA1, Role: RoleUser, Content: "how many goats"}); err != nil {
		t.Fatalf("append user: %v", err)
	}
	asst, err := conv.AppendMessage(ctx, NewMessage{
		ConversationID: c.ID, TenantID: tenantA, ActorID: actorA1, Role: RoleAssistant,
		Content: "2567 goats", Source: "cube", Mode: "governed", RequestID: "req-1",
		ToolCalls: tc, Citations: cit,
	})
	if err != nil {
		t.Fatalf("append assistant: %v", err)
	}
	if asst.Source != "cube" || string(asst.ToolCalls) == "" || string(asst.Citations) == "" {
		t.Fatalf("assistant provenance not persisted: %+v", asst)
	}

	// Appending to a thread not owned by the actor is ErrNotFound.
	if _, err := conv.AppendMessage(ctx, NewMessage{ConversationID: c.ID, TenantID: tenantA, ActorID: actorA2, Role: RoleUser, Content: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-actor append err=%v", err)
	}

	hist, err := conv.ListMessages(ctx, ListMessagesQuery{ConversationID: c.ID, TenantID: tenantA, ActorID: actorA1, PageSize: 20})
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(hist.Items) != 2 || hist.Items[0].Role != RoleUser || hist.Items[1].Role != RoleAssistant {
		t.Fatalf("history oldest-first wrong: %+v", hist.Items)
	}
	// Cross-tenant cannot read history.
	other, _ := conv.ListMessages(ctx, ListMessagesQuery{ConversationID: c.ID, TenantID: tenantB, ActorID: actorB1, PageSize: 20})
	if len(other.Items) != 0 {
		t.Fatalf("cross-tenant history leak: %+v", other.Items)
	}
}

func TestPurgeExpiredRemovesSoftDeleted(t *testing.T) {
	ctx := context.Background()
	conv, pool, tenantA, _ := newTestStores(t, ctx)
	c, _, _ := conv.Create(ctx, NewConversation{TenantID: tenantA, ActorID: actorA1})
	_, _ = conv.AppendMessage(ctx, NewMessage{ConversationID: c.ID, TenantID: tenantA, ActorID: actorA1, Role: RoleAssistant, Content: "a"})
	if err := conv.SoftDelete(ctx, tenantA, actorA1, c.ID); err != nil {
		t.Fatalf("soft-delete: %v", err)
	}

	// Not yet past retention window: nothing purged (retention_expires_at was stamped now()).
	n, err := conv.PurgeExpired(ctx, time.Now().Add(-time.Hour))
	if err != nil || n != 0 {
		t.Fatalf("early purge n=%d err=%v", n, err)
	}
	n, err = conv.PurgeExpired(ctx, time.Now().Add(time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("purge n=%d err=%v", n, err)
	}
	var msgCount int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM ceo_ai_messages WHERE conversation_id=$1::uuid`, c.ID).Scan(&msgCount)
	if msgCount != 0 {
		t.Fatalf("cascade delete failed: messages=%d", msgCount)
	}
}

// TestPurgeExpiredChunksAcrossBatches proves the sweep purges more expired threads
// than a single bounded batch claims (purgeBatchSize), i.e. it loops in bounded
// batches and converges instead of issuing one unbounded cross-tenant DELETE.
func TestPurgeExpiredChunksAcrossBatches(t *testing.T) {
	ctx := context.Background()
	conv, pool, tenantA, _ := newTestStores(t, ctx)

	const total = purgeBatchSize + 5 // spans two batches
	expired := time.Now().Add(-time.Hour).UTC()
	// Seed expired threads directly (fast, avoids per-row app writes).
	if _, err := pool.Exec(ctx, `
INSERT INTO ceo_ai_conversations (id, tenant_id, actor_id, retention_expires_at)
SELECT gen_random_uuid(), $1::uuid, gen_random_uuid(), $2::timestamptz
FROM generate_series(1, $3)`, tenantA, expired, total); err != nil {
		t.Fatalf("seed expired: %v", err)
	}
	// One live thread (future retention) must survive.
	live, _, err := conv.Create(ctx, NewConversation{TenantID: tenantA, ActorID: actorA1})
	if err != nil {
		t.Fatalf("create live: %v", err)
	}

	n, err := conv.PurgeExpired(ctx, time.Now())
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n != total {
		t.Fatalf("purged %d, want %d (multi-batch convergence)", n, total)
	}
	var remaining int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM ceo_ai_conversations WHERE tenant_id=$1::uuid`, tenantA).Scan(&remaining); err != nil {
		t.Fatalf("count remaining: %v", err)
	}
	if remaining != 1 {
		t.Fatalf("remaining=%d, want 1 (only the live thread)", remaining)
	}
	if _, err := conv.Get(ctx, tenantA, actorA1, live.ID); err != nil {
		t.Fatalf("live thread should survive purge: %v", err)
	}
}
