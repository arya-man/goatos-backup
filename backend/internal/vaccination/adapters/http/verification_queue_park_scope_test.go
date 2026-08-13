package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// projection-review: GET /vaccination/verification-queue was tenant-wide unless the CLIENT opted
// into a park filter, so a park-bound park head omitting park_id reviewed the other park's
// completions. Park scope is backend-owned: the query string may only narrow inside the grant.
func TestVerificationQueueDefaultsToActorParkAndRejectsOtherPark(t *testing.T) {
	const tenant = "00000000-0000-4000-8000-000000000001"
	const ownPark = "30000000-0000-4000-8000-000000000001"
	const otherPark = "30000000-0000-4000-8000-000000000099"

	svc := &fakeImpact{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(svc, nil))

	ctx := httpmiddleware.WithTenantID(t.Context(), tenant)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{
		{Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: ownPark},
	})

	req := httptest.NewRequest(http.MethodGet, "/vaccination/verification-queue", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if svc.gotQueuePark != ownPark {
		t.Fatalf("omitted park_id resolved to %q, want %s (tenant-wide queue leaks the other park)", svc.gotQueuePark, ownPark)
	}

	req = httptest.NewRequest(http.MethodGet, "/vaccination/verification-queue?park_id="+otherPark, nil).WithContext(ctx)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("explicit other-park status = %d want 403 body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "park_scope_forbidden") {
		t.Fatalf("body=%s, want park_scope_forbidden", rec.Body.String())
	}
}
