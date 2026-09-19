package persistence

import (
	"context"
	"errors"
	"testing"
)

// TestConversationResumeCrossTenant404 (store layer, Postgres-gated): resuming
// tenant A's conversation id as tenant B — the message history read the HTTP
// resume route performs, plus every mutation — is ErrNotFound (the handler
// maps that to 404), and A's thread and history are untouched afterwards. The
// HTTP-boundary twin lives in adapters/http/tenant_isolation_test.go.
func TestConversationResumeCrossTenant404(t *testing.T) {
	ctx := context.Background()
	conv, _, tenantA, tenantB := newTestStores(t, ctx)

	c, _, err := conv.Create(ctx, NewConversation{TenantID: tenantA, ActorID: actorA1, Title: "Coimbatore review"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := conv.AppendMessage(ctx, NewMessage{ConversationID: c.ID, TenantID: tenantA, ActorID: actorA1, Role: RoleAssistant, Content: "Kumar Traders: 2 blocked"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	// Resume as tenant B (its own CEO) with A's id.
	if _, err := conv.ListMessages(ctx, ListMessagesQuery{ConversationID: c.ID, TenantID: tenantB, ActorID: actorB1, PageSize: 20}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant ListMessages: err=%v, want ErrNotFound", err)
	}
	if _, err := conv.Get(ctx, tenantB, actorB1, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant Get: err=%v, want ErrNotFound", err)
	}
	if _, err := conv.Rename(ctx, tenantB, actorB1, c.ID, "hijack"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant Rename: err=%v, want ErrNotFound", err)
	}
	if _, err := conv.Archive(ctx, tenantB, actorB1, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant Archive: err=%v, want ErrNotFound", err)
	}
	if err := conv.SoftDelete(ctx, tenantB, actorB1, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant SoftDelete: err=%v, want ErrNotFound", err)
	}
	if _, err := conv.AppendMessage(ctx, NewMessage{ConversationID: c.ID, TenantID: tenantB, ActorID: actorB1, Role: RoleUser, Content: "and yesterday?"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant AppendMessage: err=%v, want ErrNotFound", err)
	}
	// B's own list is empty.
	page, err := conv.List(ctx, ListConversationsQuery{TenantID: tenantB, ActorID: actorB1, PageSize: 20})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("tenant B list must be empty: %+v err=%v", page.Items, err)
	}

	// A's thread is intact: title unchanged, exactly one message, not deleted.
	got, err := conv.Get(ctx, tenantA, actorA1, c.ID)
	if err != nil || got.Title != "Coimbatore review" || got.ArchivedAt != nil {
		t.Fatalf("tenant A thread must be untouched: %+v err=%v", got, err)
	}
	msgs, err := conv.ListMessages(ctx, ListMessagesQuery{ConversationID: c.ID, TenantID: tenantA, ActorID: actorA1, PageSize: 20})
	if err != nil || len(msgs.Items) != 1 || msgs.Items[0].Content != "Kumar Traders: 2 blocked" {
		t.Fatalf("tenant A history must be untouched: %+v err=%v", msgs.Items, err)
	}
}
