package http

// D0 tenant-isolation proofs at the HTTP boundary (plan-v3 D0, "Memory /
// resume" and "Audit / trace" rows): a conversation id or a request id that
// belongs to another tenant is a 404 for the caller, and the tenant is taken
// from the SESSION context only — never from a header or the path.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ceoobs "github.com/vgoats/goatos/backend/internal/ceoai/adapters/observability"
	"github.com/vgoats/goatos/backend/internal/ceoai/persistence"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// tenantConvStore is an in-memory ConvStore that mirrors the Postgres store's
// scoping contract: every read/mutation is keyed by (tenant, actor,
// conversation) and a miss on any component is ErrNotFound.
type tenantConvStore struct {
	convs map[string]persistence.Conversation // key: tenant|actor|id
	msgs  map[string][]persistence.Message    // key: tenant|actor|id
}

func newTenantConvStore() *tenantConvStore {
	return &tenantConvStore{convs: map[string]persistence.Conversation{}, msgs: map[string][]persistence.Message{}}
}

func convKey(tenant, actor, id string) string { return tenant + "|" + actor + "|" + id }

func (s *tenantConvStore) Create(_ context.Context, in persistence.NewConversation) (persistence.Conversation, bool, error) {
	c := persistence.Conversation{ID: "conv-" + in.TenantID, TenantID: in.TenantID, ActorID: in.ActorID, Title: in.Title}
	s.convs[convKey(in.TenantID, in.ActorID, c.ID)] = c
	return c, true, nil
}
func (s *tenantConvStore) Get(_ context.Context, tenant, actor, id string) (persistence.Conversation, error) {
	c, ok := s.convs[convKey(tenant, actor, id)]
	if !ok {
		return persistence.Conversation{}, persistence.ErrNotFound
	}
	return c, nil
}
func (s *tenantConvStore) List(_ context.Context, q persistence.ListConversationsQuery) (persistence.ConversationPage, error) {
	var page persistence.ConversationPage
	for _, c := range s.convs {
		if c.TenantID == q.TenantID && c.ActorID == q.ActorID {
			page.Items = append(page.Items, c)
		}
	}
	return page, nil
}
func (s *tenantConvStore) Rename(_ context.Context, tenant, actor, id, title string) (persistence.Conversation, error) {
	c, ok := s.convs[convKey(tenant, actor, id)]
	if !ok {
		return persistence.Conversation{}, persistence.ErrNotFound
	}
	c.Title = title
	s.convs[convKey(tenant, actor, id)] = c
	return c, nil
}
func (s *tenantConvStore) SoftDelete(_ context.Context, tenant, actor, id string) error {
	if _, ok := s.convs[convKey(tenant, actor, id)]; !ok {
		return persistence.ErrNotFound
	}
	delete(s.convs, convKey(tenant, actor, id))
	return nil
}
func (s *tenantConvStore) ListMessages(_ context.Context, q persistence.ListMessagesQuery) (persistence.MessagePage, error) {
	if _, ok := s.convs[convKey(q.TenantID, q.ActorID, q.ConversationID)]; !ok {
		return persistence.MessagePage{}, persistence.ErrNotFound
	}
	return persistence.MessagePage{Items: s.msgs[convKey(q.TenantID, q.ActorID, q.ConversationID)]}, nil
}

func sessionReq(method, path, tenant, actor string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(""))
	ctx := httpmiddleware.WithTenantID(r.Context(), tenant)
	ctx = httpmiddleware.WithActorID(ctx, actor)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant"}})
	return r.WithContext(ctx)
}

