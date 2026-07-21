package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/adminui/app"
	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

func TestBootstrapSetsCacheHeadersAndPassesRequestContext(t *testing.T) {
	service := &fakeBootstrapService{resp: domain.BootstrapResponse{
		CachePolicy: domain.ContractCachePolicy{ETag: `W/"rev-1"`},
	}}
	handler := NewHandler(service)

	grants := []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: "tenant-1"}}
	ctx := httpmiddleware.WithAuthGrants(
		httpmiddleware.WithActorID(
			httpmiddleware.WithTenantID(context.Background(), "tenant-1"),
			"actor-1",
		),
		grants,
	)
	req := httptest.NewRequest("GET", "/admin-web/bootstrap", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.Bootstrap(rec, req)

	if got := rec.Header().Get("ETag"); got != `W/"rev-1"` {
		t.Fatalf("ETag header = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=0, must-revalidate" {
		t.Fatalf("Cache-Control header = %q", got)
	}
	vary := strings.Join(rec.Header().Values("Vary"), ",")
	for _, want := range []string{"Authorization", httpmiddleware.TenantContextHeader} {
		if !strings.Contains(vary, want) {
			t.Fatalf("Vary header missing %q: %q", want, vary)
		}
	}
	if service.input.TenantID != "tenant-1" || service.input.ActorID != "actor-1" {
		t.Fatalf("request context not passed: %#v", service.input)
	}
	if len(service.input.Grants) != 1 || service.input.Grants[0] != grants[0] {
		t.Fatalf("auth grants not passed: %#v", service.input.Grants)
	}
}

func TestBootstrapHonorsIfNoneMatch(t *testing.T) {
	service := &fakeBootstrapService{resp: domain.BootstrapResponse{
		CachePolicy: domain.ContractCachePolicy{ETag: `W/"rev-1"`},
	}}
	handler := NewHandler(service)

	req := httptest.NewRequest("GET", "/admin-web/bootstrap", nil)
	req.Header.Set("If-None-Match", `"rev-1"`)
	rec := httptest.NewRecorder()

	handler.Bootstrap(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, body=%q", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("304 response should not include a body: %q", rec.Body.String())
	}
	if got := rec.Header().Get("ETag"); got != `W/"rev-1"` {
		t.Fatalf("ETag header = %q", got)
	}
}

type fakeBootstrapService struct {
	resp  domain.BootstrapResponse
	input app.BootstrapInput
}

func (f *fakeBootstrapService) Bootstrap(_ context.Context, input app.BootstrapInput) domain.BootstrapResponse {
	f.input = input
	return f.resp
}
