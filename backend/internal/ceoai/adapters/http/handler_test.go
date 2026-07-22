package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type fakeAsker struct {
	ans  domain.Answer
	err  error
	seen domain.Question
}

func (f *fakeAsker) Ask(_ context.Context, q domain.Question) (domain.Answer, error) {
	f.seen = q
	return f.ans, f.err
}

func withSession(r *http.Request) *http.Request {
	ctx := httpmiddleware.WithTenantID(r.Context(), "t1")
	ctx = httpmiddleware.WithActorID(ctx, "u1")
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal}})
	return r.WithContext(ctx)
}

func TestHandlerReturnsEnvelopeOnly(t *testing.T) {
	asker := &fakeAsker{ans: domain.Answer{Answer: "ok", Source: "Cube", Mode: domain.ModePlanned, RequestID: "r1"}}
	h := NewHandler(asker, nil)
	req := withSession(httptest.NewRequest(http.MethodPost, "/api/ceo-ai/ask", strings.NewReader(`{"question":"how many goats"}`)))
	rec := httptest.NewRecorder()
	h.Ask(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	// No step-trace / chain-of-thought fields may leak.
	for _, banned := range []string{"steps", "step_trace", "trace", "plan", "sql"} {
		if _, ok := got[banned]; ok {
			t.Fatalf("leaked internal field %q", banned)
		}
	}
	if asker.seen.Actor.TenantID != "t1" || asker.seen.Actor.Role != permissions.RoleCEOInternal {
		t.Fatalf("actor must come from session, got %+v", asker.seen.Actor)
	}
	// Body text must never override scope.
	if asker.seen.Text != "how many goats" {
		t.Fatalf("unexpected question text %q", asker.seen.Text)
	}
}

func TestHandlerUnauthorizedWithoutSession(t *testing.T) {
	h := NewHandler(&fakeAsker{}, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/ceo-ai/ask", strings.NewReader(`{"question":"x"}`))
	rec := httptest.NewRecorder()
	h.Ask(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestBuildActorIgnoresBodyRole(t *testing.T) {
	a := BuildActor("t1", "u1", "en", []permissions.ActiveGrant{{Role: "operator"}})
	if a.Role == permissions.RoleCEOInternal {
		t.Fatal("non-leadership grant must not yield ceo_internal role")
	}
}