// TestConversationResumeCrossTenant404: a CEO in tenant B who knows tenant A's
// conversation id gets 404 on every thread route — messages (resume), rename
// and delete — and cannot make the server adopt tenant A by sending a tenant
// header. The same id is 200 for its owner, so the 404 is scoping, not a
// missing route.
func TestConversationResumeCrossTenant404(t *testing.T) {
	store := newTenantConvStore()
	convA, _, _ := store.Create(context.Background(), persistence.NewConversation{TenantID: "tenant-A", ActorID: "ceo-A", Title: "A's thread"})
	store.msgs[convKey("tenant-A", "ceo-A", convA.ID)] = []persistence.Message{{ID: "m1", Role: persistence.RoleAssistant, Content: "Kumar Traders: 2 blocked"}}

	h := NewConversationHandler(store, nil, slog.Default())
	mux := http.NewServeMux()
	h.Register(mux)

	// Owner resumes: 200 with the message body.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, sessionReq(http.MethodGet, "/ceo-ai/conversations/"+convA.ID+"/messages", "tenant-A", "ceo-A"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Kumar Traders") {
		t.Fatalf("owner resume: want 200 with body, got %d %s", rec.Code, rec.Body.String())
	}

	// Tenant B (its own CEO) resuming A's id: 404, and no A content in the body.
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/ceo-ai/conversations/" + convA.ID + "/messages"},
		{http.MethodPatch, "/ceo-ai/conversations/" + convA.ID},
		{http.MethodDelete, "/ceo-ai/conversations/" + convA.ID},
	} {
		req := sessionReq(tc.method, tc.path, "tenant-B", "ceo-B")
		if tc.method == http.MethodPatch {
			req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"title":"hijack"}`)).WithContext(req.Context())
			req.Header.Set("Content-Type", "application/json")
		}
		// A client-supplied tenant header must be ignored: scope is the session.
		req.Header.Set("X-GoatOS-Tenant-ID", "tenant-A")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s as tenant B: want 404, got %d (%s)", tc.method, tc.path, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "Kumar Traders") || strings.Contains(rec.Body.String(), "A's thread") {
			t.Fatalf("%s %s leaked tenant A content: %s", tc.method, tc.path, rec.Body.String())
		}
	}
	// Tenant A's thread is untouched by B's attempts.
	if c, err := store.Get(context.Background(), "tenant-A", "ceo-A", convA.ID); err != nil || c.Title != "A's thread" {
		t.Fatalf("tenant A's thread must be intact after cross-tenant attempts: %+v err=%v", c, err)
	}
	// B's own list never shows A's thread.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, sessionReq(http.MethodGet, "/ceo-ai/conversations", "tenant-B", "ceo-B"))
	var listed struct {
		Conversations []map[string]any `json:"conversations"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &listed)
	if rec.Code != http.StatusOK || len(listed.Conversations) != 0 {
		t.Fatalf("tenant B list must be empty, got %d %s", rec.Code, rec.Body.String())
	}
}

// TestAdminTraceCrossTenant404 drives the MOUNTED admin trace route: an admin
// in tenant B asking for tenant A's request id gets 404 (never the trace), a
// spoofed tenant header changes nothing, and the same id is 200 for tenant A.
func TestAdminTraceCrossTenant404(t *testing.T) {
	store := ceoobs.NewMemoryTraceStore(8)
	if err := store.RecordTrace(context.Background(), ceoobs.TraceRecord{
		TenantID: "tenant-A", RequestID: "req-A-1", ActorRole: permissions.RoleCEOInternal,
		RouteTier: "sql", ToolCalled: "sql_fallback", QuestionRedacted: "source health at Coimbatore",
		Steps: []ceoobs.StepTrace{{SubQuestion: "health", Route: "sql", ToolName: "sql_fallback", RowCount: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	NewRouter(nil, nil).WithAdminTrace(NewAdminTraceHandler(store, slog.Default())).Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, sessionReq(http.MethodGet, "/ceo-ai/admin/trace/req-A-1", "tenant-A", "ceo-A"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Coimbatore") {
		t.Fatalf("owner tenant: want 200 with trace, got %d %s", rec.Code, rec.Body.String())
	}

	req := sessionReq(http.MethodGet, "/ceo-ai/admin/trace/req-A-1", "tenant-B", "ceo-B")
	req.Header.Set("X-GoatOS-Tenant-ID", "tenant-A")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant trace: want 404, got %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Coimbatore") || strings.Contains(rec.Body.String(), "sql_fallback") {
		t.Fatalf("cross-tenant trace leaked tenant A content: %s", rec.Body.String())
	}
}
