package http

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type stubAsker struct{}

func (stubAsker) Ask(context.Context, domain.Question) (domain.Answer, error) {
	return domain.Answer{Answer: "ok", Mode: domain.ModeFallback, RequestID: "r"}, nil
}

// mountedTraceRequest hits the ACTUAL mounted pattern (no /api prefix) so the
// stdlib mux matches the registered route and fills {request_id} itself. The
// auth context is installed exactly as the middleware would.
func mountedTraceRequest(tenantID, actorID, requestID string, grants []permissions.ActiveGrant) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/ceo-ai/admin/trace/"+requestID, nil)
	ctx := r.Context()
	if tenantID != "" {
		ctx = httpmiddleware.WithTenantID(ctx, tenantID)
	}
	if actorID != "" {
		ctx = httpmiddleware.WithActorID(ctx, actorID)
	}
	ctx = httpmiddleware.WithAuthGrants(ctx, grants)
	return r.WithContext(ctx)
}

// TestRouterMountsAdminTraceRoute is the OBS-RUN-2 root-cause proof: the admin
// step-trace endpoint must be REACHABLE on the mux the live server builds, not
// only exercised by the isolated handler unit test. Before the fix no route was
// registered for /ceo-ai/admin/trace/{request_id}, so a live GET returned a
// route-not-found 404 for admin and non-admin alike. Here we register the real
// Router (with the admin handler attached) on a stdlib ServeMux and assert the
// route resolves to the admin handler — a seeded admin request returns 200.
func TestRouterMountsAdminTraceRoute(t *testing.T) {
	handler := NewHandler(stubAsker{}, slog.Default())
	admin := NewAdminTraceHandler(seededStore(t, "tA", "req-1"), slog.Default())
	router := NewRouter(handler, nil).WithAdminTrace(admin)

	mux := http.NewServeMux()
	router.Register(mux)

	// Admin GET on the mounted route -> 200 (route resolved AND handler ran).
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, mountedTraceRequest("tA", "user-ceo", "req-1", adminGrants()))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin trace route must be mounted and return 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Non-admin on the SAME mounted route -> 403 from the handler gate (a 404
	// here would mean the route was never mounted).
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, mountedTraceRequest("tA", "user-op", "req-1", nonAdminGrants()))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mounted admin trace route must 403 for non-admin, got %d", rec.Code)
	}
}

// TestRouterWithoutAdminTraceLeavesRouteAbsent proves the endpoint is never
// silently open: with no admin handler attached, the route stays unmounted
// (404), it does not fall through to an unguarded surface.
func TestRouterWithoutAdminTraceLeavesRouteAbsent(t *testing.T) {
	router := NewRouter(NewHandler(stubAsker{}, slog.Default()), nil)
	mux := http.NewServeMux()
	router.Register(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ceo-ai/admin/trace/req-1", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unattached admin trace route must be absent (404), got %d", rec.Code)
	}
}

// TestRouterMountsAskRoute confirms the primary /ceo-ai/ask route is mounted on
// the same router (OBS-RUN-3: the assistant is reachable on the live mux).
func TestRouterMountsAskRoute(t *testing.T) {
	router := NewRouter(NewHandler(stubAsker{}, slog.Default()), nil)
	mux := http.NewServeMux()
	router.Register(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/ceo-ai/ask", strings.NewReader(`{"question":"hi"}`))
	// No auth context -> handler returns 401, which still proves the route is
	// mounted (a missing route would be 404).
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("/ceo-ai/ask must be mounted, got 404")
	}
}
