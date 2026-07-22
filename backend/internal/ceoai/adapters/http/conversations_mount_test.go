package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/persistence"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// fakeConvStore is an in-memory ConvStore for the mount test.
type fakeConvStore struct{}

func (fakeConvStore) Create(context.Context, persistence.NewConversation) (persistence.Conversation, bool, error) {
	return persistence.Conversation{ID: "c1", Title: "New conversation"}, true, nil
}
func (fakeConvStore) Get(context.Context, string, string, string) (persistence.Conversation, error) {
	return persistence.Conversation{ID: "c1"}, nil
}
func (fakeConvStore) List(context.Context, persistence.ListConversationsQuery) (persistence.ConversationPage, error) {
	return persistence.ConversationPage{Items: []persistence.Conversation{{ID: "c1", Title: "t"}}}, nil
}
func (fakeConvStore) Rename(_ context.Context, _, _, id, title string) (persistence.Conversation, error) {
	return persistence.Conversation{ID: id, Title: title}, nil
}
func (fakeConvStore) SoftDelete(context.Context, string, string, string) error { return nil }
func (fakeConvStore) ListMessages(context.Context, persistence.ListMessagesQuery) (persistence.MessagePage, error) {
	return persistence.MessagePage{Items: []persistence.Message{{ID: "m1", Role: persistence.RoleUser, Content: "hi"}}}, nil
}

type fakeFeedbackStore struct{}

func (fakeFeedbackStore) Upsert(context.Context, persistence.NewFeedback) (persistence.Feedback, error) {
	return persistence.Feedback{ID: "f1"}, nil
}

func leadershipReq(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	ctx := httpmiddleware.WithTenantID(r.Context(), "t1")
	ctx = httpmiddleware.WithActorID(ctx, "u1")
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant"}})
	return r.WithContext(ctx)
}

func operatorReq(method, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	ctx := httpmiddleware.WithTenantID(r.Context(), "t1")
	ctx = httpmiddleware.WithActorID(ctx, "u2")
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleOperator, ScopeType: "shed", ScopeID: "s1"}})
	return r.WithContext(ctx)
}

// TestConversationHandlerMountsThreadSurface is the launcher-visibility root-cause
// proof. The admin-web probe gates the assistant bubble on GET /ceo-ai/starters;
// before this fix no such route (nor conversations/feedback) was registered, so
// the probe 404'd, allowed!==true, and NO leadership user could ever see or open
// the assistant. This asserts the real handler is REACHABLE on the mux the live
// server builds, returns 200 for leadership (bubble shows) and 403 for a
// non-leadership session (bubble hidden), and that the whole thread + feedback
// surface resolves rather than 404ing.
func TestConversationHandlerMountsThreadSurface(t *testing.T) {
	h := NewConversationHandler(fakeConvStore{}, fakeFeedbackStore{}, nil, slog.Default())
	mux := http.NewServeMux()
	h.Register(mux)

	// The leadership capability probe: 200 with starters => launcher renders.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, leadershipReq(http.MethodGet, "/ceo-ai/starters", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ceo-ai/starters must be mounted and 200 for leadership, got %d (%s)", rec.Code, rec.Body.String())
	}
	var probe struct {
		Starters []string `json:"starters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &probe); err != nil || len(probe.Starters) == 0 {
		t.Fatalf("starters probe must return a non-empty starter list, body=%s err=%v", rec.Body.String(), err)
	}

	// Non-leadership session => 403 (bubble stays hidden), NOT 404 (unmounted).
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, operatorReq(http.MethodGet, "/ceo-ai/starters"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /ceo-ai/starters must 403 for non-leadership, got %d", rec.Code)
	}

	// The rest of the surface must resolve (never 404) for leadership.
	cases := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/ceo-ai/conversations", ""},
		{http.MethodPost, "/ceo-ai/conversations", "{}"},
		{http.MethodGet, "/ceo-ai/conversations/c1/messages", ""},
		{http.MethodPatch, "/ceo-ai/conversations/c1", `{"title":"Renamed"}`},
		{http.MethodDelete, "/ceo-ai/conversations/c1", ""},
		{http.MethodPost, "/ceo-ai/messages/m1/feedback", `{"rating":"up"}`},
	}
	for _, c := range cases {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, leadershipReq(c.method, c.path, c.body))
		if rec.Code == http.StatusNotFound {
			t.Fatalf("%s %s must be mounted, got 404", c.method, c.path)
		}
		if rec.Code >= 500 {
			t.Fatalf("%s %s returned %d: %s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}

// TestStartersServeWithoutStores proves the leadership probe/launcher gate is
// never blocked by an unwired conversation/feedback store: with nil stores the
// starters route still returns 200 for leadership.
func TestStartersServeWithoutStores(t *testing.T) {
	h := NewConversationHandler(nil, nil, nil, slog.Default())
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, leadershipReq(http.MethodGet, "/ceo-ai/starters", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("starters must serve 200 even with nil stores, got %d", rec.Code)
	}

	// A store-backed route degrades to 503 (never panic) when its store is nil.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, leadershipReq(http.MethodGet, "/ceo-ai/conversations", ""))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("conversations must 503 with nil store, got %d", rec.Code)
	}
}
